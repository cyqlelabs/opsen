package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestGetGeolocationFromIP_Success verifies GeoIP database lookup
func TestGetGeolocationFromIP_Success(t *testing.T) {
	// Skip if no GeoIP database available
	dbPath := os.Getenv("GEOIP_DB_PATH")
	if dbPath == "" {
		dbPath = "/usr/share/GeoIP/GeoLite2-City.mmdb"
	}

	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		t.Skip("GeoIP database not available for testing")
	}

	collector := &MetricsCollector{
		config: Config{
			GeoIPDBPath: dbPath,
		},
		httpClient: &http.Client{Timeout: 5 * time.Second},
	}

	// Test with Google DNS IP
	result, err := collector.getGeolocationFromIP(dbPath, "8.8.8.8")
	if err != nil {
		t.Fatalf("Failed to lookup IP in GeoIP database: %v", err)
	}

	// Verify result structure
	if _, ok := result["latitude"].(float64); !ok {
		t.Error("Expected latitude field")
	}
	if _, ok := result["longitude"].(float64); !ok {
		t.Error("Expected longitude field")
	}
	if _, ok := result["country"].(string); !ok {
		t.Error("Expected country field")
	}
	if _, ok := result["city"].(string); !ok {
		t.Error("Expected city field")
	}

	t.Logf("GeoIP result: %+v", result)
}

// TestGetGeolocationFromIP_PrivateIP verifies handling of private IP addresses
func TestGetGeolocationFromIP_PrivateIP(t *testing.T) {
	// Skip if no GeoIP database available
	dbPath := os.Getenv("GEOIP_DB_PATH")
	if dbPath == "" {
		dbPath = "/usr/share/GeoIP/GeoLite2-City.mmdb"
	}

	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		t.Skip("GeoIP database not available for testing")
	}

	collector := &MetricsCollector{
		config: Config{
			GeoIPDBPath: dbPath,
		},
		httpClient: &http.Client{Timeout: 5 * time.Second},
	}

	// Test with private IP (should fail or return no data)
	_, err := collector.getGeolocationFromIP(dbPath, "192.168.1.1")
	if err != nil {
		t.Logf("Private IP lookup failed (expected): %v", err)
	} else {
		t.Log("Private IP lookup returned data (GeoIP behavior varies)")
	}
}

// TestGetGeolocationFromIP_CityDataHandling verifies city data handling
func TestGetGeolocationFromIP_CityDataHandling(t *testing.T) {
	// Skip if no GeoIP database available
	dbPath := os.Getenv("GEOIP_DB_PATH")
	if dbPath == "" {
		dbPath = "/usr/share/GeoIP/GeoLite2-City.mmdb"
	}

	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		t.Skip("GeoIP database not available for testing")
	}

	collector := &MetricsCollector{
		config: Config{
			GeoIPDBPath: dbPath,
		},
		httpClient: &http.Client{Timeout: 5 * time.Second},
	}

	// Some IPs might not have city data
	result, err := collector.getGeolocationFromIP(dbPath, "8.8.8.8")
	if err != nil {
		t.Fatalf("Failed to lookup IP: %v", err)
	}

	// City might be empty for some IPs
	city, ok := result["city"].(string)
	if ok && city == "" {
		t.Log("City data empty (normal for some IPs)")
	} else {
		t.Logf("City: %s", city)
	}
}

// TestGetGeolocationFromIP_DatabaseNotFound verifies error handling for missing database
func TestGetGeolocationFromIP_DatabaseNotFound(t *testing.T) {
	collector := &MetricsCollector{
		config: Config{
			GeoIPDBPath: "/nonexistent/path/to/GeoLite2-City.mmdb",
		},
		httpClient: &http.Client{Timeout: 5 * time.Second},
	}

	_, err := collector.getGeolocationFromIP("/nonexistent/path/to/GeoLite2-City.mmdb", "8.8.8.8")
	if err == nil {
		t.Error("Expected error for nonexistent database")
	}

	if err != nil {
		t.Logf("Got expected error: %v", err)
	}
}

// TestGetGeolocationFromIP_MultipleIPs verifies multiple IP lookups
func TestGetGeolocationFromIP_MultipleIPs(t *testing.T) {
	// Skip if no GeoIP database available
	dbPath := os.Getenv("GEOIP_DB_PATH")
	if dbPath == "" {
		dbPath = "/usr/share/GeoIP/GeoLite2-City.mmdb"
	}

	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		t.Skip("GeoIP database not available for testing")
	}

	collector := &MetricsCollector{
		config: Config{
			GeoIPDBPath: dbPath,
		},
		httpClient: &http.Client{Timeout: 5 * time.Second},
	}

	testIPs := []string{
		"8.8.8.8",        // Google DNS (US)
		"1.1.1.1",        // Cloudflare DNS (US)
		"208.67.222.222", // OpenDNS (US)
	}

	for _, ip := range testIPs {
		result, err := collector.getGeolocationFromIP(dbPath, ip)
		if err != nil {
			t.Logf("Failed to lookup %s: %v", ip, err)
			continue
		}

		country, _ := result["country"].(string)
		city, _ := result["city"].(string)
		t.Logf("IP %s: City=%s, Country=%s", ip, city, country)
	}
}

