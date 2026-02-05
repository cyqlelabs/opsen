package main

import (
	"testing"

	"cyqle.in/opsen/common"
)

// TestMatchPathPattern tests the wildcard matching logic
func TestMatchPathPattern(t *testing.T) {
	tests := []struct {
		name        string
		requestPath string
		pattern     string
		shouldMatch bool
	}{
		// Exact matches
		{"Exact match root", "/", "/", true},
		{"Exact match path", "/api/users", "/api/users", true},
		{"Exact match with trailing", "/api/v1/", "/api/v1/", true},

		// Prefix matches (no wildcards)
		{"Prefix match simple", "/api/users", "/api", true},
		{"Prefix match nested", "/api/v1/users/123", "/api/v1", true},
		{"Prefix match root", "/anything", "/", true},
		{"Prefix no match", "/users/api", "/api", false},

		// Wildcard matches - single asterisk
		{"Wildcard catch-all", "/api/users", "/*", true},
		{"Wildcard catch-all root", "/", "/*", true},
		{"Wildcard catch-all nested", "/api/v1/users/123", "/*", true},
		{"Wildcard suffix", "/api/anything", "/api/*", true},
		{"Wildcard suffix nested", "/api/v1/users", "/api/*", true},
		{"Wildcard no match", "/users/api", "/api/*", false},

		// Wildcard matches - pattern matching
		{"Wildcard middle", "/api/v1/users", "/api/*/users", true},
		{"Wildcard middle v2", "/api/v2/users", "/api/*/users", true},
		{"Wildcard middle no match", "/api/v1/posts", "/api/*/users", false},
		{"Wildcard multiple", "/api/v1/users/123", "/api/*/users/*", true},

		// Edge cases
		{"Empty path empty pattern", "", "", true},
		{"Empty path with pattern", "", "/api", false},
		{"Path with empty pattern", "/api", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			matched, _ := matchPathPattern(tt.requestPath, tt.pattern)
			if matched != tt.shouldMatch {
				t.Errorf("matchPathPattern(%q, %q) = %v, want %v",
					tt.requestPath, tt.pattern, matched, tt.shouldMatch)
			}
		})
	}
}

// TestMatchPathPattern_Specificity tests that more specific patterns get higher scores
func TestMatchPathPattern_Specificity(t *testing.T) {
	tests := []struct {
		name         string
		requestPath  string
		patterns     []string
		expectedBest string // The pattern that should win
	}{
		{
			name:         "Exact beats prefix",
			requestPath:  "/api",
			patterns:     []string{"/api", "/"},
			expectedBest: "/api",
		},
		{
			name:         "Exact beats wildcard",
			requestPath:  "/api/users",
			patterns:     []string{"/api/*", "/api/users"},
			expectedBest: "/api/users",
		},
		{
			name:         "Prefix beats wildcard",
			requestPath:  "/api/users/123",
			patterns:     []string{"/api", "/*"},
			expectedBest: "/api",
		},
		{
			name:         "Longer prefix beats shorter",
			requestPath:  "/api/v1/users",
			patterns:     []string{"/api", "/api/v1"},
			expectedBest: "/api/v1",
		},
		{
			name:         "More specific wildcard beats less specific",
			requestPath:  "/api/v1/users",
			patterns:     []string{"/api/*", "/*"},
			expectedBest: "/api/*",
		},
		{
			name:         "Pattern with fewer wildcards wins",
			requestPath:  "/api/v1/users/123",
			patterns:     []string{"/api/*/users/*", "/api/v1/users/*"},
			expectedBest: "/api/v1/users/*",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bestPattern := ""
			bestScore := -1

			for _, pattern := range tt.patterns {
				matched, score := matchPathPattern(tt.requestPath, pattern)
				if matched && score > bestScore {
					bestScore = score
					bestPattern = pattern
				}
			}

			if bestPattern != tt.expectedBest {
				t.Errorf("Expected pattern %q to win for path %q, but %q won",
					tt.expectedBest, tt.requestPath, bestPattern)
			}
		})
	}
}

