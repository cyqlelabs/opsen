# Load Balancer Testing Guide

This document describes the testing infrastructure and how to run tests for the Opsen Load Balancer.

## Test Structure

The test suite is organized into several categories:

### Server Tests (`server/main_test.go`)
- **Stickiness Tests**: Verify sticky session functionality
  - Same backend routing for sticky sessions
  - Cross-tier affinity
  - Fallback on overload
  - Persistence across server restarts

- **Resource Allocation Tests**: Verify resource checking logic
  - CPU core availability (per-core usage)
  - Memory availability
  - Disk availability
  - Pending allocation tracking

- **Concurrent Request Tests**: Verify thread-safety and race conditions
  - No double-booking of resources
  - Race condition detection
  - Pending allocation cleanup

- **Routing Algorithm Tests**: Verify selection logic
  - Geographic distance calculation
  - Scoring algorithm
  - Stale client handling
  - Edge cases (no clients, all overloaded)

### Common Tests
- **`common/types_test.go`**: Data structure serialization/deserialization
- **`common/config_test.go`**: Configuration loading and defaults

### Middleware Tests (`server/middleware_test.go`)
- Panic recovery
- Request size limiting
- Timeout handling
- Rate limiting (per-IP)
- API key authentication
- IP whitelisting
- Security headers
- Middleware chaining

## Running Tests

### Run All Tests
```bash
cd /home/nico/dev/projects/cyqle/platform/modules/loadbalancer
go test ./...
```

### Run Tests with Coverage
```bash
go test ./... -cover
```

### Run Tests with Detailed Coverage Report
```bash
go test ./... -coverprofile=coverage.out
go tool cover -html=coverage.out -o coverage.html
```

### Run Tests with Race Detector
**IMPORTANT**: Always run tests with race detector to catch concurrency issues:
```bash
go test ./... -race
```

### Run Specific Test
```bash
go test ./server -run TestStickySession_SameBackend -v
```

### Run Tests with Verbose Output
```bash
go test ./... -v
```

### Run Tests for Specific Package
```bash
go test ./server
go test ./common
```

## Test Utilities

### Mock Infrastructure (`server/testutil_test.go`)

The test utilities provide helper functions for creating test scenarios:

#### Database Setup
```go
db, cleanup := TestDB(t)
defer cleanup()
```

#### Server Instance
```go
server := NewTestServer(t, db)
```

#### Mock Clients
```go
client := NewMockClient(MockClientOptions{
    ClientID:    "backend-1",
    TotalCPU:    8,
    CPUUsageAvg: []float64{10, 20, 30, 40, 50, 60, 70, 80},
    MemoryAvail: 16.0,
    DiskAvail:   100.0,
})
server.AddMockClient(client)
```

#### Assertions
```go
AssertClientSelected(t, client, "expected-client-id")
AssertNoClient(t, client)
```

## Key Test Scenarios

### 1. Sticky Session Consistency
Verifies that requests with the same sticky ID always route to the same backend:
```bash
go test ./server -run TestStickySession_SameBackend -v
```

### 2. Resource Allocation Under Load
Verifies that pending allocations prevent overbooking:
```bash
go test ./server -run TestResourceAllocation_PendingAllocations -v
```

### 3. Concurrent Request Handling
Simulates 50+ concurrent requests to detect race conditions:
```bash
go test ./server -run TestConcurrentRequests -race -v
```

### 4. Geographic Routing
Verifies distance-based routing preferences:
```bash
go test ./server -run TestGeographicDistance -v
```

## Continuous Integration

For CI/CD pipelines, use:
```bash
# Run all tests with race detection and coverage
go test ./... -race -coverprofile=coverage.out -covermode=atomic

# Generate coverage report
go tool cover -func=coverage.out
```

## Benchmarking

To add benchmarks, create functions with `Benchmark` prefix:
```go
func BenchmarkSelectClient(b *testing.B) {
    // Setup
    db, cleanup := TestDB(&testing.T{})
    defer cleanup()
    server := NewTestServer(&testing.T{}, db)

    // Add clients...

    b.ResetTimer()
    for i := 0; i < b.N; i++ {
        server.findBestClient(tierSpec, 0, 0)
    }
}
```

Run benchmarks:
```bash
go test ./server -bench=. -benchmem
```

## Writing New Tests

### Test Naming Convention
- Test function names: `Test<Component>_<Scenario>`
- Example: `TestStickySession_FallbackOnOverload`

### Test Structure
1. **Setup**: Create test database and server
2. **Prepare**: Add mock clients with specific configurations
3. **Execute**: Run the function being tested
4. **Assert**: Verify expected outcomes
5. **Cleanup**: Defer cleanup functions

### Example Test Template
```go
func TestNewFeature_Scenario(t *testing.T) {
    // Setup
    db, cleanup := TestDB(t)
    defer cleanup()
    server := NewTestServer(t, db)

    // Prepare
    client := NewMockClient(MockClientOptions{
        ClientID: "test-backend",
        // ... configuration
    })
    server.AddMockClient(client)

    // Execute
    result := server.someFunction(/* args */)

    // Assert
    if result != expected {
        t.Errorf("Expected %v, got %v", expected, result)
    }
}
```

## Common Issues

### Race Conditions
If race detector reports issues:
1. Check all map/slice access is protected by mutexes
2. Verify `server.mu` is used correctly (RLock for reads, Lock for writes)
3. Ensure pending allocations use proper locking

### Flaky Tests
If tests fail intermittently:
1. Check for timing dependencies (use channels/waitgroups instead of sleep)
2. Verify cleanup is complete before test ends
3. Ensure database transactions are committed

### Database Errors
If SQLite errors occur:
1. Verify `TestDB()` cleanup is deferred
2. Check database schema matches test expectations
3. Ensure tests don't interfere (each test gets isolated DB)

## Coverage Goals

Target coverage by package:
- `server/`: 80%+ (core routing logic)
- `common/`: 90%+ (simple types and config)
- `client/`: 70%+ (external dependencies make 100% difficult)

Check current coverage:
```bash
go test ./... -cover | grep -E 'coverage|ok'
```
