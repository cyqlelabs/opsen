# Configuration Files

This directory contains example configuration files for the Opsen Load Balancer.

## Files

- **server.example.yml** - Example server configuration
- **client.example.yml** - Example client configuration
- **opsen-server.service** - Systemd service for server
- **opsen-client.service** - Systemd service for client
- **Caddyfile.example** - Caddy integration example

## Usage

### Development / Testing

```bash
# Copy example configs
cp configs/server.example.yml server.yml
cp configs/client.example.yml client.yml

# Edit as needed
nano server.yml
nano client.yml

# Run with config files
./bin/opsen-server -config server.yml
./bin/opsen-client -config client.yml
```

### Production (systemd)

After running `make install`, configuration files are installed to:
- `/etc/opsen/server.yml`
- `/etc/opsen/client.yml`

Edit these files:
```bash
sudo nano /etc/opsen/server.yml
sudo nano /etc/opsen/client.yml
```

Then restart services:
```bash
sudo systemctl restart opsen-server
sudo systemctl restart opsen-client
```

## Configuration Options

**`server.example.yml` and `client.example.yml` are the authoritative, fully-commented references** for every option. The snippets below cover the core essentials; see the example files for the complete set.

### Server (server.yml)

```yaml
# Server listening port
port: 8080

# Host to bind to (0.0.0.0 for all interfaces)
host: 0.0.0.0

# SQLite database file path
database: /opt/opsen/opsen.db

# Client stale timeout in minutes
stale_minutes: 5

# Log level (debug, info, warn, error)
log_level: info
```

Additional option groups documented in `server.example.yml`:

- **Tiers** – `tiers` resource specs (vCPU, memory, storage, optional GPU/VRAM)
- **Tier detection** – `tier_field_name`, `tier_header`, `default_tier` (`""` = passthrough)
- **Reverse proxy** – `proxy_endpoints`, `proxy_sse_flush_interval_ms`, `endpoints` (path-based routing)
- **Sticky sessions** – `sticky_header`, `sticky_by_ip`, `sticky_affinity_enabled`, `pending_allocation_timeout_seconds`
- **Health checks** – probe type/path, intervals, thresholds, timeout
- **Timeouts** – `request_timeout_seconds`, `idle_timeout_seconds`, `read_header_timeout_seconds`
- **Security** – API keys, IP whitelist (CIDR), rate limiting, request size limits, `disable_security_headers`, TLS, CORS
- **Database** – `db_max_open_conns`, `db_max_idle_conns`

### Client (client.yml)

```yaml
# Load balancer server URL
server_url: http://lb.cyqle.io:8080

# Unique client identifier (auto-generated if not set)
client_id: ""

# Hostname override (uses system hostname if not set)
hostname: ""

# Time window for averaging metrics in minutes
window_minutes: 15

# Report interval in seconds
report_interval_seconds: 60

# Disk path to monitor
disk_path: /

# Log level (debug, info, warn, error)
log_level: info
```

## Command-Line Override

You can override any YAML setting with command-line flags:

```bash
# Override port from command line
./bin/opsen-server -config server.yml -port 9090

# Override server URL from command line
./bin/opsen-client -config client.yml -server http://localhost:8080
```

**Priority:** CLI flags > YAML file > defaults

## Validation

The config loader will:
- Use defaults if no config file is specified
- Return defaults if config file doesn't exist
- Fail with error if config file exists but has invalid YAML
- Allow CLI flags to override any YAML setting

## Examples

### Multi-Region Setup

**Server** (central coordinator):
```yaml
port: 8080
host: 0.0.0.0
database: /data/opsen.db
stale_minutes: 10  # Higher timeout for multi-region
log_level: info
```

**Client** (US-East backend):
```yaml
server_url: https://lb.cyqle.io:8080
window_minutes: 15
report_interval_seconds: 60
disk_path: /mnt/sessions  # Custom mount point
log_level: info
```

**Client** (EU-West backend):
```yaml
server_url: https://lb.cyqle.io:8080
window_minutes: 15
report_interval_seconds: 60
disk_path: /mnt/sessions
log_level: info
```

### Development Setup

**Server** (localhost):
```yaml
port: 8080
host: 127.0.0.1  # Localhost only
database: test.db
stale_minutes: 2  # Faster stale detection
log_level: debug
```

**Client** (localhost):
```yaml
server_url: http://localhost:8080
window_minutes: 5   # Shorter window for testing
report_interval_seconds: 10  # Faster reporting
disk_path: /
log_level: debug
```