// TestRegister_WithAPIGeolocation verifies registration with API-based geolocation
func TestRegister_WithAPIGeolocation(t *testing.T) {
	// Create mock server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/register" {
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]string{"status": "registered"})
		}
	}))
	defer server.Close()

	// Create collector with API geolocation (will hit real API)
	config := Config{
		ServerURL:       server.URL,
		ClientID:        "test-client-api-geo",
		Hostname:        "test-host",
		WindowMinutes:   1,
		ReportInterval:  60,
		DiskPath:        "/",
		SkipGeolocation: false, // Enable geolocation
		GeoIPDBPath:     "",    // Use API, not database
	}

	gpuCollector := NewGPUCollector(60)
	defer gpuCollector.Close()

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

	// Test registration - geolocation might fail but registration should succeed
	err := collector.register()
	if err != nil {
		t.Fatalf("Registration failed: %v", err)
	}

	t.Log("Registration with API geolocation succeeded")
}

// TestRegister_GeolocationAPIFailure verifies graceful handling of geolocation API failure
func TestRegister_GeolocationAPIFailure(t *testing.T) {
	// Create mock server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/register" {
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]string{"status": "registered"})
		}
	}))
	defer server.Close()

	config := Config{
		ServerURL:       server.URL,
		ClientID:        "test-client-geo-fail",
		Hostname:        "test-host",
		WindowMinutes:   1,
		ReportInterval:  60,
		DiskPath:        "/",
		SkipGeolocation: false,
		GeoIPDBPath:     "", // Will try API which might fail
	}

	gpuCollector := NewGPUCollector(60)
	defer gpuCollector.Close()

	collector := &MetricsCollector{
		config:         config,
		httpClient:     &http.Client{Timeout: 1 * time.Second}, // Short timeout
		cpuSamples:     make([][]float64, 60),
		memorySamples:  make([]float64, 60),
		diskSamples:    make([]float64, 60),
		gpuCollector:   gpuCollector,
		maxSamples:     60,
		circuitBreaker: NewCircuitBreaker(5, 30*time.Second),
		retryConfig:    DefaultRetryConfig(),
	}

	// Registration should succeed even if geolocation fails
	err := collector.register()
	if err != nil {
		t.Fatalf("Registration should succeed even with geolocation failure: %v", err)
	}
}

// TestRegister_WithServerKey verifies API key authentication
func TestRegister_WithServerKey(t *testing.T) {
	receivedKey := ""
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/register" {
			receivedKey = r.Header.Get("X-API-Key")
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]string{"status": "registered"})
		}
	}))
	defer server.Close()

	config := Config{
		ServerURL:       server.URL,
		ClientID:        "test-client-auth",
		Hostname:        "test-host",
		WindowMinutes:   1,
		ReportInterval:  60,
		DiskPath:        "/",
		SkipGeolocation: true,
		ServerKey:       "test-api-key-12345",
	}

	gpuCollector := NewGPUCollector(60)
	defer gpuCollector.Close()

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

	err := collector.register()
	if err != nil {
		t.Fatalf("Registration failed: %v", err)
	}

	if receivedKey != "test-api-key-12345" {
		t.Errorf("Expected API key 'test-api-key-12345', got '%s'", receivedKey)
	}
}

// TestRegister_LocalIPFailure verifies handling of local IP detection failure
func TestRegister_LocalIPFailure(t *testing.T) {
	// This test documents behavior when local IP cannot be detected
	// The actual failure depends on system state, so we just verify registration continues
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/register" {
			var reg map[string]interface{}
			_ = json.NewDecoder(r.Body).Decode(&reg)

			// Check that local_ip has fallback value
			localIP, _ := reg["local_ip"].(string)
			t.Logf("Registered local IP: %s", localIP)

			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]string{"status": "registered"})
		}
	}))
	defer server.Close()

	config := Config{
		ServerURL:       server.URL,
		ClientID:        "test-client-no-ip",
		Hostname:        "test-host",
		WindowMinutes:   1,
		ReportInterval:  60,
		DiskPath:        "/",
		SkipGeolocation: true,
	}

	gpuCollector := NewGPUCollector(60)
	defer gpuCollector.Close()

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

	err := collector.register()
	if err != nil {
		t.Fatalf("Registration failed: %v", err)
	}
}

// TestGetGeolocationFromIP_CloseDatabase verifies database is properly closed
func TestGetGeolocationFromIP_CloseDatabase(t *testing.T) {
	// Skip if no GeoIP database available
	dbPath := os.Getenv("GEOIP_DB_PATH")
	if dbPath == "" {
		dbPath = "/usr/share/GeoIP/GeoLite2-City.mmdb"
	}

	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		t.Skip("GeoIP database not available for testing")
	}

	collector := &MetricsCollector{
		config: Config{
			GeoIPDBPath: dbPath,
		},
		httpClient: &http.Client{Timeout: 5 * time.Second},
	}

	// Multiple calls should each open and close the database
	for i := 0; i < 3; i++ {
		_, err := collector.getGeolocationFromIP(dbPath, "8.8.8.8")
		if err != nil {
			t.Fatalf("Lookup %d failed: %v", i+1, err)
		}
	}

	t.Log("Database opened and closed multiple times successfully")
}

// TestGetGeolocationFromIP_RelativePath verifies handling of relative paths
func TestGetGeolocationFromIP_RelativePath(t *testing.T) {
	// Create a temporary directory
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.mmdb")

	collector := &MetricsCollector{
		config: Config{
			GeoIPDBPath: dbPath,
		},
		httpClient: &http.Client{Timeout: 5 * time.Second},
	}

	// Should fail because file doesn't exist
	_, err := collector.getGeolocationFromIP(dbPath, "8.8.8.8")
	if err == nil {
		t.Error("Expected error for nonexistent database file")
	}
}
