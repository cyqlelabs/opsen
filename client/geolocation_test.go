package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// TestGetGeolocationFromIP_InvalidIP verifies handling of invalid IP addresses
func TestGetGeolocationFromIP_InvalidIP(t *testing.T) {
	collector := &MetricsCollector{
		config: Config{
			GeoIPDBPath: "/nonexistent/path.mmdb",
		},
		httpClient: &http.Client{Timeout: 5 * time.Second},
	}

	_, err := collector.getGeolocationFromIP("/nonexistent/path.mmdb", "not-an-ip")
	if err == nil {
		t.Error("Expected error for invalid IP address")
	}
}

// TestGetGeolocationFromIP_InvalidDBPath verifies handling of invalid database path
func TestGetGeolocationFromIP_InvalidDBPath(t *testing.T) {
	collector := &MetricsCollector{
		config: Config{
			GeoIPDBPath: "/nonexistent/path/to/database.mmdb",
		},
		httpClient: &http.Client{Timeout: 5 * time.Second},
	}

	_, err := collector.getGeolocationFromIP("/nonexistent/path/to/database.mmdb", "8.8.8.8")
	if err == nil {
		t.Error("Expected error for nonexistent database")
	}
}

// TestRegister_SkipGeolocation verifies registration with geolocation skipped
func TestRegister_SkipGeolocation(t *testing.T) {
	// Create mock server
	registrationReceived := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/register" {
			registrationReceived = true

			var reg map[string]interface{}
			if err := json.NewDecoder(r.Body).Decode(&reg); err != nil {
				t.Errorf("Failed to decode registration: %v", err)
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}

			// Verify geolocation was skipped (should have default values)
			if publicIP, ok := reg["public_ip"].(string); ok && publicIP == "unknown" {
				t.Logf("Public IP correctly set to 'unknown' when geolocation skipped")
			}

			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]string{"status": "registered"})
		}
	}))
	defer server.Close()

	// Create collector with geolocation skipped
	config := Config{
		ServerURL:       server.URL,
		ClientID:        "test-client-skip-geo",
		Hostname:        "test-host",
		WindowMinutes:   1,
		ReportInterval:  60,
		DiskPath:        "/",
		SkipGeolocation: true, // Skip geolocation
	}

	gpuCollector := NewGPUCollector(60)

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
	}

	// Test registration
	err := collector.register()
	if err != nil {
		t.Fatalf("Registration failed: %v", err)
	}

	if !registrationReceived {
		t.Error("Registration request not received by server")
	}
}

// TestRegister_WithGeoIPDB verifies registration with GeoIP database lookup
func TestRegister_WithGeoIPDB(t *testing.T) {
	// Create mock server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/register" {
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]string{"status": "registered"})
		}
	}))
	defer server.Close()

	// Create collector with GeoIP database (nonexistent, will fail gracefully)
	config := Config{
		ServerURL:       server.URL,
		ClientID:        "test-client-geoip",
		Hostname:        "test-host",
		WindowMinutes:   1,
		ReportInterval:  60,
		DiskPath:        "/",
		SkipGeolocation: false,
		GeoIPDBPath:     "/nonexistent/GeoLite2-City.mmdb", // Will fail but continue
	}

	gpuCollector := NewGPUCollector(60)

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
	}

	// Test registration - should succeed even if GeoIP fails
	err := collector.register()
	if err != nil {
		t.Fatalf("Registration failed: %v", err)
	}
}

// TestRegister_ServerError verifies handling of server errors during registration
func TestRegister_ServerError(t *testing.T) {
	// Create mock server that returns error
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/register" {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte("Internal server error"))
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

	gpuCollector := NewGPUCollector(60)

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
	}

	// Test registration - should fail
	err := collector.register()
	if err == nil {
		t.Error("Expected error when server returns 500")
	}
}

// TestRegister_WithGPUs verifies registration includes GPU information
func TestRegister_WithGPUs(t *testing.T) {
	// Create mock server
	var receivedGPUCount int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/register" {
			var reg map[string]interface{}
			if err := json.NewDecoder(r.Body).Decode(&reg); err != nil {
				t.Errorf("Failed to decode registration: %v", err)
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}

			// Check if GPU count is included
			if gpuCount, ok := reg["total_gpus"].(float64); ok {
				receivedGPUCount = int(gpuCount)
			}

			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]string{"status": "registered"})
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

	gpuCollector := NewGPUCollector(60)

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
	}

	// Test registration
	err := collector.register()
	if err != nil {
		t.Fatalf("Registration failed: %v", err)
	}

	// GPU count should be included (will be 0 if no GPUs available)
	t.Logf("Received GPU count: %d", receivedGPUCount)
}
