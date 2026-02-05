package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"cyqle.in/opsen/common"
)

func TestHandleRegister_MultiEndpoint(t *testing.T) {
	tests := []struct {
		name              string
		registration      common.ClientRegistration
		expectedEndpoint  string
		expectedEndpoints []common.EndpointConfig
	}{
		{
			name: "Register with multiple endpoints",
			registration: common.ClientRegistration{
				ClientID:     "test-client-1",
				Hostname:     "worker-1",
				PublicIP:     "1.2.3.4",
				LocalIP:      "172.19.0.2",
				TotalCPU:     8,
				TotalMemory:  16.0,
				TotalStorage: 100.0,
				Endpoints: []common.EndpointConfig{
					{URL: "https://172.19.0.2:11000", Paths: []string{"/v1", "/api"}},
					{URL: "https://172.19.0.2:8002", Paths: []string{"/"}},
				},
			},
			expectedEndpoint: "https://172.19.0.2:11000",
			expectedEndpoints: []common.EndpointConfig{
				{URL: "https://172.19.0.2:11000", Paths: []string{"/v1", "/api"}},
				{URL: "https://172.19.0.2:8002", Paths: []string{"/"}},
			},
		},
		{
			name: "Register with EndpointURL only",
			registration: common.ClientRegistration{
				ClientID:     "test-client-2",
				Hostname:     "worker-2",
				PublicIP:     "1.2.3.5",
				LocalIP:      "172.19.0.3",
				TotalCPU:     4,
				TotalMemory:  8.0,
				TotalStorage: 50.0,
				EndpointURL:  "http://custom:9000",
			},
			expectedEndpoint:  "http://custom:9000",
			expectedEndpoints: nil,
		},
		{
			name: "Register with both - Endpoints takes precedence",
			registration: common.ClientRegistration{
				ClientID:     "test-client-3",
				Hostname:     "worker-3",
				PublicIP:     "1.2.3.6",
				LocalIP:      "172.19.0.4",
				TotalCPU:     8,
				TotalMemory:  16.0,
				TotalStorage: 100.0,
				EndpointURL:  "http://old:11000",
				Endpoints: []common.EndpointConfig{
					{URL: "http://new:11000", Paths: []string{"/v1"}},
				},
			},
			expectedEndpoint: "http://new:11000",
			expectedEndpoints: []common.EndpointConfig{
				{URL: "http://new:11000", Paths: []string{"/v1"}},
			},
		},
		{
			name: "Register without endpoint - uses LocalIP",
			registration: common.ClientRegistration{
				ClientID:     "test-client-4",
				Hostname:     "worker-4",
				PublicIP:     "1.2.3.7",
				LocalIP:      "172.19.0.5",
				TotalCPU:     2,
				TotalMemory:  4.0,
				TotalStorage: 20.0,
			},
			expectedEndpoint:  "http://172.19.0.5:11000",
			expectedEndpoints: nil,
		},
		{
			name: "Cyqle production configuration",
			registration: common.ClientRegistration{
				ClientID:     "cyqle-worker-1",
				Hostname:     "cyqle-worker-1",
				PublicIP:     "5.161.79.228",
				LocalIP:      "172.19.0.2",
				TotalCPU:     8,
				TotalMemory:  16.0,
				TotalStorage: 100.0,
				Endpoints: []common.EndpointConfig{
					{URL: "https://172.19.0.2:11000", Paths: []string{"/v1", "/api", "/auth", "/admin"}},
					{URL: "https://172.19.0.2:8002", Paths: []string{"/"}},
				},
			},
			expectedEndpoint: "https://172.19.0.2:11000",
			expectedEndpoints: []common.EndpointConfig{
				{URL: "https://172.19.0.2:11000", Paths: []string{"/v1", "/api", "/auth", "/admin"}},
				{URL: "https://172.19.0.2:8002", Paths: []string{"/"}},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db, cleanup := CreateTestDB(t)
			defer cleanup()
			server := NewTestServer(t, db)

			body, _ := json.Marshal(tt.registration)
			req := httptest.NewRequest("POST", "/register", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()

			server.handleRegister(rec, req)

			if rec.Code != http.StatusOK {
				t.Fatalf("Expected status 200, got %d: %s", rec.Code, rec.Body.String())
			}

			server.mu.RLock()
			client, ok := server.clientCache[tt.registration.ClientID]
			server.mu.RUnlock()

			if !ok {
				t.Fatalf("Client %s not found in cache", tt.registration.ClientID)
			}

			if client.Endpoint != tt.expectedEndpoint {
				t.Errorf("Expected endpoint %q, got %q", tt.expectedEndpoint, client.Endpoint)
			}

			if len(client.Endpoints) != len(tt.expectedEndpoints) {
				t.Errorf("Expected %d endpoints, got %d", len(tt.expectedEndpoints), len(client.Endpoints))
			}

			for i, expected := range tt.expectedEndpoints {
				if i >= len(client.Endpoints) {
					break
				}
				actual := client.Endpoints[i]
				if actual.URL != expected.URL {
					t.Errorf("Endpoint %d: expected URL %q, got %q", i, expected.URL, actual.URL)
				}
				if len(actual.Paths) != len(expected.Paths) {
					t.Errorf("Endpoint %d: expected %d paths, got %d", i, len(expected.Paths), len(actual.Paths))
					continue
				}
				for j, expectedPath := range expected.Paths {
					if actual.Paths[j] != expectedPath {
						t.Errorf("Endpoint %d path %d: expected %q, got %q", i, j, expectedPath, actual.Paths[j])
					}
				}
			}
		})
	}
}

func TestHandleRegister_EndpointSelection(t *testing.T) {
	t.Run("SelectEndpoint after registration", func(t *testing.T) {
		db, cleanup := CreateTestDB(t)
		defer cleanup()
		server := NewTestServer(t, db)

		registration := common.ClientRegistration{
			ClientID:     "test-select",
			Hostname:     "worker-select",
			PublicIP:     "1.2.3.8",
			LocalIP:      "172.19.0.6",
			TotalCPU:     8,
			TotalMemory:  16.0,
			TotalStorage: 100.0,
			Endpoints: []common.EndpointConfig{
				{URL: "https://172.19.0.6:11000", Paths: []string{"/v1", "/api"}},
				{URL: "https://172.19.0.6:8002", Paths: []string{"/"}},
			},
		}

		body, _ := json.Marshal(registration)
		req := httptest.NewRequest("POST", "/register", bytes.NewReader(body))
		rec := httptest.NewRecorder()

		server.handleRegister(rec, req)

		server.mu.RLock()
		client := server.clientCache["test-select"]
		server.mu.RUnlock()

		tests := []struct {
			path     string
			expected string
		}{
			{"/v1/sessions", "https://172.19.0.6:11000"},
			{"/api/users", "https://172.19.0.6:11000"},
			{"/monitor/vnc", "https://172.19.0.6:8002"},
			{"/websockify", "https://172.19.0.6:8002"},
		}

		for _, tt := range tests {
			result := client.SelectEndpoint(tt.path)
			if result != tt.expected {
				t.Errorf("SelectEndpoint(%q) = %q, expected %q", tt.path, result, tt.expected)
			}
		}
	})
}
