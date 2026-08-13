package main

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"database/sql"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"cyqle.in/opsen/common"
)

func TestParseServerFlags_Defaults(t *testing.T) {
	flags, err := parseServerFlags(nil)
	if err != nil {
		t.Fatalf("parseServerFlags returned error: %v", err)
	}

	if flags.configFile != "" || flags.dbPath != "" || flags.host != "" {
		t.Errorf("expected empty string defaults, got %+v", flags)
	}
	if flags.port != 0 || flags.staleMinutes != 0 || flags.cleanupInterval != 0 {
		t.Errorf("expected zero numeric defaults, got %+v", flags)
	}
}

func TestParseServerFlags_AllValues(t *testing.T) {
	flags, err := parseServerFlags([]string{
		"-config", "/etc/opsen/server.yml",
		"-port", "9090",
		"-db", "/var/lib/opsen.db",
		"-stale", "7",
		"-cleanup-interval", "30",
		"-host", "127.0.0.1",
	})
	if err != nil {
		t.Fatalf("parseServerFlags returned error: %v", err)
	}

	if flags.configFile != "/etc/opsen/server.yml" || flags.dbPath != "/var/lib/opsen.db" {
		t.Errorf("unexpected paths: %+v", flags)
	}
	if flags.port != 9090 || flags.staleMinutes != 7 || flags.cleanupInterval != 30 {
		t.Errorf("unexpected numbers: %+v", flags)
	}
	if flags.host != "127.0.0.1" {
		t.Errorf("unexpected host: %s", flags.host)
	}
}

func TestParseServerFlags_UnknownFlag(t *testing.T) {
	if _, err := parseServerFlags([]string{"-not-a-flag"}); err == nil {
		t.Fatal("expected an error for an unknown flag")
	}
}

func TestApplyServerFlagOverrides(t *testing.T) {
	base := func() *common.ServerConfig {
		return &common.ServerConfig{
			Port:                8080,
			Database:            "original.db",
			StaleMinutes:        5,
			CleanupIntervalSecs: 60,
			Host:                "0.0.0.0",
		}
	}

	t.Run("nil flags leave config untouched", func(t *testing.T) {
		cfg := base()
		applyServerFlagOverrides(cfg, nil)
		if cfg.Port != 8080 || cfg.Database != "original.db" || cfg.Host != "0.0.0.0" {
			t.Errorf("config changed with nil flags: %+v", cfg)
		}
	})

	t.Run("zero values do not override", func(t *testing.T) {
		cfg := base()
		applyServerFlagOverrides(cfg, &serverFlags{})
		if cfg.Port != 8080 || cfg.Database != "original.db" || cfg.StaleMinutes != 5 ||
			cfg.CleanupIntervalSecs != 60 || cfg.Host != "0.0.0.0" {
			t.Errorf("empty flags overrode config: %+v", cfg)
		}
	})

	t.Run("set values override", func(t *testing.T) {
		cfg := base()
		applyServerFlagOverrides(cfg, &serverFlags{
			port:            9999,
			dbPath:          "override.db",
			staleMinutes:    11,
			cleanupInterval: 22,
			host:            "10.0.0.1",
		})

		if cfg.Port != 9999 || cfg.Database != "override.db" || cfg.StaleMinutes != 11 ||
			cfg.CleanupIntervalSecs != 22 || cfg.Host != "10.0.0.1" {
			t.Errorf("flags were not applied: %+v", cfg)
		}
	})
}

func TestLoadServerRuntimeConfig(t *testing.T) {
	t.Run("defaults with no file", func(t *testing.T) {
		cfg, err := loadServerRuntimeConfig(nil)
		if err != nil {
			t.Fatalf("loadServerRuntimeConfig returned error: %v", err)
		}
		if cfg.Port == 0 {
			t.Error("expected a default port to be set")
		}
	})

	t.Run("file plus overrides", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "server.yml")
		source := &common.ServerConfig{
			Port:     7000,
			Database: "from-file.db",
			Host:     "192.168.0.1",
		}
		if err := common.SaveServerConfig(source, path); err != nil {
			t.Fatalf("failed to write config: %v", err)
		}

		cfg, err := loadServerRuntimeConfig(&serverFlags{configFile: path, port: 7100})
		if err != nil {
			t.Fatalf("loadServerRuntimeConfig returned error: %v", err)
		}
		if cfg.Port != 7100 {
			t.Errorf("flag should win over file, got %d", cfg.Port)
		}
		if cfg.Database != "from-file.db" || cfg.Host != "192.168.0.1" {
			t.Errorf("file values not preserved: %+v", cfg)
		}
	})

	t.Run("invalid file", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "broken.yml")
		if err := os.WriteFile(path, []byte("port: [oops"), 0644); err != nil {
			t.Fatalf("failed to write config: %v", err)
		}
		if _, err := loadServerRuntimeConfig(&serverFlags{configFile: path}); err == nil {
			t.Fatal("expected an error for an unparseable config file")
		}
	})
}

