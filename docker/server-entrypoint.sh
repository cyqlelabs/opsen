#!/bin/sh
set -e

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
