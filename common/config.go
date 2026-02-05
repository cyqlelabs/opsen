package common

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// ServerConfig represents the server configuration
type ServerConfig struct {
	Port                int    `yaml:"port"`
	Database            string `yaml:"database"`
	StaleMinutes        int    `yaml:"stale_minutes"`
	CleanupIntervalSecs int    `yaml:"cleanup_interval_seconds"`
	Host                string `yaml:"host"`
	LogLevel            string `yaml:"log_level"`
	JSONLogging         bool   `yaml:"json_logging"`           // Enable JSON structured logging

	// Proxy configuration
	ProxyEndpoints      []string `yaml:"proxy_endpoints"`       // Endpoint prefixes to proxy (e.g., ["/browse", "/api"])
	ProxySSEFlushInterval int    `yaml:"proxy_sse_flush_interval_ms"` // Flush interval for SSE/streaming in ms (0 = disabled, -1 = immediate, >0 = interval)
	TLSCertFile         string   `yaml:"tls_cert_file"`         // Path to TLS certificate file
	TLSKeyFile          string   `yaml:"tls_key_file"`          // Path to TLS key file
	TLSInsecureSkipVerify bool   `yaml:"tls_insecure_skip_verify"` // Skip TLS verification for backends (default: false)

	// Security configuration
	ServerKey           string   `yaml:"server_key"`            // Primary server key for client authentication (empty = no client auth)
	APIKeys             []string `yaml:"api_keys"`              // Additional API keys for other integrations (empty = no extra keys)
	WhitelistedIPs      []string `yaml:"whitelisted_ips"`       // IP whitelist (empty = allow all)
	RateLimitPerMinute  int      `yaml:"rate_limit_per_minute"` // Requests per minute per IP (0 = unlimited)
	RateLimitBurst      int      `yaml:"rate_limit_burst"`      // Burst capacity (default: 2x rate limit)
	MaxRequestBodyBytes int64    `yaml:"max_request_body_bytes"` // Max request body size (default: 10MB)
	RequestTimeout      int      `yaml:"request_timeout_seconds"` // Request timeout in seconds (default: 30)
	IdleTimeout         int      `yaml:"idle_timeout_seconds"`    // Idle timeout for keep-alive connections (default: 120)
	EnableCORS          bool     `yaml:"enable_cors"`           // Enable CORS
	CORSAllowedOrigins  []string `yaml:"cors_allowed_origins"`  // CORS allowed origins
	ReadHeaderTimeout   int      `yaml:"read_header_timeout_seconds"` // ReadHeaderTimeout prevents Slowloris attacks
	DisableSecurityHeaders bool  `yaml:"disable_security_headers"` // Disable automatic security headers (X-Frame-Options, X-XSS-Protection, etc.)

	// Geolocation configuration
	GeoIPDBPath         string `yaml:"geoip_db_path"`         // Optional: Path to MaxMind GeoLite2-City.mmdb for IP lookup

	// Sticky session configuration
	StickyHeader        string `yaml:"sticky_header"`         // Header name for sticky sessions (e.g., "X-Session-ID", "X-User-ID")
	StickyByIP          bool   `yaml:"sticky_by_ip"`          // Use client IP for sticky sessions when header is not present (default: false)
	StickyAffinityEnabled bool `yaml:"sticky_affinity_enabled"` // Prefer same server for different tiers (default: true)
	PendingAllocationTimeoutSecs int `yaml:"pending_allocation_timeout_seconds"` // Time before pending allocations are cleaned up (default: 120)

	// Tier selection configuration
	TierFieldName       string `yaml:"tier_field_name"`       // JSON body field name for tier (default: "tier")
	TierHeader          string `yaml:"tier_header"`           // Header name for tier (default: "X-Tier")

	// Tier specifications
	Tiers               []TierSpec `yaml:"tiers"`             // Resource requirements for each tier

	// Database configuration
	DBMaxOpenConns      int      `yaml:"db_max_open_conns"`     // Max open database connections
	DBMaxIdleConns      int      `yaml:"db_max_idle_conns"`     // Max idle database connections
	DBConnMaxLifetime   int      `yaml:"db_conn_max_lifetime"`  // Connection max lifetime in seconds

	// Graceful shutdown
	ShutdownTimeout     int      `yaml:"shutdown_timeout_seconds"` // Graceful shutdown timeout (default: 30)

	// Health check configuration
	HealthCheckEnabled         bool   `yaml:"health_check_enabled"`          // Enable active health checks (default: true)
	HealthCheckIntervalSecs    int    `yaml:"health_check_interval_seconds"` // Health check interval (default: 10)
	HealthCheckTimeoutSecs     int    `yaml:"health_check_timeout_seconds"`  // Health check timeout (default: 2)
	HealthCheckType            string `yaml:"health_check_type"`             // "tcp" or "http" (default: tcp)
	HealthCheckPath            string `yaml:"health_check_path"`             // HTTP path for health checks (default: /health)
	HealthCheckUnhealthyThreshold int `yaml:"health_check_unhealthy_threshold"` // Consecutive failures before unhealthy (default: 3)
	HealthCheckHealthyThreshold   int `yaml:"health_check_healthy_threshold"`   // Consecutive successes before healthy (default: 2)
}

