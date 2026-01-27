#!/bin/sh
set -e

# Build CLI flags from environment variables
# Note: Only basic flags are supported at CLI level; use config file for advanced options
FLAGS=""

# Basic configuration
[ -n "$OPSEN_CLIENT_SERVER_URL" ] && FLAGS="$FLAGS -server $OPSEN_CLIENT_SERVER_URL"
[ -n "$OPSEN_CLIENT_CLIENT_ID" ] && FLAGS="$FLAGS -id $OPSEN_CLIENT_CLIENT_ID"

# Metrics collection
[ -n "$OPSEN_CLIENT_WINDOW_MINUTES" ] && FLAGS="$FLAGS -window $OPSEN_CLIENT_WINDOW_MINUTES"
[ -n "$OPSEN_CLIENT_INTERVAL_SECONDS" ] && FLAGS="$FLAGS -interval $OPSEN_CLIENT_INTERVAL_SECONDS"
[ -n "$OPSEN_CLIENT_DISK_PATH" ] && FLAGS="$FLAGS -disk $OPSEN_CLIENT_DISK_PATH"

# Execute client with optional config file
CONFIG_FILE="${OPSEN_CONFIG_FILE:-/etc/opsen/config.yml}"
if [ -f "$CONFIG_FILE" ]; then
    FLAGS="-config $CONFIG_FILE $FLAGS"
    echo "Using config file: $CONFIG_FILE"
fi

echo "Starting Opsen Client with flags: $FLAGS"
exec /usr/local/bin/opsen-client $FLAGS "$@"