func TestBuildTierSpecs(t *testing.T) {
	cfg := &common.ServerConfig{
		Tiers: []common.TierSpec{
			{Name: "small", VCPU: 2, MemoryGB: 4},
			{Name: "large", VCPU: 16, MemoryGB: 32, GPU: 1, GPUMemoryGB: 16},
		},
	}

	specs := buildTierSpecs(cfg)

	if len(specs) != 2 {
		t.Fatalf("expected 2 tiers, got %d", len(specs))
	}
	if specs["small"].VCPU != 2 {
		t.Errorf("unexpected small tier: %+v", specs["small"])
	}
	if specs["large"].GPU != 1 || specs["large"].GPUMemoryGB != 16 {
		t.Errorf("unexpected large tier: %+v", specs["large"])
	}
}

func TestBuildTierSpecs_Empty(t *testing.T) {
	specs := buildTierSpecs(&common.ServerConfig{})
	if len(specs) != 0 {
		t.Errorf("expected no tiers, got %v", specs)
	}
}

func TestStickyEnabled(t *testing.T) {
	cases := []struct {
		name   string
		config common.ServerConfig
		want   bool
	}{
		{"disabled", common.ServerConfig{}, false},
		{"by header", common.ServerConfig{StickyHeader: "X-Session-ID"}, true},
		{"by ip", common.ServerConfig{StickyByIP: true}, true},
		{"both", common.ServerConfig{StickyHeader: "X-Session-ID", StickyByIP: true}, true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := tc.config
			if got := stickyEnabled(&cfg); got != tc.want {
				t.Errorf("expected %v, got %v", tc.want, got)
			}
		})
	}
}

// testServerConfig returns a config suitable for an in-test server instance.
func testServerConfig(t *testing.T) *common.ServerConfig {
	t.Helper()

	return &common.ServerConfig{
		Port:                         0, // OS-assigned
		Host:                         "127.0.0.1",
		Database:                     filepath.Join(t.TempDir(), "startup-test.db"),
		StaleMinutes:                 5,
		CleanupIntervalSecs:          60,
		StickyHeader:                 "X-Session-ID",
		StickyAffinityEnabled:        true,
		PendingAllocationTimeoutSecs: 120,
		TierFieldName:                "tier",
		TierHeader:                   "X-Tier",
		MaxRequestBodyBytes:          1 << 20,
		RequestTimeout:               10,
		IdleTimeout:                  30,
		ReadHeaderTimeout:            5,
		ShutdownTimeout:              2,
		DBMaxOpenConns:               5,
		DBMaxIdleConns:               2,
		DBConnMaxLifetime:            300,
		Tiers: []common.TierSpec{
			{Name: "small", VCPU: 1, MemoryGB: 1, StorageGB: 1},
		},
	}
}

func TestNewServerFromConfig(t *testing.T) {
	cfg := testServerConfig(t)
	cfg.ProxyEndpoints = []string{"/api"}
	cfg.GeoIPDBPath = "/tmp/geo.mmdb"

	server, err := newServerFromConfig(cfg)
	if err != nil {
		t.Fatalf("newServerFromConfig returned error: %v", err)
	}
	defer closeDB(t, server.db)

	if server.db == nil {
		t.Fatal("expected a database handle")
	}
	if server.stickyHeader != "X-Session-ID" || !server.stickyAffinityEnabled {
		t.Errorf("sticky configuration not applied: %+v", server)
	}
	if server.staleTimeout != 5*time.Minute {
		t.Errorf("expected a 5 minute stale timeout, got %s", server.staleTimeout)
	}
	if server.cleanupInterval != 60*time.Second {
		t.Errorf("expected a 60s cleanup interval, got %s", server.cleanupInterval)
	}
	if len(server.tierSpecs) != 1 {
		t.Errorf("expected 1 tier spec, got %d", len(server.tierSpecs))
	}
	if len(server.proxyEndpoints) != 1 || server.proxyEndpoints[0] != "/api" {
		t.Errorf("unexpected proxy endpoints: %v", server.proxyEndpoints)
	}
	if server.geoIPDBPath != "/tmp/geo.mmdb" {
		t.Errorf("unexpected geoip path: %s", server.geoIPDBPath)
	}
	if server.clientCache == nil || server.stickyAssignments == nil || server.pendingAllocations == nil {
		t.Error("expected all caches to be initialized")
	}
}

