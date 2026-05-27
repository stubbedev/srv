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
  mingw* | msys* | cygwin*) error "Windows is not supported by srv releases. Use brew on macOS/Linux or build from source." ;;
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
}

# Get latest release version. Sets VERSION (with leading v, e.g. v1.2.3) and
# VERSION_NUM (without, e.g. 1.2.3) — release artifacts encode the bare version
# while the release tag carries the v prefix.
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
  VERSION_NUM="${VERSION#v}"
}

# Download a URL to a temp file, print the temp file path
fetch_url() {
  url="$1"
  tmpfile=$(mktemp)
  if command -v curl >/dev/null 2>&1; then
    curl -fsSL "$url" -o "$tmpfile" || { rm -f "$tmpfile"; return 1; }
  else
    wget -q "$url" -O "$tmpfile" || { rm -f "$tmpfile"; return 1; }
  fi
  echo "$tmpfile"
}

# Download tarball, verify checksum, extract, print the extracted binary path
download() {
  TARBALL="srv-${VERSION_NUM}-${PLATFORM}.tar.gz"
  URL="https://github.com/${REPO}/releases/download/${VERSION}/${TARBALL}"
  SHA_URL="${URL}.sha256"

  info "Downloading srv ${VERSION} for ${PLATFORM}..." >&2

  TARFILE=$(fetch_url "$URL") || error "Download failed (${URL})"

  if command -v sha256sum >/dev/null 2>&1; then
    info "Verifying checksum..." >&2
    SHAFILE=$(fetch_url "$SHA_URL") || { rm -f "$TARFILE"; error "Failed to download checksum (${SHA_URL})"; }
    EXPECTED=$(awk '{print $1}' "$SHAFILE")
    rm -f "$SHAFILE"
    if [ -z "$EXPECTED" ]; then
      rm -f "$TARFILE"
      error "No checksum found in ${TARBALL}.sha256"
    fi
    ACTUAL=$(sha256sum "$TARFILE" | awk '{print $1}')
    if [ "$ACTUAL" != "$EXPECTED" ]; then
      rm -f "$TARFILE"
      error "Checksum mismatch: expected ${EXPECTED}, got ${ACTUAL}"
    fi
    info "Checksum verified" >&2
  fi

  EXTRACT_DIR=$(mktemp -d)
  if ! tar -xzf "$TARFILE" -C "$EXTRACT_DIR"; then
    rm -f "$TARFILE"
    rm -rf "$EXTRACT_DIR"
    error "Failed to extract ${TARBALL}"
  fi
  rm -f "$TARFILE"

  STAGE="${EXTRACT_DIR}/srv-${VERSION_NUM}-${PLATFORM}"
  if [ ! -x "${STAGE}/srv" ]; then
    rm -rf "$EXTRACT_DIR"
    error "Extracted tarball did not contain ${STAGE}/srv"
  fi

  chmod +x "${STAGE}/srv"
  echo "${STAGE}/srv"
}

# Install binary
install_binary() {
  SRC=$1
  info "Installing to ${INSTALL_DIR}..."
  if ! $SUDO mv "$SRC" "${INSTALL_DIR}/${BINARY}"; then
    # If installation failed and we haven't already tried fallback
    if [ "$INSTALL_DIR" != "$FALLBACK_DIR" ]; then
      warn "Failed to install to ${INSTALL_DIR}"
      use_fallback_dir
      info "Retrying installation to ${INSTALL_DIR}..."
      if ! mv "$SRC" "${INSTALL_DIR}/${BINARY}"; then
        rm -f "$SRC"
        error "Failed to install binary to ${INSTALL_DIR}"
      fi
    else
      rm -f "$SRC"
      error "Failed to install binary to ${INSTALL_DIR}"
    fi
  fi
  # Clean up the parent extract dir if it's now empty.
  rmdir "$(dirname "$SRC")" 2>/dev/null || true
  rmdir "$(dirname "$(dirname "$SRC")")" 2>/dev/null || true
}

# Verify installation
verify() {
  if ! command -v mkcert >/dev/null 2>&1; then
    warn "mkcert is required at runtime but not on PATH."
    warn "  brew install mkcert   # macOS"
    warn "  apt install mkcert    # Debian/Ubuntu"
    warn "  nix profile install nixpkgs#mkcert"
  fi

  if command -v srv >/dev/null 2>&1; then
    info "Installed $(srv version 2>/dev/null || echo "srv")"
    info "Run 'srv install' to get started"
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
  SRC=$(download)
  install_binary "$SRC"
  verify
}

main
