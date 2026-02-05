package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"cyqle.in/opsen/common"
)

func TestHandleProxy_WebSocketDetectionAndBody(t *testing.T) {
	tests := []struct {
		name              string
		setupRequest      func() *http.Request
		requestBody       string
		isWebSocket       bool
		expectedBodyRead  bool
		tier              string
	}{
		{
			name: "Regular HTTP POST with JSON body",
			setupRequest: func() *http.Request {
				body := `{"tier":"pro","client_lat":40.7128,"client_lon":-74.0060}`
				req := httptest.NewRequest("POST", "/v1/sessions", strings.NewReader(body))
				req.Header.Set("Content-Type", "application/json")
				return req
			},
			requestBody:      `{"tier":"pro","client_lat":40.7128,"client_lon":-74.0060}`,
			isWebSocket:      false,
			expectedBodyRead: true,
			tier:             "pro",
		},
		{
			name: "WebSocket upgrade request - body not read",
			setupRequest: func() *http.Request {
				req := httptest.NewRequest("GET", "/websockify?tier=pro", nil)
				req.Header.Set("Upgrade", "websocket")
				req.Header.Set("Connection", "Upgrade")
				req.Header.Set("Sec-WebSocket-Key", "dGhlIHNhbXBsZSBub25jZQ==")
				req.Header.Set("Sec-WebSocket-Version", "13")
				return req
			},
			requestBody:      "",
			isWebSocket:      true,
			expectedBodyRead: false,
			tier:             "pro",
		},
		{
			name: "Regular GET request",
			setupRequest: func() *http.Request {
				req := httptest.NewRequest("GET", "/v1/sessions?tier=pro", nil)
				return req
			},
			requestBody:      "",
			isWebSocket:      false,
			expectedBodyRead: false,
			tier:             "pro",
		},
	}

	for i, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db, cleanup := CreateTestDB(t)
			defer cleanup()

			// Create server with custom config that includes "pro" tier
			server := NewTestServerWithConfig(t, db, func(config *common.ServerConfig) {
				config.Tiers = append(config.Tiers,
					common.TierSpec{Name: "pro", VCPU: 4, MemoryGB: 8.0, StorageGB: 50},
				)
			})

			// Create a test backend server to verify the proxied request
			backendCalled := false
			var receivedBody string
			var wasWebSocket bool

			backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				backendCalled = true
				wasWebSocket = r.Header.Get("Upgrade") == "websocket"

				// Read body
				buf := new(bytes.Buffer)
				_, _ = buf.ReadFrom(r.Body)
				receivedBody = buf.String()

				// Always return OK for this test
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{"status":"ok"}`))
			}))
			defer backend.Close()

			// Create mock client with available resources
			clientID := fmt.Sprintf("ws-test-client-%d", i)
			client := NewMockClient(MockClientOptions{
				ClientID:     clientID,
				Hostname:     "ws-test-host",
				TotalCPU:     8,
				TotalMemory:  16.0,
				MemoryAvail:  14.0,
				TotalStorage: 100.0,
				DiskAvail:    90.0,
				CPUUsageAvg:  []float64{10, 10, 10, 10, 10, 10, 10, 10},
			})
			client.Endpoints = []common.EndpointConfig{
				{URL: backend.URL, Paths: []string{"/v1", "/api"}},
				{URL: backend.URL, Paths: []string{"/"}},
			}
			client.Endpoint = backend.URL
			server.AddMockClient(client)

			// Make the request
			req := tt.setupRequest()
			rec := httptest.NewRecorder()

			server.handleProxy(rec, req)

			// Verify backend was called
			if !backendCalled {
				t.Error("Backend was not called")
				return
			}

			// Verify WebSocket detection
			if tt.isWebSocket != wasWebSocket {
				t.Errorf("WebSocket detection mismatch: expected %v, got %v", tt.isWebSocket, wasWebSocket)
			}

			// Verify body handling
			if tt.expectedBodyRead {
				if receivedBody != tt.requestBody {
					t.Errorf("Expected body %q to be forwarded, got %q", tt.requestBody, receivedBody)
				}
			} else {
				// For non-body requests (GET) and WebSocket, body should be empty
				if receivedBody != "" {
					t.Errorf("Expected empty body, got %q", receivedBody)
				}
			}
		})
	}
}

func TestHandleProxy_HTTPWebSocketConcurrent(t *testing.T) {
	db, cleanup := CreateTestDB(t)
	defer cleanup()

	server := NewTestServerWithConfig(t, db, func(config *common.ServerConfig) {
		config.Tiers = append(config.Tiers,
			common.TierSpec{Name: "pro", VCPU: 4, MemoryGB: 8.0, StorageGB: 50},
		)
	})

	// Create test backend
	var httpCount, wsCount atomic.Int32
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Upgrade") == "websocket" {
			wsCount.Add(1)
		} else {
			httpCount.Add(1)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	defer backend.Close()

	// Create mock clients with resources
	for i := 0; i < 3; i++ {
		client := NewMockClient(MockClientOptions{
			ClientID:     fmt.Sprintf("concurrent-client-%d", i),
			Hostname:     "concurrent-test",
			TotalCPU:     8,
			TotalMemory:  16.0,
			MemoryAvail:  14.0,
			TotalStorage: 100.0,
			DiskAvail:    90.0,
			CPUUsageAvg:  []float64{10, 10, 10, 10, 10, 10, 10, 10},
		})
		client.Endpoint = backend.URL
		client.Endpoints = []common.EndpointConfig{
			{URL: backend.URL, Paths: []string{"/v1", "/api"}},
			{URL: backend.URL, Paths: []string{"/"}},
		}
		server.AddMockClient(client)
	}

	// Send concurrent HTTP and WebSocket requests
	done := make(chan bool, 20)

	// HTTP requests
	for i := 0; i < 10; i++ {
		go func(id int) {
			defer func() { done <- true }()
			body := map[string]interface{}{"tier": "pro"}
			bodyBytes, _ := json.Marshal(body)
			req := httptest.NewRequest("POST", "/v1/sessions", bytes.NewReader(bodyBytes))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("X-Session-ID", fmt.Sprintf("http-session-%d", id))
			rec := httptest.NewRecorder()

			server.handleProxy(rec, req)
		}(i)
	}

	// WebSocket requests
	for i := 0; i < 10; i++ {
		go func(id int) {
			defer func() { done <- true }()
			req := httptest.NewRequest("GET", "/websockify?tier=pro", nil)
			req.Header.Set("Upgrade", "websocket")
			req.Header.Set("Connection", "Upgrade")
			req.Header.Set("X-Session-ID", fmt.Sprintf("ws-session-%d", id))
			rec := httptest.NewRecorder()

			server.handleProxy(rec, req)
		}(i)
	}

	// Wait for all requests to complete
	for i := 0; i < 20; i++ {
		<-done
	}

	// Verify both types of requests were processed
	if httpCount.Load() == 0 {
		t.Error("No HTTP requests were processed")
	}
	if wsCount.Load() == 0 {
		t.Error("No WebSocket requests were processed")
	}

	t.Logf("Processed %d HTTP requests and %d WebSocket requests concurrently", httpCount.Load(), wsCount.Load())
}

func TestHandleProxy_StickySessionsWithWebSocket(t *testing.T) {
	db, cleanup := CreateTestDB(t)
	defer cleanup()

	server := NewTestServerWithConfig(t, db, func(config *common.ServerConfig) {
		config.Tiers = append(config.Tiers,
			common.TierSpec{Name: "pro", VCPU: 4, MemoryGB: 8.0, StorageGB: 50},
		)
	})

	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	defer backend.Close()

	// Register multiple mock clients with plenty of resources
	for i := 0; i < 3; i++ {
		client := NewMockClient(MockClientOptions{
			ClientID:     fmt.Sprintf("sticky-client-%d", i),
			Hostname:     "sticky-test",
			TotalCPU:     16,
			TotalMemory:  32.0,
			MemoryAvail:  30.0,
			TotalStorage: 200.0,
			DiskAvail:    190.0,
			CPUUsageAvg:  []float64{5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5},
		})
		client.Endpoint = backend.URL
		server.AddMockClient(client)
	}

	sessionID := "test-session-123"
	tier := "pro"
	var firstClientID string

	// Make first HTTP request with session ID
	httpBody := map[string]interface{}{"tier": tier}
	httpBodyBytes, _ := json.Marshal(httpBody)
	httpReq := httptest.NewRequest("POST", "/v1/sessions", bytes.NewReader(httpBodyBytes))
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("X-Session-ID", sessionID)
	httpRec := httptest.NewRecorder()

	server.handleProxy(httpRec, httpReq)

	// Capture which client was selected
	server.mu.RLock()
	if tierMap, ok := server.stickyAssignments[sessionID]; ok {
		if clientID, ok := tierMap[tier]; ok {
			firstClientID = clientID
		}
	}
	server.mu.RUnlock()

	if firstClientID == "" {
		t.Fatal("No sticky assignment created for HTTP request")
	}

	// Make WebSocket request with same session ID
	wsReq := httptest.NewRequest("GET", "/websockify?tier=pro", nil)
	wsReq.Header.Set("Upgrade", "websocket")
	wsReq.Header.Set("Connection", "Upgrade")
	wsReq.Header.Set("X-Session-ID", sessionID)
	wsRec := httptest.NewRecorder()

	server.handleProxy(wsRec, wsReq)

	// Verify same client was selected
	server.mu.RLock()
	tierMap, ok := server.stickyAssignments[sessionID]
	server.mu.RUnlock()

	if !ok {
		t.Fatal("Sticky assignment lost after WebSocket request")
	}

	clientID, ok := tierMap[tier]
	if !ok {
		t.Fatalf("No assignment for tier %s", tier)
	}

	if clientID != firstClientID {
		t.Errorf("WebSocket request routed to different client. Expected %s, got %s", firstClientID, clientID)
	}

	t.Logf("Both HTTP and WebSocket requests correctly routed to client %s via sticky session", firstClientID)
}

func TestHandleProxy_PathBasedRoutingWithWebSocket(t *testing.T) {
	db, cleanup := CreateTestDB(t)
	defer cleanup()

	server := NewTestServerWithConfig(t, db, func(config *common.ServerConfig) {
		config.Tiers = append(config.Tiers,
			common.TierSpec{Name: "pro", VCPU: 4, MemoryGB: 8.0, StorageGB: 50},
		)
	})

	var apiBackendCalled, monitorBackendCalled bool

	// Create separate backends for API and monitor
	apiBackend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		apiBackendCalled = true
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"service":"api"}`))
	}))
	defer apiBackend.Close()

	monitorBackend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		monitorBackendCalled = true
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"service":"monitor"}`))
	}))
	defer monitorBackend.Close()

	// Create mock client with multiple endpoints and plenty of resources
	client := NewMockClient(MockClientOptions{
		ClientID:     "path-test-client",
		Hostname:     "path-test-host",
		TotalCPU:     16,
		TotalMemory:  32.0,
		MemoryAvail:  30.0,
		TotalStorage: 200.0,
		DiskAvail:    190.0,
		CPUUsageAvg:  []float64{5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5},
	})
	client.Endpoints = []common.EndpointConfig{
		{URL: apiBackend.URL, Paths: []string{"/v1", "/api"}},
		{URL: monitorBackend.URL, Paths: []string{"/"}},
	}
	client.Endpoint = apiBackend.URL
	server.AddMockClient(client)

	// Send HTTP request to API endpoint
	apiBodyBytes, _ := json.Marshal(map[string]interface{}{"tier": "pro"})
	apiReq := httptest.NewRequest("POST", "/v1/sessions", bytes.NewReader(apiBodyBytes))
	apiReq.Header.Set("Content-Type", "application/json")
	apiRec := httptest.NewRecorder()

	server.handleProxy(apiRec, apiReq)

	if !apiBackendCalled {
		t.Error("API backend was not called for /v1 path")
	}

	// Send WebSocket request to monitor endpoint
	wsReq := httptest.NewRequest("GET", "/websockify?tier=pro", nil)
	wsReq.Header.Set("Upgrade", "websocket")
	wsReq.Header.Set("Connection", "Upgrade")
	wsRec := httptest.NewRecorder()

	server.handleProxy(wsRec, wsReq)

	if !monitorBackendCalled {
		t.Error("Monitor backend was not called for /websockify path")
	}

	t.Log("Path-based routing correctly separated HTTP API and WebSocket monitor traffic")
}

func TestWebSocketDetection(t *testing.T) {
	tests := []struct {
		name        string
		headers     map[string]string
		expectWS    bool
		description string
	}{
		{
			name: "Standard WebSocket upgrade",
			headers: map[string]string{
				"Upgrade":    "websocket",
				"Connection": "Upgrade",
			},
			expectWS:    true,
			description: "Standard WebSocket upgrade headers",
		},
		{
			name: "WebSocket with mixed case",
			headers: map[string]string{
				"Upgrade":    "WebSocket",
				"Connection": "Upgrade",
			},
			expectWS:    true,
			description: "Case-insensitive WebSocket detection",
		},
		{
			name: "WebSocket with keep-alive in Connection",
			headers: map[string]string{
				"Upgrade":    "websocket",
				"Connection": "keep-alive, Upgrade",
			},
			expectWS:    true,
			description: "Connection header with multiple values",
		},
		{
			name: "Not a WebSocket - HTTP/2 upgrade",
			headers: map[string]string{
				"Upgrade":    "h2c",
				"Connection": "Upgrade",
			},
			expectWS:    false,
			description: "Different upgrade protocol",
		},
		{
			name: "Not a WebSocket - missing Connection",
			headers: map[string]string{
				"Upgrade": "websocket",
			},
			expectWS:    false,
			description: "Upgrade without Connection header",
		},
		{
			name: "Not a WebSocket - missing Upgrade",
			headers: map[string]string{
				"Connection": "Upgrade",
			},
			expectWS:    false,
			description: "Connection without Upgrade header",
		},
		{
			name:        "Not a WebSocket - regular HTTP",
			headers:     map[string]string{},
			expectWS:    false,
			description: "Regular HTTP request",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/test", nil)
			for key, value := range tt.headers {
				req.Header.Set(key, value)
			}

			isWebSocket := strings.ToLower(req.Header.Get("Upgrade")) == "websocket" &&
				strings.Contains(strings.ToLower(req.Header.Get("Connection")), "upgrade")

			if isWebSocket != tt.expectWS {
				t.Errorf("%s: expected isWebSocket=%v, got %v", tt.description, tt.expectWS, isWebSocket)
			}
		})
	}
}
