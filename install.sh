#!/usr/bin/env bash
# ============================================================
# cardinal-wings installer
#
# Installs a prebuilt release binary from GitHub Releases as a
# systemd service and writes a working config with a generated
# API key. No Go toolchain required.
#
# Usage (recommended — avoids pipe issues):
#   curl -fsSL https://raw.githubusercontent.com/animesao/cardinal-wings/main/install.sh -o /tmp/install-wings.sh
#   sudo bash /tmp/install-wings.sh
#
# Alternative (may fail on some systems due to SIGPIPE):
#   curl -fsSL https://raw.githubusercontent.com/animesao/cardinal-wings/main/install.sh | sudo bash
#
#   sudo bash install.sh              # install latest release
#   sudo bash install.sh v0.4.2       # install a specific tag
#   sudo bash install.sh local        # build from this checkout (needs Go)
#
# Environment:
#   WINGS_TLS=1      generate a self-signed cert and enable TLS
#   WINGS_HOST=...   bind address (default 127.0.0.1; use 0.0.0.0 for remote panel)
#   WINGS_PORT=...   bind port   (default 8080)
# ============================================================
set -euo pipefail

OWNER="animesao"
REPO="cardinal-wings"
RAW="https://raw.githubusercontent.com/${OWNER}/${REPO}/main"

VERSION="${1:-latest}"
PREFIX="${PREFIX:-/usr/local}"
BIN_DIR="${PREFIX}/bin"
CONF_DIR="/etc/cardinal-wings"
SYSTEMD_DIR="/etc/systemd/system"

WINGS_TLS="${WINGS_TLS:-0}"
WINGS_HOST="${WINGS_HOST:-127.0.0.1}"
WINGS_PORT="${WINGS_PORT:-8080}"

# ------------------------------------------------------------
# helpers
# ------------------------------------------------------------
if [ "$(id -u)" != "0" ]; then
  echo "error: run as root (sudo) — need to write ${BIN_DIR}, ${CONF_DIR}, ${SYSTEMD_DIR}" >&2
  exit 1
fi

have() { command -v "$1" >/dev/null 2>&1; }

# Robust download helper — writes to a temp file first, then moves.
# This avoids SIGPIPE and "Failure writing output to destination" errors
# that happen when piping curl | bash or when the target path is on a
# slow/mounted filesystem.
download() {
  local url="$1" dest="$2"
  local tmp="${dest}.tmp.$$"
  local try

  for try in 1 2 3; do
    if curl -fsSL --retry 2 --connect-timeout 10 --max-time 300 \
         "${url}" -o "${tmp}"; then
      # Verify we actually got a non-empty file
      if [ -s "${tmp}" ]; then
        mv -f "${tmp}" "${dest}"
        return 0
      fi
      echo "warning: downloaded empty file from ${url}" >&2
      rm -f "${tmp}"
    else
      echo "warning: download attempt ${try}/3 failed for ${url}" >&2
      rm -f "${tmp}"
    fi
    sleep 2
  done

  echo "error: failed to download ${url} after 3 attempts" >&2
  return 1
}

OS="$(uname -s | tr '[:upper:]' '[:lower:]')"
ARCH="$(uname -m)"
case "${ARCH}" in
  x86_64)  ARCH="amd64" ;;
  aarch64|arm64) ARCH="arm64" ;;
esac
case "${OS}" in
  linux)  ASSET="cardinal-wings-linux-${ARCH}" ;;
  darwin) ASSET="cardinal-wings-darwin-${ARCH}" ;;
  *) echo "unsupported platform: ${OS}/${ARCH}" >&2; exit 1 ;;
esac

echo "==> cardinal-wings installer"
echo "    platform: ${OS}/${ARCH}   bind: ${WINGS_HOST}:${WINGS_PORT}"

# ------------------------------------------------------------
# 1. binary (prebuilt by default; local build only with `local`)
# ------------------------------------------------------------
mkdir -p "${BIN_DIR}"

