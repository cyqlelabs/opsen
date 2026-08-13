package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"cyqle.in/opsen/common"
)

func TestParseClientFlags_Defaults(t *testing.T) {
	flags, err := parseClientFlags(nil)
	if err != nil {
		t.Fatalf("parseClientFlags returned error: %v", err)
	}

	if flags.configFile != "" || flags.serverURL != "" || flags.diskPath != "" || flags.clientID != "" {
		t.Errorf("expected empty string defaults, got %+v", flags)
	}
	if flags.windowMinutes != 0 || flags.reportInterval != 0 {
		t.Errorf("expected zero numeric defaults, got %+v", flags)
	}
}

func TestParseClientFlags_AllValues(t *testing.T) {
	flags, err := parseClientFlags([]string{
		"-config", "/etc/opsen/client.yml",
		"-server", "https://lb.example.com",
		"-window", "30",
		"-interval", "45",
		"-disk", "/data",
		"-id", "client-42",
	})
	if err != nil {
		t.Fatalf("parseClientFlags returned error: %v", err)
	}

	if flags.configFile != "/etc/opsen/client.yml" {
		t.Errorf("unexpected config file: %s", flags.configFile)
	}
	if flags.serverURL != "https://lb.example.com" {
		t.Errorf("unexpected server URL: %s", flags.serverURL)
	}
	if flags.windowMinutes != 30 {
		t.Errorf("unexpected window: %d", flags.windowMinutes)
	}
	if flags.reportInterval != 45 {
		t.Errorf("unexpected interval: %d", flags.reportInterval)
	}
	if flags.diskPath != "/data" {
		t.Errorf("unexpected disk path: %s", flags.diskPath)
	}
	if flags.clientID != "client-42" {
		t.Errorf("unexpected client id: %s", flags.clientID)
	}
}

func TestParseClientFlags_UnknownFlag(t *testing.T) {
	// ContinueOnError writes usage to stderr; silence it for the test run.
	if _, err := parseClientFlags([]string{"-nope"}); err == nil {
		t.Fatal("expected an error for an unknown flag")
	}
}

func TestApplyClientFlagOverrides(t *testing.T) {
	base := func() *common.ClientConfig {
		return &common.ClientConfig{
			ServerURL:      "http://original:8080",
			WindowMinutes:  15,
			ReportInterval: 60,
			DiskPath:       "/",
			ClientID:       "original-id",
		}
	}

	sameAsBase := func(t *testing.T, cfg *common.ClientConfig) {
		t.Helper()
		want := base()
		if cfg.ServerURL != want.ServerURL || cfg.WindowMinutes != want.WindowMinutes ||
			cfg.ReportInterval != want.ReportInterval || cfg.DiskPath != want.DiskPath ||
			cfg.ClientID != want.ClientID {
			t.Errorf("config changed unexpectedly: %+v", cfg)
		}
	}

	t.Run("nil flags leave config untouched", func(t *testing.T) {
		cfg := base()
		applyClientFlagOverrides(cfg, nil)
		sameAsBase(t, cfg)
	})

	t.Run("zero values do not override", func(t *testing.T) {
		cfg := base()
		applyClientFlagOverrides(cfg, &clientFlags{})
		sameAsBase(t, cfg)
	})

	t.Run("set values override", func(t *testing.T) {
		cfg := base()
		applyClientFlagOverrides(cfg, &clientFlags{
			serverURL:      "http://override:9090",
			windowMinutes:  5,
			reportInterval: 10,
			diskPath:       "/mnt",
			clientID:       "override-id",
		})

		if cfg.ServerURL != "http://override:9090" {
			t.Errorf("server URL not overridden: %s", cfg.ServerURL)
		}
		if cfg.WindowMinutes != 5 {
			t.Errorf("window not overridden: %d", cfg.WindowMinutes)
		}
		if cfg.ReportInterval != 10 {
			t.Errorf("interval not overridden: %d", cfg.ReportInterval)
		}
		if cfg.DiskPath != "/mnt" {
			t.Errorf("disk path not overridden: %s", cfg.DiskPath)
		}
		if cfg.ClientID != "override-id" {
			t.Errorf("client id not overridden: %s", cfg.ClientID)
		}
	})
}

