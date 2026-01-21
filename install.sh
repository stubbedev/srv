#!/bin/sh
set -e

REPO="stubbedev/srv"
INSTALL_DIR="/usr/local/bin"
FALLBACK_DIR="$HOME/.local/bin"
BINARY="srv"

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[0;33m'
NC='\033[0m'

info() { printf "${GREEN}▸${NC} %s\n" "$1"; }
warn() { printf "${YELLOW}▸${NC} %s\n" "$1"; }
error() {
  printf "${RED}▸${NC} %s\n" "$1" >&2
  exit 1
}

# Check if we need sudo or should use fallback directory
check_sudo() {
  if [ -w "$INSTALL_DIR" ]; then
    SUDO=""
    return
  fi

  if [ "$(id -u)" -eq 0 ]; then
    SUDO=""
    return
  fi

  if ! command -v sudo >/dev/null 2>&1; then
    warn "Cannot write to ${INSTALL_DIR} and sudo is not available"
    use_fallback_dir
    return
  fi

  SUDO="sudo"
  info "Installation will require sudo access to ${INSTALL_DIR}"
}

# Use fallback directory if main installation fails
use_fallback_dir() {
  info "Using fallback directory: ${FALLBACK_DIR}"
  INSTALL_DIR="$FALLBACK_DIR"
  SUDO=""

  # Create directory if it doesn't exist
  if [ ! -d "$INSTALL_DIR" ]; then
    mkdir -p "$INSTALL_DIR" || error "Failed to create ${INSTALL_DIR}"
  fi

  if [ ! -w "$INSTALL_DIR" ]; then
    error "Cannot write to fallback directory ${INSTALL_DIR}"
  fi
}

# Detect OS and architecture
detect_platform() {
  OS=$(uname -s | tr '[:upper:]' '[:lower:]')
  ARCH=$(uname -m)

  case "$OS" in
  linux) OS="linux" ;;
  darwin) OS="darwin" ;;
  freebsd) OS="freebsd" ;;
  mingw* | msys* | cygwin*) OS="windows" ;;
  *) error "Unsupported OS: $OS" ;;
  esac

  case "$ARCH" in
  x86_64 | amd64) ARCH="amd64" ;;
  aarch64 | arm64) ARCH="arm64" ;;
  armv7l) ARCH="armv7" ;;
  i386 | i686) ARCH="386" ;;
  *) error "Unsupported architecture: $ARCH" ;;
  esac

  PLATFORM="${OS}-${ARCH}"
  if [ "$OS" = "windows" ]; then
    BINARY="srv.exe"
  fi
}

# Get latest release version
get_latest_version() {
  info "Fetching latest version..."
  if command -v curl >/dev/null 2>&1; then
    VERSION=$(curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest" | grep '"tag_name"' | cut -d'"' -f4)
  elif command -v wget >/dev/null 2>&1; then
    VERSION=$(wget -qO- "https://api.github.com/repos/${REPO}/releases/latest" | grep '"tag_name"' | cut -d'"' -f4)
  else
    error "curl or wget required"
  fi
  if [ -z "$VERSION" ]; then
    error "Failed to get latest version"
  fi
}

# Download binary
download() {
  URL="https://github.com/${REPO}/releases/download/${VERSION}/srv-${PLATFORM}"
  TMPFILE=$(mktemp)

  info "Downloading srv ${VERSION} for ${PLATFORM}..." >&2

  if command -v curl >/dev/null 2>&1; then
    curl -fsSL "$URL" -o "$TMPFILE" || error "Download failed"
  else
    wget -q "$URL" -O "$TMPFILE" || error "Download failed"
  fi

  chmod +x "$TMPFILE"
  echo "$TMPFILE"
}

# Install binary
install_binary() {
  TMPFILE=$1
  info "Installing to ${INSTALL_DIR}..."
  if ! $SUDO mv "$TMPFILE" "${INSTALL_DIR}/${BINARY}"; then
    # If installation failed and we haven't already tried fallback
    if [ "$INSTALL_DIR" != "$FALLBACK_DIR" ]; then
      warn "Failed to install to ${INSTALL_DIR}"
      use_fallback_dir
      info "Retrying installation to ${INSTALL_DIR}..."
      if ! mv "$TMPFILE" "${INSTALL_DIR}/${BINARY}"; then
        rm -f "$TMPFILE"
        error "Failed to install binary to ${INSTALL_DIR}"
      fi
    else
      rm -f "$TMPFILE"
      error "Failed to install binary to ${INSTALL_DIR}"
    fi
  fi
}

# Verify installation
verify() {
  if command -v srv >/dev/null 2>&1; then
    info "Installed $(srv version 2>/dev/null || echo "srv")"
    info "Run 'srv init' to get started"
  else
    warn "Installed to ${INSTALL_DIR}/${BINARY}"
    warn "Add ${INSTALL_DIR} to your PATH"

    # Provide helpful PATH instructions for fallback directory
    if [ "$INSTALL_DIR" = "$FALLBACK_DIR" ]; then
      warn ""
      warn "To add ${INSTALL_DIR} to your PATH, run:"
      if [ -n "$BASH_VERSION" ]; then
        warn "  echo 'export PATH=\"\$HOME/.local/bin:\$PATH\"' >> ~/.bashrc"
        warn "  source ~/.bashrc"
      elif [ -n "$ZSH_VERSION" ]; then
        warn "  echo 'export PATH=\"\$HOME/.local/bin:\$PATH\"' >> ~/.zshrc"
        warn "  source ~/.zshrc"
      else
        warn "  echo 'export PATH=\"\$HOME/.local/bin:\$PATH\"' >> ~/.profile"
        warn "  source ~/.profile"
      fi
    fi
  fi
}

main() {
  check_sudo
  detect_platform
  get_latest_version
  TMPFILE=$(download)
  install_binary "$TMPFILE"
  verify
}

main