if [ "${VERSION}" = "local" ] || [ "${VERSION}" = "dev" ]; then
  if ! have go; then
    echo "error: 'local' needs the Go toolchain (https://go.dev/dl/)." >&2
    echo "       or drop the argument to install a prebuilt release binary." >&2
    exit 1
  fi
  echo "==> building from local source"
  go build -trimpath \
    -ldflags="-s -w -X github.com/animesao/cardinal-wings/internal/server.version=dev" \
    -o "${BIN_DIR}/cardinal-wings" .
else
  if [ "${VERSION}" = "latest" ]; then
    echo "==> resolving latest release"
    TMP_TAG="/tmp/cardinal-wings.latest.json"
    if ! curl -fsSL --retry 3 --connect-timeout 10 -o "${TMP_TAG}" \
      "https://api.github.com/repos/${OWNER}/${REPO}/releases/latest"; then
      echo "error: could not fetch latest release info from GitHub API" >&2
      echo "       check your internet connection and try again" >&2
      exit 1
    fi
    tagline="$(grep -m1 '"tag_name"' "${TMP_TAG}" 2>/dev/null || true)"
    for cand in \
      "$(printf '%s' "${tagline}" | sed -n 's/.*"tag_name": *"\([^"]*\)".*/\1/p')" \
      "$(printf '%s' "${tagline}" | grep -o '"[0-9][0-9]*\.[0-9][0-9]*\.[0-9][0-9]*"' | tr -d '"')"
    do
      [ -n "${cand}" ] && VERSION="v${cand#v}" && break
    done
    rm -f "${TMP_TAG}"
    if [ -z "${VERSION}" ] || ! printf '%s' "${VERSION}" | grep -qE '^v[0-9]+\.[0-9]+\.[0-9]+$'; then
      echo "error: could not resolve a valid latest release tag (got '${VERSION}')" >&2
      echo "       use:  install.sh <tag>   e.g. install.sh v0.4.4" >&2
      exit 1
    fi
  elif ! printf '%s' "${VERSION}" | grep -qE '^v[0-9]+\.[0-9]+\.[0-9]+$'; then
    echo "error: invalid version '${VERSION}' (expected e.g. v0.4.4)" >&2
    exit 1
  fi
  url="https://github.com/${OWNER}/${REPO}/releases/download/${VERSION}/${ASSET}"
  echo "==> downloading ${VERSION} (${ASSET})"
  if ! download "${url}" "${BIN_DIR}/cardinal-wings"; then
    echo "" >&2
    echo "Tip: if piping via 'curl | bash', try downloading the script first:" >&2
    echo "  curl -fsSL ${RAW}/install.sh -o /tmp/install-wings.sh" >&2
    echo "  sudo bash /tmp/install-wings.sh" >&2
    exit 1
  fi
  chmod +x "${BIN_DIR}/cardinal-wings"
fi

# ------------------------------------------------------------
# 2. config — generated once, with a real API key inside
# ------------------------------------------------------------
mkdir -p "${CONF_DIR}"
chmod 700 "${CONF_DIR}"

if [ -f "${CONF_DIR}/config.toml" ]; then
  echo "==> ${CONF_DIR}/config.toml already exists — keeping it"
else
  API_KEY="$(head -c 24 /dev/urandom | od -An -tx1 | tr -d ' \n')"
  TLS_BLOCK=""
  if [ "${WINGS_TLS}" = "1" ]; then
    if ! have openssl; then
      echo "error: WINGS_TLS=1 needs openssl" >&2; exit 1
    fi
    echo "==> generating self-signed TLS certificate"
    openssl req -x509 -newkey rsa:2048 -nodes -days 365 \
      -subj "/CN=cardinal-wings" \
      -keyout "${CONF_DIR}/server.key" -out "${CONF_DIR}/server.crt" 2>/dev/null
    chmod 600 "${CONF_DIR}/server.key" "${CONF_DIR}/server.crt"
    TLS_BLOCK=$'\ntls_cert = "'"${CONF_DIR}/server.crt"'"'\ntls_key  = "'"${CONF_DIR}/server.key"'"'
  fi

  umask 077
  cat > "${CONF_DIR}/config.toml" <<EOF
# cardinal-wings configuration
# generated by install.sh — keep this file secret (it contains the API key)

[server]
host = "${WINGS_HOST}"     # 127.0.0.1 = local panel only; 0.0.0.0 = remote panel
port = ${WINGS_PORT}${TLS_BLOCK}

[rate_limit]
ip_tps = 25
ip_burst = 50
key_tps = 10
key_burst = 30
max_clients = 4096

# Panel credentials. role: "admin" (full access) or "readonly".
[[keys]]
name = "panel"
key = "${API_KEY}"
role = "admin"

# Remote cluster nodes (optional):
# [[nodes]]
# name = "node-2"
# address = "http://10.0.0.2:2375"
# token = "that-node-serve-token"
# enabled = true
EOF
  chmod 600 "${CONF_DIR}/config.toml"
  echo "==> wrote ${CONF_DIR}/config.toml"
  echo ""
  echo "    ┌──────────────────────────────────────────────┐"
  echo "    │  API KEY (give this to the panel):           │"
  echo "    │  ${API_KEY}  │"
  echo "    └──────────────────────────────────────────────┘"
  echo ""
fi

# ------------------------------------------------------------
# 3. systemd unit (from the checkout, else downloaded)
# ------------------------------------------------------------
if [ -f "systemd/cardinal-wings.service" ]; then
  UNIT_SRC="systemd/cardinal-wings.service"
else
  echo "==> fetching systemd unit"
  UNIT_SRC="/tmp/cardinal-wings.service"
  curl -fsSL --retry 2 --connect-timeout 10 "${RAW}/systemd/cardinal-wings.service" -o "${UNIT_SRC}"
fi
install -m 644 "${UNIT_SRC}" "${SYSTEMD_DIR}/cardinal-wings.service"
systemctl daemon-reload 2>/dev/null || true

# ------------------------------------------------------------
# 4. cardinal runtime check (wings drives `cardinal serve`)
# ------------------------------------------------------------
if have cardinal; then
  echo "==> cardinal runtime found: $(command -v cardinal)"
else
  echo ""
  echo "    NOTE: 'cardinal' was not found in PATH."
  echo "    wings spawns 'cardinal serve' to manage containers, so install it first:"
  echo "      curl -fsSL https://raw.githubusercontent.com/animesao/cardinal/main/install.sh | sudo bash"
  echo ""
fi

# ------------------------------------------------------------
# 5. verify the binary actually runs
# ------------------------------------------------------------
echo "==> verifying binary"
if "${BIN_DIR}/cardinal-wings" --help >/dev/null 2>&1 || "${BIN_DIR}/cardinal-wings" --version >/dev/null 2>&1; then
  echo "    binary OK"
else
  if timeout 3 "${BIN_DIR}/cardinal-wings" --config "${CONF_DIR}/config.toml" >/dev/null 2>&1; then
    echo "    binary OK (started briefly)"
  else
    echo "    warning: could not verify the binary automatically" >&2
  fi
fi

echo
echo "cardinal-wings installed: ${BIN_DIR}/cardinal-wings"
echo
echo "Next steps:"
echo "  1. systemctl enable --now cardinal-wings"
echo "  2. systemctl status cardinal-wings          # should be active (running)"
echo "  3. curl -s localhost:${WINGS_PORT}/v1/ping   # should print \"pong\""
echo "     curl -s -H \"Authorization: Bearer <API_KEY>\" localhost:${WINGS_PORT}/v1/version"
echo "  4. add the node to the panel: host=$(hostname -I 2>/dev/null | awk '{print $1}'), port=${WINGS_PORT}"
echo "     and paste the API key from ${CONF_DIR}/config.toml"
echo
if [ "${WINGS_HOST}" = "127.0.0.1" ]; then
  echo "NOTE: bound to loopback — only the local panel can reach it."
  echo "      For a remote panel re-run with WINGS_HOST=0.0.0.0 (TLS recommended)."
fi
