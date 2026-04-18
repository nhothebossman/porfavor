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

# macOS: remove quarantine flag so Gatekeeper doesn't block it
if [ "$GOOS" = "darwin" ]; then
  xattr -d com.apple.quarantine "$TMP" 2>/dev/null || true
fi

# Install
mkdir -p "$INSTALL_DIR"
mv "$TMP" "$INSTALL_DIR/$BIN"

echo "  Installed to $INSTALL_DIR/$BIN"

# PATH hint — only shown if install dir isn't already in PATH
if ! echo "$PATH" | grep -q "$INSTALL_DIR"; then
  echo ""
  echo "  $INSTALL_DIR is not in your PATH."
  echo "  Add this line to your shell config:"
  echo ""
  if [ "$IS_TERMUX" = "1" ]; then
    echo "    (Termux: already in PATH)"
  elif [ "$GOOS" = "darwin" ]; then
    SHELL_NAME="$(basename "$SHELL")"
    if [ "$SHELL_NAME" = "zsh" ]; then
      echo "    echo 'export PATH=\"\$HOME/.local/bin:\$PATH\"' >> ~/.zshrc && source ~/.zshrc"
    else
      echo "    echo 'export PATH=\"\$HOME/.local/bin:\$PATH\"' >> ~/.bash_profile && source ~/.bash_profile"
    fi
  else
    echo "    echo 'export PATH=\"\$HOME/.local/bin:\$PATH\"' >> ~/.bashrc && source ~/.bashrc"
  fi
  echo ""
fi

# Verify
if "$INSTALL_DIR/$BIN" --version &>/dev/null; then
  echo "  $("$INSTALL_DIR/$BIN" --version) installed successfully ✓"
else
  echo "  Installed. Run: porfavor"
fi

echo ""
