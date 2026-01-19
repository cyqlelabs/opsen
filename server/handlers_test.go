package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"cyqle.in/opsen/common"
)

// TestHandleRegister verifies client registration endpoint
func TestHandleRegister(t *testing.T) {
	db, cleanup := CreateTestDB(t)
	defer cleanup()

	server := NewTestServer(t, db)

	// Create registration payload
	registration := common.ClientRegistration{
		ClientID:     "new-client-1",
		Hostname:     "host-1",
		TotalCPU:     8,
		TotalMemory:  16.0,
		TotalStorage: 100.0,
		PublicIP:     "1.2.3.4",
		LocalIP:      "192.168.1.10",
		EndpointURL:  "http://192.168.1.10:11000",
		Latitude:     40.7128,
		Longitude:    -74.0060,
		City:         "New York",
		Country:      "US",
	}

	body, _ := json.Marshal(registration)
	req := httptest.NewRequest("POST", "/register", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	server.handleRegister(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected status 200, got %d: %s", rec.Code, rec.Body.String())
	}

	// Verify client was registered
	server.mu.RLock()
	_, exists := server.clientCache["new-client-1"]
	server.mu.RUnlock()

	if !exists {
		t.Error("Client should be in cache after registration")
	}
}

// TestHandleStats verifies stats reporting endpoint
func TestHandleStats(t *testing.T) {
	db, cleanup := CreateTestDB(t)
	defer cleanup()

	server := NewTestServer(t, db)

	// First register a client
	client := NewMockClient(MockClientOptions{
		ClientID: "stats-client",
		Hostname: "stats-host",
	})
	server.AddMockClient(client)

	// Send stats update
	stats := common.ResourceStats{
		ClientID:    "stats-client",
		CPUUsageAvg: []float64{10.0, 20.0, 30.0, 40.0},
		MemoryUsed:  8.0,
		MemoryTotal: 16.0,
		DiskUsed:    50.0,
		DiskTotal:   100.0,
		Timestamp:   time.Now(),
	}

	body, _ := json.Marshal(stats)
	req := httptest.NewRequest("POST", "/stats", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	server.handleStats(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected status 200, got %d: %s", rec.Code, rec.Body.String())
	}

	// Verify stats were updated
	server.mu.RLock()
	cachedClient, exists := server.clientCache["stats-client"]
	server.mu.RUnlock()

	if !exists {
		t.Fatal("Client should exist in cache")
	}

	if len(cachedClient.Stats.CPUUsageAvg) != 4 {
		t.Errorf("Expected 4 CPU cores, got %d", len(cachedClient.Stats.CPUUsageAvg))
	}
}

// TestHandleRoute verifies routing endpoint
func TestHandleRoute(t *testing.T) {
	db, cleanup := CreateTestDB(t)
	defer cleanup()

	server := NewTestServer(t, db)

	// Register a client with available resources
	client := NewMockClient(MockClientOptions{
		ClientID:    "route-client",
		Hostname:    "route-host",
		TotalCPU:    8,
		TotalMemory: 16.0,
		CPUUsageAvg: []float64{10, 10, 10, 10, 10, 10, 10, 10},
		MemoryUsed:  4.0,
		MemoryAvail: 12.0,
	})
	server.AddMockClient(client)

	// Create routing request
	routeReq := common.RoutingRequest{
		Tier:     "lite",
		ClientIP: "1.2.3.4",
	}

	body, _ := json.Marshal(routeReq)
	req := httptest.NewRequest("POST", "/route", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	server.handleRoute(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected status 200, got %d: %s", rec.Code, rec.Body.String())
	}

	// Verify response
	var response common.RoutingResponse
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if response.ClientID != "route-client" {
		t.Errorf("Expected route-client, got %s", response.ClientID)
	}

	if response.Endpoint == "" {
		t.Error("Expected non-empty endpoint")
	}
}

// TestHandleRoute_NoAvailableClients verifies error when no clients available
func TestHandleRoute_NoAvailableClients(t *testing.T) {
	db, cleanup := CreateTestDB(t)
	defer cleanup()

	server := NewTestServer(t, db)

	// Don't register any clients

	routeReq := common.RoutingRequest{
		Tier:     "lite",
		ClientIP: "1.2.3.4",
	}

	body, _ := json.Marshal(routeReq)
	req := httptest.NewRequest("POST", "/route", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	server.handleRoute(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("Expected status 503, got %d", rec.Code)
	}
}

// TestHandleRoute_WithStickySession verifies sticky session routing
func TestHandleRoute_WithStickySession(t *testing.T) {
	db, cleanup := CreateTestDB(t)
	defer cleanup()

	server := NewTestServer(t, db)
	server.config.StickyHeader = "X-Session-ID"

	// Register two clients
	client1 := NewMockClient(MockClientOptions{
		ClientID:    "sticky-1",
		TotalCPU:    8,
		CPUUsageAvg: []float64{10, 10, 10, 10, 10, 10, 10, 10},
	})
	client2 := NewMockClient(MockClientOptions{
		ClientID:    "sticky-2",
		TotalCPU:    8,
		CPUUsageAvg: []float64{10, 10, 10, 10, 10, 10, 10, 10},
	})

	server.AddMockClient(client1)
	server.AddMockClient(client2)

	// First request creates sticky assignment
	routeReq := common.RoutingRequest{
		Tier:     "lite",
		ClientIP: "1.2.3.4",
	}

	body, _ := json.Marshal(routeReq)
	req := httptest.NewRequest("POST", "/route", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Session-ID", "session-123")
	rec := httptest.NewRecorder()

	server.handleRoute(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("First request failed: %d", rec.Code)
	}

	var firstResponse common.RoutingResponse
	json.NewDecoder(rec.Body).Decode(&firstResponse)
	firstClient := firstResponse.ClientID

	// Second request with same sticky ID should get same client
	body2, _ := json.Marshal(routeReq)
	req2 := httptest.NewRequest("POST", "/route", bytes.NewReader(body2))
	req2.Header.Set("Content-Type", "application/json")
	req2.Header.Set("X-Session-ID", "session-123")
	rec2 := httptest.NewRecorder()

	server.handleRoute(rec2, req2)

	var secondResponse common.RoutingResponse
	json.NewDecoder(rec2.Body).Decode(&secondResponse)

	if secondResponse.ClientID != firstClient {
		t.Errorf("Expected sticky session to route to %s, got %s", firstClient, secondResponse.ClientID)
	}
}

// TestHandleRegister_InvalidJSON verifies error handling for bad JSON
func TestHandleRegister_InvalidJSON(t *testing.T) {
	db, cleanup := CreateTestDB(t)
	defer cleanup()

	server := NewTestServer(t, db)

	req := httptest.NewRequest("POST", "/register", bytes.NewReader([]byte("invalid json")))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	server.handleRegister(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Expected status 400, got %d", rec.Code)
	}
}

// TestHandleStats_InvalidJSON verifies error handling for bad JSON
func TestHandleStats_InvalidJSON(t *testing.T) {
	db, cleanup := CreateTestDB(t)
	defer cleanup()

	server := NewTestServer(t, db)

	req := httptest.NewRequest("POST", "/stats", bytes.NewReader([]byte("invalid json")))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	server.handleStats(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Expected status 400, got %d", rec.Code)
	}
}

// TestHandleRoute_InvalidTier verifies error for unknown tier
func TestHandleRoute_InvalidTier(t *testing.T) {
	db, cleanup := CreateTestDB(t)
	defer cleanup()

	server := NewTestServer(t, db)

	routeReq := common.RoutingRequest{
		Tier:     "non-existent-tier",
		ClientIP: "1.2.3.4",
	}

	body, _ := json.Marshal(routeReq)
	req := httptest.NewRequest("POST", "/route", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	server.handleRoute(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Expected status 400 for invalid tier, got %d", rec.Code)
	}
}

// TestHandleRegister_MethodNotAllowed verifies GET request is rejected
func TestHandleRegister_MethodNotAllowed(t *testing.T) {
	db, cleanup := CreateTestDB(t)
	defer cleanup()

	server := NewTestServer(t, db)

	req := httptest.NewRequest("GET", "/register", nil)
	rec := httptest.NewRecorder()

	server.handleRegister(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("Expected status 405, got %d", rec.Code)
	}
}

// TestHandleRegister_EndpointFallback verifies endpoint construction fallbacks
func TestHandleRegister_EndpointFallback(t *testing.T) {
	db, cleanup := CreateTestDB(t)
	defer cleanup()

	server := NewTestServer(t, db)

	tests := []struct {
		name             string
		endpointURL      string
		localIP          string
		publicIP         string
		expectedEndpoint string
	}{
		{
			name:             "ExplicitEndpoint",
			endpointURL:      "http://custom:9000",
			localIP:          "192.168.1.10",
			publicIP:         "1.2.3.4",
			expectedEndpoint: "http://custom:9000",
		},
		{
			name:             "LocalIPFallback",
			endpointURL:      "",
			localIP:          "192.168.1.10",
			publicIP:         "1.2.3.4",
			expectedEndpoint: "http://192.168.1.10:11000",
		},
		{
			name:             "PublicIPFallback",
			endpointURL:      "",
			localIP:          "",
			publicIP:         "1.2.3.4",
			expectedEndpoint: "http://1.2.3.4:11000",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			registration := common.ClientRegistration{
				ClientID:     "endpoint-test-" + tt.name,
				Hostname:     "host",
				TotalCPU:     8,
				TotalMemory:  16.0,
				TotalStorage: 100.0,
				PublicIP:     tt.publicIP,
				LocalIP:      tt.localIP,
				EndpointURL:  tt.endpointURL,
			}

			body, _ := json.Marshal(registration)
			req := httptest.NewRequest("POST", "/register", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()

			server.handleRegister(rec, req)

			if rec.Code != http.StatusOK {
				t.Fatalf("Expected status 200, got %d", rec.Code)
			}

			// Verify endpoint in cache
			server.mu.RLock()
			client, exists := server.clientCache[registration.ClientID]
			server.mu.RUnlock()

			if !exists {
				t.Fatal("Client should exist in cache")
			}

			if client.Endpoint != tt.expectedEndpoint {
				t.Errorf("Expected endpoint %s, got %s", tt.expectedEndpoint, client.Endpoint)
			}
		})
	}
}

// TestHandleRegister_DuplicateEndpoint verifies duplicate client removal
func TestHandleRegister_DuplicateEndpoint(t *testing.T) {
	db, cleanup := CreateTestDB(t)
	defer cleanup()

	server := NewTestServer(t, db)

	// Register first client
	client1 := common.ClientRegistration{
		ClientID:     "client-1",
		Hostname:     "host-1",
		TotalCPU:     8,
		TotalMemory:  16.0,
		TotalStorage: 100.0,
		LocalIP:      "192.168.1.10",
		EndpointURL:  "http://192.168.1.10:11000",
	}

	body1, _ := json.Marshal(client1)
	req1 := httptest.NewRequest("POST", "/register", bytes.NewReader(body1))
	req1.Header.Set("Content-Type", "application/json")
	rec1 := httptest.NewRecorder()
	server.handleRegister(rec1, req1)

	// Register second client with same endpoint
	client2 := common.ClientRegistration{
		ClientID:     "client-2",
		Hostname:     "host-2",
		TotalCPU:     8,
		TotalMemory:  16.0,
		TotalStorage: 100.0,
		LocalIP:      "192.168.1.10",
		EndpointURL:  "http://192.168.1.10:11000",
	}

	body2, _ := json.Marshal(client2)
	req2 := httptest.NewRequest("POST", "/register", bytes.NewReader(body2))
	req2.Header.Set("Content-Type", "application/json")
	rec2 := httptest.NewRecorder()
	server.handleRegister(rec2, req2)

	// Verify first client was removed, second client exists
	server.mu.RLock()
	_, exists1 := server.clientCache["client-1"]
	_, exists2 := server.clientCache["client-2"]
	server.mu.RUnlock()

	if exists1 {
		t.Error("Client-1 should have been removed as duplicate")
	}
	if !exists2 {
		t.Error("Client-2 should exist")
	}
}

// TestHandleStats_MethodNotAllowed verifies GET request is rejected
func TestHandleStats_MethodNotAllowed(t *testing.T) {
	db, cleanup := CreateTestDB(t)
	defer cleanup()

	server := NewTestServer(t, db)

	req := httptest.NewRequest("GET", "/stats", nil)
	rec := httptest.NewRecorder()

	server.handleStats(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("Expected status 405, got %d", rec.Code)
	}
}

// TestHandleStats_MissingClientID verifies validation of required fields
func TestHandleStats_MissingClientID(t *testing.T) {
	db, cleanup := CreateTestDB(t)
	defer cleanup()

	server := NewTestServer(t, db)

	stats := common.ResourceStats{
		ClientID:    "", // Missing
		CPUUsageAvg: []float64{10.0},
		MemoryUsed:  8.0,
		MemoryTotal: 16.0,
	}

	body, _ := json.Marshal(stats)
	req := httptest.NewRequest("POST", "/stats", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	server.handleStats(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Expected status 400 for missing client_id, got %d", rec.Code)
	}
}

// TestHandleRoute_MethodNotAllowed verifies GET request is rejected
func TestHandleRoute_MethodNotAllowed(t *testing.T) {
	db, cleanup := CreateTestDB(t)
	defer cleanup()

	server := NewTestServer(t, db)

	req := httptest.NewRequest("GET", "/route", nil)
	rec := httptest.NewRecorder()

	server.handleRoute(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("Expected status 405, got %d", rec.Code)
	}
}
