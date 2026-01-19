package common

import (
	"os"
	"testing"
)

// TestSaveAndLoadClientConfig verifies client config save/load round-trip
func TestSaveAndLoadClientConfig(t *testing.T) {
	// Create temporary config file
	tempFile, err := os.CreateTemp("", "client-config-*.yml")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tempFile.Name())
	tempFile.Close()

	// Create test config
	originalConfig := ClientConfig{
		ServerURL:      "http://test-server:9090",
		ClientID:       "test-client-123",
		Hostname:       "test-hostname",
		EndpointURL:    "http://test-client:11000",
		WindowMinutes:  15,
		ReportInterval: 60,
		GeoIPDBPath:    "/path/to/geoip.mmdb",
		LogLevel:       "debug",
	}

	// Save config
	err = SaveClientConfig(&originalConfig, tempFile.Name())
	if err != nil {
		t.Fatalf("Failed to save config: %v", err)
	}

	// Load config
	loadedConfig, err := LoadClientConfig(tempFile.Name())
	if err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}

	// Verify all fields match
	if loadedConfig.ServerURL != originalConfig.ServerURL {
		t.Errorf("ServerURL mismatch: expected %s, got %s", originalConfig.ServerURL, loadedConfig.ServerURL)
	}
	if loadedConfig.ClientID != originalConfig.ClientID {
		t.Errorf("ClientID mismatch: expected %s, got %s", originalConfig.ClientID, loadedConfig.ClientID)
	}
	if loadedConfig.Hostname != originalConfig.Hostname {
		t.Errorf("Hostname mismatch: expected %s, got %s", originalConfig.Hostname, loadedConfig.Hostname)
	}
	if loadedConfig.WindowMinutes != originalConfig.WindowMinutes {
		t.Errorf("WindowMinutes mismatch: expected %d, got %d", originalConfig.WindowMinutes, loadedConfig.WindowMinutes)
	}
	if loadedConfig.ReportInterval != originalConfig.ReportInterval {
		t.Errorf("ReportInterval mismatch: expected %d, got %d", originalConfig.ReportInterval, loadedConfig.ReportInterval)
	}
	if loadedConfig.LogLevel != originalConfig.LogLevel {
		t.Errorf("LogLevel mismatch: expected %s, got %s", originalConfig.LogLevel, loadedConfig.LogLevel)
	}
}

// TestSaveServerConfig_InvalidPath verifies error handling for invalid paths
func TestSaveServerConfig_InvalidPath(t *testing.T) {
	config := ServerConfig{Port: 8080}

	// Try to save to invalid path
	err := SaveServerConfig(&config, "/invalid/path/that/does/not/exist/config.yml")
	if err == nil {
		t.Error("Expected error when saving to invalid path")
	}
}

// TestSaveClientConfig_InvalidPath verifies error handling for invalid paths
func TestSaveClientConfig_InvalidPath(t *testing.T) {
	config := ClientConfig{ServerURL: "http://localhost:8080"}

	// Try to save to invalid path
	err := SaveClientConfig(&config, "/invalid/path/that/does/not/exist/config.yml")
	if err == nil {
		t.Error("Expected error when saving to invalid path")
	}
}

// TestLoadServerConfig_NonExistentFile verifies default config when file doesn't exist
func TestLoadServerConfig_NonExistentFile(t *testing.T) {
	config, err := LoadServerConfig("/nonexistent/config.yml")
	if err != nil {
		t.Fatalf("LoadServerConfig should return defaults when file doesn't exist, got error: %v", err)
	}

	// Verify defaults are set
	if config.Port == 0 {
		t.Error("Expected default port to be set")
	}
	if len(config.Tiers) == 0 {
		t.Error("Expected default tiers to be set")
	}
}

// TestLoadClientConfig_NonExistentFile verifies default config when file doesn't exist
func TestLoadClientConfig_NonExistentFile(t *testing.T) {
	config, err := LoadClientConfig("/nonexistent/config.yml")
	if err != nil {
		t.Fatalf("LoadClientConfig should return defaults when file doesn't exist, got error: %v", err)
	}

	// Verify defaults are set
	if config.WindowMinutes == 0 {
		t.Error("Expected default WindowMinutes to be set")
	}
	if config.ReportInterval == 0 {
		t.Error("Expected default ReportInterval to be set")
	}
}
