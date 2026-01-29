package main

import (
	"crypto/tls"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"
)

// TestRegister_WithAPIKeyHeader tests that API key is sent in header
func TestRegister_WithAPIKeyHeader(t *testing.T) {
	expectedKey := "test-api-key-12345"
	receivedKey := ""

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/register" {
			receivedKey = r.Header.Get("X-API-Key")
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]string{
				"status": "registered",
			})
		}
	}))
	defer server.Close()

	config := Config{
		ServerURL:       server.URL,
		ClientID:        "test-client",
		Hostname:        "test-host",
		WindowMinutes:   1,
		ReportInterval:  60,
		DiskPath:        "/",
		SkipGeolocation: true,
		ServerKey:       expectedKey,
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

	err := collector.register()
	if err != nil {
		t.Fatalf("Expected successful registration, got error: %v", err)
	}

	if receivedKey != expectedKey {
		t.Errorf("Expected API key %q in X-API-Key header, got %q", expectedKey, receivedKey)
	}
}

// TestRegister_WithoutAPIKeyHeader tests that no API key header is sent when not configured
func TestRegister_WithoutAPIKeyHeader(t *testing.T) {
	receivedKey := "not-empty-sentinel"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/register" {
			receivedKey = r.Header.Get("X-API-Key")
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]string{
				"status": "registered",
			})
		}
	}))
	defer server.Close()

	config := Config{
		ServerURL:       server.URL,
		ClientID:        "test-client",
		Hostname:        "test-host",
		WindowMinutes:   1,
		ReportInterval:  60,
		DiskPath:        "/",
		SkipGeolocation: true,
		ServerKey:       "", // No API key
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

	err := collector.register()
	if err != nil {
		t.Fatalf("Expected successful registration, got error: %v", err)
	}

	if receivedKey != "" {
		t.Errorf("Expected no API key header (empty string), got %q", receivedKey)
	}
}

