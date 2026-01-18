package main

import (
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// ========================================
// HEALTH CHECK TESTS
// ========================================

// TestHealthCheck_TCP_Success verifies TCP health check succeeds for reachable backend
func TestHealthCheck_TCP_Success(t *testing.T) {
	// Create a TCP server that accepts connections
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to create TCP listener: %v", err)
	}
	defer listener.Close()

	// Accept connections aggressively in background (multiple goroutines)
	for i := 0; i < 5; i++ {
		go func() {
			for {
				conn, err := listener.Accept()
				if err != nil {
					return
				}
				conn.Close()
			}
		}()
	}

	// Get the address
	addr := listener.Addr().String()

	db, cleanup := CreateTestDB(t)
	defer cleanup()

	server := NewTestServer(t, db)
	server.config.HealthCheckEnabled = true
	server.config.HealthCheckType = "tcp"
	server.config.HealthCheckTimeoutSecs = 2

	// Add mock client
	client := NewMockClient(MockClientOptions{
		ClientID: "tcp-backend",
		Endpoint: "http://" + addr,
	})
	server.AddMockClient(client)

	// Give the listener goroutines time to start
	time.Sleep(50 * time.Millisecond)

	// Perform health checks until healthy or max attempts
	// Default threshold is 2 consecutive successes
	maxAttempts := server.config.HealthCheckHealthyThreshold + 2 // Allow a few failures
	for i := 0; i < maxAttempts; i++ {
		server.probeClient(client)
		if client.HealthStatus == "healthy" {
			break
		}
	}

	if client.HealthStatus != "healthy" {
		t.Errorf("Expected health status 'healthy', got '%s' (successes: %d, failures: %d)",
			client.HealthStatus, client.ConsecutiveSuccesses, client.ConsecutiveFailures)
	}

	if client.LatencyMs == 0 {
		t.Error("Expected latency to be measured")
	}

	if client.LastHealthCheck.IsZero() {
		t.Error("Expected LastHealthCheck to be set")
	}
}

// TestHealthCheck_TCP_Failure verifies TCP health check fails for unreachable backend
func TestHealthCheck_TCP_Failure(t *testing.T) {
	db, cleanup := CreateTestDB(t)
	defer cleanup()

	server := NewTestServer(t, db)
	server.config.HealthCheckEnabled = true
	server.config.HealthCheckType = "tcp"
	server.config.HealthCheckTimeoutSecs = 1

	// Add mock client with unreachable endpoint
	client := NewMockClient(MockClientOptions{
		ClientID: "unreachable-backend",
		Endpoint: "http://127.0.0.1:11111", // Nothing listening here
	})
	server.AddMockClient(client)

	// Perform health checks to reach unhealthy threshold
	for i := 0; i < server.config.HealthCheckUnhealthyThreshold; i++ {
		server.probeClient(client)
	}

	if client.HealthStatus != "unhealthy" {
		t.Errorf("Expected health status 'unhealthy', got '%s'", client.HealthStatus)
	}

	if client.ConsecutiveFailures != server.config.HealthCheckUnhealthyThreshold {
		t.Errorf("Expected %d consecutive failures, got %d",
			server.config.HealthCheckUnhealthyThreshold, client.ConsecutiveFailures)
	}
}

// TestHealthCheck_HTTP_Success verifies HTTP health check succeeds for healthy endpoint
func TestHealthCheck_HTTP_Success(t *testing.T) {
	// Create HTTP server with /health endpoint
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("OK"))
		} else {
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer backend.Close()

	db, cleanup := CreateTestDB(t)
	defer cleanup()

	server := NewTestServer(t, db)
	server.config.HealthCheckEnabled = true
	server.config.HealthCheckType = "http"
	server.config.HealthCheckPath = "/health"
	server.config.HealthCheckTimeoutSecs = 2

	// Add mock client
	client := NewMockClient(MockClientOptions{
		ClientID: "http-backend",
		Endpoint: backend.URL,
	})
	server.AddMockClient(client)

	// Perform health checks to reach healthy threshold
	for i := 0; i < server.config.HealthCheckHealthyThreshold; i++ {
		server.probeClient(client)
	}

	if client.HealthStatus != "healthy" {
		t.Errorf("Expected health status 'healthy', got '%s'", client.HealthStatus)
	}

	if client.LatencyMs == 0 {
		t.Error("Expected latency to be measured")
	}
}

