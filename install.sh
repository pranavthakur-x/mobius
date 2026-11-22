#!/usr/bin/env bash
# Mobius Installer Script
# Works on Linux and macOS (Intel & Apple Silicon)

set -e

REPO="pranavthakur-x/mobius"
INSTALL_DIR="/usr/local/bin"
BIN_NAME="mobius"

# Colors
GREEN='\033[0;32m'
BLUE='\033[0;34m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
NC='\033[0m' # No Color

echo -e "${BLUE}==>${NC} Installing ${GREEN}mobius${NC} — High-Performance AI Agent Harness..."

# Detect OS
OS="$(uname -s | tr '[:upper:]' '[:lower:]')"
case "$OS" in
  linux*)  TARGET_OS="linux" ;;
  darwin*) TARGET_OS="darwin" ;;
  *)
    echo -e "${RED}Error: Unsupported operating system: $OS${NC}"
    exit 1
    ;;
esac

# Detect Architecture
ARCH="$(uname -m)"
case "$ARCH" in
  x86_64|amd64) TARGET_ARCH="amd64" ;;
  arm64|aarch64) TARGET_ARCH="arm64" ;;
  *)
    echo -e "${RED}Error: Unsupported architecture: $ARCH${NC}"
    exit 1
    ;;
esac

echo -e "${BLUE}==>${NC} Detected platform: ${TARGET_OS}/${TARGET_ARCH}"

# Fetch latest release tag from GitHub API
LATEST_TAG=$(curl -fsSL --connect-timeout 15 --max-time 30 "https://api.github.com/repos/${REPO}/releases/latest" | grep '"tag_name":' | sed -E 's/.*"([^"]+)".*/\1/')

if [ -z "$LATEST_TAG" ]; then
  echo -e "${YELLOW}Notice: No pre-built release binary found yet.${NC}"
  if command -v go >/dev/null 2>&1; then
    echo -e "${BLUE}==>${NC} Compiling directly with Go: 'go install github.com/${REPO}/cmd/mobius@latest'..."
    go install "github.com/${REPO}/cmd/mobius@latest"
    echo -e "${GREEN}✓ Successfully installed mobius to $(go env GOPATH)/bin/mobius${NC}"
    exit 0
  else
    echo -e "${RED}Error: GitHub release not found and Go is not installed.${NC}"
    exit 1
  fi
fi

VERSION="${LATEST_TAG#v}"
TARBALL="mobius_${VERSION}_${TARGET_OS}_${TARGET_ARCH}.tar.gz"
DOWNLOAD_URL="https://github.com/${REPO}/releases/download/${LATEST_TAG}/${TARBALL}"

TMP_DIR="$(mktemp -d)"
trap 'rm -rf "$TMP_DIR"' EXIT

echo -e "${BLUE}==>${NC} Downloading ${DOWNLOAD_URL}..."
if ! curl -fL --progress-bar --connect-timeout 15 --max-time 120 --retry 2 "$DOWNLOAD_URL" -o "$TMP_DIR/$TARBALL"; then
  echo -e "${RED}Error: Failed to download release tarball.${NC}"
  exit 1
fi

echo -e "${BLUE}==>${NC} Extracting binary..."
tar -xzf "$TMP_DIR/$TARBALL" -C "$TMP_DIR"
EXTRACTED_BIN="$TMP_DIR/mobius_${VERSION}_${TARGET_OS}_${TARGET_ARCH}/${BIN_NAME}"
if [ ! -f "$EXTRACTED_BIN" ]; then
  EXTRACTED_BIN="$TMP_DIR/${BIN_NAME}"
fi

# Install target (never block on interactive sudo inside curl | bash)
SUDO=""
if [ ! -w "$INSTALL_DIR" ]; then
  if command -v sudo >/dev/null 2>&1 && sudo -n true 2>/dev/null; then
    SUDO="sudo"
  else
    mkdir -p "$HOME/.local/bin"
    INSTALL_DIR="$HOME/.local/bin"
  fi
fi

echo -e "${BLUE}==>${NC} Installing to ${INSTALL_DIR}/${BIN_NAME}..."
$SUDO cp "$EXTRACTED_BIN" "$INSTALL_DIR/$BIN_NAME"
$SUDO chmod +x "$INSTALL_DIR/$BIN_NAME"

echo -e "${GREEN}✓ Successfully installed mobius (${LATEST_TAG}) to ${INSTALL_DIR}/${BIN_NAME}!${NC}"
if [[ ":$PATH:" != *":$INSTALL_DIR:"* ]]; then
  echo -e "${YELLOW}Note: ${INSTALL_DIR} is not in your PATH. Add this to your ~/.bashrc or ~/.zshrc:${NC}"
  echo -e "  export PATH=\"${INSTALL_DIR}:\$PATH\""
fi
echo ""
echo -e "Quickstart:"
echo -e "  export ANTHROPIC_API_KEY=\"your-key\"   # or OPENAI_API_KEY / GEMINI_API_KEY"
echo -e "  cd ~/your-project"
echo -e "  mobius"