func TestNewServerFromConfig_RestoresPersistedState(t *testing.T) {
	cfg := testServerConfig(t)

	// First instance: persist a client registration and a sticky assignment.
	first, err := newServerFromConfig(cfg)
	if err != nil {
		t.Fatalf("newServerFromConfig returned error: %v", err)
	}

	if _, err := first.db.Exec(`
		INSERT INTO clients (client_id, hostname, public_ip, local_ip, latitude, longitude,
			country, city, total_cpu, total_memory, total_storage, endpoint, last_seen)
		VALUES ('c1', 'host-1', '1.2.3.4', '10.0.0.1', 0, 0, 'US', 'NYC', 8, 16, 100,
			'http://10.0.0.1:3000', datetime('now'))`); err != nil {
		t.Fatalf("failed to insert client: %v", err)
	}
	if _, err := first.db.Exec(`
		INSERT INTO sticky_assignments (sticky_id, tier, client_id)
		VALUES ('session-1', 'small', 'c1')`); err != nil {
		t.Fatalf("failed to insert sticky assignment: %v", err)
	}
	closeDB(t, first.db)

	// Second instance over the same database file must reload both.
	second, err := newServerFromConfig(cfg)
	if err != nil {
		t.Fatalf("newServerFromConfig returned error: %v", err)
	}
	defer closeDB(t, second.db)

	second.mu.RLock()
	_, clientLoaded := second.clientCache["c1"]
	assigned := second.stickyAssignments["session-1"]["small"]
	second.mu.RUnlock()

	if !clientLoaded {
		t.Error("expected the persisted client to be loaded into the cache")
	}
	if assigned != "c1" {
		t.Errorf("expected the sticky assignment to be restored, got %q", assigned)
	}
}

func TestNewServerFromConfig_DatabaseError(t *testing.T) {
	cfg := testServerConfig(t)
	// A path whose parent directory does not exist cannot be opened.
	cfg.Database = filepath.Join(t.TempDir(), "missing-dir", "server.db")

	server, err := newServerFromConfig(cfg)
	if err == nil {
		closeDB(t, server.db)
		t.Fatal("expected an error for an unusable database path")
	}
	if !strings.Contains(err.Error(), "failed to initialize database") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestBuildHandler_RoutesManagementEndpoints(t *testing.T) {
	cfg := testServerConfig(t)
	server, err := newServerFromConfig(cfg)
	if err != nil {
		t.Fatalf("newServerFromConfig returned error: %v", err)
	}
	defer closeDB(t, server.db)

	handler := buildHandler(server, cfg)

	for _, path := range []string{"/health", "/clients"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("%s: expected 200, got %d", path, rec.Code)
		}
	}

	// No proxy endpoints are configured, so unknown paths fall through to 404.
	req := httptest.NewRequest(http.MethodGet, "/unknown", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404 for an unregistered path, got %d", rec.Code)
	}
}

func TestBuildHandler_SecurityHeadersToggle(t *testing.T) {
	cfg := testServerConfig(t)
	server, err := newServerFromConfig(cfg)
	if err != nil {
		t.Fatalf("newServerFromConfig returned error: %v", err)
	}
	defer closeDB(t, server.db)

	withHeaders := buildHandler(server, cfg)
	rec := httptest.NewRecorder()
	withHeaders.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/health", nil))
	if rec.Header().Get("X-Frame-Options") == "" {
		t.Error("expected security headers to be applied by default")
	}

	cfg.DisableSecurityHeaders = true
	withoutHeaders := buildHandler(server, cfg)
	rec = httptest.NewRecorder()
	withoutHeaders.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/health", nil))
	if rec.Header().Get("X-Frame-Options") != "" {
		t.Error("expected security headers to be disabled")
	}
}

