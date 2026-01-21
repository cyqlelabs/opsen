#!/bin/bash
# Opsen Installation Script
# Downloads and installs pre-built binaries from GitHub releases
# Falls back to building from source if binaries not available

set -e

# Configuration
REPO="cyqlelabs/opsen"
INSTALL_DIR="${INSTALL_DIR:-/usr/local/bin}"
BINARY_SERVER="opsen-server"
BINARY_CLIENT="opsen-client"
VERSION="${VERSION:-latest}"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Print colored output
info() { echo -e "${GREEN}[INFO]${NC} $1"; }
warn() { echo -e "${YELLOW}[WARN]${NC} $1"; }
error() { echo -e "${RED}[ERROR]${NC} $1"; exit 1; }

# Detect OS and architecture
detect_platform() {
    OS=$(uname -s | tr '[:upper:]' '[:lower:]')
    ARCH=$(uname -m)

    case $OS in
        linux)
            OS="linux"
            ;;
        darwin)
            OS="darwin"
            ;;
        mingw* | msys* | cygwin*)
            OS="windows"
            ;;
        *)
            error "Unsupported operating system: $OS"
            ;;
    esac

    case $ARCH in
        x86_64 | amd64)
            ARCH="amd64"
            ;;
        aarch64 | arm64)
            ARCH="arm64"
            ;;
        armv7l | armv6l)
            ARCH="arm"
            ;;
        *)
            error "Unsupported architecture: $ARCH"
            ;;
    esac

    info "Detected platform: $OS-$ARCH"
}

# Check if a command exists
command_exists() {
    command -v "$1" >/dev/null 2>&1
}

# Get latest release version
get_latest_version() {
    if command_exists curl; then
        VERSION=$(curl -sL "https://api.github.com/repos/$REPO/releases/latest" | grep '"tag_name"' | cut -d'"' -f4)
    elif command_exists wget; then
        VERSION=$(wget -qO- "https://api.github.com/repos/$REPO/releases/latest" | grep '"tag_name"' | cut -d'"' -f4)
    else
        error "Neither curl nor wget found. Please install one of them."
    fi

    if [ -z "$VERSION" ]; then
        warn "Could not fetch latest version, falling back to build from source"
        return 1
    fi

    info "Latest version: $VERSION"
}

# Download and extract binary
download_binary() {
    local binary_name=$1
    local archive_name="${binary_name}_${VERSION}_${OS}_${ARCH}.tar.gz"
    local download_url="https://github.com/$REPO/releases/download/$VERSION/$archive_name"
    local tmp_dir=$(mktemp -d)

    info "Downloading $binary_name from $download_url"

    if command_exists curl; then
        if ! curl -sL "$download_url" -o "$tmp_dir/$archive_name"; then
            warn "Failed to download $binary_name"
            return 1
        fi
    elif command_exists wget; then
        if ! wget -q "$download_url" -O "$tmp_dir/$archive_name"; then
            warn "Failed to download $binary_name"
            return 1
        fi
    fi

    info "Extracting $archive_name"
    tar -xzf "$tmp_dir/$archive_name" -C "$tmp_dir"

    if [ ! -f "$tmp_dir/$binary_name" ]; then
        error "Binary not found in archive"
    fi

    # Install binary
    if [ -w "$INSTALL_DIR" ]; then
        mv "$tmp_dir/$binary_name" "$INSTALL_DIR/$binary_name"
        chmod +x "$INSTALL_DIR/$binary_name"
    else
        info "Installing to $INSTALL_DIR (requires sudo)"
        sudo mv "$tmp_dir/$binary_name" "$INSTALL_DIR/$binary_name"
        sudo chmod +x "$INSTALL_DIR/$binary_name"
    fi

    info "Installed $binary_name to $INSTALL_DIR/$binary_name"
    rm -rf "$tmp_dir"
}

# Build from source as fallback
build_from_source() {
    warn "Building from source..."

    if ! command_exists go; then
        error "Go is not installed. Please install Go 1.21+ from https://go.dev/dl/"
    fi

    GO_VERSION=$(go version | awk '{print $3}' | sed 's/go//')
    info "Found Go version: $GO_VERSION"

    # Clone or use current directory
    if [ -f "go.mod" ] && grep -q "cyqle.in/opsen" go.mod; then
        info "Building in current directory"
        BUILD_DIR="."
    else
        info "Cloning repository"
        BUILD_DIR=$(mktemp -d)
        git clone "https://github.com/$REPO.git" "$BUILD_DIR"
        cd "$BUILD_DIR"
    fi

    # Build binaries
    info "Building server..."
    cd server && go build -o "../bin/$BINARY_SERVER" . && cd ..

    info "Building client..."
    cd client && go build -o "../bin/$BINARY_CLIENT" . && cd ..

    # Install binaries
    if [ -w "$INSTALL_DIR" ]; then
        cp "bin/$BINARY_SERVER" "$INSTALL_DIR/"
        cp "bin/$BINARY_CLIENT" "$INSTALL_DIR/"
        chmod +x "$INSTALL_DIR/$BINARY_SERVER"
        chmod +x "$INSTALL_DIR/$BINARY_CLIENT"
    else
        info "Installing to $INSTALL_DIR (requires sudo)"
        sudo cp "bin/$BINARY_SERVER" "$INSTALL_DIR/"
        sudo cp "bin/$BINARY_CLIENT" "$INSTALL_DIR/"
        sudo chmod +x "$INSTALL_DIR/$BINARY_SERVER"
        sudo chmod +x "$INSTALL_DIR/$BINARY_CLIENT"
    fi

    info "Built and installed from source"
}

# Main installation process
main() {
    echo "=== Opsen Load Balancer Installation ==="
    echo ""

    detect_platform

    # Try to download pre-built binaries
    if [ "$VERSION" = "latest" ]; then
        if ! get_latest_version; then
            build_from_source
            exit 0
        fi
    fi

    # Download server and client
    if download_binary "$BINARY_SERVER" && download_binary "$BINARY_CLIENT"; then
        info "Installation complete!"
    else
        warn "Failed to download pre-built binaries"
        build_from_source
    fi

    echo ""
    echo "=== Installation Complete ==="
    echo ""
    echo "Verify installation:"
    echo "  $BINARY_SERVER -version"
    echo "  $BINARY_CLIENT -version"
    echo ""
    echo "Quick start:"
    echo "  1. Create server.yml configuration"
    echo "  2. Run: $BINARY_SERVER -config server.yml"
    echo "  3. Run: $BINARY_CLIENT -config client.yml"
    echo ""
    echo "See README.md for detailed usage instructions"
    echo "https://github.com/$REPO"
}

main
