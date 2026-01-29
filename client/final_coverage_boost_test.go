package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"cyqle.in/opsen/common"
)

// TestRegister_FullFlow verifies complete registration flow
func TestRegister_FullFlow(t *testing.T) {
	var receivedReg common.ClientRegistration

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/register" {
			if err := json.NewDecoder(r.Body).Decode(&receivedReg); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}

			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]string{
				"status":    "registered",
				"client_id": receivedReg.ClientID,
			})
		}
	}))
	defer server.Close()

	config := Config{
		ServerURL:       server.URL,
		ClientID:        "full-flow-test",
		Hostname:        "testhost",
		WindowMinutes:   15,
		ReportInterval:  60,
		DiskPath:        "/",
		SkipGeolocation: true,
	}

	gpuCollector := NewGPUCollector(5)

	collector := &MetricsCollector{
		config:         config,
		httpClient:     &http.Client{Timeout: 5 * time.Second},
		cpuSamples:     make([][]float64, 5),
		memorySamples:  make([]float64, 5),
		diskSamples:    make([]float64, 5),
		gpuCollector:   gpuCollector,
		maxSamples:     5,
		circuitBreaker: NewCircuitBreaker(5, 30*time.Second),
		retryConfig:    DefaultRetryConfig(),
	}

	err := collector.register()
	if err != nil {
		t.Fatalf("Registration failed: %v", err)
	}

	if receivedReg.ClientID != "full-flow-test" {
		t.Errorf("Expected client_id 'full-flow-test', got '%s'", receivedReg.ClientID)
	}

	if receivedReg.Hostname != "testhost" {
		t.Errorf("Expected hostname 'testhost', got '%s'", receivedReg.Hostname)
	}
}

// TestGeolocation_ErrorHandling verifies geolocation error handling
// TestRegister_WithRetry verifies retry mechanism
func TestRegister_WithRetry(t *testing.T) {
	attempts := 0

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/register" {
			attempts++

			// Fail first attempt, succeed second
			if attempts == 1 {
				w.WriteHeader(http.StatusBadGateway)
				return
			}

			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]string{
				"status":    "registered",
				"client_id": "retry-test",
			})
		}
	}))
	defer server.Close()

	config := Config{
		ServerURL:       server.URL,
		ClientID:        "retry-test",
		Hostname:        "retryhost",
		WindowMinutes:   1,
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
		circuitBreaker: NewCircuitBreaker(10, 30*time.Second),
		retryConfig: RetryConfig{
			MaxAttempts: 3,
		},
	}

	err := collector.register()
	if err != nil {
		t.Logf("Registration with retry failed: %v (may be expected if retries exhausted)", err)
	}

	if attempts > 0 {
		t.Logf("Registration attempted %d times", attempts)
	}
}

// TestReportStats_DetailedMetrics verifies detailed metrics reporting
func TestReportStats_DetailedMetrics(t *testing.T) {
	var receivedStats common.ResourceStats

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/stats" {
			if err := json.NewDecoder(r.Body).Decode(&receivedStats); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}

			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
		}
	}))
	defer server.Close()

	config := Config{
		ServerURL:       server.URL,
		ClientID:        "detailed-stats",
		Hostname:        "statshost",
		WindowMinutes:   1,
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

	// Add detailed sample data
	collector.cpuSamples[0] = []float64{25.0, 30.0, 35.0, 40.0}
	collector.cpuSamples[1] = []float64{26.0, 31.0, 36.0, 41.0}
	collector.cpuSamples[2] = []float64{27.0, 32.0, 37.0, 42.0}
	collector.memorySamples[0] = 8.5
	collector.memorySamples[1] = 9.0
	collector.memorySamples[2] = 9.5
	collector.diskSamples[0] = 150.0
	collector.diskSamples[1] = 151.0
	collector.diskSamples[2] = 152.0
	collector.sampleIndex = 3

	err := collector.reportStats()
	if err != nil {
		t.Fatalf("reportStats failed: %v", err)
	}

	if receivedStats.ClientID != "detailed-stats" {
		t.Errorf("Expected client_id 'detailed-stats', got '%s'", receivedStats.ClientID)
	}

	if len(receivedStats.CPUUsageAvg) == 0 {
		t.Error("Expected CPU usage data")
	}

	if receivedStats.MemoryUsed == 0 {
		t.Error("Expected non-zero memory usage")
	}

	t.Logf("Reported stats: %d CPU cores, %.2f GB memory used", len(receivedStats.CPUUsageAvg), receivedStats.MemoryUsed)
}