// ClientConfig represents the client configuration
type ClientConfig struct {
	ServerURL       string           `yaml:"server_url"`
	ClientID        string           `yaml:"client_id"`
	Hostname        string           `yaml:"hostname"`
	WindowMinutes   int              `yaml:"window_minutes"`
	ReportInterval  int              `yaml:"report_interval_seconds"`
	DiskPath        string           `yaml:"disk_path"`
	LogLevel        string           `yaml:"log_level"`
	EndpointURL     string           `yaml:"endpoint_url"`
	Endpoints       []EndpointConfig `yaml:"endpoints"`
	GeoIPDBPath     string           `yaml:"geoip_db_path"`
	SkipGeolocation bool             `yaml:"skip_geolocation"`
	InsecureTLS     bool             `yaml:"insecure_tls"`
	ServerKey       string           `yaml:"server_key"`
}

// LoadServerConfig loads server configuration from YAML file
func LoadServerConfig(path string) (*ServerConfig, error) {
	// Default configuration with standard tiers and security defaults
	config := &ServerConfig{
		Port:                8080,
		Database:            "opsen.db",
		StaleMinutes:        5,
		CleanupIntervalSecs: 60,
		Host:                "0.0.0.0",
		LogLevel:            "info",
		JSONLogging:         false,

		// Proxy defaults
		ProxySSEFlushInterval: -1,         // Immediate flush for SSE support by default

		// Security defaults
		RateLimitPerMinute:  60,           // 60 requests per minute per IP
		RateLimitBurst:      120,          // Allow burst of 120 requests
		MaxRequestBodyBytes: 10 * 1024 * 1024, // 10MB max request body
		RequestTimeout:      30,           // 30 second request timeout
		IdleTimeout:         120,          // 120 second idle timeout (2 minutes)
		ReadHeaderTimeout:   10,           // 10 second read header timeout (Slowloris protection)
		TLSInsecureSkipVerify: false,      // Secure by default
		DisableSecurityHeaders: false,     // Enable security headers by default

		// Sticky session defaults
		StickyHeader:          "",         // Disabled by default
		StickyByIP:            false,      // Disabled by default
		StickyAffinityEnabled: true,       // When enabled, prefer same server across tiers
		PendingAllocationTimeoutSecs: 120, // 2 minutes default

		// Tier selection defaults
		TierFieldName:         "tier",     // Default JSON field name
		TierHeader:            "X-Tier",   // Default header name

		// Database defaults
		DBMaxOpenConns:      25,
		DBMaxIdleConns:      5,
		DBConnMaxLifetime:   300, // 5 minutes

		// Graceful shutdown
		ShutdownTimeout:     30,

		// Health check defaults
		HealthCheckEnabled:            true,
		HealthCheckIntervalSecs:       10,
		HealthCheckTimeoutSecs:        2,
		HealthCheckType:               "tcp",
		HealthCheckPath:               "/health",
		HealthCheckUnhealthyThreshold: 3,
		HealthCheckHealthyThreshold:   2,

		Tiers: []TierSpec{
			{Name: "free", VCPU: 1, MemoryGB: 1.0, StorageGB: 0},
			{Name: "lite", VCPU: 1, MemoryGB: 1.0, StorageGB: 5},
			{Name: "pro-standard", VCPU: 2, MemoryGB: 4.0, StorageGB: 20},
			{Name: "pro-turbo", VCPU: 4, MemoryGB: 8.0, StorageGB: 30},
			{Name: "pro-max", VCPU: 8, MemoryGB: 16.0, StorageGB: 40},
		},
	}

	// If no config file specified or doesn't exist, return defaults
	if path == "" {
		return config, nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return config, nil
		}
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	if err := yaml.Unmarshal(data, config); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}

	// If no tiers configured, use defaults
	if len(config.Tiers) == 0 {
		config.Tiers = []TierSpec{
			{Name: "free", VCPU: 1, MemoryGB: 1.0, StorageGB: 0},
			{Name: "lite", VCPU: 1, MemoryGB: 1.0, StorageGB: 5},
			{Name: "pro-standard", VCPU: 2, MemoryGB: 4.0, StorageGB: 20},
			{Name: "pro-turbo", VCPU: 4, MemoryGB: 8.0, StorageGB: 30},
			{Name: "pro-max", VCPU: 8, MemoryGB: 16.0, StorageGB: 40},
		}
	}

	return config, nil
}

// LoadClientConfig loads client configuration from YAML file
func LoadClientConfig(path string) (*ClientConfig, error) {
	// Default configuration
	config := &ClientConfig{
		ServerURL:      "http://localhost:8080",
		WindowMinutes:  15,
		ReportInterval: 60,
		DiskPath:       "/",
		LogLevel:       "info",
	}

	// If no config file specified or doesn't exist, return defaults
	if path == "" {
		return config, nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return config, nil
		}
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	if err := yaml.Unmarshal(data, config); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}

	return config, nil
}

// SaveServerConfig saves server configuration to YAML file
func SaveServerConfig(config *ServerConfig, path string) error {
	data, err := yaml.Marshal(config)
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}

	return nil
}

// SaveClientConfig saves client configuration to YAML file
func SaveClientConfig(config *ClientConfig, path string) error {
	data, err := yaml.Marshal(config)
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}

	return nil
}
