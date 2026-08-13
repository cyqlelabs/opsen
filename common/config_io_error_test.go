package common

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadServerConfig_UnreadablePath(t *testing.T) {
	// Reading a directory as a config file fails with something other than
	// "not exist", so the error must be surfaced rather than swallowed.
	_, err := LoadServerConfig(t.TempDir())
	if err == nil {
		t.Fatal("expected an error when the config path is a directory")
	}
	if !strings.Contains(err.Error(), "failed to read config file") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestLoadClientConfig_UnreadablePath(t *testing.T) {
	_, err := LoadClientConfig(t.TempDir())
	if err == nil {
		t.Fatal("expected an error when the config path is a directory")
	}
	if !strings.Contains(err.Error(), "failed to read config file") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestSaveServerConfig_UnwritablePath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing-dir", "server.yml")

	err := SaveServerConfig(&ServerConfig{Port: 8080}, path)
	if err == nil {
		t.Fatal("expected an error when the target directory does not exist")
	}
	if !strings.Contains(err.Error(), "failed to write config file") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestSaveClientConfig_UnwritablePath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing-dir", "client.yml")

	err := SaveClientConfig(&ClientConfig{ServerURL: "http://localhost:8080"}, path)
	if err == nil {
		t.Fatal("expected an error when the target directory does not exist")
	}
	if !strings.Contains(err.Error(), "failed to write config file") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestSaveAndLoadServerConfig_RoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "server.yml")

	original := &ServerConfig{
		Port:           9100,
		Database:       "round-trip.db",
		Host:           "10.1.2.3",
		LogLevel:       "debug",
		JSONLogging:    true,
		ProxyEndpoints: []string{"/api", "/browse"},
		Tiers: []TierSpec{
			{Name: "gpu", VCPU: 8, MemoryGB: 32, StorageGB: 100, GPU: 1, GPUMemoryGB: 16},
		},
	}

	if err := SaveServerConfig(original, path); err != nil {
		t.Fatalf("SaveServerConfig returned error: %v", err)
	}

	loaded, err := LoadServerConfig(path)
	if err != nil {
		t.Fatalf("LoadServerConfig returned error: %v", err)
	}

	if loaded.Port != original.Port || loaded.Database != original.Database ||
		loaded.Host != original.Host || loaded.LogLevel != original.LogLevel || !loaded.JSONLogging {
		t.Errorf("scalar fields did not round-trip: %+v", loaded)
	}
	if len(loaded.ProxyEndpoints) != 2 || loaded.ProxyEndpoints[1] != "/browse" {
		t.Errorf("proxy endpoints did not round-trip: %v", loaded.ProxyEndpoints)
	}
	if len(loaded.Tiers) != 1 || loaded.Tiers[0].GPU != 1 || loaded.Tiers[0].GPUMemoryGB != 16 {
		t.Errorf("tiers did not round-trip: %+v", loaded.Tiers)
	}
}

func TestSaveAndLoadClientConfig_RoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "client.yml")

	original := &ClientConfig{
		ServerURL:       "https://lb.example.com",
		ClientID:        "client-round-trip",
		Hostname:        "worker-1",
		WindowMinutes:   10,
		ReportInterval:  30,
		DiskPath:        "/data",
		EndpointURL:     "http://worker-1:3000",
		SkipGeolocation: true,
		Endpoints: []EndpointConfig{
			{URL: "http://worker-1:3000", Paths: []string{"/api/*"}},
		},
	}

	if err := SaveClientConfig(original, path); err != nil {
		t.Fatalf("SaveClientConfig returned error: %v", err)
	}

	loaded, err := LoadClientConfig(path)
	if err != nil {
		t.Fatalf("LoadClientConfig returned error: %v", err)
	}

	if loaded.ServerURL != original.ServerURL || loaded.ClientID != original.ClientID ||
		loaded.Hostname != original.Hostname || loaded.WindowMinutes != original.WindowMinutes {
		t.Errorf("scalar fields did not round-trip: %+v", loaded)
	}
	if len(loaded.Endpoints) != 1 || loaded.Endpoints[0].Paths[0] != "/api/*" {
		t.Errorf("endpoints did not round-trip: %+v", loaded.Endpoints)
	}
}
