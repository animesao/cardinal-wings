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
# Alternative:
#   curl -fsSL https://raw.githubusercontent.com/animesao/cardinal-wings/main/install.sh | sudo bash
#
#   sudo bash install.sh              # install latest release
#   sudo bash install.sh v0.4.2       # install a specific tag
#   sudo bash install.sh local        # build from local checkout (needs Go)
#
# Environment:
#   WINGS_TLS=1      generate a self-signed cert and enable TLS
#   WINGS_HOST=...   bind address (default 127.0.0.1; use 0.0.0.0 for remote panel)
#   WINGS_PORT=...   bind port   (default 8080)
#   WINGS_SFTP=0|1   enable per-container SFTP server (default 1)
#   WINGS_SFTP_HOST= bind address for SFTP (default 0.0.0.0)
#   WINGS_SFTP_PORT= SFTP listen port (default 2022)
#   WINGS_NO_FIREWALL=1  skip automatic firewall port opening
#   WINGS_MIRROR=...    mirror base for binaries (default https://cardinal.spcfy.eu/downloads/wings;
#                       set WINGS_MIRROR="" to force GitHub-only)
#   WINGS_SKIP_VERIFY=1 disable SHA256 verification against the mirror
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
WINGS_SFTP="${WINGS_SFTP:-1}"
WINGS_SFTP_HOST="${WINGS_SFTP_HOST:-0.0.0.0}"
WINGS_SFTP_PORT="${WINGS_SFTP_PORT:-2022}"
WINGS_NO_FIREWALL="${WINGS_NO_FIREWALL:-0}"
WINGS_MIRROR="${WINGS_MIRROR:-https://cardinal.spcfy.eu/downloads/wings}"
WINGS_SKIP_VERIFY="${WINGS_SKIP_VERIFY:-0}"

# ------------------------------------------------------------
# helpers
# ------------------------------------------------------------
if [ "$(id -u)" != "0" ]; then
  echo "error: run as root (sudo) -- need to write ${BIN_DIR}, ${CONF_DIR}, ${SYSTEMD_DIR}" >&2
  exit 1
fi

have() { command -v "$1" >/dev/null 2>&1; }