func TestLoadClientRuntimeConfig_GeneratesIDAndHostname(t *testing.T) {
	config, err := loadClientRuntimeConfig(nil)
	if err != nil {
		t.Fatalf("loadClientRuntimeConfig returned error: %v", err)
	}

	if config.ClientID == "" {
		t.Error("expected an auto-generated client ID")
	}
	if config.Hostname == "" {
		t.Error("expected the local hostname to be resolved")
	}
	if hostname, _ := os.Hostname(); config.Hostname != hostname {
		t.Errorf("expected hostname %q, got %q", hostname, config.Hostname)
	}
}

func TestLoadClientRuntimeConfig_FromFileWithOverrides(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "client.yml")

	source := &common.ClientConfig{
		ServerURL:       "http://from-file:8080",
		ClientID:        "file-client",
		Hostname:        "file-host",
		WindowMinutes:   20,
		ReportInterval:  90,
		DiskPath:        "/srv",
		EndpointURL:     "http://backend:3000",
		GeoIPDBPath:     "/tmp/geo.mmdb",
		SkipGeolocation: true,
		InsecureTLS:     true,
		ServerKey:       "secret",
	}
	if err := common.SaveClientConfig(source, path); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	config, err := loadClientRuntimeConfig(&clientFlags{
		configFile: path,
		serverURL:  "http://from-flag:9090",
	})
	if err != nil {
		t.Fatalf("loadClientRuntimeConfig returned error: %v", err)
	}

	if config.ServerURL != "http://from-flag:9090" {
		t.Errorf("flag should win over file, got %s", config.ServerURL)
	}
	if config.ClientID != "file-client" {
		t.Errorf("expected client id from file, got %s", config.ClientID)
	}
	if config.Hostname != "file-host" {
		t.Errorf("expected hostname from file, got %s", config.Hostname)
	}
	if config.WindowMinutes != 20 || config.ReportInterval != 90 {
		t.Errorf("unexpected window/interval: %d/%d", config.WindowMinutes, config.ReportInterval)
	}
	if config.DiskPath != "/srv" || config.EndpointURL != "http://backend:3000" {
		t.Errorf("unexpected disk/endpoint: %s / %s", config.DiskPath, config.EndpointURL)
	}
	if !config.SkipGeolocation || !config.InsecureTLS {
		t.Errorf("expected boolean settings to be carried over: %+v", config)
	}
	if config.ServerKey != "secret" || config.GeoIPDBPath != "/tmp/geo.mmdb" {
		t.Errorf("unexpected key/geoip path: %s / %s", config.ServerKey, config.GeoIPDBPath)
	}
}

func TestLoadClientRuntimeConfig_InvalidFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "broken.yml")
	if err := os.WriteFile(path, []byte("server_url: [unterminated"), 0644); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	if _, err := loadClientRuntimeConfig(&clientFlags{configFile: path}); err == nil {
		t.Fatal("expected an error for an unparseable config file")
	}
}

func TestNewHTTPClient(t *testing.T) {
	t.Run("secure by default", func(t *testing.T) {
		client := newHTTPClient(Config{})
		if client.Timeout != 30*time.Second {
			t.Errorf("expected a 30s timeout, got %s", client.Timeout)
		}
		if client.Transport != nil {
			t.Errorf("expected the default transport, got %T", client.Transport)
		}
	})

	t.Run("insecure TLS opt-in", func(t *testing.T) {
		client := newHTTPClient(Config{InsecureTLS: true})
		transport, ok := client.Transport.(*http.Transport)
		if !ok {
			t.Fatalf("expected *http.Transport, got %T", client.Transport)
		}
		if transport.TLSClientConfig == nil || !transport.TLSClientConfig.InsecureSkipVerify {
			t.Errorf("expected InsecureSkipVerify to be set, got %+v", transport.TLSClientConfig)
		}
	})
}