func TestBuildHandler_CORSAndRateLimiting(t *testing.T) {
	cfg := testServerConfig(t)
	cfg.EnableCORS = true
	cfg.CORSAllowedOrigins = []string{"https://app.example.com"}
	cfg.RateLimitPerMinute = 60
	cfg.RateLimitBurst = 1

	server, err := newServerFromConfig(cfg)
	if err != nil {
		t.Fatalf("newServerFromConfig returned error: %v", err)
	}
	defer closeDB(t, server.db)

	handler := buildHandler(server, cfg)

	req := httptest.NewRequest(http.MethodOptions, "/clients", nil)
	req.Header.Set("Origin", "https://app.example.com")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "https://app.example.com" {
		t.Errorf("expected the CORS origin to be echoed, got %q", got)
	}

	// The burst is 1, so a second request from the same IP is throttled.
	get := func() *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/clients", nil)
		req.Header.Set("Origin", "https://app.example.com")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec
	}

	first := get()
	second := get()

	if first.Code != http.StatusOK {
		t.Errorf("expected the first request to succeed, got %d", first.Code)
	}
	if second.Code != http.StatusTooManyRequests {
		t.Errorf("expected the second request to be rate limited, got %d", second.Code)
	}
}

func TestBuildHandler_ProxyEndpointRegistration(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := w.Write([]byte("backend-response")); err != nil {
			t.Errorf("failed to write response: %v", err)
		}
	}))
	defer backend.Close()

	cfg := testServerConfig(t)
	// "/" is the wildcard form, which logs a warning and proxies everything.
	cfg.ProxyEndpoints = []string{"/"}

	server, err := newServerFromConfig(cfg)
	if err != nil {
		t.Fatalf("newServerFromConfig returned error: %v", err)
	}
	defer closeDB(t, server.db)

	client := NewMockClient(MockClientOptions{
		ClientID: "proxy-client",
		Endpoint: backend.URL,
	})
	client.HealthStatus = "healthy"
	server.AddMockClient(client)

	handler := buildHandler(server, cfg)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/anything", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("expected the request to be proxied, got %d", rec.Code)
	}
	if body := rec.Body.String(); body != "backend-response" {
		t.Errorf("unexpected proxied body: %q", body)
	}
}

func TestNewHTTPServer_AppliesTimeouts(t *testing.T) {
	cfg := testServerConfig(t)
	cfg.Port = 8123
	cfg.Host = "127.0.0.1"

	httpServer := newHTTPServer(cfg, http.NewServeMux())

	if httpServer.Addr != "127.0.0.1:8123" {
		t.Errorf("unexpected address: %s", httpServer.Addr)
	}
	if httpServer.ReadTimeout != 10*time.Second {
		t.Errorf("unexpected read timeout: %s", httpServer.ReadTimeout)
	}
	if httpServer.WriteTimeout != 0 {
		t.Errorf("write timeout must stay disabled for SSE, got %s", httpServer.WriteTimeout)
	}
	if httpServer.IdleTimeout != 30*time.Second {
		t.Errorf("unexpected idle timeout: %s", httpServer.IdleTimeout)
	}
	if httpServer.ReadHeaderTimeout != 5*time.Second {
		t.Errorf("unexpected read header timeout: %s", httpServer.ReadHeaderTimeout)
	}
	if httpServer.MaxHeaderBytes != 1<<20 {
		t.Errorf("unexpected max header bytes: %d", httpServer.MaxHeaderBytes)
	}
}

// startTestServer runs runServer on an OS-assigned port and returns its base URL
// plus a stop function that shuts the server down and waits for it to exit.
func startTestServer(t *testing.T, cfg *common.ServerConfig, scheme string) (string, func()) {
	t.Helper()

	ctx, cancel := context.WithCancel(context.Background())
	addrCh := make(chan net.Addr, 1)
	errCh := make(chan error, 1)

	go func() {
		errCh <- runServer(ctx, cfg, func(addr net.Addr) { addrCh <- addr })
	}()

	var addr net.Addr
	select {
	case addr = <-addrCh:
	case err := <-errCh:
		cancel()
		t.Fatalf("runServer exited before becoming ready: %v", err)
	case <-time.After(10 * time.Second):
		cancel()
		t.Fatal("timed out waiting for the server to bind")
	}

	stop := func() {
		cancel()
		select {
		case err := <-errCh:
			if err != nil {
				t.Errorf("runServer returned error: %v", err)
			}
		case <-time.After(10 * time.Second):
			t.Error("runServer did not shut down in time")
		}
	}

	return fmt.Sprintf("%s://%s", scheme, addr.String()), stop
}

