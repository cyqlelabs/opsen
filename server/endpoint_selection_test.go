package main

import (
	"testing"

	"cyqle.in/opsen/common"
)

func TestClientState_SelectEndpoint(t *testing.T) {
	tests := []struct {
		name      string
		client    *ClientState
		path      string
		expected  string
	}{
		{
			name: "Match first endpoint by path prefix",
			client: &ClientState{
				Endpoint: "http://fallback:11000",
				Endpoints: []common.EndpointConfig{
					{URL: "http://api:11000", Paths: []string{"/v1", "/api"}},
					{URL: "http://monitor:8002", Paths: []string{"/"}},
				},
			},
			path:     "/v1/sessions",
			expected: "http://api:11000",
		},
		{
			name: "Match second endpoint by path prefix",
			client: &ClientState{
				Endpoint: "http://fallback:11000",
				Endpoints: []common.EndpointConfig{
					{URL: "http://api:11000", Paths: []string{"/v1", "/api"}},
					{URL: "http://monitor:8002", Paths: []string{"/"}},
				},
			},
			path:     "/monitor/vnc",
			expected: "http://monitor:8002",
		},
		{
			name: "Match API endpoint with /api prefix",
			client: &ClientState{
				Endpoint: "http://fallback:11000",
				Endpoints: []common.EndpointConfig{
					{URL: "http://api:11000", Paths: []string{"/v1", "/api"}},
					{URL: "http://monitor:8002", Paths: []string{"/"}},
				},
			},
			path:     "/api/users",
			expected: "http://api:11000",
		},
		{
			name: "Fallback to first endpoint when no match",
			client: &ClientState{
				Endpoint: "http://fallback:11000",
				Endpoints: []common.EndpointConfig{
					{URL: "http://api:11000", Paths: []string{"/v1", "/api"}},
					{URL: "http://monitor:8002", Paths: []string{"/admin"}},
				},
			},
			path:     "/other/path",
			expected: "http://api:11000",
		},
		{
			name: "Use Endpoint when Endpoints is empty",
			client: &ClientState{
				Endpoint:  "http://single:11000",
				Endpoints: []common.EndpointConfig{},
			},
			path:     "/any/path",
			expected: "http://single:11000",
		},
		{
			name: "Use Endpoint when Endpoints is nil",
			client: &ClientState{
				Endpoint:  "http://single:11000",
				Endpoints: nil,
			},
			path:     "/any/path",
			expected: "http://single:11000",
		},
		{
			name: "Match root path explicitly",
			client: &ClientState{
				Endpoint: "http://fallback:11000",
				Endpoints: []common.EndpointConfig{
					{URL: "http://api:11000", Paths: []string{"/v1", "/api"}},
					{URL: "http://root:8002", Paths: []string{"/"}},
				},
			},
			path:     "/",
			expected: "http://root:8002",
		},
		{
			name: "Match auth endpoint",
			client: &ClientState{
				Endpoint: "http://fallback:11000",
				Endpoints: []common.EndpointConfig{
					{URL: "http://api:11000", Paths: []string{"/v1", "/api", "/auth", "/admin"}},
					{URL: "http://monitor:8002", Paths: []string{"/"}},
				},
			},
			path:     "/auth/login",
			expected: "http://api:11000",
		},
		{
			name: "Match admin endpoint",
			client: &ClientState{
				Endpoint: "http://fallback:11000",
				Endpoints: []common.EndpointConfig{
					{URL: "http://api:11000", Paths: []string{"/v1", "/api", "/auth", "/admin"}},
					{URL: "http://monitor:8002", Paths: []string{"/"}},
				},
			},
			path:     "/admin/users",
			expected: "http://api:11000",
		},
		{
			name: "Cyqle production config - API request",
			client: &ClientState{
				Endpoint: "http://fallback:11000",
				Endpoints: []common.EndpointConfig{
					{URL: "https://172.19.0.2:11000", Paths: []string{"/v1", "/api", "/auth", "/admin"}},
					{URL: "https://172.19.0.2:8002", Paths: []string{"/"}},
				},
			},
			path:     "/v1/sessions/create",
			expected: "https://172.19.0.2:11000",
		},
		{
			name: "Cyqle production config - Monitor request",
			client: &ClientState{
				Endpoint: "http://fallback:11000",
				Endpoints: []common.EndpointConfig{
					{URL: "https://172.19.0.2:11000", Paths: []string{"/v1", "/api", "/auth", "/admin"}},
					{URL: "https://172.19.0.2:8002", Paths: []string{"/"}},
				},
			},
			path:     "/websockify",
			expected: "https://172.19.0.2:8002",
		},
		{
			name: "Empty path uses fallback",
			client: &ClientState{
				Endpoint: "http://fallback:11000",
				Endpoints: []common.EndpointConfig{
					{URL: "http://api:11000", Paths: []string{"/v1", "/api"}},
					{URL: "http://monitor:8002", Paths: []string{"/monitor"}},
				},
			},
			path:     "",
			expected: "http://api:11000",
		},
		{
			name: "Multiple matching paths - first match wins",
			client: &ClientState{
				Endpoint: "http://fallback:11000",
				Endpoints: []common.EndpointConfig{
					{URL: "http://first:11000", Paths: []string{"/v1"}},
					{URL: "http://second:11000", Paths: []string{"/v1"}},
				},
			},
			path:     "/v1/test",
			expected: "http://first:11000",
		},
		{
			name: "Path specificity - longer path wins over catch-all",
			client: &ClientState{
				Endpoint: "http://fallback:11000",
				Endpoints: []common.EndpointConfig{
					{URL: "http://api:11000", Paths: []string{"/v1", "/api"}},
					{URL: "http://monitor:8002", Paths: []string{"/"}},
				},
			},
			path:     "/v1/sessions",
			expected: "http://api:11000", // /v1 (length 3) wins over / (length 1)
		},
		{
			name: "Path specificity - subscriptions without specific path uses catch-all",
			client: &ClientState{
				Endpoint: "http://fallback:11000",
				Endpoints: []common.EndpointConfig{
					{URL: "http://api:11000", Paths: []string{"/v1", "/api", "/auth", "/admin"}},
					{URL: "http://monitor:8002", Paths: []string{"/"}},
				},
			},
			path:     "/subscriptions/current",
			expected: "http://monitor:8002", // Only / matches, so monitor endpoint wins
		},
		{
			name: "Path specificity - subscriptions with specific path configured",
			client: &ClientState{
				Endpoint: "http://fallback:11000",
				Endpoints: []common.EndpointConfig{
					{URL: "http://api:11000", Paths: []string{"/v1", "/api", "/auth", "/admin", "/subscriptions"}},
					{URL: "http://monitor:8002", Paths: []string{"/"}},
				},
			},
			path:     "/subscriptions/current",
			expected: "http://api:11000", // /subscriptions (length 14) wins over / (length 1)
		},
		{
			name: "Path specificity - longer specific path wins",
			client: &ClientState{
				Endpoint: "http://fallback:11000",
				Endpoints: []common.EndpointConfig{
					{URL: "http://short:11000", Paths: []string{"/api"}},
					{URL: "http://long:8002", Paths: []string{"/api/v2"}},
				},
			},
			path:     "/api/v2/users",
			expected: "http://long:8002", // /api/v2 (length 7) wins over /api (length 4)
		},
		{
			name: "Path specificity - equal length prefers first endpoint",
			client: &ClientState{
				Endpoint: "http://fallback:11000",
				Endpoints: []common.EndpointConfig{
					{URL: "http://first:11000", Paths: []string{"/api"}},
					{URL: "http://second:8002", Paths: []string{"/app"}},
				},
			},
			path:     "/api/test",
			expected: "http://first:11000", // Both /api and /app same length, first endpoint wins
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.client.SelectEndpoint(tt.path)
			if result != tt.expected {
				t.Errorf("SelectEndpoint(%q) = %q, expected %q", tt.path, result, tt.expected)
			}
		})
	}
}

