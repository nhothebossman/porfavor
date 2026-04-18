#!/usr/bin/env bash
set -e

REPO="nhothebossman/porfavor"
BIN="porfavor"
VERSION="latest"

# Detect OS
OS="$(uname -s)"
ARCH="$(uname -m)"

case "$OS" in
  Linux*)  GOOS="linux" ;;
  Darwin*) GOOS="darwin" ;;
  *)       echo "Unsupported OS: $OS"; exit 1 ;;
esac

case "$ARCH" in
  x86_64)          GOARCH="amd64" ;;
  arm64|aarch64)   GOARCH="arm64" ;;
  armv7l)          GOARCH="arm" ;;
  *)               echo "Unsupported arch: $ARCH"; exit 1 ;;
esac

# Detect Termux
if [ -n "$PREFIX" ] && [ -d "$PREFIX/bin" ]; then
  INSTALL_DIR="$PREFIX/bin"
  IS_TERMUX=1
elif [ "$GOOS" = "darwin" ]; then
  INSTALL_DIR="/usr/local/bin"
  IS_TERMUX=0
else
  INSTALL_DIR="$HOME/.local/bin"
  IS_TERMUX=0
fi

FILENAME="${BIN}-${GOOS}-${GOARCH}"
URL="https://github.com/${REPO}/releases/${VERSION}/download/${FILENAME}"

echo ""
echo "  Por Favor installer"
echo "  ════════════════════════════════════"
echo "  OS:      $GOOS"
echo "  Arch:    $GOARCH"
echo "  Target:  $INSTALL_DIR/$BIN"
echo ""

# Download
TMP="$(mktemp)"
echo "  Downloading $FILENAME..."

if command -v curl &>/dev/null; then
  curl -fsSL "$URL" -o "$TMP"
elif command -v wget &>/dev/null; then
  wget -q "$URL" -O "$TMP"
else
  echo "  Error: curl or wget required"
  exit 1
fi

chmod +x "$TMP"

# Install
mkdir -p "$INSTALL_DIR"
mv "$TMP" "$INSTALL_DIR/$BIN"

echo "  Installed to $INSTALL_DIR/$BIN"

# PATH hint
if ! echo "$PATH" | grep -q "$INSTALL_DIR"; then
  echo ""
  echo "  Add to your shell config:"
  if [ "$IS_TERMUX" = "1" ]; then
    echo "    (already in PATH via Termux)"
  else
    echo "    export PATH=\"\$HOME/.local/bin:\$PATH\""
  fi
fi

# Verify
if "$INSTALL_DIR/$BIN" --version &>/dev/null; then
  echo ""
  echo "  $("$INSTALL_DIR/$BIN" --version) installed successfully ✓"
else
  echo "  Installed. Run: porfavor"
fi

echo ""
