#!/bin/bash

# Download GeoLite2-City MaxMind database
#
# Usage:
#   ./scripts/download-geoip.sh [LICENSE_KEY]
#   MAXMIND_LICENSE_KEY=your_key ./scripts/download-geoip.sh
#
# Get your free license key from: https://www.maxmind.com/en/geolite2/signup
#
# The database will be saved to the repository root as GeoLite2-City.mmdb

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Get the directory where this script is located
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
TARGET_DIR="$REPO_ROOT"
TARGET_FILE="$TARGET_DIR/GeoLite2-City.mmdb"

# MaxMind license key (can be provided as argument or environment variable)
LICENSE_KEY="${1:-$MAXMIND_LICENSE_KEY}"

if [ -z "$LICENSE_KEY" ]; then
    echo -e "${RED}ERROR: MaxMind license key is required${NC}"
    echo ""
    echo "Usage:"
    echo "  $0 YOUR_LICENSE_KEY"
    echo "  or"
    echo "  MAXMIND_LICENSE_KEY=YOUR_LICENSE_KEY $0"
    echo ""
    echo "To get a free license key:"
    echo "  1. Sign up at: https://www.maxmind.com/en/geolite2/signup"
    echo "  2. Verify your email"
    echo "  3. Generate a license key at: https://www.maxmind.com/en/accounts/current/license-key"
    echo ""
    exit 1
fi

echo -e "${GREEN}Downloading GeoLite2-City database...${NC}"
echo "Target: $TARGET_FILE"
echo ""

# Check if file already exists
if [ -f "$TARGET_FILE" ]; then
    FILE_SIZE=$(du -h "$TARGET_FILE" | cut -f1)
    FILE_AGE=$(find "$TARGET_FILE" -mtime +30 2>/dev/null && echo "old" || echo "recent")

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

# Create temporary directory
TMP_DIR=$(mktemp -d)
trap "rm -rf $TMP_DIR" EXIT

# Download the database (tar.gz format)
DOWNLOAD_URL="https://download.maxmind.com/app/geoip_download?edition_id=GeoLite2-City&license_key=${LICENSE_KEY}&suffix=tar.gz"
TARBALL="$TMP_DIR/GeoLite2-City.tar.gz"
ERROR_FILE="$TMP_DIR/curl_error.txt"

echo "Downloading from MaxMind..."

# Attempt download with verbose error output
set +e  # Temporarily disable exit on error
HTTP_CODE=$(curl -w "%{http_code}" -L "$DOWNLOAD_URL" -o "$TARBALL" -s -S 2>"$ERROR_FILE")
CURL_EXIT=$?
set -e

# Check if download was successful
if [ $CURL_EXIT -ne 0 ] || [ "$HTTP_CODE" != "200" ]; then
    echo -e "${RED}ERROR: Download failed (HTTP $HTTP_CODE)${NC}"
    echo ""

    # Show curl error details if available
    if [ -s "$ERROR_FILE" ]; then
        echo "Error details:"
        cat "$ERROR_FILE"
        echo ""
    fi

    # Provide specific guidance based on HTTP code
    if [ "$HTTP_CODE" = "401" ]; then
        echo -e "${YELLOW}License key authentication failed (HTTP 401)${NC}"
        echo ""
        echo "Troubleshooting steps:"
        echo "  1. Verify your license key is correct (no extra spaces or characters)"
        echo "  2. Ensure the license key is active at:"
        echo "     https://www.maxmind.com/en/accounts/current/license-key"
        echo "  3. If you just created the key, wait a few minutes and try again"
        echo "  4. Make sure your MaxMind account is verified (check your email)"
        echo ""
        echo "If you don't have a license key yet:"
        echo "  1. Sign up at: https://www.maxmind.com/en/geolite2/signup"
        echo "  2. Verify your email"
        echo "  3. Generate a license key at the URL above"
    elif [ "$HTTP_CODE" = "404" ]; then
        echo "The GeoLite2-City database was not found."
        echo "This may indicate an issue with MaxMind's API."
    else
        echo "Possible reasons:"
        echo "  - Network connection issues"
        echo "  - MaxMind API is temporarily down"
        echo "  - Invalid license key"
    fi

    exit 1
fi

# Check if download was successful
if [ ! -f "$TARBALL" ] || [ ! -s "$TARBALL" ]; then
    echo -e "${RED}ERROR: Downloaded file is empty or missing${NC}"
    exit 1
fi

echo "Extracting database..."
tar -xzf "$TARBALL" -C "$TMP_DIR"

# Find the .mmdb file (it's in a dated subdirectory)
MMDB_FILE=$(find "$TMP_DIR" -name "GeoLite2-City.mmdb" | head -n 1)

if [ -z "$MMDB_FILE" ] || [ ! -f "$MMDB_FILE" ]; then
    echo -e "${RED}ERROR: Could not find GeoLite2-City.mmdb in downloaded archive${NC}"
    exit 1
fi

# Copy to target location
cp "$MMDB_FILE" "$TARGET_FILE"

# Get file size
FILE_SIZE=$(du -h "$TARGET_FILE" | cut -f1)

echo ""
echo -e "${GREEN}✓ Successfully downloaded GeoLite2-City database${NC}"
echo "  Location: $TARGET_FILE"
echo "  Size: $FILE_SIZE"
echo ""
echo -e "${YELLOW}Note:${NC} This database should be updated monthly for best results"
echo "      MaxMind updates GeoLite2 on the first Tuesday of each month"
echo ""
echo "To use this database, set the following in your config files:"
echo "  Server: geoip_db_path: $TARGET_FILE"
echo "  Client: geoip_db_path: $TARGET_FILE"
