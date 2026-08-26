#!/usr/bin/env bash
# cardinal-wings installer — downloads a tagged release binary from GitHub
# Releases and installs it as a systemd service.
# Usage: curl -fsSL install.sh | bash   (installs "latest")
#        ./install.sh v0.1.0            (or a specific tag)
#        ./install.sh local             (build from the current directory)
set -euo pipefail

OWNER="animesao"
REPO="cardinal-wings"

# Version to install. "latest" resolves via the GitHub API; "local" builds
# from the current directory (requires Go); anything else is a tag.
VERSION="${1:-latest}"

PREFIX="${PREFIX:-/usr/local}"
BIN_DIR="${PREFIX}/bin"
CONF_DIR="/etc/cardinal-wings"

# Detect OS and architecture for the release asset name.
OS="$(uname -s | tr '[:upper:]' '[:lower:]')"
ARCH="$(uname -m)"
case "${ARCH}" in
    x86_64)  ARCH="amd64" ;;
    aarch64) ARCH="arm64" ;;
esac
case "${OS}" in
    linux) ASSET="cardinal-wings-linux-${ARCH}" ;;
    darwin) ASSET="cardinal-wings-darwin-${ARCH}" ;;
    *)
        echo "unsupported platform: ${OS}/${ARCH}" >&2
        exit 1
        ;;
esac

echo "==> cardinal-wings installer"
echo "    platform: ${OS}/${ARCH}"
echo "    prefix:   ${PREFIX}"

mkdir -p "${BIN_DIR}"

if [ "${VERSION}" = "local" ] || [ "${VERSION}" = "dev" ]; then
    echo "==> building from local source"
    go build -trimpath \
        -ldflags="-s -w -X github.com/animesao/cardinal-wings/internal/server.version=dev" \
        -o "${BIN_DIR}/cardinal-wings" .
else
    if [ "${VERSION}" = "latest" ]; then
        echo "==> resolving latest release"
        VERSION="$(curl -fsSL "https://api.github.com/repos/${OWNER}/${REPO}/releases/latest" \
            | grep -m1 '"tag_name"' | sed 's/.*"tag_name": "\(.*\)".*/\1/')"
    fi
    url="https://github.com/${OWNER}/${REPO}/releases/download/${VERSION}/${ASSET}"
    echo "==> downloading ${VERSION}"
    curl -fsSL "${url}" -o "${BIN_DIR}/cardinal-wings"
    chmod +x "${BIN_DIR}/cardinal-wings"
fi

echo "==> installing config"
if [ ! -f "${CONF_DIR}/config.toml" ]; then
    mkdir -p "${CONF_DIR}"
    chmod 700 "${CONF_DIR}"
    install -m 600 /dev/null "${CONF_DIR}/config.toml"
    echo "    wrote empty ${CONF_DIR}/config.toml — edit it to add keys"
else
    echo "    ${CONF_DIR}/config.toml already present — leaving it untouched"
fi
install -m 600 "${CONF_DIR}/config.toml" "${CONF_DIR}/config.example.toml" 2>/dev/null || true

echo "==> installing systemd unit"
if [ -f systemd/cardinal-wings.service ]; then
    install -m 644 systemd/cardinal-wings.service /etc/systemd/system/cardinal-wings.service
    systemctl daemon-reload
fi

echo
echo "cardinal-wings installed: ${BIN_DIR}/cardinal-wings"
echo "  1. edit  ${CONF_DIR}/config.toml  (add [[keys]])"
echo "  2. run   systemctl enable --now cardinal-wings"
echo "  3. check systemctl status cardinal-wings"