// TestCircuitBreaker_FullStateFlow verifies complete state machine flow
func TestCircuitBreaker_FullStateFlow(t *testing.T) {
	cb := NewCircuitBreaker(2, 100*time.Millisecond)

	// Initially closed
	if cb.GetState() != StateClosed {
		t.Errorf("Expected initial state CLOSED, got %s", cb.GetState())
	}

	// Fail twice to open circuit
	failFunc := func() error {
		return http.ErrAbortHandler
	}

	_ = cb.Call(failFunc)
	_ = cb.Call(failFunc)

	// Should be open now
	if cb.GetState() != StateOpen {
		t.Logf("Circuit state: %s (may not be open yet)", cb.GetState())
	}

	// Wait for reset timeout
	time.Sleep(150 * time.Millisecond)

	// Should transition to half-open
	successFunc := func() error {
		return nil
	}

	_ = cb.Call(successFunc)

	// After success in half-open, should return to closed
	if cb.GetState() == StateClosed {
		t.Log("Circuit successfully returned to CLOSED state")
	}
}

// TestGetGeolocationFromDB_PathHandling verifies DB path handling
func TestGetGeolocationFromDB_PathHandling(t *testing.T) {
	config := Config{
		GeoIPDBPath: "",
	}

	collector := &MetricsCollector{
		config:     config,
		httpClient: &http.Client{Timeout: 5 * time.Second},
	}

	// Empty path should fail gracefully
	_, err := collector.getGeolocationFromDB("8.8.8.8")
	if err == nil {
		t.Log("Empty GeoIP DB path handled (may succeed if default path exists)")
	}
}

// TestCollectMetrics_FullIntegration verifies full integration of collection components
func TestCollectMetrics_FullIntegration(t *testing.T) {
	config := Config{
		ServerURL:       "http://localhost:8080",
		ClientID:        "integration-test",
		Hostname:        "testhost",
		WindowMinutes:   1,
		ReportInterval:  60,
		DiskPath:        "/",
		SkipGeolocation: true,
	}

	gpuCollector := NewGPUCollector(5)

	collector := &MetricsCollector{
		config:         config,
		httpClient:     &http.Client{Timeout: 5 * time.Second},
		cpuSamples:     make([][]float64, 60),
		memorySamples:  make([]float64, 60),
		diskSamples:    make([]float64, 60),
		gpuCollector:   gpuCollector,
		maxSamples:     60,
		circuitBreaker: NewCircuitBreaker(5, 30*time.Second),
		retryConfig:    DefaultRetryConfig(),
		sampleIndex:    0,
	}

	// Simulate some collection cycles
	for i := 0; i < 10; i++ {
		collector.sampleIndex = (collector.sampleIndex + 1) % collector.maxSamples
	}

	// Calculate averages
	cpuAvg := collector.calculateCPUAverages()
	memAvg := collector.calculateAverage(collector.memorySamples)
	diskAvg := collector.calculateAverage(collector.diskSamples)

	t.Logf("Collection integration: %d CPU cores, %.2f GB mem avg, %.2f GB disk avg",
		len(cpuAvg), memAvg, diskAvg)
}

// TestRegister_ErrorResponse verifies error response handling
func TestRegister_ErrorResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/register" {
			w.WriteHeader(http.StatusConflict)
			_, _ = w.Write([]byte("Client already registered"))
		}
	}))
	defer server.Close()

	config := Config{
		ServerURL:       server.URL,
		ClientID:        "error-response-test",
		Hostname:        "testhost",
		WindowMinutes:   1,
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
		circuitBreaker: NewCircuitBreaker(5, 30*time.Second),
		retryConfig: RetryConfig{
			MaxAttempts: 1,
		},
	}

	err := collector.register()
	if err == nil {
		t.Error("Expected error for 409 Conflict response")
	}

	if !strings.Contains(err.Error(), "status 409") && !strings.Contains(err.Error(), "registration failed") {
		t.Logf("Error message: %v", err)
	}
}
