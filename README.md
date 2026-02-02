<div align="center">

<h1 style="font-size: 3.5em;">Opsen</h1>

### Resource-Aware Load Balancer for Intelligent Traffic Routing

[![Tests](https://github.com/cyqlelabs/opsen/actions/workflows/test.yml/badge.svg)](https://github.com/cyqlelabs/opsen/actions/workflows/test.yml)
[![codecov](https://codecov.io/gh/cyqlelabs/opsen/branch/main/graph/badge.svg)](https://codecov.io/gh/cyqlelabs/opsen)
[![Go Version](https://img.shields.io/github/go-mod/go-version/cyqlelabs/opsen)](https://github.com/cyqlelabs/opsen/blob/main/go.mod)
[![License](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](LICENSE)

</div>

Production-ready load balancer that routes traffic based on real-time CPU, RAM, disk, and GPU availability—not round-robin guesswork. Routes to the best backend based on actual resource metrics, geography, and configurable tier requirements.

**Key Features:**

- 🎯 **Smart Routing** - Resource-aware (CPU, RAM, disk, GPU) + geography-based routing
- 📦 **Simple Deployment** - Two binaries (server + client), Docker support, YAML config, systemd integration
- 🛡️ **Production Security** - API keys, IP whitelisting, rate limiting, TLS, request size limits, timeouts
- 🔄 **High Reliability** - Circuit breaker, exponential backoff, panic recovery, graceful shutdown
- 🎯 **Sticky Sessions** - Configurable session affinity via custom headers or client IP
- 🚀 **Built-in Reverse Proxy** - SSE/streaming support, automatic tier detection, path preservation
- ⚡ **Performance** - <15µs routing (100 backends), in-memory decisions, connection pooling

## Quick Installation

### One-Line Install (Linux/macOS)

```bash
curl -sSL https://raw.githubusercontent.com/cyqlelabs/opsen/main/scripts/install.sh | bash
```

This automatically detects your platform and installs pre-built binaries to `/usr/local/bin`.

### Docker

**Pre-built images are available** from GitHub Container Registry (no authentication required):

- `ghcr.io/cyqlelabs/opsen-server:latest`
- `ghcr.io/cyqlelabs/opsen-client:latest`

**Quick Start with Docker Compose (Recommended for testing):**

```bash
git clone https://github.com/cyqlelabs/opsen.git
cd opsen
docker compose up -d

# Verify it's running
curl http://localhost:8080/health
```

This starts a server and two example clients. The server runs on `localhost:8080`.

**Production Deployment:**

```bash
# Build images
docker compose build

# Start with custom environment variables
OPSEN_SERVER_PORT=9000 docker compose up -d

# Or use custom config files
docker compose -f docker-compose.production.yml up -d
```

**Individual Containers (using pre-built images):**

```bash
# Server
docker run -d \
  -p 8080:8080 \
  -v opsen-data:/data \
  -e OPSEN_SERVER_PORT=8080 \
  -e OPSEN_SERVER_DATABASE=/data/opsen.db \
  ghcr.io/cyqlelabs/opsen-server:latest

# Client
docker run -d \
  -v /proc:/host/proc:ro \
  -v /sys:/host/sys:ro \
  -e OPSEN_CLIENT_SERVER_URL=http://opsen-server:8080 \
  -e OPSEN_CLIENT_WINDOW_MINUTES=15 \
  ghcr.io/cyqlelabs/opsen-client:latest
```

**Build from source (optional):**

```bash
# Server
docker build -f Dockerfile.server -t opsen-server .

# Client
docker build -f Dockerfile.client -t opsen-client .
```

**Environment Variables:**

Server:

- `OPSEN_SERVER_PORT` - Port to listen on (default: 8080)
- `OPSEN_SERVER_HOST` - Host to bind to (default: 0.0.0.0)
- `OPSEN_SERVER_DATABASE` - Database path (default: /data/opsen.db)
- `OPSEN_SERVER_STALE_TIMEOUT` - Client stale timeout in minutes (default: 5)

Client:

- `OPSEN_CLIENT_SERVER_URL` - Load balancer server URL (required)
- `OPSEN_CLIENT_WINDOW_MINUTES` - Metrics averaging window (default: 15)
- `OPSEN_CLIENT_INTERVAL_SECONDS` - Report interval (default: 60)
- `OPSEN_CLIENT_DISK_PATH` - Disk path to monitor (default: /)

For advanced options (TLS, auth, logging, sticky sessions, etc.), mount a config file:

```bash
docker run -v ./server.yml:/etc/opsen/config.yml:ro ghcr.io/cyqlelabs/opsen-server:latest
```

**GPU Support:**

For GPU monitoring, use NVIDIA Container Runtime:

```bash
docker run --gpus all -e OPSEN_CLIENT_SERVER_URL=http://server:8080 ghcr.io/cyqlelabs/opsen-client:latest
```

**Image Sizes:**

- Server: ~33MB (Alpine-based)
- Client: ~100MB (Debian-based, required for GPU support)

See [docker-compose.yml](docker-compose.yml) for complete examples.

### Manual Installation

**Download pre-built binaries** from [GitHub Releases](https://github.com/cyqlelabs/opsen/releases/latest):

```bash
# Linux AMD64
wget https://github.com/cyqlelabs/opsen/releases/latest/download/opsen-server_VERSION_linux_amd64.tar.gz
wget https://github.com/cyqlelabs/opsen/releases/latest/download/opsen-client_VERSION_linux_amd64.tar.gz
tar xzf opsen-server_VERSION_linux_amd64.tar.gz
tar xzf opsen-client_VERSION_linux_amd64.tar.gz
sudo mv opsen-server opsen-client /usr/local/bin/
```

**Build from source** (requires Go 1.23+):

```bash
git clone https://github.com/cyqlelabs/opsen.git
cd opsen
make all
sudo make install
```

### Verify Installation

```bash
opsen-server -version
opsen-client -version
```

## Table of Contents

- [Quick Installation](#quick-installation)
  - [One-Line Install](#one-line-install-linuxmacos)
  - [Docker](#docker)
  - [Manual Installation](#manual-installation)
- [Architecture](#architecture)
- [Building from Source](#building-from-source)
- [Scripts](#scripts)
  - [`scripts/download-geoip.sh`](#scriptsdownload-geoipsh)
  - [`scripts/generate-tls-cert.sh`](#scriptsgenerate-tls-certsh)
- [GeoIP Setup (Optional)](#geoip-setup-optional)
- [Usage](#usage)
  - [Server](#server)
  - [Client](#client)
- [API Endpoints](#api-endpoints)
  - [POST /register](#post-register)
  - [POST /stats](#post-stats)
  - [POST /route](#post-route)
  - [GET /health](#get-health)
  - [GET /clients](#get-clients)
- [Routing Algorithm](#routing-algorithm)
  - [Sticky Sessions (Optional)](#sticky-sessions-optional)
  - [Standard Routing (No Sticky Header)](#standard-routing-no-sticky-header)
  - [Resource Availability & Race Condition Protection](#resource-availability--race-condition-protection)
- [Systemd Integration](#systemd-integration)
- [Application Integration](#application-integration)
  - [Option 1: Built-in Reverse Proxy (Recommended)](#option-1-built-in-reverse-proxy-recommended)
  - [Option 2: Manual Backend Selection](#option-2-manual-backend-selection)
- [Database Schema](#database-schema)
  - [Table: `clients`](#table-clients)
  - [Table: `stats`](#table-stats)
  - [Table: `sticky_assignments`](#table-sticky_assignments)
- [Monitoring](#monitoring)
  - [Health Check](#health-check)
  - [List Clients](#list-clients)
  - [Test Routing](#test-routing)
  - [Logs](#logs)
- [Performance](#performance)
- [Health Checks & Latency Tracking](#health-checks--latency-tracking)
- [Security Features](#security-features)
- [Reliability Features](#reliability-features)
- [License](#license)

## Architecture

**Server** (`opsen-server`) - Central routing coordinator that receives metrics and makes routing decisions based on resource availability, geography, and tier requirements.

**Client** (`opsen-client`) - Runs on each backend, collects CPU/RAM/disk/GPU metrics (15min avg), reports to server every 60s. Supports NVIDIA GPUs via NVML (gracefully disabled if absent). Automatically downloads and uses MaxMind GeoIP database for location detection.

**Tiers** - Fully customizable resource specifications (vCPU, memory, storage, optional GPU + VRAM). Define tiers matching your infrastructure and pricing model.

| Tier          | vCPU | Memory | Storage | GPU | GPU Memory |
| ------------- | ---- | ------ | ------- | --- | ---------- |
| small         | 1    | 1 GB   | 5 GB    | -   | -          |
| medium        | 2    | 4 GB   | 20 GB   | -   | -          |
| large         | 4    | 8 GB   | 30 GB   | -   | -          |
| gpu-inference | 8    | 32 GB  | 100 GB  | 1   | 16 GB      |
| gpu-training  | 16   | 64 GB  | 500 GB  | 2   | 48 GB      |

## Building from Source

```bash
# Build both client and server
make all

# Build server only
make build-server

# Build client only
make build-client

# Install binaries and systemd services
sudo make install

# Download Go dependencies
make deps
```

Binaries are output to `bin/`:

- `bin/opsen-server` - Load balancer server
- `bin/opsen-client` - Metrics collector client

## Scripts

The repository includes helpful scripts for common setup tasks:

### `scripts/download-geoip.sh`

Downloads the MaxMind GeoLite2-City database for geographic routing from Opsen's S3 mirror.

```bash
./scripts/download-geoip.sh [TARGET_PATH]
```

**Note:** The client automatically downloads this database on first run. This script is only needed for:

- Manual server-side GeoIP setup (optional, for routing request geolocation)
- Updating the database (recommended monthly)
- Custom installation paths

Source: `https://cyqle-opsen.s3.us-east-2.amazonaws.com/GeoLite2-City.mmdb` (no authentication required)

### `scripts/generate-tls-cert.sh`

Generates self-signed TLS certificates with Subject Alternative Names (SANs) for development/testing.

```bash
./scripts/generate-tls-cert.sh [cert_dir] [domain] [days]

# Examples:
./scripts/generate-tls-cert.sh                          # Default: ./certs, lb.cyqle.local, 365 days
./scripts/generate-tls-cert.sh ./ssl lb.example.com 730 # Custom directory, domain, and validity
```

Outputs `server.crt` and `server.key` ready for use in your server configuration.

## GeoIP Setup (Automatic)

**Client geolocation is automatic** - the client downloads the GeoIP database on first run to `./GeoLite2-City.mmdb`.

**Server geolocation is optional** - only needed if you want distance calculation from routing request origin:

```bash
# Download for server (optional - only for multi-datacenter routing)
./scripts/download-geoip.sh

# Configure in server YAML
geoip_db_path: ./GeoLite2-City.mmdb
```

**When server GeoIP is needed:**

- ✓ Multi-datacenter deployments with routing requests from different regions
- ✗ Single datacenter (backends already have location via auto-download)

Update monthly (first Tuesday) for best accuracy.

## Usage

### Server

Create `server.yml` (all settings shown with defaults):

```yaml
# Basic
port: 8080
host: 0.0.0.0
database: /opt/opsen/opsen.db
stale_minutes: 5
log_level: info # debug, info, warn, error, fatal
json_logging: false

# Security
server_key: "" # Client auth (opsen-client must match)
api_keys: [] # Additional API keys for other integrations
whitelisted_ips: [] # CIDR ranges (empty = allow all)
rate_limit_per_minute: 60 # Requests per minute per IP (0 = disabled)
rate_limit_burst: 120 # Burst capacity
max_request_body_bytes: 10485760 # 10MB
request_timeout_seconds: 30
read_header_timeout_seconds: 10 # Slowloris protection
disable_security_headers: false # Disable X-Frame-Options, X-XSS-Protection, etc. (e.g., when using WAF)

# TLS
tls_cert_file: "" # Empty = HTTP only
tls_key_file: ""
tls_insecure_skip_verify: false # For self-signed certs (dev only!)

# CORS
enable_cors: false
cors_allowed_origins: []

# Reverse Proxy
proxy_endpoints: [] # e.g., ["/api", "/browse"]
proxy_sse_flush_interval_ms: -1 # -1=immediate (SSE), 0=disabled, >0=interval

# Geolocation
geoip_db_path: "" # Path to GeoLite2-City.mmdb

# Sticky Sessions
sticky_header: "" # e.g., "X-Session-ID", "X-User-ID" (empty = disabled)
sticky_by_ip: false # Use client IP when header not present
sticky_affinity_enabled: true
pending_allocation_timeout_seconds: 120

# Tier Detection
tier_field_name: "tier" # JSON body field
tier_header: "X-Tier" # HTTP header

# Database
db_max_open_conns: 25
db_max_idle_conns: 5
db_conn_max_lifetime: 300
cleanup_interval_seconds: 60
shutdown_timeout_seconds: 30

# Tiers (customize to your infrastructure)
tiers:
  - name: small
    vcpu: 1
    memory_gb: 1.0
    storage_gb: 5
  - name: medium
    vcpu: 2
    memory_gb: 4.0
    storage_gb: 20
  - name: gpu-inference # GPU example
    vcpu: 8
    memory_gb: 32.0
    storage_gb: 100
    gpu: 1
    gpu_memory_gb: 16.0
```

**Run:**

```bash
./bin/opsen-server -config server.yml

# CLI flags override YAML
./bin/opsen-server -config server.yml -port 9000 -stale 10
```

### Client

Create `client.yml`:

```yaml
# Basic
server_url: http://lb.example.com:8080
server_key: "" # Must match server's server_key (if set)
endpoint_url: "" # Override (default: http://{local_ip}:11000)

# Metrics
window_minutes: 15 # Averaging window
report_interval_seconds: 60
disk_path: /

# Identity
client_id: "" # Auto-generated UUID if empty
hostname: "" # Uses system hostname if empty

# Geolocation (auto-downloads GeoIP database on first run)
skip_geolocation: false # Skip entirely (fastest)
geoip_db_path: "" # Auto-downloads to ./GeoLite2-City.mmdb if not set

# Logging & TLS
log_level: info
insecure_tls: false # Dev only - skip cert verification
```

**Important: `endpoint_url` Configuration**

The `endpoint_url` defines where this backend accepts traffic. The load balancer uses this URL to route requests and perform health checks.

- **Format**: `http://hostname:port` or `https://hostname:port`
- **Must be accessible** from the load balancer server
- **Run one client per backend**, each with a unique `endpoint_url`

**Example: Multiple Backends**

```yaml
# backend-1.yml
endpoint_url: http://backend-1.internal:8000

# backend-2.yml
endpoint_url: http://backend-2.internal:8000

# backend-3.yml
endpoint_url: http://backend-3.internal:9000
```

Each client monitors its own resources and reports to the same load balancer server.

**Run:**

```bash
./bin/opsen-client -config client.yml

# CLI flags override YAML
./bin/opsen-client -config client.yml -server http://lb.example.com:9000 -window 20
```

## API Endpoints

All endpoints except `/health` support API key auth (`X-API-Key` header). Rate limited per IP (60/min, burst 120 by default; can be disabled). Security headers included automatically.

### POST /register

Register backend. Required before stats reporting or routing.

**Request:** `client_id`, `hostname`, `public_ip`, `local_ip`, `latitude`, `longitude`, `country`, `city`, `total_cpu`, `total_memory_gb`, `total_storage_gb`, optional: `total_gpus`, `gpu_models`, `endpoint_url`

**Response:** `{"status": "registered"}`

### POST /stats

Report metrics (every 60s default).

**Request:** `client_id`, `hostname`, `timestamp`, `cpu_cores`, `cpu_usage_avg` (per-core array), `memory_*`, `disk_*`, optional: `gpus[]` (device*id, name, utilization_pct, memory*\*, temperature_c, power_draw_w)

**Response:** `{"status": "ok"}`

### POST /route

Get routing decision.

**Request:** `tier`, `client_ip`, optional: `client_lat`, `client_lon`
**Headers:** Optional sticky session header (e.g., `X-Session-ID`)

**Response:** `client_id`, `endpoint`, `hostname`, `distance_km`

### GET /health

Server health (no auth required).

**Response:** `status`, `timestamp`, `total_clients`, `active_clients`

### GET /clients

List backends with current metrics.

**Response:** Array of: `client_id`, `hostname`, `endpoint`, `location`, `cpu_*`, `memory_*`, `disk_*`, `gpus[]`, `last_seen`, `is_active`

## Routing Algorithm

The server uses a **weighted scoring algorithm** with sticky session support to select the optimal backend:

### Sticky Sessions (Optional)

The load balancer supports session affinity via two methods:

- **Header-based stickiness** (`sticky_header`): Uses a custom HTTP header as the sticky identifier
- **IP-based stickiness** (`sticky_by_ip`): Uses client IP address as the sticky identifier

When enabled, the load balancer provides session affinity:

1. **First request**: Standard routing algorithm selects best server, creates assignment `(sticky_id, tier) → server`
2. **Subsequent requests**: Same `sticky_id + tier` always routes to the assigned server (if healthy)
3. **Affinity mode** (`sticky_affinity_enabled: true`): Different tiers from same `sticky_id` prefer the same server
4. **Automatic fallback**: If assigned server is unavailable or overloaded, selects a new server

**Configuration options:**

- `sticky_header: "X-Session-ID"` + `sticky_by_ip: false` - Header-based only (authenticated users)
- `sticky_header: ""` + `sticky_by_ip: true` - IP-based only (anonymous users, no session tracking)
- Both enabled - Header takes precedence; IP used as fallback when header not present

**Use cases:**

- `X-Session-ID`: Per-session stickiness (different sessions can go to different servers)
- `X-User-ID`: All sessions from same user prefer same server (when affinity enabled)
- `X-Device-ID`: All sessions from same device prefer same server
- IP-based: Anonymous users without session IDs (e.g., public APIs, CDN origins)

### Standard Routing (No Sticky Header)

The server uses a **weighted scoring algorithm** to select the optimal backend:

```
score = distance_km + (avg_cpu_usage_pct * 1.0) + (memory_usage_pct * 1.0) + (gpu_usage_pct * 1.5) + latency_ms
```

Where:

- `avg_cpu_usage_pct` = Average usage of the **N least-loaded cores** (representing what a new session would experience)
- `memory_usage_pct` = Total memory usage percentage (used/total \* 100)
- `gpu_usage_pct` = Average GPU utilization across all GPUs (if tier requires GPUs)
- `latency_ms` = Round-trip latency to backend from health checks (EWMA smoothed, 0 if health checks disabled)
- GPU gets **higher weight (1.5x)** as GPU workloads are more sensitive to resource contention

Lower scores are better. The algorithm:

1. **Filters** clients with insufficient resources:
   - CPU: At least N cores with <80% average usage (accounting for pending allocations)
   - Memory: At least N GB available (accounting for pending allocations)
   - Disk: At least N GB available (accounting for pending allocations)
   - GPU: At least N GPUs available with sufficient VRAM (if tier requires GPUs)

2. **Calculates distance** from end user to backend (Haversine formula)

3. **Computes score** combining distance and resource utilization:
   - **CPU scoring**: Calculates the average of the N least-loaded cores (sorted by usage)
   - **Memory scoring**: Uses total memory usage percentage (not accounting for pending allocations)
   - **Note**: Pending allocations affect filtering (step 1) but not scoring (step 3)

4. **Selects** the client with the lowest score

5. **Reserves resources** immediately to prevent race conditions

### Resource Availability & Race Condition Protection

**Pending allocations** prevent concurrent requests from all selecting the same overloaded server:

- When a server is selected, resources are immediately reserved in-memory
- Subsequent requests see reduced available capacity (actual + pending allocations)
- Reservations expire after `pending_allocation_timeout_seconds` (default: 120s)
- Duplicate allocations for same `sticky_id + tier` are automatically deduplicated

**CPU Availability Details:**

- A CPU core is considered "available" if its average usage over the time window is <80%
- For scoring, the algorithm selects the N **least-loaded cores** and averages their usage
- This represents the actual CPU resources a new session would consume

**Example:** For a `medium` tier (2 vCPU, 4GB RAM, 20GB storage):

- Server has 8GB free RAM, 0 pending allocations → Available: 8GB
- Request A reserves 4GB → Available: 4GB (for concurrent requests)
- Request B reserves 4GB → Available: 0GB
- Request C finds different server (race condition prevented!)

Backend must have:

- ≥2 cores with <80% usage (minus pending CPU allocations)
- ≥4GB free memory (minus pending memory allocations)
- ≥20GB free disk space (minus pending disk allocations)

## Systemd Integration

After running `make install`, manage services with systemd:

```bash
# Server
sudo systemctl start opsen-server
sudo systemctl enable opsen-server
sudo systemctl status opsen-server
journalctl -u opsen-server -f

# Client (on each backend)
sudo systemctl start opsen-client
sudo systemctl enable opsen-client
sudo systemctl status opsen-client
journalctl -u opsen-client -f
```

Edit service files at:

- `/etc/systemd/system/opsen-server.service`
- `/etc/systemd/system/opsen-client.service`

After changes: `sudo systemctl daemon-reload`

## Application Integration

### Option 1: Built-in Reverse Proxy (Recommended)

Configure paths to proxy, point frontend to load balancer. Zero code changes needed.

**Server:**

```yaml
proxy_endpoints: ["/api", "/v1"] # Or "/*" for all paths
sticky_header: "X-Session-ID" # Optional
proxy_sse_flush_interval_ms: -1 # SSE support: -1=immediate, 0=disabled, >0=interval
```

**Frontend:**

```javascript
// Change base URL only - all existing API calls work unchanged
const API_BASE = "https://lb.example.com:8080"; // Was: https://backend1.example.com

fetch(`${API_BASE}/api/users`, {
  headers: {
    "X-Session-ID": sessionId, // Optional: sticky sessions
  },
  body: JSON.stringify({ tier: "medium", ...data }), // Tier auto-detected
});

// SSE/streaming works automatically
const eventSource = new EventSource(`${API_BASE}/events/stream`);
```

**Tier Detection (priority order):**

1. JSON body field (`tier_field_name`, default: "tier")
2. Query parameter (`?tier=medium`)
3. HTTP header (`tier_header`, default: "X-Tier")
4. Default: "lite"

Customize field names in server.yml:

```yaml
tier_field_name: "subscription_level"
tier_header: "X-Subscription-Level"
```

**Benefits:** Path preservation, SSE support, sticky sessions, no routing logic needed

---

### Option 2: Manual Backend Selection

Call `/route` endpoint from your app, forward request to returned `endpoint`.

```javascript
async function handleRequest(req, res) {
  const { endpoint } = await fetch("http://lb.example.com:8080/route", {
    method: "POST",
    body: JSON.stringify({ tier, client_ip, client_lat, client_lon })
  }).then(r => r.json());

  const result = await fetch(`${endpoint}/api/resource`, { ... });
  res.json(await result.json());
}
```

**Use when:** Custom routing logic, own proxy layer, or request modification needed

## Database Schema

SQLite database stores:

### Table: `clients`

- `client_id` (TEXT, PRIMARY KEY)
- `hostname` (TEXT)
- `public_ip` (TEXT)
- `latitude`, `longitude` (REAL)
- `country`, `city` (TEXT)
- `total_cpu`, `total_memory`, `total_storage` (INTEGER/REAL)
- `total_gpus` (INTEGER) - Total number of GPUs (0 if none)
- `gpu_models` (TEXT) - JSON array of GPU model names
- `endpoint` (TEXT) - HTTP endpoint for this backend
- `created_at`, `last_seen` (TIMESTAMP)

### Table: `stats`

- `id` (INTEGER, PRIMARY KEY)
- `client_id` (TEXT, FOREIGN KEY)
- `timestamp` (TIMESTAMP)
- `cpu_cores` (INTEGER)
- `cpu_usage_json` (TEXT) - JSON array of per-core usage
- `memory_total`, `memory_used`, `memory_avail` (REAL)
- `disk_total`, `disk_used`, `disk_avail` (REAL)
- `gpu_stats_json` (TEXT) - JSON array of GPU metrics

### Table: `sticky_assignments`

- `sticky_id` (TEXT, NOT NULL) - Value from sticky header
- `tier` (TEXT, NOT NULL)
- `client_id` (TEXT, FOREIGN KEY)
- `created_at`, `last_used` (TIMESTAMP)
- PRIMARY KEY: `(sticky_id, tier)`

Indexes:

- `idx_stats_client_time` on `stats(client_id, timestamp DESC)`
- `idx_clients_last_seen` on `clients(last_seen)`
- `idx_sticky_last_used` on `sticky_assignments(last_used)`
- `idx_sticky_client` on `sticky_assignments(client_id)`
- `idx_sticky_id` on `sticky_assignments(sticky_id)`

## Monitoring

### Health Check

```bash
curl http://localhost:8080/health
```

### List Clients

```bash
curl http://localhost:8080/clients | jq
```

### Test Routing

```bash
curl -X POST http://localhost:8080/route \
  -H "Content-Type: application/json" \
  -H "X-API-Key: your-secret-api-key-here" \
  -d '{
    "tier": "medium",
    "client_ip": "203.0.113.45",
    "client_lat": 40.7128,
    "client_lon": -74.0060
  }' | jq
```

### Logs

```bash
# Server logs
journalctl -u opsen-server -f

# Client logs
journalctl -u opsen-client -f
```

## Performance

**Benchmarked on AMD Ryzen 9 5900X (24 cores), verified across 5 runs:**

| Metric                 | Value              | Notes                                                                       |
| ---------------------- | ------------------ | --------------------------------------------------------------------------- |
| **Server Latency**     | 140 ns → 14.8 µs   | Scales O(n): 0.14 µs (1 client), 1.5 µs (10 clients), 14.8 µs (100 clients) |
| **Concurrent Routing** | 3.7 µs             | 1000 concurrent requests, 100% success rate (5/5 runs identical)            |
| **Scalability**        | 150 clients tested | 1000 requests with 100% success, no race conditions                         |
| **Client Overhead**    | 0.33%              | Combined CPU+RAM+disk+2×GPU at 1 sample/sec (5/5 runs identical)            |
| **Memory (Server)**    | ~8 MB              | Baseline heap allocation, in-memory routing cache                           |
| **Memory (Client)**    | ~3-7 MB            | Varies by CPU core count and GPU monitoring                                 |
| **Database**           | SQLite + WAL       | Off critical path - persistence only, not routing                           |

**All routing decisions are in-memory with no database I/O on the critical path.**

**Reproduce benchmarks:**

```bash
# Routing latency benchmarks (no test output)
go test ./server -bench=BenchmarkRoutingLatency -benchmem -run='^$'

# Scalability tests (includes test output)
go test ./server -run TestScalability -v

# Run all tests with race detector
go test -race ./...
```

## Health Checks & Latency Tracking

Active health checks verify backends are reachable and measure latency. Enabled by default.

**Configuration:**

```yaml
health_check_enabled: true # Enable active probes (default: true)
health_check_type: "tcp" # "tcp" or "http" (default: tcp)
health_check_interval_seconds: 10 # Probe interval (default: 10)
health_check_timeout_seconds: 2 # Probe timeout (default: 2)
health_check_path: "/health" # HTTP path (default: /health)
health_check_unhealthy_threshold: 3 # Failures before unhealthy (default: 3)
health_check_healthy_threshold: 2 # Successes before healthy (default: 2)
```

**Behavior:**

- **TCP probes** - Verify backend port is accepting connections (fast, lightweight)
- **HTTP probes** - GET request to `endpoint + health_check_path`, expects 2xx/3xx status
- **Latency** - Measured on each probe, uses EWMA (exponential weighted moving average) for smoothing
- **Routing impact** - Unhealthy backends excluded, latency added to routing score (lower = better)
- **Sticky sessions** - Automatically removed for unhealthy backends, reassigned on next request
- **Status transitions** - `unknown` → `healthy` (after 2 successes) → `unhealthy` (after 3 failures) → `healthy` (recoverable)

**View health status:**

```bash
curl http://localhost:8080/clients | jq '.[] | {hostname, health_status, latency_ms}'
```

**Example output:**

```json
{
  "hostname": "backend-1",
  "health_status": "healthy",
  "latency_ms": "12.5"
}
```

**When backend goes down:**

1. Health checks fail (3 consecutive failures)
2. Status changes to `unhealthy`
3. Sticky assignments removed automatically
4. Backend excluded from routing
5. Requests fail over to healthy backends

**Recovery:**

1. Backend comes back online
2. Health checks succeed (2 consecutive successes)
3. Status changes to `healthy`
4. Backend rejoins routing pool

## Security Features

**API Key Authentication** - `api_keys[]`, `server_key` in server.yml. Clients send `X-API-Key` header. Use 32+ char random keys, rotate periodically.

**IP Whitelisting** - `whitelisted_ips[]` (CIDR ranges). Empty = allow all.

**Rate Limiting** - Token bucket per IP with continuous token refill. `rate_limit_per_minute: 60`, `rate_limit_burst: 120`. Returns 429 on excess. Set `rate_limit_per_minute: 0` to disable (useful for trusted networks, internal APIs, or when rate limiting is handled by upstream WAF/CDN).

**Request Size Limits** - `max_request_body_bytes: 10485760` (10MB). Returns 413 on excess.

**Timeout Enforcement** - `request_timeout_seconds: 30`, `read_header_timeout_seconds: 10` (Slowloris protection).

**TLS/HTTPS** - `tls_cert_file`, `tls_key_file`. `tls_insecure_skip_verify: false` (backend verification, dev only).

**CORS** - `enable_cors: true`, `cors_allowed_origins[]`.

**Security Headers** - Auto-added: `X-Content-Type-Options`, `X-Frame-Options`, `X-XSS-Protection`, `Strict-Transport-Security` (HTTPS). Can be disabled via `disable_security_headers: true` (e.g., when using a WAF/reverse proxy that manages headers).

**Input Validation** - Content-Type, path traversal, host injection, IP formats, tier names.

## Reliability Features

**Circuit Breaker (Client)** - CLOSED → OPEN (5 failures) → HALF-OPEN (30s) → CLOSED. Prevents cascading failures.

**Retry Logic** - Exponential backoff: 5 attempts, 1s → 2s → 4s → 8s → 16s (max 30s).

**Panic Recovery** - Server: 500 error + stack trace. Client: logs + continues.

**Graceful Shutdown** - `shutdown_timeout_seconds: 30`. Waits for in-flight requests, cancels goroutines, closes DB.

**Database Pooling** - `db_max_open_conns: 25`, `db_max_idle_conns: 5`, `db_conn_max_lifetime: 300`.

**Structured Logging** - `log_level: info`, `json_logging: true`. JSON or plain text with timestamp, level, file, line, data.

## License

This project is licensed under the Apache License 2.0 - see the [LICENSE](LICENSE) file for details.

Copyright 2026 Opsen Contributors
