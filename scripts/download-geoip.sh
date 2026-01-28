#!/bin/bash

# Download GeoLite2-City MaxMind database from Opsen S3 mirror
#
# Usage:
#   ./scripts/download-geoip.sh [TARGET_PATH]
#
# The database will be saved to the repository root as GeoLite2-City.mmdb
# No license key required - downloads directly from Opsen's public S3 bucket.

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Get the directory where this script is located
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
TARGET_FILE="${1:-$REPO_ROOT/GeoLite2-City.mmdb}"

# Download URL (Opsen S3 mirror)
DOWNLOAD_URL="https://cyqle-opsen.s3.us-east-2.amazonaws.com/GeoLite2-City.mmdb"

echo -e "${GREEN}Downloading GeoLite2-City database...${NC}"
echo "Source: $DOWNLOAD_URL"
echo "Target: $TARGET_FILE"
echo ""

# Check if file already exists
if [ -f "$TARGET_FILE" ]; then
    FILE_SIZE=$(du -h "$TARGET_FILE" | cut -f1)

    echo -e "${YELLOW}Existing database found:${NC}"
    echo "  Location: $TARGET_FILE"
    echo "  Size: $FILE_SIZE"
    echo ""

    read -p "Do you want to re-download and replace it? (y/N): " -n 1 -r
    echo
    if [[ ! $REPLY =~ ^[Yy]$ ]]; then
        echo "Download cancelled. Using existing database."
        exit 0
    fi
    echo ""
fi

# Download database
echo "Downloading..."
if ! curl -f -L "$DOWNLOAD_URL" -o "$TARGET_FILE" --progress-bar; then
    echo ""
    echo -e "${RED}ERROR: Download failed${NC}"
    echo ""
    echo "Please check:"
    echo "  - Network connection"
    echo "  - URL accessibility: $DOWNLOAD_URL"
    exit 1
fi

# Verify downloaded file
if [ ! -f "$TARGET_FILE" ] || [ ! -s "$TARGET_FILE" ]; then
    echo -e "${RED}ERROR: Downloaded file is empty or missing${NC}"
    exit 1
fi

# Get file size
FILE_SIZE=$(du -h "$TARGET_FILE" | cut -f1)

echo ""
echo -e "${GREEN}✓ Successfully downloaded GeoLite2-City database${NC}"
echo "  Location: $TARGET_FILE"
echo "  Size: $FILE_SIZE"
echo ""
echo -e "${YELLOW}Note:${NC} This database should be updated monthly for best accuracy"
echo ""
echo "To use this database, configure your YAML files:"
echo "  Server: geoip_db_path: $TARGET_FILE"
echo "  Client: geoip_db_path: $TARGET_FILE"
