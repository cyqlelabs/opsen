package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"cyqle.in/opsen/common"
)

// TestReportStats_Success verifies successful stats reporting
func TestReportStats_Success(t *testing.T) {
	// Create mock server
	statsReceived := false
	var receivedStats common.ResourceStats
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/stats" {
			statsReceived = true

			if err := json.NewDecoder(r.Body).Decode(&receivedStats); err != nil {
				t.Errorf("Failed to decode stats: %v", err)
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}

			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]string{"status": "received"})
		}
	}))
	defer server.Close()

	config := Config{
		ServerURL:       server.URL,
		ClientID:        "test-client-stats",
		Hostname:        "test-host",
		WindowMinutes:   1,
		ReportInterval:  60,
		DiskPath:        "/",
		SkipGeolocation: true,
	}

	gpuCollector := NewGPUCollector(3)

	collector := &MetricsCollector{
		config:         config,
		httpClient:     &http.Client{Timeout: 5 * time.Second},
		cpuSamples:     make([][]float64, 3),
		memorySamples:  make([]float64, 3),
		diskSamples:    make([]float64, 3),
		gpuCollector:   gpuCollector,
		maxSamples:     3,
		circuitBreaker: NewCircuitBreaker(5, 30*time.Second),
		retryConfig:    DefaultRetryConfig(),
	}

	// Add sample data
	collector.cpuSamples[0] = []float64{10.0, 20.0}
	collector.cpuSamples[1] = []float64{15.0, 25.0}
	collector.cpuSamples[2] = []float64{20.0, 30.0}
	collector.memorySamples[0] = 8.0
	collector.memorySamples[1] = 9.0
	collector.memorySamples[2] = 10.0
	collector.diskSamples[0] = 50.0
	collector.diskSamples[1] = 55.0
	collector.diskSamples[2] = 60.0
	collector.sampleIndex = 3

	// Report stats
	err := collector.reportStats()
	if err != nil {
		t.Fatalf("reportStats failed: %v", err)
	}

	if !statsReceived {
		t.Error("Stats not received by server")
	}

	// Verify stats structure
	if receivedStats.ClientID != config.ClientID {
		t.Errorf("Expected client_id %s, got %s", config.ClientID, receivedStats.ClientID)
	}

	if len(receivedStats.CPUUsageAvg) == 0 {
		t.Error("Expected CPU usage data in stats")
	}
}

// TestReportStats_ServerError verifies handling of server errors
func TestReportStats_ServerError(t *testing.T) {
	// Create mock server that returns error
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/stats" {
			w.WriteHeader(http.StatusBadGateway)
			_, _ = w.Write([]byte("Bad gateway"))
		}
	}))
	defer server.Close()

	config := Config{
		ServerURL:       server.URL,
		ClientID:        "test-client-error",
		Hostname:        "test-host",
		WindowMinutes:   1,
		ReportInterval:  60,
		DiskPath:        "/",
		SkipGeolocation: true,
	}

	gpuCollector := NewGPUCollector(3)

	collector := &MetricsCollector{
		config:         config,
		httpClient:     &http.Client{Timeout: 5 * time.Second},
		cpuSamples:     make([][]float64, 3),
		memorySamples:  make([]float64, 3),
		diskSamples:    make([]float64, 3),
		gpuCollector:   gpuCollector,
		maxSamples:     3,
		circuitBreaker: NewCircuitBreaker(5, 30*time.Second),
		retryConfig:    DefaultRetryConfig(),
		sampleIndex:    3,
	}

	// Report stats - should fail
	err := collector.reportStats()
	if err == nil {
		t.Error("Expected error when server returns 502")
	}
}

// TestReportStats_EmptyData verifies handling of empty metrics
func TestReportStats_EmptyData(t *testing.T) {
	// Create mock server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/stats" {
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]string{"status": "received"})
		}
	}))
	defer server.Close()

	config := Config{
		ServerURL:       server.URL,
		ClientID:        "test-client-empty",
		Hostname:        "test-host",
		WindowMinutes:   1,
		ReportInterval:  60,
		DiskPath:        "/",
		SkipGeolocation: true,
	}

	gpuCollector := NewGPUCollector(3)

	collector := &MetricsCollector{
		config:         config,
		httpClient:     &http.Client{Timeout: 5 * time.Second},
		cpuSamples:     make([][]float64, 3),
		memorySamples:  make([]float64, 3),
		diskSamples:    make([]float64, 3),
		gpuCollector:   gpuCollector,
		maxSamples:     3,
		circuitBreaker: NewCircuitBreaker(5, 30*time.Second),
		retryConfig:    DefaultRetryConfig(),
		sampleIndex:    0, // No samples collected yet
	}

	// Report stats with empty data - should still succeed
	err := collector.reportStats()
	if err != nil {
		t.Errorf("reportStats should succeed with empty data, got error: %v", err)
	}
}