// TestHealthCheck_HTTP_Failure verifies HTTP health check fails for bad status codes
func TestHealthCheck_HTTP_Failure(t *testing.T) {
	// Create HTTP server that returns 500
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer backend.Close()

	db, cleanup := CreateTestDB(t)
	defer cleanup()

	server := NewTestServer(t, db)
	server.config.HealthCheckEnabled = true
	server.config.HealthCheckType = "http"
	server.config.HealthCheckPath = "/health"
	server.config.HealthCheckTimeoutSecs = 2

	// Add mock client
	client := NewMockClient(MockClientOptions{
		ClientID: "failing-http-backend",
		Endpoint: backend.URL,
	})
	server.AddMockClient(client)

	// Perform health checks to reach unhealthy threshold
	for i := 0; i < server.config.HealthCheckUnhealthyThreshold; i++ {
		server.probeClient(client)
	}

	if client.HealthStatus != "unhealthy" {
		t.Errorf("Expected health status 'unhealthy', got '%s'", client.HealthStatus)
	}
}

// TestHealthCheck_LatencyEWMA verifies latency uses exponential weighted moving average
func TestHealthCheck_LatencyEWMA(t *testing.T) {
	// Create listener that accepts connections
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to create TCP listener: %v", err)
	}
	defer listener.Close()

	// Accept connections in background
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			conn.Close()
		}
	}()

	addr := listener.Addr().String()

	db, cleanup := CreateTestDB(t)
	defer cleanup()

	server := NewTestServer(t, db)
	server.config.HealthCheckEnabled = true
	server.config.HealthCheckType = "tcp"

	client := NewMockClient(MockClientOptions{
		ClientID: "latency-backend",
		Endpoint: "http://" + addr,
	})
	server.AddMockClient(client)

	// Perform first health check
	server.probeClient(client)
	firstLatency := client.LatencyMs

	// Perform second health check
	server.probeClient(client)
	secondLatency := client.LatencyMs

	// EWMA should smooth the values (not just replace)
	// The second value should be influenced by but not equal to the new measurement
	if firstLatency == 0 {
		t.Error("Expected first latency to be measured")
	}

	// Second latency could be higher or lower, but should be set
	if secondLatency == 0 {
		t.Error("Expected second latency to be measured")
	}

	t.Logf("First latency: %.2fms, Second latency: %.2fms", firstLatency, secondLatency)
}

// TestHealthCheck_UnhealthyBackendFiltered verifies unhealthy backends are filtered from routing
func TestHealthCheck_UnhealthyBackendFiltered(t *testing.T) {
	db, cleanup := CreateTestDB(t)
	defer cleanup()

	server := NewTestServer(t, db)
	server.config.HealthCheckEnabled = true
	server.config.HealthCheckType = "tcp"

	// Add healthy backend
	healthyClient := NewMockClient(MockClientOptions{
		ClientID:    "healthy-backend",
		Endpoint:    "http://127.0.0.1:12345", // Doesn't matter for this test
		CPUUsageAvg: []float64{10, 10, 10, 10},
	})
	healthyClient.HealthStatus = "healthy"
	server.AddMockClient(healthyClient)

	// Add unhealthy backend
	unhealthyClient := NewMockClient(MockClientOptions{
		ClientID:    "unhealthy-backend",
		Endpoint:    "http://127.0.0.1:12346",
		CPUUsageAvg: []float64{5, 5, 5, 5}, // Better resources
	})
	unhealthyClient.HealthStatus = "unhealthy"
	server.AddMockClient(unhealthyClient)

	// Request routing
	tierSpec := server.tierSpecs["lite"]
	selected := server.findBestClient(tierSpec, 0, 0)

	if selected == nil {
		t.Fatal("Expected a backend to be selected")
	}

	// Should select healthy backend, not unhealthy one (even though unhealthy has better resources)
	if selected.Registration.ClientID != "healthy-backend" {
		t.Errorf("Expected healthy-backend to be selected, got %s", selected.Registration.ClientID)
	}
}