func TestRunServer_ServesAndShutsDown(t *testing.T) {
	cfg := testServerConfig(t)
	cfg.HealthCheckEnabled = true
	cfg.HealthCheckIntervalSecs = 1
	cfg.HealthCheckTimeoutSecs = 1
	cfg.CleanupIntervalSecs = 1

	baseURL, stop := startTestServer(t, cfg, "http")

	resp, err := http.Get(baseURL + "/health")
	if err != nil {
		t.Fatalf("health request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200 from /health, got %d", resp.StatusCode)
	}

	stop()

	// After shutdown the port must no longer accept requests.
	if _, err := http.Get(baseURL + "/health"); err == nil {
		t.Error("expected the server to stop accepting connections after shutdown")
	}
}

func TestRunServer_TLS(t *testing.T) {
	certFile, keyFile := writeSelfSignedCert(t)

	cfg := testServerConfig(t)
	cfg.TLSCertFile = certFile
	cfg.TLSKeyFile = keyFile

	baseURL, stop := startTestServer(t, cfg, "https")
	defer stop()

	client := &http.Client{
		Timeout: 10 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
	}

	resp, err := client.Get(baseURL + "/health")
	if err != nil {
		t.Fatalf("HTTPS health request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200 from /health over TLS, got %d", resp.StatusCode)
	}
}

func TestRunServer_ListenError(t *testing.T) {
	// Occupy a port, then ask the server to bind to the same one.
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to reserve a port: %v", err)
	}
	defer listener.Close()

	cfg := testServerConfig(t)
	cfg.Host = "127.0.0.1"
	cfg.Port = listener.Addr().(*net.TCPAddr).Port

	err = runServer(context.Background(), cfg, nil)
	if err == nil {
		t.Fatal("expected a listen error for an occupied port")
	}
	if !strings.Contains(err.Error(), "failed to listen") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestRunServer_DatabaseError(t *testing.T) {
	cfg := testServerConfig(t)
	cfg.Database = filepath.Join(t.TempDir(), "missing-dir", "server.db")

	if err := runServer(context.Background(), cfg, nil); err == nil {
		t.Fatal("expected runServer to fail when the database cannot be opened")
	}
}

func TestRunServer_TLSCertificateError(t *testing.T) {
	cfg := testServerConfig(t)
	cfg.Host = "127.0.0.1"
	cfg.TLSCertFile = filepath.Join(t.TempDir(), "missing.crt")
	cfg.TLSKeyFile = filepath.Join(t.TempDir(), "missing.key")

	err := runServer(context.Background(), cfg, nil)
	if err == nil {
		t.Fatal("expected runServer to fail with missing TLS material")
	}
	if !strings.Contains(err.Error(), "server error") {
		t.Errorf("unexpected error: %v", err)
	}
}

// writeSelfSignedCert generates a throwaway certificate/key pair for TLS tests.
func writeSelfSignedCert(t *testing.T) (certFile, keyFile string) {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("failed to generate key: %v", err)
	}

	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "opsen-test"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
		DNSNames:     []string{"localhost"},
		IsCA:         true,
	}

	der, err := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("failed to create certificate: %v", err)
	}

	dir := t.TempDir()
	certFile = filepath.Join(dir, "server.crt")
	keyFile = filepath.Join(dir, "server.key")

	certPEM, err := os.Create(certFile)
	if err != nil {
		t.Fatalf("failed to create cert file: %v", err)
	}
	defer closeFile(t, certPEM)
	if err := pem.Encode(certPEM, &pem.Block{Type: "CERTIFICATE", Bytes: der}); err != nil {
		t.Fatalf("failed to write certificate: %v", err)
	}

	keyBytes, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatalf("failed to marshal key: %v", err)
	}
	keyPEM, err := os.Create(keyFile)
	if err != nil {
		t.Fatalf("failed to create key file: %v", err)
	}
	defer closeFile(t, keyPEM)
	if err := pem.Encode(keyPEM, &pem.Block{Type: "EC PRIVATE KEY", Bytes: keyBytes}); err != nil {
		t.Fatalf("failed to write key: %v", err)
	}

	return certFile, keyFile
}

// closeDB closes a database handle, failing the test if the close errors.
func closeDB(t *testing.T, db *sql.DB) {
	t.Helper()
	if err := db.Close(); err != nil {
		t.Errorf("failed to close database: %v", err)
	}
}

// closeFile closes a file, failing the test if the close errors.
func closeFile(t *testing.T, f *os.File) {
	t.Helper()
	if err := f.Close(); err != nil {
		t.Errorf("failed to close file: %v", err)
	}
}
