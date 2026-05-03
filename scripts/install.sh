#!/usr/bin/env bash
# install.sh — fetch the latest MX Script binary for the current
# OS / architecture and put it on $PATH.
#
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/jlkdevelop/mxscript/main/scripts/install.sh | bash
#
# Installs into $HOME/.mx/bin/mx (creates the dir if missing). To
# pin a specific version: pass MX_VERSION as an env var.
#
#   curl ... | MX_VERSION=v0.77.0 bash

set -euo pipefail

MX_VERSION="${MX_VERSION:-latest}"
INSTALL_DIR="${INSTALL_DIR:-$HOME/.mx/bin}"
REPO="jlkdevelop/mxscript"

# --- Detect platform -----------------------------------------------------
OS=""
case "$(uname -s)" in
  Darwin) OS="darwin" ;;
  Linux)  OS="linux" ;;
  *)      echo "Unsupported OS: $(uname -s)" >&2; exit 1 ;;
esac
ARCH=""
case "$(uname -m)" in
  x86_64|amd64) ARCH="amd64" ;;
  arm64|aarch64) ARCH="arm64" ;;
  *) echo "Unsupported arch: $(uname -m)" >&2; exit 1 ;;
esac

# --- Resolve version -----------------------------------------------------
if [ "$MX_VERSION" = "latest" ]; then
  MX_VERSION=$(curl -fsSL "https://api.github.com/repos/$REPO/releases/latest" \
    | grep -o '"tag_name": *"[^"]*"' | head -1 | sed 's/.*"v\?\([^"]*\)".*/v\1/')
  if [ -z "$MX_VERSION" ]; then
    echo "Failed to resolve latest version" >&2
    exit 1
  fi
fi

VERSION_NUM="${MX_VERSION#v}"
ARCHIVE="mxscript_${VERSION_NUM}_${OS}_${ARCH}.tar.gz"
URL="https://github.com/$REPO/releases/download/$MX_VERSION/$ARCHIVE"

echo "Installing MX Script $MX_VERSION ($OS/$ARCH)..."
mkdir -p "$INSTALL_DIR"
TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT

if ! curl -fsSL -o "$TMP/$ARCHIVE" "$URL"; then
  echo "Failed to download $URL" >&2
  echo "Falling back to 'go install'..." >&2
  if command -v go > /dev/null; then
    go install "github.com/$REPO@$MX_VERSION"
    cp "$(go env GOPATH)/bin/mxscript" "$INSTALL_DIR/mx"
    chmod +x "$INSTALL_DIR/mx"
    echo "Installed via go install -> $INSTALL_DIR/mx"
    exit 0
  fi
  echo "And no Go toolchain available. Aborting." >&2
  exit 1
fi

tar -xzf "$TMP/$ARCHIVE" -C "$TMP"
# GoReleaser names the binary `mx` inside the archive.
mv "$TMP/mx" "$INSTALL_DIR/mx"
chmod +x "$INSTALL_DIR/mx"

echo ""
echo "✓ Installed mx -> $INSTALL_DIR/mx"
echo ""
echo "Add the install dir to your PATH if it isn't already:"
echo "  export PATH=\"$INSTALL_DIR:\$PATH\""
echo ""
"$INSTALL_DIR/mx" version