func TestClientState_SelectEndpoint_EdgeCases(t *testing.T) {
	t.Run("Nil client returns empty string", func(t *testing.T) {
		var client *ClientState
		result := client.SelectEndpoint("/test")
		if result != "" {
			t.Errorf("Expected empty string for nil client, got %q", result)
		}
	})

	t.Run("Empty Endpoint and Endpoints returns empty", func(t *testing.T) {
		client := &ClientState{
			Endpoint:  "",
			Endpoints: []common.EndpointConfig{},
		}
		result := client.SelectEndpoint("/test")
		if result != "" {
			t.Errorf("Expected empty string, got %q", result)
		}
	})

	t.Run("Endpoints with empty URL", func(t *testing.T) {
		client := &ClientState{
			Endpoint: "http://fallback:11000",
			Endpoints: []common.EndpointConfig{
				{URL: "", Paths: []string{"/v1"}},
				{URL: "http://valid:8002", Paths: []string{"/"}},
			},
		}
		result := client.SelectEndpoint("/v1/test")
		if result != "" {
			t.Errorf("Expected empty string for empty URL match, got %q", result)
		}
	})

	t.Run("Endpoints with empty Paths array", func(t *testing.T) {
		client := &ClientState{
			Endpoint: "http://fallback:11000",
			Endpoints: []common.EndpointConfig{
				{URL: "http://nopaths:11000", Paths: []string{}},
				{URL: "http://withpaths:8002", Paths: []string{"/"}},
			},
		}
		result := client.SelectEndpoint("/test")
		if result != "http://withpaths:8002" {
			t.Errorf("Expected second endpoint, got %q", result)
		}
	})
}

func TestClientState_SelectEndpoint_PerformanceScenarios(t *testing.T) {
	t.Run("Many endpoints - performance check", func(t *testing.T) {
		endpoints := make([]common.EndpointConfig, 100)
		for i := 0; i < 100; i++ {
			endpoints[i] = common.EndpointConfig{
				URL:   "http://backend" + string(rune(i)) + ":11000",
				Paths: []string{"/path" + string(rune(i))},
			}
		}
		endpoints = append(endpoints, common.EndpointConfig{
			URL:   "http://target:8002",
			Paths: []string{"/target"},
		})

		client := &ClientState{
			Endpoint:  "http://fallback:11000",
			Endpoints: endpoints,
		}

		result := client.SelectEndpoint("/target/test")
		if result != "http://target:8002" {
			t.Errorf("Expected to find target endpoint, got %q", result)
		}
	})
}