func TestNewMetricsCollector(t *testing.T) {
	config := Config{
		ClientID:      "collector-test",
		WindowMinutes: 2,
		DiskPath:      "/",
	}
	httpClient := newHTTPClient(config)

	collector := newMetricsCollector(config, httpClient)
	defer collector.gpuCollector.Close()

	if collector.maxSamples != 120 {
		t.Errorf("expected 120 samples for a 2 minute window, got %d", collector.maxSamples)
	}
	if len(collector.cpuSamples) != 120 || len(collector.memorySamples) != 120 || len(collector.diskSamples) != 120 {
		t.Errorf("sample buffers sized incorrectly: cpu=%d mem=%d disk=%d",
			len(collector.cpuSamples), len(collector.memorySamples), len(collector.diskSamples))
	}
	if collector.httpClient != httpClient {
		t.Error("expected the supplied HTTP client to be used")
	}
	if collector.circuitBreaker == nil || collector.gpuCollector == nil {
		t.Error("expected a circuit breaker and GPU collector to be created")
	}
	if collector.retryConfig.MaxAttempts != DefaultRetryConfig().MaxAttempts {
		t.Errorf("expected the default retry config, got %+v", collector.retryConfig)
	}
}

func TestCollectSample_StoresMetricsAndAdvances(t *testing.T) {
	collector := &MetricsCollector{
		config:        Config{DiskPath: "/"},
		httpClient:    &http.Client{},
		cpuSamples:    make([][]float64, 2),
		memorySamples: make([]float64, 2),
		diskSamples:   make([]float64, 2),
		gpuCollector:  &GPUCollector{sampleWindow: make([][]common.GPUStats, 2), maxSamples: 2},
		maxSamples:    2,
	}

	collector.collectSample()

	if collector.sampleIndex != 1 {
		t.Errorf("expected sample index 1, got %d", collector.sampleIndex)
	}
	if len(collector.cpuSamples[0]) == 0 {
		t.Error("expected per-core CPU samples to be recorded")
	}
	if collector.memorySamples[0] <= 0 {
		t.Errorf("expected a positive memory sample, got %f", collector.memorySamples[0])
	}

	// Second sample wraps the window back to index 0.
	collector.collectSample()
	if collector.sampleIndex != 0 {
		t.Errorf("expected sample index to wrap to 0, got %d", collector.sampleIndex)
	}
}

func TestCollectSample_CollectsGPUMetricsWhenEnabled(t *testing.T) {
	stub := stubWithDevices("GPU-0")
	stub.install(t)

	collector := &MetricsCollector{
		config:        Config{DiskPath: "/"},
		httpClient:    &http.Client{},
		cpuSamples:    make([][]float64, 2),
		memorySamples: make([]float64, 2),
		diskSamples:   make([]float64, 2),
		gpuCollector:  NewGPUCollector(2),
		maxSamples:    2,
	}
	defer collector.gpuCollector.Close()

	collector.collectSample()

	averages := collector.gpuCollector.CalculateAverages()
	if len(averages) != 1 || averages[0].UtilizationPct != 50 {
		t.Errorf("expected GPU sample to be collected, got %+v", averages)
	}
}