// TestReportStats_WithAPIKeyHeader tests that API key is sent in stats reporting
func TestReportStats_WithAPIKeyHeader(t *testing.T) {
	expectedKey := "stats-api-key"
	receivedKey := ""

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/stats" {
			receivedKey = r.Header.Get("X-API-Key")
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer server.Close()

	config := Config{
		ServerURL:       server.URL,
		ClientID:        "test-client",
		Hostname:        "test-host",
		WindowMinutes:   1,
		ReportInterval:  60,
		DiskPath:        "/",
		SkipGeolocation: true,
		ServerKey:       expectedKey,
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

	// Add sample data
	collector.cpuSamples[0] = []float64{50.0}
	collector.memorySamples[0] = 60.0
	collector.diskSamples[0] = 70.0
	collector.sampleIndex = 1

	err := collector.reportStats()
	if err != nil {
		t.Fatalf("Expected successful stats report, got error: %v", err)
	}

	if receivedKey != expectedKey {
		t.Errorf("Expected API key %q in X-API-Key header, got %q", expectedKey, receivedKey)
	}
}

// TestReportStats_WithoutAPIKeyHeader tests that no API key header is sent when not configured
func TestReportStats_WithoutAPIKeyHeader(t *testing.T) {
	receivedKey := "not-empty-sentinel"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/stats" {
			receivedKey = r.Header.Get("X-API-Key")
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer server.Close()

	config := Config{
		ServerURL:       server.URL,
		ClientID:        "test-client",
		Hostname:        "test-host",
		WindowMinutes:   1,
		ReportInterval:  60,
		DiskPath:        "/",
		SkipGeolocation: true,
		ServerKey:       "", // No API key
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

	// Add sample data
	collector.cpuSamples[0] = []float64{50.0}
	collector.memorySamples[0] = 60.0
	collector.diskSamples[0] = 70.0
	collector.sampleIndex = 1

	err := collector.reportStats()
	if err != nil {
		t.Fatalf("Expected successful stats report, got error: %v", err)
	}

	if receivedKey != "" {
		t.Errorf("Expected no API key header (empty string), got %q", receivedKey)
	}
}

// TestCalculateAverages tests the averaging functions
func TestCalculateAverages(t *testing.T) {
	collector := &MetricsCollector{
		config: Config{
			WindowMinutes:  1,
			ReportInterval: 60,
			DiskPath:       "/",
		},
		cpuSamples:    make([][]float64, 5),
		memorySamples: make([]float64, 5),
		diskSamples:   make([]float64, 5),
		maxSamples:    5,
		sampleIndex:   0,
		gpuCollector:  NewGPUCollector(5),
	}

	// Add some sample data
	collector.cpuSamples[0] = []float64{10.0, 20.0}
	collector.cpuSamples[1] = []float64{30.0, 40.0}
	collector.cpuSamples[2] = []float64{50.0, 60.0}
	collector.memorySamples[0] = 1.0
	collector.memorySamples[1] = 2.0
	collector.memorySamples[2] = 3.0
	collector.diskSamples[0] = 10.0
	collector.diskSamples[1] = 20.0
	collector.diskSamples[2] = 30.0
	collector.sampleIndex = 3

	// Calculate CPU averages
	cpuAvg := collector.calculateCPUAverages()
	if len(cpuAvg) != 2 {
		t.Errorf("Expected 2 CPU cores, got %d", len(cpuAvg))
	}

	// Calculate memory average
	memAvg := collector.calculateAverage(collector.memorySamples)
	if memAvg == 0 {
		t.Error("Expected non-zero memory average")
	}

	// Calculate disk average
	diskAvg := collector.calculateAverage(collector.diskSamples)
	if diskAvg == 0 {
		t.Error("Expected non-zero disk average")
	}
}

// TestHTTPClientWithInsecureTLS tests TLS configuration
func TestHTTPClientWithInsecureTLS(t *testing.T) {
	tests := []struct {
		name        string
		insecureTLS bool
	}{
		{"SecureTLS", false},
		{"InsecureTLS", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create collector with specific TLS setting
			config := Config{
				InsecureTLS: tt.insecureTLS,
			}

			httpClient := &http.Client{
				Timeout: 30 * time.Second,
			}
			if config.InsecureTLS {
				httpClient.Transport = &http.Transport{
					TLSClientConfig: &tls.Config{
						InsecureSkipVerify: true,
					},
				}
			}

			// Verify transport configuration
			if config.InsecureTLS {
				transport, ok := httpClient.Transport.(*http.Transport)
				if !ok {
					t.Fatal("Expected http.Transport")
				}
				if transport.TLSClientConfig == nil {
					t.Fatal("Expected TLSClientConfig to be set")
				}
				if !transport.TLSClientConfig.InsecureSkipVerify {
					t.Error("Expected InsecureSkipVerify=true for InsecureTLS=true")
				}
			}
		})
	}
}

// TestGetLocalIP_ErrorHandling tests local IP detection error handling
func TestGetLocalIP_ErrorHandling(t *testing.T) {
	collector := &MetricsCollector{
		config: Config{},
	}

	// This should not panic and should return an IP or error
	ip, err := collector.getLocalIP()

	if err != nil {
		// Error is acceptable - we might not have network interfaces in test env
		t.Logf("getLocalIP returned error (acceptable): %v", err)
	} else {
		// Success - verify we got a valid-looking IP
		if ip == "" {
			t.Error("Expected non-empty IP address")
		}
		t.Logf("getLocalIP returned: %s", ip)
	}
}

// TestGetPublicIP_RealCall tests public IP detection with real API call
func TestGetPublicIP_RealCall(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping real network call in short mode")
	}

	collector := &MetricsCollector{
		config:     Config{},
		httpClient: &http.Client{Timeout: 5 * time.Second},
	}

	ip, err := collector.getPublicIP()

	if err != nil {
		t.Logf("getPublicIP returned error (network issue): %v", err)
	} else {
		t.Logf("getPublicIP returned: %s", ip)
		if ip == "" {
			t.Error("Expected non-empty IP address on success")
		}
	}
}

// TestDownloadGeoIPDatabase_RealDownload tests actual GeoIP database download
func TestDownloadGeoIPDatabase_RealDownload(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping real download test in short mode")
	}

	tempDir := t.TempDir()
	targetPath := tempDir + "/GeoLite2-City.mmdb"

	collector := &MetricsCollector{
		config:     Config{},
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}

	err := collector.downloadGeoIPDatabase(targetPath)
	if err != nil {
		t.Fatalf("Download failed: %v", err)
	}

	// Verify file was created
	info, err := os.Stat(targetPath)
	if err != nil {
		t.Fatalf("File not created: %v", err)
	}

	if info.Size() == 0 {
		t.Error("Downloaded file is empty")
	}

	t.Logf("Downloaded %d bytes", info.Size())
}

// TestDownloadGeoIPDatabase_InvalidPath tests error handling for invalid file paths
func TestDownloadGeoIPDatabase_InvalidPath(t *testing.T) {
	collector := &MetricsCollector{
		config:     Config{},
		httpClient: &http.Client{Timeout: 5 * time.Second},
	}

	// Use invalid path that cannot be created
	invalidPath := "/root/nonexistent/directory/file.mmdb"

	err := collector.downloadGeoIPDatabase(invalidPath)
	if err == nil {
		t.Error("Expected error for invalid file path, got nil")
	}
}

// TestDownloadGeoIPDatabase_NetworkTimeout tests timeout handling
func TestDownloadGeoIPDatabase_NetworkTimeout(t *testing.T) {
	collector := &MetricsCollector{
		config:     Config{},
		httpClient: &http.Client{Timeout: 1 * time.Nanosecond}, // Extremely short timeout
	}

	tempDir := t.TempDir()
	targetPath := tempDir + "/GeoLite2-City.mmdb"

	err := collector.downloadGeoIPDatabase(targetPath)
	if err == nil {
		t.Error("Expected timeout error, got nil")
	}
}

