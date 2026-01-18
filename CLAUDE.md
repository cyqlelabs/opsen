# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

Opsen is a resource-aware load balancer written in Go. It intelligently routes traffic based on real-time CPU, RAM, disk, and GPU availability on backend servers, with optional sticky session support and geographic routing.

**Architecture:**
- **Server** (`opsen-server`) - Central routing coordinator that receives metrics from clients and makes routing decisions
- **Client** (`opsen-client`) - Runs on each backend server, collects system metrics and reports to the server
- **Common** (`common/`) - Shared types, configuration loading, and tier specifications

## Building and Testing

```bash
# Build both server and client
make all

# Build individually
make build-server
make build-client

# Run all tests
go test ./...

# Run tests with race detector (IMPORTANT for concurrency testing)
go test -race ./...

# Run specific test
go test ./server -run TestStickySession_SameBackend -v

# Generate coverage report
make test-coverage-html

# Run benchmarks
make benchmark

# Install to system (copies binaries to /usr/local/bin and systemd services)
sudo make install

# Clean build artifacts
make clean
```

## Running Locally

```bash
# Server (with config file)
./bin/opsen-server -config server.yml

# Client (with config file)
./bin/opsen-client -config client.yml

# Or use command-line flags (overrides YAML config)
./bin/opsen-server -port 8080 -db test.db -stale 5
./bin/opsen-client -server http://localhost:8080 -window 15 -interval 60
```

## Code Architecture

### Server (`server/`)

**Key components:**
- `Server` struct: Main server instance with client cache, sticky assignments, and pending allocations
- `ClientState` struct: Cached state of each registered backend (registration + latest stats)
- `PendingAllocation` struct: Tracks reserved resources to prevent race conditions during concurrent routing

**Core routing flow:**
1. Client registers via `/register` → stored in `clients` table and `clientCache`
2. Client sends stats via `/stats` → stored in `stats` table and updates `clientCache`
3. Application requests backend via `/route` → `findBestClient()` selects optimal backend
4. Resources are reserved in `pendingAllocations` to prevent double-booking
5. Response includes endpoint URL to forward requests to

**Routing algorithm (`findBestClient()`):**
- Filters clients with sufficient resources (accounting for pending allocations)
- Checks sticky assignments first (if sticky header present)
- Calculates score: `distance_km + (avg_cpu_usage_pct * 1.0) + (memory_usage_pct * 1.0) + (gpu_usage_pct * 1.5)`
- CPU scoring uses N **least-loaded cores** (representing what new session would use)
- GPU gets higher weight (1.5x) as GPU workloads are more sensitive to contention
- Selects client with lowest score
- Creates pending allocation to prevent race conditions

**Concurrency safety:**
- `Server.mu` protects all shared state (`clientCache`, `stickyAssignments`, `pendingAllocations`)
- Use `RLock()` for reads, `Lock()` for writes
- Database uses connection pooling (configured via YAML)
- Middleware includes panic recovery

**Database schema:**
- `clients` table: Client registration data (hostname, IP, geolocation, total resources including GPUs)
- `stats` table: Time-series resource usage data (CPU per-core, memory, disk, GPU metrics)
- `sticky_assignments` table: Sticky session mappings (sticky_id + tier → client_id)

### Client (`client/`)

**Key components:**
- `MetricsCollector`: Collects CPU, memory, disk metrics over time window
- `GPUCollector`: Collects NVIDIA GPU metrics (gracefully disabled if no GPUs present)
- `CircuitBreaker`: Prevents cascading failures (CLOSED → OPEN → HALF-OPEN)
- Retry logic with exponential backoff for registration

**Metrics collection:**
- Samples system metrics every 1 second using `gopsutil`
- Samples GPU metrics using NVML (NVIDIA Management Library) if available
- Averages samples over configurable time window (default: 15 minutes)
- Reports averaged stats to server every 60 seconds
- Handles geolocation via MaxMind GeoIP database or ipapi.co API
- GPU monitoring gracefully disabled if no NVIDIA GPUs present

### Common (`common/`)

**Configuration (`config.go`):**
- `LoadServerConfig()` - Loads server YAML config with defaults
- `LoadClientConfig()` - Loads client YAML config with defaults
- Priority: CLI flags > YAML file > hardcoded defaults

**Types (`types.go`):**
- `TierSpec` - Resource requirements for a tier (vCPU, memory, storage, optional GPU + GPU memory)
- `GPUStats` - Per-GPU metrics (utilization, VRAM, temperature, power draw)
- `ResourceStats` - Current resource usage from client (including GPU array)
- `ClientRegistration` - Initial registration data (including total GPUs and models)
- `RoutingRequest/Response` - Routing endpoint payloads

## Key Features to Understand

### 1. Sticky Sessions
- Configured via `sticky_header` in server.yml (e.g., "X-Session-ID")
- Maps `(sticky_id, tier) → client_id` in database
- `sticky_affinity_enabled`: Different tiers from same sticky_id prefer same server
- Automatic fallback if assigned server is overloaded or unavailable