# Robust download helper -- writes to a temp file first, then moves.
# Avoids "curl: (23) Failure writing output to destination" from pipes.
download() {
  local url="$1" dest="$2"
  local tmp="${dest}.tmp.$$"
  local try
  for try in 1 2 3; do
    # Try normally first, then force IPv4 — some networks black-hole IPv6 and
    # stall transfers without any error (the classic "installer hangs" case).
    if curl -fsSL --retry 2 --connect-timeout 15 --max-time 300 "${url}" -o "${tmp}" \
         || curl -4 -fsSL --retry 2 --connect-timeout 15 --max-time 300 "${url}" -o "${tmp}"; then
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
# 1. binary
# ------------------------------------------------------------
mkdir -p "${BIN_DIR}"

if [ "${VERSION}" = "local" ] || [ "${VERSION}" = "dev" ]; then
  if ! have go; then
    echo "error: 'local' needs the Go toolchain (https://go.dev/dl/)." >&2
    exit 1
  fi
  echo "==> building from local source"
  go build -trimpath \
    -ldflags="-s -w -X github.com/animesao/cardinal-wings/internal/server.version=dev" \
    -o "${BIN_DIR}/cardinal-wings" .
else
  # Resolve the version: prefer the site mirror, fall back to the GitHub API.
  MIRROR_VER=""
  if [ "${VERSION}" = "latest" ]; then
    if [ -n "${WINGS_MIRROR}" ]; then
      MIRROR_VER="$(curl -fsSL --retry 2 --connect-timeout 12 --max-time 25 "${WINGS_MIRROR}/VERSION" 2>/dev/null | tail -1 | tr -d '[:space:]')"
      if [ -n "${MIRROR_VER}" ] && printf '%s' "${MIRROR_VER}" | grep -qE '^v?[0-9]+\.[0-9]+\.[0-9]+$'; then
        VERSION="v${MIRROR_VER#v}"
        echo "==> latest release (mirror): ${VERSION}"
      else
        MIRROR_VER=""
        echo "==> mirror has no version (${WINGS_MIRROR}) — resolving via GitHub API"
      fi
    fi
    if [ -z "${MIRROR_VER}" ]; then
      echo "==> resolving latest release"
      TMP_TAG="/tmp/cardinal-wings.latest.json"
      if ! curl -fsSL --retry 3 --connect-timeout 10 --max-time 25 -o "${TMP_TAG}" \
        "https://api.github.com/repos/${OWNER}/${REPO}/releases/latest"; then
        echo "error: could not fetch latest release info" >&2
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
        exit 1
      fi
    fi
  elif ! printf '%s' "${VERSION}" | grep -qE '^v[0-9]+\.[0-9]+\.[0-9]+$'; then
    echo "error: invalid version '${VERSION}' (expected e.g. v0.4.4)" >&2
    exit 1
  fi

  # Download the binary — try the site mirror first, then GitHub.
  dl_ok=0
  if [ -n "${WINGS_MIRROR}" ]; then
    echo "==> downloading ${VERSION} (${ASSET}) from mirror"
    if download "${WINGS_MIRROR}/${VERSION}/${ASSET}" "${BIN_DIR}/cardinal-wings"; then
      dl_ok=1
    else
      echo "    mirror unavailable — falling back to GitHub"
    fi
  fi
  if [ "${dl_ok}" = "0" ]; then
    echo "==> downloading ${VERSION} (${ASSET}) from GitHub"
    if ! download "https://github.com/${OWNER}/${REPO}/releases/download/${VERSION}/${ASSET}" "${BIN_DIR}/cardinal-wings"; then
      echo "" >&2
      echo "Tip: try downloading the script first then running it:" >&2
      echo "  curl -fsSL ${RAW}/install.sh -o /tmp/install-wings.sh" >&2
      echo "  sudo bash /tmp/install-wings.sh" >&2
      exit 1
    fi
  fi
  chmod +x "${BIN_DIR}/cardinal-wings"

  # Verify against the mirror's SHA256SUMS when available.
  if [ "${WINGS_SKIP_VERIFY}" != "1" ] && [ -n "${WINGS_MIRROR}" ]; then
    TMP_SUMS="/tmp/cardinal-wings.sha256"
    rm -f "${TMP_SUMS}"
    if curl -fsSL --retry 2 --connect-timeout 10 --max-time 25 "${WINGS_MIRROR}/${VERSION}/SHA256SUMS" -o "${TMP_SUMS}" 2>/dev/null; then
      expected="$(grep "  ${ASSET}$" "${TMP_SUMS}" | awk '{print $1}' || true)"
      if [ -n "${expected}" ]; then
        actual="$(sha256sum "${BIN_DIR}/cardinal-wings" | awk '{print $1}')"
        if [ "${actual}" != "${expected}" ]; then
          echo "error: SHA256 mismatch for ${ASSET}" >&2
          rm -f "${BIN_DIR}/cardinal-wings" "${TMP_SUMS}"
          exit 1
        fi
        echo "==> SHA256 verified (${expected:0:16}…)"
      fi
    fi
    rm -f "${TMP_SUMS}"
  fi
fi

# ------------------------------------------------------------
# 2. config -- generated once, with a real API key inside
# ------------------------------------------------------------
mkdir -p "${CONF_DIR}"
chmod 700 "${CONF_DIR}"

if [ -f "${CONF_DIR}/config.toml" ]; then
  echo "==> ${CONF_DIR}/config.toml already exists -- keeping it"
else
  API_KEY="$(head -c 24 /dev/urandom | od -An -tx1 | tr -d ' \n')"

  # Generate TLS cert if requested
  if [ "${WINGS_TLS}" = "1" ]; then
    if ! have openssl; then
      echo "error: WINGS_TLS=1 needs openssl" >&2; exit 1
    fi
    echo "==> generating self-signed TLS certificate"
    openssl req -x509 -newkey rsa:2048 -nodes -days 365 \
      -subj "/CN=cardinal-wings" \
      -keyout "${CONF_DIR}/server.key" -out "${CONF_DIR}/server.crt" 2>/dev/null
    chmod 600 "${CONF_DIR}/server.key" "${CONF_DIR}/server.crt"
  fi

  # Write config file -- avoid complex quoting by writing directly
  umask 077
  {
    echo "# cardinal-wings configuration"
    echo "# generated by install.sh"
    echo ""
    echo "[server]"
    echo "host = \"${WINGS_HOST}\""
    echo "port = ${WINGS_PORT}"
    if [ "${WINGS_TLS}" = "1" ]; then
      echo "tls_cert = \"${CONF_DIR}/server.crt\""
      echo "tls_key  = \"${CONF_DIR}/server.key\""
    fi
    echo "sftp_enabled = ${WINGS_SFTP}"
    echo "sftp_host = \"${WINGS_SFTP_HOST}\""
    echo "sftp_port = ${WINGS_SFTP_PORT}"
    echo ""
    echo "[rate_limit]"
    echo "ip_tps = 25"
    echo "ip_burst = 50"
    echo "key_tps = 10"
    echo "key_burst = 30"
    echo "max_clients = 4096"
    echo ""
    echo "[[keys]]"
    echo "name = \"panel\""
    echo "key = \"${API_KEY}\""
    echo "role = \"admin\""
  } > "${CONF_DIR}/config.toml"
  chmod 600 "${CONF_DIR}/config.toml"

  echo "==> wrote ${CONF_DIR}/config.toml"
  echo ""
  echo "    +----------------------------------------------+"
  echo "    |  API KEY (give this to the panel):           |"
  echo "    |  ${API_KEY}  |"
  echo "    +----------------------------------------------+"
  echo ""
fi

# ------------------------------------------------------------
# 3. systemd unit
# ------------------------------------------------------------
# The systemd unit is embedded so the installer never needs GitHub for it.
# Override with WINGS_SYSTEMD_URL to supply a custom unit.
unit_body() {
  cat <<EOF
[Unit]
Description=cardinal-wings REST API daemon
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
ExecStart=${BIN_DIR}/cardinal-wings --config ${CONF_DIR}/config.toml
Restart=on-failure
RestartSec=5s
Environment=CARDINAL_DATA_DIR=/root/.cardinal
StandardOutput=journal
StandardError=journal

[Install]
WantedBy=multi-user.target
EOF
}
UNIT_SRC="/tmp/cardinal-wings.service"
unit_body > "${UNIT_SRC}"
if [ -n "${WINGS_SYSTEMD_URL:-}" ]; then
  echo "==> downloading custom systemd unit from WINGS_SYSTEMD_URL"
  if curl -fsSL --retry 2 --connect-timeout 10 "${WINGS_SYSTEMD_URL}" -o "${UNIT_SRC}"; then
    chmod 644 "${UNIT_SRC}"
  else
    echo "    warn: could not fetch WINGS_SYSTEMD_URL — using the embedded unit" >&2
    unit_body > "${UNIT_SRC}"
  fi
fi
install -m 644 "${UNIT_SRC}" "${SYSTEMD_DIR}/cardinal-wings.service"
systemctl daemon-reload 2>/dev/null || true

# ------------------------------------------------------------
# 4. cardinal runtime check
# ------------------------------------------------------------
if have cardinal; then
  CARDINAL_BIN="$(command -v cardinal)"
  echo "==> cardinal runtime found: ${CARDINAL_BIN}"
  echo "==> enabling cardinal boot supervisor"
  if "${CARDINAL_BIN}" bootstrap --install; then
    echo "    cardinal-bootstrap enabled"
  else
    echo "    warning: could not enable cardinal-bootstrap automatically" >&2
    echo "    run manually: sudo cardinal bootstrap --install" >&2
  fi
else
  echo ""
  echo "    NOTE: 'cardinal' was not found in PATH."
  echo "    Install it first:"
  echo "      curl -fsSL https://raw.githubusercontent.com/animesao/cardinal/main/install.sh | sudo bash"
  echo ""
fi

# ------------------------------------------------------------
# 5. open firewall ports
# ------------------------------------------------------------
open_ports=()
if [ "${WINGS_HOST}" != "127.0.0.1" ] && [ "${WINGS_HOST}" != "localhost" ] && [ "${WINGS_HOST}" != "::1" ]; then
  open_ports+=("${WINGS_PORT}/tcp")   # wings API (remote panel)
fi
if [ "${WINGS_SFTP}" = "1" ] && [ "${WINGS_SFTP_HOST}" != "127.0.0.1" ] && [ "${WINGS_SFTP_HOST}" != "localhost" ]; then
  open_ports+=("${WINGS_SFTP_PORT}/tcp")  # per-container SFTP
fi

open_firewall() {
  if [ "${WINGS_NO_FIREWALL}" = "1" ] || [ "${#open_ports[@]}" -eq 0 ]; then
    return 0
  fi
  echo "==> opening firewall ports: ${open_ports[*]}"
  if have ufw; then
    if command -v ufw >/dev/null 2>&1; then
      ufw allow "${open_ports[@]}" 2>/dev/null && return 0
    fi
  fi
  if have firewall-cmd; then
    local port
    for port in "${open_ports[@]}"; do
      firewall-cmd --permanent --add-port="${port}" 2>/dev/null || true
    done
    firewall-cmd --reload 2>/dev/null || true
    return 0
  fi
  if have iptables; then
    local port
    for port in "${open_ports[@]}"; do
      iptables -I INPUT -p "${port#*/}" --dport "${port%/*}" -j ACCEPT 2>/dev/null || true
    done
    return 0
  fi
  echo "    warning: no ufw/firewalld/iptables found — open ports manually:"
  echo "    ${open_ports[*]}"
}
open_firewall || true

# ------------------------------------------------------------
# 6. verify the binary
# ------------------------------------------------------------
echo "==> verifying binary"
if "${BIN_DIR}/cardinal-wings" --help >/dev/null 2>&1 || "${BIN_DIR}/cardinal-wings" --version >/dev/null 2>&1; then
  echo "    binary OK"
else
  if timeout 3 "${BIN_DIR}/cardinal-wings" --config "${CONF_DIR}/config.toml" >/dev/null 2>&1; then
    echo "    binary OK (started briefly)"
  else
    echo "    warning: could not verify binary automatically" >&2
  fi
fi

# ------------------------------------------------------------
# 7. start / restart the service
# ------------------------------------------------------------
systemctl enable cardinal-wings 2>/dev/null || true
if systemctl is-active --quiet cardinal-wings 2>/dev/null; then
  # Already running (e.g. an upgrade): restart so the new binary takes effect.
  systemctl restart cardinal-wings 2>/dev/null && echo "==> cardinal-wings restarted with the new binary"
else
  systemctl start cardinal-wings 2>/dev/null && echo "==> cardinal-wings started and enabled for next boot"
fi

echo
echo "cardinal-wings installed: ${BIN_DIR}/cardinal-wings"
echo
echo "Next steps:"
echo "  1. systemctl status cardinal-wings          # should be active (running)"
echo "  2. systemctl status cardinal-bootstrap      # boot recovery supervisor"
echo "  3. curl -s localhost:${WINGS_PORT}/v1/ping   # should print pong"

# ------------------------------------------------------------
# Panel binding summary
# ------------------------------------------------------------
SCHEME="http"
[ "${WINGS_TLS}" = "1" ] && SCHEME="https"

# Внешний (публичный) IP ноды, а не loopback.
external_ip() {
  local ip url
  for url in "https://api.ipify.org" "https://ifconfig.me/ip" "https://icanhazip.com"; do
    ip="$(curl -fsSL --connect-timeout 5 --max-time 10 "$url" 2>/dev/null | tr -d '[:space:]')"
    if [ -n "$ip" ] && printf '%s' "$ip" | grep -qE '^[0-9]+\.[0-9]+\.[0-9]+\.[0-9]+$'; then
      echo "$ip"; return 0
    fi
  done
  hostname -I 2>/dev/null | tr ' ' '\n' | grep -vE '^(127\.|::)' | head -1
}
EXT_IP="$(external_ip || true)"
LOCAL_IP="$(hostname -I 2>/dev/null | awk '{print $1}')"
LOOPBACK_NOTE=0
case "${WINGS_HOST}" in
  127.0.0.1|localhost|::1|"")
    BIND_URL="${SCHEME}://${EXT_IP:-127.0.0.1}:${WINGS_PORT}"      # отображаемое: внешний IP
    LOOPBACK_NOTE=1 ;;
  0.0.0.0|"::")
    BIND_URL="${SCHEME}://${EXT_IP:-$LOCAL_IP}:${WINGS_PORT}" ;;
  *)
    BIND_URL="${SCHEME}://${WINGS_HOST}:${WINGS_PORT}" ;;
