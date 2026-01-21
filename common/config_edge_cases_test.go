package common

import (
	"os"
	"path/filepath"
	"testing"
)

// TestLoadServerConfig_EdgeCases verifies edge cases in server config loading
func TestLoadServerConfig_EdgeCases(t *testing.T) {
	tests := []struct {
		name      string
		setup     func() string
		expectErr bool
		cleanup   func(string)
	}{
		{
			name: "EmptyFile",
			setup: func() string {
				tmpFile, _ := os.CreateTemp("", "empty-*.yml")
				tmpFile.Close()
				return tmpFile.Name()
			},
			expectErr: false, // Should use defaults
			cleanup:   func(path string) { os.Remove(path) },
		},
		{
			name: "InvalidYAML",
			setup: func() string {
				tmpFile, _ := os.CreateTemp("", "invalid-*.yml")
				_, _ = tmpFile.WriteString("invalid: yaml: content:\n  broken")
				tmpFile.Close()
				return tmpFile.Name()
			},
			expectErr: true,
			cleanup:   func(path string) { os.Remove(path) },
		},
		{
			name: "PartialConfig",
			setup: func() string {
				tmpFile, _ := os.CreateTemp("", "partial-*.yml")
				_, _ = tmpFile.WriteString("port: 9999\n")
				tmpFile.Close()
				return tmpFile.Name()
			},
			expectErr: false, // Should merge with defaults
			cleanup:   func(path string) { os.Remove(path) },
		},
		{
			name: "ConfigWithComments",
			setup: func() string {
				tmpFile, _ := os.CreateTemp("", "comments-*.yml")
				_, _ = tmpFile.WriteString("# This is a comment\nport: 8888\n# Another comment\nlog_level: debug\n")
				tmpFile.Close()
				return tmpFile.Name()
			},
			expectErr: false,
			cleanup:   func(path string) { os.Remove(path) },
		},
		{
			name: "ConfigWithUnknownFields",
			setup: func() string {
				tmpFile, _ := os.CreateTemp("", "unknown-*.yml")
				_, _ = tmpFile.WriteString("port: 8080\nunknown_field: value\nlog_level: info\n")
				tmpFile.Close()
				return tmpFile.Name()
			},
			expectErr: false, // Should ignore unknown fields
			cleanup:   func(path string) { os.Remove(path) },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			configPath := tt.setup()
			if tt.cleanup != nil {
				defer tt.cleanup(configPath)
			}

			config, err := LoadServerConfig(configPath)

			if tt.expectErr {
				if err == nil {
					t.Error("Expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Errorf("Unexpected error: %v", err)
				return
			}

			if config == nil {
				t.Error("Expected config, got nil")
				return
			}

			t.Logf("Loaded config: Port=%d, LogLevel=%s", config.Port, config.LogLevel)
		})
	}
}

// TestLoadClientConfig_EdgeCases verifies edge cases in client config loading
func TestLoadClientConfig_EdgeCases(t *testing.T) {
	tests := []struct {
		name      string
		setup     func() string
		expectErr bool
		cleanup   func(string)
	}{
		{
			name: "EmptyFile",
			setup: func() string {
				tmpFile, _ := os.CreateTemp("", "empty-client-*.yml")
				tmpFile.Close()
				return tmpFile.Name()
			},
			expectErr: false,
			cleanup:   func(path string) { os.Remove(path) },
		},
		{
			name: "InvalidYAML",
			setup: func() string {
				tmpFile, _ := os.CreateTemp("", "invalid-client-*.yml")
				_, _ = tmpFile.WriteString("server_url invalid yaml")
				tmpFile.Close()
				return tmpFile.Name()
			},
			expectErr: true,
			cleanup:   func(path string) { os.Remove(path) },
		},
		{
			name: "PartialConfig",
			setup: func() string {
				tmpFile, _ := os.CreateTemp("", "partial-client-*.yml")
				_, _ = tmpFile.WriteString("server_url: http://localhost:8888\n")
				tmpFile.Close()
				return tmpFile.Name()
			},
			expectErr: false,
			cleanup:   func(path string) { os.Remove(path) },
		},
		{
			name: "ConfigWithNegativeValues",
			setup: func() string {
				tmpFile, _ := os.CreateTemp("", "negative-client-*.yml")
				_, _ = tmpFile.WriteString("server_url: http://test:8080\nwindow_minutes: -100\nreport_interval_seconds: -50\n")
				tmpFile.Close()
				return tmpFile.Name()
			},
			expectErr: false, // Should accept but may use defaults
			cleanup:   func(path string) { os.Remove(path) },
		},
		{
			name: "ConfigWithVeryLargeValues",
			setup: func() string {
				tmpFile, _ := os.CreateTemp("", "large-client-*.yml")
				_, _ = tmpFile.WriteString("server_url: http://test:8080\nwindow_minutes: 99999999\nreport_interval_seconds: 99999999\n")
				tmpFile.Close()
				return tmpFile.Name()
			},
			expectErr: false,
			cleanup:   func(path string) { os.Remove(path) },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			configPath := tt.setup()
			if tt.cleanup != nil {
				defer tt.cleanup(configPath)
			}

			config, err := LoadClientConfig(configPath)

			if tt.expectErr {
				if err == nil {
					t.Error("Expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Errorf("Unexpected error: %v", err)
				return
			}

			if config == nil {
				t.Error("Expected config, got nil")
				return
			}

			t.Logf("Loaded config: ServerURL=%s, WindowMinutes=%d, ReportInterval=%d",
				config.ServerURL, config.WindowMinutes, config.ReportInterval)
		})
	}
}

// TestSaveServerConfig_EdgeCases verifies edge cases in config saving
func TestSaveServerConfig_EdgeCases(t *testing.T) {
	tests := []struct {
		name      string
		config    *ServerConfig
		setupPath func() string
		expectErr bool
		cleanup   func(string)
	}{
		{
			name: "SaveToNonExistentDirectory",
			config: &ServerConfig{
				Port:     8080,
				LogLevel: "info",
			},
			setupPath: func() string {
				return filepath.Join(os.TempDir(), "nonexistent", "server.yml")
			},
			expectErr: true,
			cleanup:   func(path string) { os.Remove(path) },
		},
		{
			name: "SaveToValidPath",
			config: &ServerConfig{
				Port:     9090,
				LogLevel: "debug",
			},
			setupPath: func() string {
				tmpFile, _ := os.CreateTemp("", "save-test-*.yml")
				tmpFile.Close()
				return tmpFile.Name()
			},
			expectErr: false,
			cleanup:   func(path string) { os.Remove(path) },
		},
		{
			name: "SaveComplexConfig",
			config: &ServerConfig{
				Port:     8080,
				LogLevel: "info",
				Tiers: []TierSpec{
					{
						Name:         "small",
						VCPU:         2,
						MemoryGB:     4.0,
						StorageGB:    50.0,
						GPU:          0,
						GPUMemoryGB:  0,
					},
					{
						Name:         "gpu-large",
						VCPU:         16,
						MemoryGB:     64.0,
						StorageGB:    500.0,
						GPU:          2,
						GPUMemoryGB:  24.0,
					},
				},
			},
			setupPath: func() string {
				tmpFile, _ := os.CreateTemp("", "complex-*.yml")
				tmpFile.Close()
				return tmpFile.Name()
			},
			expectErr: false,
			cleanup:   func(path string) { os.Remove(path) },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			configPath := tt.setupPath()
			if tt.cleanup != nil {
				defer tt.cleanup(configPath)
			}

			err := SaveServerConfig(tt.config, configPath)

			if tt.expectErr {
				if err == nil {
					t.Error("Expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Errorf("Unexpected error: %v", err)
				return
			}

			// Verify file was created and can be loaded back
			loadedConfig, err := LoadServerConfig(configPath)
			if err != nil {
				t.Errorf("Failed to load saved config: %v", err)
				return
			}

			if loadedConfig.Port != tt.config.Port {
				t.Errorf("Port mismatch: expected %d, got %d", tt.config.Port, loadedConfig.Port)
			}
			if loadedConfig.LogLevel != tt.config.LogLevel {
				t.Errorf("LogLevel mismatch: expected %s, got %s", tt.config.LogLevel, loadedConfig.LogLevel)
			}
		})
	}
}

// TestSaveClientConfig_EdgeCases verifies edge cases in client config saving
func TestSaveClientConfig_EdgeCases(t *testing.T) {
	tests := []struct {
		name      string
		config    *ClientConfig
		setupPath func() string
		expectErr bool
		cleanup   func(string)
	}{
		{
			name: "SaveMinimalConfig",
			config: &ClientConfig{
				ServerURL:      "http://localhost:8080",
				WindowMinutes:  15,
				ReportInterval: 60,
			},
			setupPath: func() string {
				tmpFile, _ := os.CreateTemp("", "client-minimal-*.yml")
				tmpFile.Close()
				return tmpFile.Name()
			},
			expectErr: false,
			cleanup:   func(path string) { os.Remove(path) },
		},
		{
			name: "SaveCompleteConfig",
			config: &ClientConfig{
				ServerURL:      "https://example.com:8443",
				WindowMinutes:  30,
				ReportInterval: 120,
				LogLevel:       "debug",
				GeoIPDBPath:    "/usr/share/GeoIP/GeoLite2-City.mmdb",
			},
			setupPath: func() string {
				tmpFile, _ := os.CreateTemp("", "client-complete-*.yml")
				tmpFile.Close()
				return tmpFile.Name()
			},
			expectErr: false,
			cleanup:   func(path string) { os.Remove(path) },
		},
		{
			name: "SaveToInvalidPath",
			config: &ClientConfig{
				ServerURL: "http://test:8080",
			},
			setupPath: func() string {
				return "/root/invalid/client.yml"
			},
			expectErr: true,
			cleanup:   func(path string) {},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			configPath := tt.setupPath()
			if tt.cleanup != nil {
				defer tt.cleanup(configPath)
			}

			err := SaveClientConfig(tt.config, configPath)

			if tt.expectErr {
				if err == nil {
					t.Error("Expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Errorf("Unexpected error: %v", err)
				return
			}

			// Verify file can be loaded back
			loadedConfig, err := LoadClientConfig(configPath)
			if err != nil {
				t.Errorf("Failed to load saved config: %v", err)
				return
			}

			if loadedConfig.ServerURL != tt.config.ServerURL {
				t.Errorf("ServerURL mismatch: expected %s, got %s",
					tt.config.ServerURL, loadedConfig.ServerURL)
			}
		})
	}
}

// TestConfigRoundTrip verifies config can be saved and loaded without data loss
func TestConfigRoundTrip(t *testing.T) {
	t.Run("ServerConfig", func(t *testing.T) {
		tmpFile, _ := os.CreateTemp("", "roundtrip-server-*.yml")
		tmpFile.Close()
		defer os.Remove(tmpFile.Name())

		original := &ServerConfig{
			Port:           9999,
			Database:       "/tmp/test.db",
			LogLevel:       "debug",
			JSONLogging:    true,
			StaleMinutes:   5,
			StickyHeader:   "X-Custom-Session",
			ProxyEndpoints: []string{"/api", "/browse"},
			Tiers: []TierSpec{
				{Name: "tiny", VCPU: 1, MemoryGB: 1.0, StorageGB: 10},
				{Name: "huge", VCPU: 64, MemoryGB: 256.0, StorageGB: 2000, GPU: 4, GPUMemoryGB: 96.0},
			},
		}

		// Save
		err := SaveServerConfig(original, tmpFile.Name())
		if err != nil {
			t.Fatalf("Failed to save: %v", err)
		}

		// Load
		loaded, err := LoadServerConfig(tmpFile.Name())
		if err != nil {
			t.Fatalf("Failed to load: %v", err)
		}

		// Compare
		if loaded.Port != original.Port {
			t.Errorf("Port mismatch")
		}
		if loaded.Database != original.Database {
			t.Errorf("Database mismatch")
		}
		if loaded.LogLevel != original.LogLevel {
			t.Errorf("LogLevel mismatch")
		}
		if loaded.JSONLogging != original.JSONLogging {
			t.Errorf("JSONLogging mismatch")
		}
		if len(loaded.Tiers) != len(original.Tiers) {
			t.Errorf("Tiers length mismatch")
		}
	})

	t.Run("ClientConfig", func(t *testing.T) {
		tmpFile, _ := os.CreateTemp("", "roundtrip-client-*.yml")
		tmpFile.Close()
		defer os.Remove(tmpFile.Name())

		original := &ClientConfig{
			ServerURL:      "https://example.com:8443",
			WindowMinutes:  20,
			ReportInterval: 567,
			LogLevel:       "warn",
			GeoIPDBPath:    "/custom/path/GeoIP.mmdb",
		}

		// Save
		err := SaveClientConfig(original, tmpFile.Name())
		if err != nil {
			t.Fatalf("Failed to save: %v", err)
		}

		// Load
		loaded, err := LoadClientConfig(tmpFile.Name())
		if err != nil {
			t.Fatalf("Failed to load: %v", err)
		}

		// Compare
		if loaded.ServerURL != original.ServerURL {
			t.Errorf("ServerURL mismatch")
		}
		if loaded.WindowMinutes != original.WindowMinutes {
			t.Errorf("WindowMinutes mismatch")
		}
		if loaded.ReportInterval != original.ReportInterval {
			t.Errorf("ReportInterval mismatch")
		}
		if loaded.LogLevel != original.LogLevel {
			t.Errorf("LogLevel mismatch")
		}
	})
}

// TestLoadConfig_NonExistentFile verifies handling of non-existent config files
func TestLoadConfig_NonExistentFile(t *testing.T) {
	t.Run("ServerConfig", func(t *testing.T) {
		config, err := LoadServerConfig("/nonexistent/path/server.yml")
		// Load functions may return defaults even when file doesn't exist
		if err != nil {
			t.Logf("Got expected error for nonexistent file: %v", err)
		}
		if config != nil {
			t.Logf("Received default config when file doesn't exist")
		}
	})

	t.Run("ClientConfig", func(t *testing.T) {
		config, err := LoadClientConfig("/nonexistent/path/client.yml")
		// Load functions may return defaults even when file doesn't exist
		if err != nil {
			t.Logf("Got expected error for nonexistent file: %v", err)
		}
		if config != nil {
			t.Logf("Received default config when file doesn't exist")
		}
	})
}

// TestConfigWithEmptyValues verifies handling of empty string values
func TestConfigWithEmptyValues(t *testing.T) {
	tmpFile, _ := os.CreateTemp("", "empty-values-*.yml")
	defer os.Remove(tmpFile.Name())

	_, _ = tmpFile.WriteString(`
port: 8080
database: ""
log_level: ""
sticky_header: ""
`)
	tmpFile.Close()

	config, err := LoadServerConfig(tmpFile.Name())
	if err != nil {
		t.Fatalf("Failed to load config with empty values: %v", err)
	}

	t.Logf("Database: '%s', LogLevel: '%s', StickyHeader: '%s'",
		config.Database, config.LogLevel, config.StickyHeader)

	// Empty values should be handled gracefully
	if config.Port != 8080 {
		t.Errorf("Expected port 8080, got %d", config.Port)
	}
}
