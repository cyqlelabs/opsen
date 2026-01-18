package common

import (
	"os"
	"testing"
)

// TestLoadServerConfig_Defaults verifies default server configuration
func TestLoadServerConfig_Defaults(t *testing.T) {
	config, err := LoadServerConfig("")
	if err != nil {
		t.Fatalf("Failed to load default config: %v", err)
	}

	// Verify default values
	if config.Port != 8080 {
		t.Errorf("Expected default port 8080, got %d", config.Port)
	}
	if config.Database != "opsen.db" {
		t.Errorf("Expected default database 'opsen.db', got %s", config.Database)
	}
	if config.StaleMinutes != 5 {
		t.Errorf("Expected default stale minutes 5, got %d", config.StaleMinutes)
	}
	if config.RateLimitPerMinute != 60 {
		t.Errorf("Expected default rate limit 60, got %d", config.RateLimitPerMinute)
	}
	if config.StickyAffinityEnabled != true {
		t.Error("Expected sticky affinity enabled by default")
	}
	if config.PendingAllocationTimeoutSecs != 120 {
		t.Errorf("Expected default pending allocation timeout 120s, got %d", config.PendingAllocationTimeoutSecs)
	}
	if config.TierFieldName != "tier" {
		t.Errorf("Expected default tier field name 'tier', got %s", config.TierFieldName)
	}
	if config.TierHeader != "X-Tier" {
		t.Errorf("Expected default tier header 'X-Tier', got %s", config.TierHeader)
	}

	// Verify default tiers
	if len(config.Tiers) != 5 {
		t.Errorf("Expected 5 default tiers, got %d", len(config.Tiers))
	}

	// Check tier names
	tierNames := make(map[string]bool)
	for _, tier := range config.Tiers {
		tierNames[tier.Name] = true
	}
	expectedTiers := []string{"free", "lite", "pro-standard", "pro-turbo", "pro-max"}
	for _, name := range expectedTiers {
		if !tierNames[name] {
			t.Errorf("Missing default tier: %s", name)
		}
	}
}

// TestLoadServerConfig_YAML verifies YAML configuration loading
func TestLoadServerConfig_YAML(t *testing.T) {
	// Create temporary YAML config file
	yamlContent := `
port: 9090
database: custom.db
stale_minutes: 10
sticky_header: X-Custom-Session
sticky_affinity_enabled: false
pending_allocation_timeout_seconds: 60
rate_limit_per_minute: 100
tier_field_name: subscription_level
tier_header: X-Subscription-Level
tiers:
  - name: custom-tier
    vcpu: 12
    memory_gb: 24.0
    storage_gb: 50
`

	tmpFile, err := os.CreateTemp("", "config-*.yml")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())

	if _, err := tmpFile.WriteString(yamlContent); err != nil {
		t.Fatalf("Failed to write config: %v", err)
	}
	tmpFile.Close()

	// Load config from file
	config, err := LoadServerConfig(tmpFile.Name())
	if err != nil {
		t.Fatalf("Failed to load config from YAML: %v", err)
	}

	// Verify loaded values
	if config.Port != 9090 {
		t.Errorf("Expected port 9090, got %d", config.Port)
	}
	if config.Database != "custom.db" {
		t.Errorf("Expected database 'custom.db', got %s", config.Database)
	}
	if config.StaleMinutes != 10 {
		t.Errorf("Expected stale minutes 10, got %d", config.StaleMinutes)
	}
	if config.StickyHeader != "X-Custom-Session" {
		t.Errorf("Expected sticky header 'X-Custom-Session', got %s", config.StickyHeader)
	}
	if config.StickyAffinityEnabled != false {
		t.Error("Expected sticky affinity disabled")
	}
	if config.PendingAllocationTimeoutSecs != 60 {
		t.Errorf("Expected pending allocation timeout 60s, got %d", config.PendingAllocationTimeoutSecs)
	}
	if config.TierFieldName != "subscription_level" {
		t.Errorf("Expected tier field name 'subscription_level', got %s", config.TierFieldName)
	}
	if config.TierHeader != "X-Subscription-Level" {
		t.Errorf("Expected tier header 'X-Subscription-Level', got %s", config.TierHeader)
	}

	// Verify custom tier
	if len(config.Tiers) != 1 {
		t.Fatalf("Expected 1 custom tier, got %d", len(config.Tiers))
	}
	tier := config.Tiers[0]
	if tier.Name != "custom-tier" {
		t.Errorf("Expected tier name 'custom-tier', got %s", tier.Name)
	}
	if tier.VCPU != 12 {
		t.Errorf("Expected tier VCPU 12, got %d", tier.VCPU)
	}
	if tier.MemoryGB != 24.0 {
		t.Errorf("Expected tier memory 24.0 GB, got %.1f", tier.MemoryGB)
	}
}