esac
API_KEY="${API_KEY:-}"
if [ -z "${API_KEY}" ] && [ -f "${CONF_DIR}/config.toml" ]; then
  API_KEY="$(sed -n 's/^key *= *"\([^"]*\)".*/\1/p' "${CONF_DIR}/config.toml" | head -1)"
fi

# Проверка связи с внешним IP (best-effort; ICMP может быть закрыт).
PING_NOTE=""
if [ -n "${EXT_IP}" ] && command -v ping >/dev/null 2>&1; then
  ping_msg="нет ответа (ICMP может быть закрыт)"
  ping -c1 -W2 "${EXT_IP}" >/dev/null 2>&1 && ping_msg="OK (пингуется)"
  PING_NOTE="   Ping : ${ping_msg}"
fi

echo
echo "  ============================================================"
echo "   PANEL BINDING  —  впиши эти данные в форму \"New node\" панели"
echo "   ------------------------------------------------------------"
echo "   URL   : ${BIND_URL}"
echo "   Token : ${API_KEY:-<read from ${CONF_DIR}/config.toml>}"
[ -n "${PING_NOTE}" ] && echo "${PING_NOTE}"
echo "  ------------------------------------------------------------"
echo "   Панель → Admin → Nodes → Add node → URL + Token."
echo "  ============================================================"
if [ "${LOOPBACK_NOTE}" = "1" ]; then
  echo
  echo "NOTE: wings слушает только loopback (${WINGS_HOST}) — для внешней панели"
  echo "      переустанови с WINGS_HOST=0.0.0.0 (рекомендуется TLS). URL выше —"
  echo "      внешний IP ноды, но этот адрес откроется только когда wings будет"
  echo "      слушать не только 127.0.0.1."
fi
echo
echo "Open ports (already added to the firewall):"
echo "  ${WINGS_PORT}/tcp  — wings API"
if [ "${WINGS_SFTP}" = "1" ]; then
  echo "  ${WINGS_SFTP_PORT}/tcp — per-container SFTP (Filezilla/WinSCP)"
fi
echo
if [ "${WINGS_HOST}" = "127.0.0.1" ]; then
  echo "NOTE: wings API bound to loopback -- only the local panel can reach it."
  echo "      For a remote panel re-run with WINGS_HOST=0.0.0.0 (TLS recommended)."
else
  echo "NOTE: wings API is bound to ${WINGS_HOST} -- make sure the panel can reach ${BIND_URL}."
fi
if [ "${WINGS_SFTP}" = "1" ] && [ "${WINGS_SFTP_HOST}" != "0.0.0.0" ] && [ "${WINGS_SFTP_HOST}" != "::" ]; then
  echo "NOTE: SFTP bound to ${WINGS_SFTP_HOST} -- clients must reach this address on port ${WINGS_SFTP_PORT}."
fi
