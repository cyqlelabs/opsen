package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"cyqle.in/opsen/common"
)

// invalidURLCollector returns a collector whose server URL cannot be turned
// into an HTTP request, exercising the request-construction error paths.
func invalidURLCollector() *MetricsCollector {
	return &MetricsCollector{
		config: Config{
			// A control character in the URL makes http.NewRequest fail.
			ServerURL:       "http://invalid\x7fhost",
			ClientID:        "bad-url-client",
			Hostname:        "bad-url-host",
			DiskPath:        "/",
			SkipGeolocation: true,
		},
		httpClient:    &http.Client{Timeout: time.Second},
		cpuSamples:    make([][]float64, 1),
		memorySamples: make([]float64, 1),
		diskSamples:   make([]float64, 1),
		gpuCollector:  &GPUCollector{sampleWindow: make([][]common.GPUStats, 1), maxSamples: 1},
		maxSamples:    1,
	}
}

func TestRegister_InvalidServerURL(t *testing.T) {
	err := invalidURLCollector().register()
	if err == nil {
		t.Fatal("expected register to fail for an unbuildable request URL")
	}
}

func TestReportStats_InvalidServerURL(t *testing.T) {
	err := invalidURLCollector().reportStats()
	if err == nil {
		t.Fatal("expected reportStats to fail for an unbuildable request URL")
	}
	if !strings.Contains(err.Error(), "failed to create request") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestReportStats_UnreachableServer(t *testing.T) {
	// Start and immediately stop a server so the port is closed.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	url := server.URL
	server.Close()

	collector := newTestCollector(t, url)

	err := collector.reportStats()
	if err == nil {
		t.Fatal("expected reportStats to fail against a closed server")
	}
	if !strings.Contains(err.Error(), "http request failed") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestReportStats_NonOKStatusIncludesBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "tier unknown", http.StatusBadRequest)
	}))
	defer server.Close()

	collector := newTestCollector(t, server.URL)

	err := collector.reportStats()
	if err == nil {
		t.Fatal("expected reportStats to fail on a non-200 response")
	}
	if !strings.Contains(err.Error(), "tier unknown") {
		t.Errorf("expected the response body in the error, got %v", err)
	}
}

func TestReportStats_SendsAPIKeyHeader(t *testing.T) {
	var gotKey string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotKey = r.Header.Get("X-API-Key")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	collector := newTestCollector(t, server.URL)
	collector.config.ServerKey = "s3cret"

	if err := collector.reportStats(); err != nil {
		t.Fatalf("reportStats returned error: %v", err)
	}
	if gotKey != "s3cret" {
		t.Errorf("expected the server key to be sent, got %q", gotKey)
	}
}

func TestReportStats_IncludesGPUStats(t *testing.T) {
	stub := stubWithDevices("GPU-0", "GPU-1")
	stub.install(t)

	var received common.ResourceStats
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := decodeJSON(r, &received); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	collector := newTestCollector(t, server.URL)
	collector.gpuCollector = NewGPUCollector(3)
	defer collector.gpuCollector.Close()

	if err := collector.gpuCollector.CollectSample(); err != nil {
		t.Fatalf("CollectSample returned error: %v", err)
	}

	if err := collector.reportStats(); err != nil {
		t.Fatalf("reportStats returned error: %v", err)
	}

	if len(received.GPUs) != 2 {
		t.Fatalf("expected 2 GPUs in the reported stats, got %d", len(received.GPUs))
	}
	if received.GPUs[0].UtilizationPct != 50 {
		t.Errorf("expected 50%% utilization, got %.1f", received.GPUs[0].UtilizationPct)
	}
}

func TestRegister_IncludesGPUInventory(t *testing.T) {
	stub := stubWithDevices("Tesla-T4", "Tesla-T4")
	stub.install(t)

	var received common.ClientRegistration
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := decodeJSON(r, &received); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	collector := newTestCollector(t, server.URL)
	collector.gpuCollector = NewGPUCollector(3)
	defer collector.gpuCollector.Close()

	if err := collector.register(); err != nil {
		t.Fatalf("register returned error: %v", err)
	}

	if received.TotalGPUs != 2 {
		t.Errorf("expected 2 GPUs to be registered, got %d", received.TotalGPUs)
	}
	if len(received.GPUModels) != 2 || received.GPUModels[0] != "Tesla-T4" {
		t.Errorf("unexpected GPU models: %v", received.GPUModels)
	}
}