### 2. Resource Allocation & Race Conditions
- **Problem**: Concurrent routing requests could all select the same server, causing overload
- **Solution**: `pendingAllocations` reserves resources immediately upon selection
- Resources expire after `pending_allocation_timeout_seconds` (default: 120s)
- Filtering checks: `available = actual - pending`, but scoring uses only actual usage
- Duplicate allocations for same `sticky_id + tier` are deduplicated

### 3. CPU Availability
- A core is "available" if avg usage over time window is <80%
- For scoring, algorithm selects N **least-loaded cores** and averages their usage
- This represents the actual CPU resources a new session would consume

### 4. Built-in Reverse Proxy
- Server can proxy requests configured in `proxy_endpoints` (e.g., ["/api", "/browse"])
- Automatically extracts tier from request body, query params, or headers
- Forwards request to selected backend with path preserved
- Supports sticky sessions via custom headers
- **SSE Support**: Fully supports Server-Sent Events (SSE) and streaming responses
  - Configured via `proxy_sse_flush_interval_ms` in server config
  - `-1` = immediate flush (default, best for SSE)
  - `0` = no flush (buffered, not suitable for SSE)
  - `>0` = flush at interval in milliseconds
  - Implementation: Sets `FlushInterval` on `httputil.ReverseProxy` (see `handleProxy()` in `server/main.go:1158`)

### 5. Active Health Checks & Latency Tracking
- **Enabled by default**, runs in background goroutine (`runHealthChecks()`)
- **Two probe types**:
  - TCP: Fast connection check to backend port (default)
  - HTTP: GET request to configured path, expects 2xx/3xx status
- **Latency measurement**: EWMA (exponential weighted moving average) with alpha=0.3
- **Health states**: `unknown` → `healthy` (2 successes) → `unhealthy` (3 failures) → recoverable
- **Routing impact**: Unhealthy backends filtered in `findBestClient()`, latency added to score
- **Sticky session handling**: Automatically removes assignments for unhealthy backends
- **Implementation**: `probeClient()`, `probeTCP()`, `probeHTTP()`, `updateHealthStatus()` in `server/main.go`
- **Configuration**: Fully configurable intervals, thresholds, timeout via server YAML config

## Testing

**Test structure:**
- `server/main_test.go` - Core routing, sticky sessions, resource allocation, concurrency
- `server/proxy_test.go` - Reverse proxy functionality including SSE streaming
- `server/health_test.go` - Health checks (TCP/HTTP probes, latency tracking, failover)
- `server/middleware_test.go` - Middleware (auth, rate limiting, timeouts, panic recovery)
- `common/types_test.go` - Type serialization
- `common/config_test.go` - Configuration loading

**Test utilities (`server/testutil_test.go`):**
- `TestDB(t)` - Creates isolated in-memory SQLite database for testing
- `NewTestServer(t, db)` - Creates server instance for testing
- `NewMockClient(opts)` - Creates mock client with specific resource configuration
- `AssertClientSelected(t, client, expectedID)` - Asserts routing selected expected backend

**Always run with race detector:**
```bash
go test -race ./...
```

## Common Development Tasks

### Adding a New Tier
1. Edit tier specifications in server YAML config:
```yaml
tiers:
  - name: enterprise
    vcpu: 16
    memory_gb: 32.0
    storage_gb: 100
  - name: gpu-inference  # GPU tier example
    vcpu: 8
    memory_gb: 32.0
    storage_gb: 100
    gpu: 1               # Requires 1 GPU
    gpu_memory_gb: 16.0  # Requires 16GB VRAM
```
2. No code changes needed - tiers are fully configurable

### Modifying Routing Algorithm
- Edit `findBestClient()` in `server/main.go`
- Adjust scoring weights or filtering criteria
- Add tests in `server/main_test.go`
- Run with race detector to verify concurrency safety

### Adding New Middleware
- Create middleware function in `server/middleware.go`
- Follow signature: `func(http.Handler) http.Handler`
- Add to middleware chain in `main()`
- Add tests in `server/middleware_test.go`

### Adding API Endpoint
- Add handler function in `server/main.go`
- Register in `main()` with appropriate middleware
- Update database schema if needed (in `initDatabase()`)
- Add tests

## Security Considerations

The load balancer includes production-ready security features:
- API key authentication (`X-API-Key` header)
- IP whitelisting (CIDR ranges)
- Rate limiting (per-IP token bucket)
- Request size limits
- Timeout enforcement (including Slowloris protection)
- TLS/HTTPS support
- CORS configuration
- Security headers (automatically added)
- Input validation (tier names, IP addresses, path traversal)

When modifying request handlers, maintain these security boundaries.

## Module Path

The Go module path is `cyqle.in/opsen`. Import common package as:
```go
import "cyqle.in/opsen/common"
```

## Dependencies

Key dependencies (see `go.mod`):
- `github.com/mattn/go-sqlite3` - SQLite database driver
- `github.com/shirou/gopsutil/v3` - System metrics collection (CPU, memory, disk)
- `github.com/NVIDIA/go-nvml` - NVIDIA GPU metrics collection (optional, gracefully disabled)
- `github.com/oschwald/geoip2-golang` - GeoIP lookups
- `github.com/google/uuid` - UUID generation
- `gopkg.in/yaml.v3` - YAML configuration parsing