// TestClientState_SelectEndpoint_Wildcards tests wildcard patterns in endpoint selection
func TestClientState_SelectEndpoint_Wildcards(t *testing.T) {
	tests := []struct {
		name        string
		client      *ClientState
		requestPath string
		expected    string
	}{
		{
			name: "Wildcard catch-all",
			client: &ClientState{
				Endpoint: "http://fallback:11000",
				Endpoints: []common.EndpointConfig{
					{URL: "http://catchall:9000", Paths: []string{"/*"}},
				},
			},
			requestPath: "/anything/goes/here",
			expected:    "http://catchall:9000",
		},
		{
			name: "Wildcard suffix /api/*",
			client: &ClientState{
				Endpoint: "http://fallback:11000",
				Endpoints: []common.EndpointConfig{
					{URL: "http://api:8080", Paths: []string{"/api/*"}},
					{URL: "http://other:9000", Paths: []string{"/other/*"}},
				},
			},
			requestPath: "/api/users/123",
			expected:    "http://api:8080",
		},
		{
			name: "Exact match beats wildcard",
			client: &ClientState{
				Endpoint: "http://fallback:11000",
				Endpoints: []common.EndpointConfig{
					{URL: "http://wildcard:8080", Paths: []string{"/api/*"}},
					{URL: "http://exact:9000", Paths: []string{"/api/users"}},
				},
			},
			requestPath: "/api/users",
			expected:    "http://exact:9000",
		},
		{
			name: "Prefix beats wildcard",
			client: &ClientState{
				Endpoint: "http://fallback:11000",
				Endpoints: []common.EndpointConfig{
					{URL: "http://wildcard:8080", Paths: []string{"/*"}},
					{URL: "http://prefix:9000", Paths: []string{"/api"}},
				},
			},
			requestPath: "/api/users",
			expected:    "http://prefix:9000",
		},
		{
			name: "More specific wildcard wins",
			client: &ClientState{
				Endpoint: "http://fallback:11000",
				Endpoints: []common.EndpointConfig{
					{URL: "http://general:8080", Paths: []string{"/*"}},
					{URL: "http://specific:9000", Paths: []string{"/api/*"}},
				},
			},
			requestPath: "/api/v1/users",
			expected:    "http://specific:9000",
		},
		{
			name: "Wildcard pattern matching",
			client: &ClientState{
				Endpoint: "http://fallback:11000",
				Endpoints: []common.EndpointConfig{
					{URL: "http://users:8080", Paths: []string{"/api/*/users"}},
					{URL: "http://posts:9000", Paths: []string{"/api/*/posts"}},
				},
			},
			requestPath: "/api/v1/users",
			expected:    "http://users:8080",
		},
		{
			name: "Complex pattern with multiple wildcards",
			client: &ClientState{
				Endpoint: "http://fallback:11000",
				Endpoints: []common.EndpointConfig{
					{URL: "http://complex:8080", Paths: []string{"/api/*/users/*"}},
					{URL: "http://other:9000", Paths: []string{"/other"}},
				},
			},
			requestPath: "/api/v1/users/123",
			expected:    "http://complex:8080",
		},
		{
			name: "Wildcard no match falls back to first endpoint",
			client: &ClientState{
				Endpoint: "http://fallback:11000",
				Endpoints: []common.EndpointConfig{
					{URL: "http://api:8080", Paths: []string{"/api/*"}},
					{URL: "http://other:9000", Paths: []string{"/other/*"}},
				},
			},
			requestPath: "/nomatch/path",
			expected:    "http://api:8080", // First endpoint as fallback
		},
		{
			name: "Mixed wildcards and prefixes",
			client: &ClientState{
				Endpoint: "http://fallback:11000",
				Endpoints: []common.EndpointConfig{
					{URL: "http://wildcard:8080", Paths: []string{"/api/*", "/v1/*"}},
					{URL: "http://prefix:9000", Paths: []string{"/api/v1", "/monitor"}},
					{URL: "http://catchall:7000", Paths: []string{"/"}},
				},
			},
			requestPath: "/api/v1/users",
			expected:    "http://prefix:9000", // Prefix /api/v1 beats wildcard /api/*
		},
		{
			name: "Root wildcard /* vs root prefix /",
			client: &ClientState{
				Endpoint: "http://fallback:11000",
				Endpoints: []common.EndpointConfig{
					{URL: "http://wildcard:8080", Paths: []string{"/*"}},
					{URL: "http://prefix:9000", Paths: []string{"/"}},
				},
			},
			requestPath: "/anything",
			expected:    "http://prefix:9000", // Prefix / beats wildcard /*
		},
		{
			name: "Multiple paths with wildcards",
			client: &ClientState{
				Endpoint: "http://fallback:11000",
				Endpoints: []common.EndpointConfig{
					{URL: "http://multi:8080", Paths: []string{"/api/*", "/v1/*", "/auth/*"}},
				},
			},
			requestPath: "/auth/login",
			expected:    "http://multi:8080",
		},
		{
			name: "Wildcard with exact match in same endpoint",
			client: &ClientState{
				Endpoint: "http://fallback:11000",
				Endpoints: []common.EndpointConfig{
					{URL: "http://mixed:8080", Paths: []string{"/api/*", "/api/users"}},
				},
			},
			requestPath: "/api/users",
			expected:    "http://mixed:8080", // Exact match within same endpoint wins
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.client.SelectEndpoint(tt.requestPath)
			if result != tt.expected {
				t.Errorf("SelectEndpoint(%q) = %q, want %q",
					tt.requestPath, result, tt.expected)
			}
		})
	}
}