func TestCalculateCPUAverages_LocksAndAverages(t *testing.T) {
	collector := &MetricsCollector{
		cpuSamples: [][]float64{
			{10, 20},
			{30, 40},
			{}, // empty samples are skipped
		},
		maxSamples: 3,
	}

	averages := collector.calculateCPUAverages()

	if len(averages) != 2 {
		t.Fatalf("expected 2 core averages, got %d", len(averages))
	}
	if averages[0] != 20 || averages[1] != 30 {
		t.Errorf("expected [20 30], got %v", averages)
	}
}

func TestDownloadGeoIPDatabase_UnwritableTarget(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		if _, err := w.Write([]byte("database-bytes")); err != nil {
			t.Errorf("failed to write response: %v", err)
		}
	}))
	defer server.Close()

	collector := newTestCollector(t, server.URL)
	originalURL := geoIPDownloadURL
	geoIPDownloadURL = server.URL + "/GeoLite2-City.mmdb"
	defer func() { geoIPDownloadURL = originalURL }()

	// A path inside a file (rather than a directory) cannot be created.
	dir := t.TempDir()
	blocker := filepath.Join(dir, "not-a-dir")
	if err := os.WriteFile(blocker, []byte("x"), 0644); err != nil {
		t.Fatalf("failed to create blocker file: %v", err)
	}

	err := collector.downloadGeoIPDatabase(filepath.Join(blocker, "GeoLite2-City.mmdb"))
	if err == nil {
		t.Fatal("expected the download to fail when the target cannot be created")
	}
	if !strings.Contains(err.Error(), "failed to create file") {
		t.Errorf("unexpected error: %v", err)
	}
}

// decodeJSON decodes a JSON request body into v.
func decodeJSON(r *http.Request, v interface{}) error {
	defer func() { _ = r.Body.Close() }()
	return json.NewDecoder(r.Body).Decode(v)
}

func TestDownloadGeoIPDatabase_WritesFile(t *testing.T) {
	payload := []byte("fake-geoip-database")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := w.Write(payload); err != nil {
			t.Errorf("failed to write response: %v", err)
		}
	}))
	defer server.Close()

	originalURL := geoIPDownloadURL
	geoIPDownloadURL = server.URL + "/GeoLite2-City.mmdb"
	defer func() { geoIPDownloadURL = originalURL }()

	collector := newTestCollector(t, server.URL)
	target := filepath.Join(t.TempDir(), "GeoLite2-City.mmdb")

	if err := collector.downloadGeoIPDatabase(target); err != nil {
		t.Fatalf("downloadGeoIPDatabase returned error: %v", err)
	}

	written, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("failed to read the downloaded file: %v", err)
	}
	if string(written) != string(payload) {
		t.Errorf("expected %q, got %q", payload, written)
	}
}

func TestDownloadGeoIPDatabase_NonOKStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "gone", http.StatusNotFound)
	}))
	defer server.Close()

	originalURL := geoIPDownloadURL
	geoIPDownloadURL = server.URL + "/GeoLite2-City.mmdb"
	defer func() { geoIPDownloadURL = originalURL }()

	collector := newTestCollector(t, server.URL)
	target := filepath.Join(t.TempDir(), "GeoLite2-City.mmdb")

	err := collector.downloadGeoIPDatabase(target)
	if err == nil {
		t.Fatal("expected an error for a non-200 download response")
	}
	if !strings.Contains(err.Error(), "404") {
		t.Errorf("expected the status code in the error, got %v", err)
	}
	if _, statErr := os.Stat(target); !os.IsNotExist(statErr) {
		t.Error("no file should be created for a failed download")
	}
}

func TestDownloadGeoIPDatabase_UnreachableSource(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	url := server.URL
	server.Close()

	originalURL := geoIPDownloadURL
	geoIPDownloadURL = url + "/GeoLite2-City.mmdb"
	defer func() { geoIPDownloadURL = originalURL }()

	collector := newTestCollector(t, url)

	err := collector.downloadGeoIPDatabase(filepath.Join(t.TempDir(), "db.mmdb"))
	if err == nil {
		t.Fatal("expected an error when the download source is unreachable")
	}
	if !strings.Contains(err.Error(), "failed to download database") {
		t.Errorf("unexpected error: %v", err)
	}
}