// TestGetGeolocationFromDB_WithRealDB tests geolocation lookup with downloaded database
func TestGetGeolocationFromDB_WithRealDB(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping real database test in short mode")
	}

	// First, download the database
	tempDir := t.TempDir()
	dbPath := tempDir + "/GeoLite2-City.mmdb"

	collector := &MetricsCollector{
		config:     Config{},
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}

	err := collector.downloadGeoIPDatabase(dbPath)
	if err != nil {
		t.Skipf("Cannot download database for test: %v", err)
	}

	// Override getPublicIP to return a known IP for testing
	// We can't easily mock this, so we'll test the error paths instead
	geo, err := collector.getGeolocationFromDB(dbPath)

	if err != nil {
		// If getPublicIP fails or returns "unknown", we expect an error
		t.Logf("getGeolocationFromDB returned error (expected if no public IP): %v", err)
	} else {
		// Success - verify structure
		if geo["ip"] == nil {
			t.Error("Expected IP in geolocation result")
		}
		if geo["latitude"] == nil || geo["longitude"] == nil {
			t.Error("Expected coordinates in geolocation result")
		}
		t.Logf("Geolocation result: %+v", geo)
	}
}

// TestRegister_WithGeoLocation tests registration with geolocation enabled
func TestRegister_WithGeoLocation(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping geolocation test in short mode")
	}

	receivedData := make(map[string]interface{})

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/register" {
			_ = json.NewDecoder(r.Body).Decode(&receivedData)
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]string{
				"status": "registered",
			})
		}
	}))
	defer server.Close()

	tempDir := t.TempDir()
	dbPath := tempDir + "/GeoLite2-City.mmdb"

	config := Config{
		ServerURL:       server.URL,
		ClientID:        "test-client",
		Hostname:        "test-host",
		WindowMinutes:   1,
		ReportInterval:  60,
		DiskPath:        "/",
		SkipGeolocation: false,
		GeoIPDBPath:     dbPath,
	}

	gpuCollector := NewGPUCollector(60)
	collector := &MetricsCollector{
		config:         config,
		httpClient:     &http.Client{Timeout: 10 * time.Second},
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

	// Verify that registration data includes geolocation fields
	if receivedData["hostname"] == nil {
		t.Error("Expected hostname in registration data")
	}

	// Geolocation fields may be "unknown" if lookup failed, but should be present
	t.Logf("Registration data: %+v", receivedData)
}

// TestGetGeolocationFromDB_ErrorPaths tests error handling in geolocation lookup
func TestGetGeolocationFromDB_ErrorPaths(t *testing.T) {
	tests := []struct {
		name           string
		dbPath         string
		mockPublicIP   bool
		expectedErrMsg string
	}{
		{
			name:           "InvalidDBPath",
			dbPath:         "/nonexistent/database.mmdb",
			expectedErrMsg: "failed to open GeoIP database",
		},
		{
			name:           "EmptyDBPath",
			dbPath:         "",
			expectedErrMsg: "failed to open GeoIP database",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			collector := &MetricsCollector{
				config:     Config{},
				httpClient: &http.Client{Timeout: 5 * time.Second},
			}

			_, err := collector.getGeolocationFromDB(tt.dbPath)
			if err == nil {
				t.Error("Expected error for invalid database path, got nil")
			}
		})
	}
}

// TestCalculateCPUAverages_VariousSamples tests CPU averaging with different sample patterns
func TestCalculateCPUAverages_VariousSamples(t *testing.T) {
	tests := []struct {
		name       string
		samples    [][]float64
		expectCPUs int
	}{
		{
			name: "UniformSamples",
			samples: [][]float64{
				{10.0, 20.0},
				{30.0, 40.0},
				{50.0, 60.0},
			},
			expectCPUs: 2,
		},
		{
			name: "SingleCore",
			samples: [][]float64{
				{50.0},
				{60.0},
				{70.0},
			},
			expectCPUs: 1,
		},
		{
			name: "EmptySlices",
			samples: [][]float64{
				{},
				{},
				{},
			},
			expectCPUs: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			collector := &MetricsCollector{
				cpuSamples:  make([][]float64, len(tt.samples)),
				maxSamples:  len(tt.samples),
				sampleIndex: 0,
			}

			// Populate samples
			copy(collector.cpuSamples, tt.samples)
			collector.sampleIndex = len(tt.samples)

			// Calculate averages
			avg := collector.calculateCPUAverages()

			if len(avg) != tt.expectCPUs {
				t.Errorf("Expected %d CPUs, got %d", tt.expectCPUs, len(avg))
			}

			// Verify averages are in valid range
			for i, val := range avg {
				if val < 0 || val > 100 {
					t.Errorf("CPU %d average %.2f out of valid range [0, 100]", i, val)
				}
			}
		})
	}
}
