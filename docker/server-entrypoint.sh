#!/bin/sh
set -e

# MaxMind GeoIP Database Auto-Download
GEOIP_DB="/opt/geoip/GeoLite2-City.mmdb"
if [ ! -f "$GEOIP_DB" ] && [ -n "$MAXMIND_LICENSE_KEY" ] && [ -n "$MAXMIND_ACCOUNT_ID" ]; then
    echo "Downloading MaxMind GeoLite2-City database..."
    DOWNLOAD_URL="https://download.maxmind.com/app/geoip_download?edition_id=GeoLite2-City&license_key=${MAXMIND_LICENSE_KEY}&suffix=tar.gz"

    if curl -sSL "$DOWNLOAD_URL" -o /tmp/geoip.tar.gz; then
        tar -xzf /tmp/geoip.tar.gz -C /tmp
        find /tmp -name "GeoLite2-City.mmdb" -exec mv {} "$GEOIP_DB" \;
        rm -rf /tmp/geoip.tar.gz /tmp/GeoLite2-City_*
        echo "MaxMind database downloaded successfully"
    else
        echo "Warning: Failed to download MaxMind database. Falling back to ipapi.co API"
    fi
elif [ -f "$GEOIP_DB" ]; then
    echo "MaxMind GeoLite2-City database already present at $GEOIP_DB"
else
    echo "No MaxMind credentials provided. Server will fall back to ipapi.co API for geolocation"
fi

# Build CLI flags from environment variables
# Note: Only basic flags are supported at CLI level; use config file for advanced options
FLAGS=""

# Basic configuration
[ -n "$OPSEN_SERVER_PORT" ] && FLAGS="$FLAGS -port $OPSEN_SERVER_PORT"
[ -n "$OPSEN_SERVER_HOST" ] && FLAGS="$FLAGS -host $OPSEN_SERVER_HOST"
[ -n "$OPSEN_SERVER_DATABASE" ] && FLAGS="$FLAGS -db $OPSEN_SERVER_DATABASE"

# Stale client timeout
[ -n "$OPSEN_SERVER_STALE_TIMEOUT" ] && FLAGS="$FLAGS -stale $OPSEN_SERVER_STALE_TIMEOUT"

# Cleanup interval
[ -n "$OPSEN_SERVER_CLEANUP_INTERVAL" ] && FLAGS="$FLAGS -cleanup-interval $OPSEN_SERVER_CLEANUP_INTERVAL"

# Execute server with optional config file
CONFIG_FILE="${OPSEN_CONFIG_FILE:-/etc/opsen/config.yml}"
if [ -f "$CONFIG_FILE" ]; then
    FLAGS="-config $CONFIG_FILE $FLAGS"
    echo "Using config file: $CONFIG_FILE"
fi

echo "Starting Opsen Server with flags: $FLAGS"
exec /usr/local/bin/opsen-server $FLAGS "$@"