// TestClientState_SelectEndpoint_BackwardCompatibility ensures existing behavior still works
func TestClientState_SelectEndpoint_BackwardCompatibility(t *testing.T) {
	tests := []struct {
		name        string
		client      *ClientState
		requestPath string
		expected    string
	}{
		{
			name: "Simple prefix matching still works",
			client: &ClientState{
				Endpoint: "http://fallback:11000",
				Endpoints: []common.EndpointConfig{
					{URL: "http://api:8080", Paths: []string{"/api"}},
					{URL: "http://monitor:9000", Paths: []string{"/monitor"}},
				},
			},
			requestPath: "/api/users",
			expected:    "http://api:8080",
		},
		{
			name: "Root path prefix matching",
			client: &ClientState{
				Endpoint: "http://fallback:11000",
				Endpoints: []common.EndpointConfig{
					{URL: "http://api:8080", Paths: []string{"/api"}},
					{URL: "http://root:9000", Paths: []string{"/"}},
				},
			},
			requestPath: "/other/path",
			expected:    "http://root:9000",
		},
		{
			name: "Longer prefix wins",
			client: &ClientState{
				Endpoint: "http://fallback:11000",
				Endpoints: []common.EndpointConfig{
					{URL: "http://short:8080", Paths: []string{"/api"}},
					{URL: "http://long:9000", Paths: []string{"/api/v1"}},
				},
			},
			requestPath: "/api/v1/users",
			expected:    "http://long:9000",
		},
		{
			name: "First endpoint fallback when no match",
			client: &ClientState{
				Endpoint: "http://fallback:11000",
				Endpoints: []common.EndpointConfig{
					{URL: "http://first:8080", Paths: []string{"/api"}},
					{URL: "http://second:9000", Paths: []string{"/monitor"}},
				},
			},
			requestPath: "/nomatch",
			expected:    "http://first:8080",
		},
		{
			name: "Single endpoint_url when endpoints is empty",
			client: &ClientState{
				Endpoint:  "http://single:11000",
				Endpoints: []common.EndpointConfig{},
			},
			requestPath: "/any/path",
			expected:    "http://single:11000",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.client.SelectEndpoint(tt.requestPath)
			if result != tt.expected {
				t.Errorf("SelectEndpoint(%q) = %q, want %q",
					tt.requestPath, result, tt.expected)
			}
		})
	}
}