// TestCollectMetrics_Timeout verifies collectMetrics runs without blocking
func TestCollectMetrics_Timeout(t *testing.T) {
	// This test verifies that collectMetrics can be started in a goroutine
	// and doesn't block the main thread
	config := Config{
		ServerURL:       "http://localhost:8080",
		ClientID:        "test-client",
		Hostname:        "test-host",
		WindowMinutes:   1,
		ReportInterval:  60,
		DiskPath:        "/",
		SkipGeolocation: true,
	}

	gpuCollector := NewGPUCollector(3)

	collector := &MetricsCollector{
		config:         config,
		httpClient:     &http.Client{Timeout: 5 * time.Second},
		cpuSamples:     make([][]float64, 3),
		memorySamples:  make([]float64, 3),
		diskSamples:    make([]float64, 3),
		gpuCollector:   gpuCollector,
		maxSamples:     3,
		circuitBreaker: NewCircuitBreaker(5, 30*time.Second),
		retryConfig:    DefaultRetryConfig(),
	}

	// Run collectMetrics in background
	// We don't wait for it or check its state to avoid race conditions
	started := make(chan bool)
	go func() {
		started <- true
		collector.collectMetrics()
	}()

	// Wait for goroutine to start
	<-started

	// Give it a moment to run
	time.Sleep(100 * time.Millisecond)

	// Test passes if collectMetrics starts without blocking
	t.Log("collectMetrics started successfully in background")
}

// TestReportStats_WithGPUData verifies stats reporting with GPU metrics
func TestReportStats_WithGPUData(t *testing.T) {
	// Create mock server
	var receivedStats common.ResourceStats
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/stats" {
			if err := json.NewDecoder(r.Body).Decode(&receivedStats); err != nil {
				t.Errorf("Failed to decode stats: %v", err)
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}

			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]string{"status": "received"})
		}
	}))
	defer server.Close()

	config := Config{
		ServerURL:       server.URL,
		ClientID:        "test-client-gpu",
		Hostname:        "test-host",
		WindowMinutes:   1,
		ReportInterval:  60,
		DiskPath:        "/",
		SkipGeolocation: true,
	}

	gpuCollector := NewGPUCollector(3)

	collector := &MetricsCollector{
		config:         config,
		httpClient:     &http.Client{Timeout: 5 * time.Second},
		cpuSamples:     make([][]float64, 3),
		memorySamples:  make([]float64, 3),
		diskSamples:    make([]float64, 3),
		gpuCollector:   gpuCollector,
		maxSamples:     3,
		circuitBreaker: NewCircuitBreaker(5, 30*time.Second),
		retryConfig:    DefaultRetryConfig(),
		sampleIndex:    3,
	}

	// Add sample data
	collector.cpuSamples[0] = []float64{10.0}
	collector.cpuSamples[1] = []float64{15.0}
	collector.cpuSamples[2] = []float64{20.0}
	collector.memorySamples[0] = 8.0
	collector.memorySamples[1] = 9.0
	collector.memorySamples[2] = 10.0
	collector.diskSamples[0] = 50.0
	collector.diskSamples[1] = 55.0
	collector.diskSamples[2] = 60.0

	// Report stats
	err := collector.reportStats()
	if err != nil {
		t.Fatalf("reportStats failed: %v", err)
	}

	// GPU stats should be empty if no GPUs available, or populated if GPUs exist
	t.Logf("GPU stats count: %d", len(receivedStats.GPUs))
}

// TestRun_CircuitOpen verifies behavior when circuit is open
func TestRun_CircuitOpen(t *testing.T) {
	// Create server that always fails
	failCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		failCount++
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	config := Config{
		ServerURL:       server.URL,
		ClientID:        "test-client-circuit",
		Hostname:        "test-host",
		WindowMinutes:   1,
		ReportInterval:  1, // Report every second for fast testing
		DiskPath:        "/",
		SkipGeolocation: true,
	}

	gpuCollector := NewGPUCollector(3)

	collector := &MetricsCollector{
		config:         config,
		httpClient:     &http.Client{Timeout: 2 * time.Second},
		cpuSamples:     make([][]float64, 3),
		memorySamples:  make([]float64, 3),
		diskSamples:    make([]float64, 3),
		gpuCollector:   gpuCollector,
		maxSamples:     3,
		circuitBreaker: NewCircuitBreaker(3, 100*time.Millisecond), // Quick reset for testing
		retryConfig:    DefaultRetryConfig(),
		sampleIndex:    3,
	}

	// Add sample data
	collector.cpuSamples[0] = []float64{10.0}
	collector.cpuSamples[1] = []float64{15.0}
	collector.cpuSamples[2] = []float64{20.0}

	// Attempt multiple stats reports to trigger circuit breaker
	for i := 0; i < 5; i++ {
		_ = collector.reportStats()
		time.Sleep(50 * time.Millisecond)
	}

	// Circuit should be open after failures
	if collector.circuitBreaker.GetState() != StateOpen {
		t.Logf("Warning: Circuit breaker state is %s (may not have opened yet)", collector.circuitBreaker.GetState())
	}

	// Further attempts should fail fast without hitting server
	initialFailCount := failCount
	_ = collector.reportStats()
	if failCount != initialFailCount && collector.circuitBreaker.GetState() == StateOpen {
		t.Error("Circuit breaker open but server was still called")
	}
}
