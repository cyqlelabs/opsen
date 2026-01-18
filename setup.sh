#!/bin/bash
# Setup script for Opsen Load Balancer
# Run this script to download dependencies and build binaries

set -e

echo "=== Opsen Load Balancer Setup ==="
echo ""

# Check Go version
GO_VERSION=$(go version | awk '{print $3}')
echo "✓ Go version: $GO_VERSION"

# Download dependencies
echo ""
echo "Downloading Go dependencies..."
go mod download
go mod tidy

echo "✓ Dependencies downloaded"

# Create bin directory
mkdir -p bin

# Build server
echo ""
echo "Building server..."
cd server && go build -o ../bin/opsen-server main.go
cd ..
echo "✓ Server built: bin/opsen-server"

# Build client
echo ""
echo "Building client..."
cd client && go build -o ../bin/opsen-client main.go
cd ..
echo "✓ Client built: bin/opsen-client"

# Check binaries
echo ""
echo "=== Build Complete ==="
ls -lh bin/
echo ""

# Display next steps
cat <<EOF
Next Steps:
-----------

1. Test the server:
   ./bin/opsen-server -port 8080 -db test.db

2. Test the client (in another terminal):
   ./bin/opsen-client -server http://localhost:8080 -window 5 -interval 10

3. Check server health:
   curl http://localhost:8080/health

4. Install systemd services (optional):
   sudo make install

For more information, see README.md
EOF