func TestReportStatsWithCircuitBreaker(t *testing.T) {
	var failing atomic.Bool

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if failing.Load() {
			http.Error(w, "boom", http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	collector := newTestCollector(t, server.URL)

	// Successful report: circuit breaker stays closed.
	collector.reportStatsWithCircuitBreaker()
	if state := collector.circuitBreaker.GetState(); state != StateClosed {
		t.Errorf("expected a closed circuit after success, got %s", state)
	}

	// Failing reports are logged, not returned, and eventually open the circuit.
	failing.Store(true)
	for i := 0; i < 3; i++ {
		collector.reportStatsWithCircuitBreaker()
	}
	if collector.circuitBreaker.GetFailures() == 0 {
		t.Error("expected failures to be recorded on the circuit breaker")
	}
	if state := collector.circuitBreaker.GetState(); state != StateOpen {
		t.Errorf("expected the circuit to open after repeated failures, got %s", state)
	}

	// With the circuit open the call is skipped entirely (ErrCircuitOpen branch).
	collector.reportStatsWithCircuitBreaker()
}

// newTestCollector builds a collector pointed at the given server with a
// circuit breaker and retry policy tuned for fast tests.
func newTestCollector(t *testing.T, serverURL string) *MetricsCollector {
	t.Helper()

	config := Config{
		ServerURL:       serverURL,
		ClientID:        "test-client",
		Hostname:        "test-host",
		WindowMinutes:   1,
		ReportInterval:  1,
		DiskPath:        "/",
		SkipGeolocation: true,
	}

	collector := &MetricsCollector{
		config:         config,
		httpClient:     &http.Client{Timeout: 5 * time.Second},
		cpuSamples:     make([][]float64, 3),
		memorySamples:  make([]float64, 3),
		diskSamples:    make([]float64, 3),
		gpuCollector:   &GPUCollector{sampleWindow: make([][]common.GPUStats, 3), maxSamples: 3},
		maxSamples:     3,
		circuitBreaker: NewCircuitBreaker(3, 30*time.Second),
		retryConfig: RetryConfig{
			MaxAttempts:  2,
			InitialDelay: time.Millisecond,
			MaxDelay:     2 * time.Millisecond,
			Multiplier:   2.0,
		},
	}

	return collector
}

func TestRunCollector_RegistersReportsAndStopsOnCancel(t *testing.T) {
	var registrations, reports atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/register":
			registrations.Add(1)
			var reg common.ClientRegistration
			if err := json.NewDecoder(r.Body).Decode(&reg); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			w.WriteHeader(http.StatusOK)
		case "/stats":
			reports.Add(1)
			w.WriteHeader(http.StatusOK)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	collector := newTestCollector(t, server.URL)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errCh := make(chan error, 1)
	go func() { errCh <- runCollector(ctx, collector) }()

	// The reporting ticker fires once per second; wait for the first report.
	deadline := time.After(5 * time.Second)
	for reports.Load() == 0 {
		select {
		case err := <-errCh:
			t.Fatalf("runCollector returned early: %v", err)
		case <-deadline:
			t.Fatal("timed out waiting for the first stats report")
		case <-time.After(20 * time.Millisecond):
		}
	}

	cancel()

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("runCollector returned error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("runCollector did not stop after context cancellation")
	}

	if registrations.Load() != 1 {
		t.Errorf("expected exactly one registration, got %d", registrations.Load())
	}
}

func TestRunCollector_RegistrationFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusUnauthorized)
	}))
	defer server.Close()

	collector := newTestCollector(t, server.URL)

	err := runCollector(context.Background(), collector)
	if err == nil {
		t.Fatal("expected registration failure to be reported")
	}
	if got := err.Error(); got == "" {
		t.Error("expected a descriptive error message")
	}
}

func TestRunClient_EndToEnd(t *testing.T) {
	var registered atomic.Bool

	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/register" {
			registered.Store(true)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	config := Config{
		ServerURL:       server.URL,
		ClientID:        "run-client-test",
		Hostname:        "run-client-host",
		WindowMinutes:   1,
		ReportInterval:  1,
		DiskPath:        "/",
		SkipGeolocation: true,
		// The httptest TLS server uses a self-signed certificate, which
		// exercises the insecure-TLS transport path.
		InsecureTLS: true,
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var wg sync.WaitGroup
	var runErr error
	wg.Add(1)
	go func() {
		defer wg.Done()
		runErr = runClient(ctx, config)
	}()

	deadline := time.After(5 * time.Second)
	for !registered.Load() {
		select {
		case <-deadline:
			t.Fatal("timed out waiting for registration")
		case <-time.After(20 * time.Millisecond):
		}
	}

	cancel()
	wg.Wait()

	if runErr != nil {
		t.Fatalf("runClient returned error: %v", runErr)
	}
}

func TestRunClient_TLSVerificationEnforcedByDefault(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	config := Config{
		ServerURL:       server.URL,
		ClientID:        "tls-strict",
		Hostname:        "tls-strict-host",
		WindowMinutes:   1,
		ReportInterval:  1,
		DiskPath:        "/",
		SkipGeolocation: true,
		InsecureTLS:     false,
	}

	// Registration must fail against the self-signed certificate. Keep the
	// retry budget short by driving runCollector with a custom retry config.
	collector := newMetricsCollector(config, newHTTPClient(config))
	defer collector.gpuCollector.Close()
	collector.retryConfig = RetryConfig{MaxAttempts: 1, InitialDelay: time.Millisecond, MaxDelay: time.Millisecond, Multiplier: 1}

	if err := runCollector(context.Background(), collector); err == nil {
		t.Fatal("expected TLS verification to reject the self-signed certificate")
	}

	// Sanity check: the same server is reachable with verification disabled.
	insecure := newHTTPClient(Config{InsecureTLS: true})
	insecure.Timeout = 5 * time.Second
	resp, err := insecure.Get(server.URL)
	if err != nil {
		t.Fatalf("insecure client should reach the test server: %v", err)
	}
	resp.Body.Close()
}
