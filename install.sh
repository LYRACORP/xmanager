#!/bin/bash
set -euo pipefail

REPO="lyracorp/xmanager"
BINARY="xmanager"
INSTALL_DIR="/usr/local/bin"

detect_platform() {
    OS=$(uname -s | tr '[:upper:]' '[:lower:]')
    ARCH=$(uname -m)

    case "$ARCH" in
        x86_64|amd64) ARCH="amd64" ;;
        aarch64|arm64) ARCH="arm64" ;;
        *) echo "Unsupported architecture: $ARCH"; exit 1 ;;
    esac

    case "$OS" in
        linux) OS="linux" ;;
        darwin) OS="darwin" ;;
        *) echo "Unsupported OS: $OS"; exit 1 ;;
    esac

    echo "${OS}_${ARCH}"
}

get_latest_version() {
    curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest" | \
        grep '"tag_name"' | sed -E 's/.*"v([^"]+)".*/\1/'
}

main() {
    echo "==> XManager TUI Installer"
    echo ""

    PLATFORM=$(detect_platform)
    echo "==> Detected platform: ${PLATFORM}"

    echo "==> Fetching latest version..."
    VERSION=$(get_latest_version)
    if [ -z "$VERSION" ]; then
        echo "Error: Could not determine latest version."
        exit 1
    fi
    echo "==> Latest version: v${VERSION}"

    FILENAME="${BINARY}_${VERSION}_${PLATFORM}.tar.gz"
    URL="https://github.com/${REPO}/releases/download/v${VERSION}/${FILENAME}"

    TMPDIR=$(mktemp -d)
    trap "rm -rf $TMPDIR" EXIT

    echo "==> Downloading ${URL}..."
    curl -fsSL "$URL" -o "${TMPDIR}/${FILENAME}"

    echo "==> Extracting..."
    tar xzf "${TMPDIR}/${FILENAME}" -C "$TMPDIR"

    echo "==> Installing to ${INSTALL_DIR}/${BINARY}..."
    if [ -w "$INSTALL_DIR" ]; then
        cp "${TMPDIR}/${BINARY}" "${INSTALL_DIR}/${BINARY}"
        chmod +x "${INSTALL_DIR}/${BINARY}"
        ln -sf "${INSTALL_DIR}/${BINARY}" "${INSTALL_DIR}/vpsm"
    else
        sudo cp "${TMPDIR}/${BINARY}" "${INSTALL_DIR}/${BINARY}"
        sudo chmod +x "${INSTALL_DIR}/${BINARY}"
        sudo ln -sf "${INSTALL_DIR}/${BINARY}" "${INSTALL_DIR}/vpsm"
    fi

    echo ""
    echo "==> XManager TUI v${VERSION} installed successfully!"
    echo ""
    echo "    Run 'xmanager' or 'vpsm' to get started."
    echo ""
}

main "$@"