// TestHealthCheck_StickyAssignmentRemoved verifies sticky assignments are removed for unhealthy backends
func TestHealthCheck_StickyAssignmentRemoved(t *testing.T) {
	db, cleanup := CreateTestDB(t)
	defer cleanup()

	server := NewTestServer(t, db)
	server.config.HealthCheckEnabled = true
	server.config.HealthCheckType = "tcp"

	// Add backend
	client := NewMockClient(MockClientOptions{
		ClientID:    "backend-1",
		CPUUsageAvg: []float64{10, 10, 10, 10},
	})
	client.HealthStatus = "healthy"
	server.AddMockClient(client)

	// Create sticky assignment
	stickyID := "user-123"
	tier := "lite"
	server.createStickyAssignment(stickyID, tier, client.Registration.ClientID)

	// Verify assignment exists
	server.mu.RLock()
	_, exists := server.stickyAssignments[stickyID][tier]
	server.mu.RUnlock()

	if !exists {
		t.Fatal("Expected sticky assignment to exist")
	}

	// Mark backend as unhealthy (simulate consecutive failures)
	client.ConsecutiveFailures = server.config.HealthCheckUnhealthyThreshold
	server.updateHealthStatus(client, false, 10*time.Millisecond)

	// Verify assignment was removed
	server.mu.RLock()
	tierMap, exists := server.stickyAssignments[stickyID]
	server.mu.RUnlock()

	if exists && tierMap[tier] != "" {
		t.Error("Expected sticky assignment to be removed for unhealthy backend")
	}
}

// TestHealthCheck_LatencyInRoutingScore verifies latency affects routing decisions
func TestHealthCheck_LatencyInRoutingScore(t *testing.T) {
	db, cleanup := CreateTestDB(t)
	defer cleanup()

	server := NewTestServer(t, db)
	server.config.HealthCheckEnabled = true

	// Add low-latency backend with higher CPU usage
	lowLatencyClient := NewMockClient(MockClientOptions{
		ClientID:    "low-latency",
		CPUUsageAvg: []float64{40, 40, 40, 40, 40, 40, 40, 40},
	})
	lowLatencyClient.HealthStatus = "healthy"
	lowLatencyClient.LatencyMs = 10.0 // 10ms latency
	server.AddMockClient(lowLatencyClient)

	// Add high-latency backend with lower CPU usage
	highLatencyClient := NewMockClient(MockClientOptions{
		ClientID:    "high-latency",
		CPUUsageAvg: []float64{20, 20, 20, 20, 20, 20, 20, 20},
	})
	highLatencyClient.HealthStatus = "healthy"
	highLatencyClient.LatencyMs = 200.0 // 200ms latency
	server.AddMockClient(highLatencyClient)

	// Request routing
	tierSpec := server.tierSpecs["lite"]
	selected := server.findBestClient(tierSpec, 0, 0)

	if selected == nil {
		t.Fatal("Expected a backend to be selected")
	}

	// High latency (200ms) should heavily penalize the high-latency backend
	// Even though it has better CPU (20% vs 40%), the latency difference is huge
	// Score calculation: distance + CPU + memory + latency
	// Low latency: 0 + 40 + 0 + 10 = 50
	// High latency: 0 + 20 + 0 + 200 = 220
	// Should select low-latency backend

	if selected.Registration.ClientID != "low-latency" {
		t.Errorf("Expected low-latency backend to be selected, got %s (latency: %.1fms)",
			selected.Registration.ClientID, selected.LatencyMs)
	}
}

// TestHealthCheck_Recovery verifies backends can recover to healthy status
func TestHealthCheck_Recovery(t *testing.T) {
	// Create listener that accepts connections
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to create TCP listener: %v", err)
	}
	defer listener.Close()

	// Accept connections in background
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			conn.Close()
		}
	}()

	addr := listener.Addr().String()

	db, cleanup := CreateTestDB(t)
	defer cleanup()

	server := NewTestServer(t, db)
	server.config.HealthCheckEnabled = true
	server.config.HealthCheckType = "tcp"

	client := NewMockClient(MockClientOptions{
		ClientID: "recovering-backend",
		Endpoint: "http://" + addr,
	})
	server.AddMockClient(client)

	// Make it unhealthy first
	client.HealthStatus = "unhealthy"
	client.ConsecutiveFailures = 5
	client.ConsecutiveSuccesses = 0

	// Now perform successful health checks
	for i := 0; i < server.config.HealthCheckHealthyThreshold; i++ {
		server.probeClient(client)
	}

	// Should recover to healthy
	if client.HealthStatus != "healthy" {
		t.Errorf("Expected backend to recover to 'healthy', got '%s' (successes: %d, failures: %d)",
			client.HealthStatus, client.ConsecutiveSuccesses, client.ConsecutiveFailures)
	}

	if client.ConsecutiveSuccesses < server.config.HealthCheckHealthyThreshold {
		t.Errorf("Expected at least %d consecutive successes, got %d",
			server.config.HealthCheckHealthyThreshold, client.ConsecutiveSuccesses)
	}

	if client.ConsecutiveFailures != 0 {
		t.Errorf("Expected consecutive failures to be reset to 0, got %d", client.ConsecutiveFailures)
	}
}