// TestLoadClientConfig_Defaults verifies default client configuration
func TestLoadClientConfig_Defaults(t *testing.T) {
	config, err := LoadClientConfig("")
	if err != nil {
		t.Fatalf("Failed to load default config: %v", err)
	}

	// Verify default values
	if config.ServerURL != "http://localhost:8080" {
		t.Errorf("Expected default server URL 'http://localhost:8080', got %s", config.ServerURL)
	}
	if config.WindowMinutes != 15 {
		t.Errorf("Expected default window 15 minutes, got %d", config.WindowMinutes)
	}
	if config.ReportInterval != 60 {
		t.Errorf("Expected default report interval 60s, got %d", config.ReportInterval)
	}
	if config.DiskPath != "/" {
		t.Errorf("Expected default disk path '/', got %s", config.DiskPath)
	}
	if config.LogLevel != "info" {
		t.Errorf("Expected default log level 'info', got %s", config.LogLevel)
	}
}

// TestLoadClientConfig_YAML verifies YAML configuration loading for client
func TestLoadClientConfig_YAML(t *testing.T) {
	yamlContent := `
server_url: https://lb.example.com:8443
window_minutes: 30
report_interval_seconds: 120
disk_path: /data
endpoint_url: https://backend.example.com:11000
skip_geolocation: true
insecure_tls: true
`

	tmpFile, err := os.CreateTemp("", "client-config-*.yml")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())

	if _, err := tmpFile.WriteString(yamlContent); err != nil {
		t.Fatalf("Failed to write config: %v", err)
	}
	tmpFile.Close()

	config, err := LoadClientConfig(tmpFile.Name())
	if err != nil {
		t.Fatalf("Failed to load client config: %v", err)
	}

	// Verify loaded values
	if config.ServerURL != "https://lb.example.com:8443" {
		t.Errorf("Expected server URL 'https://lb.example.com:8443', got %s", config.ServerURL)
	}
	if config.WindowMinutes != 30 {
		t.Errorf("Expected window 30 minutes, got %d", config.WindowMinutes)
	}
	if config.ReportInterval != 120 {
		t.Errorf("Expected report interval 120s, got %d", config.ReportInterval)
	}
	if config.EndpointURL != "https://backend.example.com:11000" {
		t.Errorf("Expected endpoint URL 'https://backend.example.com:11000', got %s", config.EndpointURL)
	}
	if !config.SkipGeolocation {
		t.Error("Expected skip_geolocation to be true")
	}
	if !config.InsecureTLS {
		t.Error("Expected insecure_tls to be true")
	}
}

// TestSaveAndLoadServerConfig verifies round-trip save/load
func TestSaveAndLoadServerConfig(t *testing.T) {
	originalConfig := &ServerConfig{
		Port:                         9999,
		Database:                     "test.db",
		StaleMinutes:                 10,
		StickyHeader:                 "X-Test-Session",
		StickyAffinityEnabled:        false,
		PendingAllocationTimeoutSecs: 180,
		Tiers: []TierSpec{
			{Name: "test", VCPU: 4, MemoryGB: 8.0, StorageGB: 100},
		},
	}

	tmpFile, err := os.CreateTemp("", "save-config-*.yml")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())
	tmpFile.Close()

	// Save config
	if err := SaveServerConfig(originalConfig, tmpFile.Name()); err != nil {
		t.Fatalf("Failed to save config: %v", err)
	}

	// Load config back
	loadedConfig, err := LoadServerConfig(tmpFile.Name())
	if err != nil {
		t.Fatalf("Failed to load saved config: %v", err)
	}

	// Verify values match
	if loadedConfig.Port != originalConfig.Port {
		t.Errorf("Port mismatch after save/load")
	}
	if loadedConfig.Database != originalConfig.Database {
		t.Errorf("Database mismatch after save/load")
	}
	if loadedConfig.StickyHeader != originalConfig.StickyHeader {
		t.Errorf("StickyHeader mismatch after save/load")
	}
	if len(loadedConfig.Tiers) != len(originalConfig.Tiers) {
		t.Errorf("Tiers count mismatch after save/load")
	}
}

// TestServerConfig_SecurityDefaults verifies security-related defaults
func TestServerConfig_SecurityDefaults(t *testing.T) {
	config, err := LoadServerConfig("")
	if err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}

	// Verify security defaults
	if config.RateLimitPerMinute != 60 {
		t.Errorf("Expected rate limit 60/min, got %d", config.RateLimitPerMinute)
	}
	if config.RateLimitBurst != 120 {
		t.Errorf("Expected rate burst 120, got %d", config.RateLimitBurst)
	}
	if config.MaxRequestBodyBytes != 10*1024*1024 {
		t.Errorf("Expected max body 10MB, got %d", config.MaxRequestBodyBytes)
	}
	if config.RequestTimeout != 30 {
		t.Errorf("Expected request timeout 30s, got %d", config.RequestTimeout)
	}
	if config.ReadHeaderTimeout != 10 {
		t.Errorf("Expected read header timeout 10s, got %d", config.ReadHeaderTimeout)
	}
	if config.TLSInsecureSkipVerify != false {
		t.Error("Expected TLS verification enabled by default")
	}
}
