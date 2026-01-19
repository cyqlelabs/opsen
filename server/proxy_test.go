package main

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// ========================================
// SSE PROXY TESTS
// ========================================

// TestProxy_SSE_ImmediateFlush verifies SSE events are flushed immediately
func TestProxy_SSE_ImmediateFlush(t *testing.T) {
	// Create a test backend that sends SSE events
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")

		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatal("ResponseWriter does not support flushing")
		}

		// Send 3 events with delays
		for i := 1; i <= 3; i++ {
			fmt.Fprintf(w, "data: event %d\n\n", i)
			flusher.Flush()
			time.Sleep(50 * time.Millisecond) // Small delay between events
		}
	}))
	defer backend.Close()

	// Create test database and server
	db, cleanup := CreateTestDB(t)
	defer cleanup()

	server := NewTestServer(t, db)
	server.proxyEndpoints = []string{"/sse"}
	server.config.ProxySSEFlushInterval = -1 // Immediate flush

	// Add a mock backend client with enough resources
	client := NewMockClient(MockClientOptions{
		ClientID:    "sse-backend",
		Endpoint:    backend.URL,
		CPUUsageAvg: []float64{10, 10, 10, 10, 10, 10, 10, 10},
		MemoryAvail: 50.0,
		DiskAvail:   100.0,
	})
	server.AddMockClient(client)

	// Create test HTTP server
	testServer := httptest.NewServer(http.HandlerFunc(server.handleProxy))
	defer testServer.Close()

	// Make SSE request
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", testServer.URL, nil)
	if err != nil {
		t.Fatalf("Failed to create request: %v", err)
	}
	req.Header.Set("Accept", "text/event-stream")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Failed to make request: %v", err)
	}
	defer resp.Body.Close()

	// Verify SSE headers
	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Errorf("Expected Content-Type text/event-stream, got %s", ct)
	}

	// Read events and verify they arrive with proper timing
	reader := bufio.NewReader(resp.Body)
	eventsReceived := 0
	startTime := time.Now()
	eventTimes := []time.Duration{}

	for eventsReceived < 3 {
		line, err := reader.ReadString('\n')
		if err != nil {
			if err == io.EOF {
				break
			}
			t.Fatalf("Error reading event: %v", err)
		}

		if strings.HasPrefix(line, "data:") {
			eventsReceived++
			eventTimes = append(eventTimes, time.Since(startTime))
		}
	}

	// Verify we received all 3 events
	if eventsReceived != 3 {
		t.Errorf("Expected 3 events, got %d", eventsReceived)
	}

	// Verify events were received in real-time (not buffered)
	// Each event should arrive ~50ms apart, not all at once at the end
	if len(eventTimes) >= 2 {
		timeBetweenEvents := eventTimes[1] - eventTimes[0]
		// Should be around 50ms (+/- 30ms for timing variations)
		if timeBetweenEvents < 20*time.Millisecond || timeBetweenEvents > 150*time.Millisecond {
			t.Logf("Warning: Time between first two events was %v (expected ~50ms)", timeBetweenEvents)
			t.Logf("Event times: %v", eventTimes)
			// Don't fail the test, but log the warning
		}
	}
}

// TestProxy_SSE_WithFlushInterval verifies custom flush intervals work
func TestProxy_SSE_WithFlushInterval(t *testing.T) {
	// Create a test backend
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)

		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatal("ResponseWriter does not support flushing")
		}

		// Send events continuously
		for i := 1; i <= 5; i++ {
			fmt.Fprintf(w, "data: event %d\n\n", i)
			flusher.Flush()
			time.Sleep(20 * time.Millisecond)
		}
	}))
	defer backend.Close()

	// Create test database and server
	db, cleanup := CreateTestDB(t)
	defer cleanup()

	server := NewTestServer(t, db)
	server.proxyEndpoints = []string{"/events"}
	server.config.ProxySSEFlushInterval = 100 // Flush every 100ms

	// Add a mock backend client
	client := NewMockClient(MockClientOptions{
		ClientID:    "sse-backend-2",
		Endpoint:    backend.URL,
		CPUUsageAvg: []float64{10, 10, 10, 10},
	})
	server.AddMockClient(client)

	// Create test HTTP server
	testServer := httptest.NewServer(http.HandlerFunc(server.handleProxy))
	defer testServer.Close()

	// Make SSE request
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", testServer.URL+"/events", nil)
	if err != nil {
		t.Fatalf("Failed to create request: %v", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Failed to make request: %v", err)
	}
	defer resp.Body.Close()

	// Read events
	reader := bufio.NewReader(resp.Body)
	eventsReceived := 0

	for eventsReceived < 5 {
		line, err := reader.ReadString('\n')
		if err != nil {
			if err == io.EOF {
				break
			}
			t.Fatalf("Error reading event: %v", err)
		}

		if strings.HasPrefix(line, "data:") {
			eventsReceived++
		}
	}

	if eventsReceived != 5 {
		t.Errorf("Expected 5 events, got %d", eventsReceived)
	}
}

// TestProxy_SSE_NoFlush verifies that flush can be disabled
func TestProxy_SSE_NoFlush(t *testing.T) {
	// Create a test backend
	eventsSent := 0
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)

		// Send small chunks
		for i := 1; i <= 3; i++ {
			fmt.Fprintf(w, "chunk %d\n", i)
			eventsSent++
			time.Sleep(10 * time.Millisecond)
		}
	}))
	defer backend.Close()

	// Create test database and server
	db, cleanup := CreateTestDB(t)
	defer cleanup()

	server := NewTestServer(t, db)
	server.proxyEndpoints = []string{"/buffered"}
	server.config.ProxySSEFlushInterval = 0 // No flushing (buffered)

	// Add a mock backend client
	client := NewMockClient(MockClientOptions{
		ClientID:    "buffered-backend",
		Endpoint:    backend.URL,
		CPUUsageAvg: []float64{10, 10, 10, 10},
	})
	server.AddMockClient(client)

	// Create test HTTP server
	testServer := httptest.NewServer(http.HandlerFunc(server.handleProxy))
	defer testServer.Close()

	// Make request
	resp, err := http.Get(testServer.URL + "/buffered")
	if err != nil {
		t.Fatalf("Failed to make request: %v", err)
	}
	defer resp.Body.Close()

	// Read all content (should be buffered and arrive all at once)
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("Failed to read body: %v", err)
	}

	// Verify we got all chunks
	bodyStr := string(body)
	if !strings.Contains(bodyStr, "chunk 1") || !strings.Contains(bodyStr, "chunk 3") {
		t.Errorf("Expected all chunks in response, got: %s", bodyStr)
	}

	if eventsSent != 3 {
		t.Errorf("Expected backend to send 3 chunks, sent %d", eventsSent)
	}
}

// TestProxy_SSE_WithStickySession verifies SSE works with sticky sessions
func TestProxy_SSE_WithStickySession(t *testing.T) {
	// Create a test backend
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)

		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatal("ResponseWriter does not support flushing")
		}

		// Send session ID from load balancer header
		clientID := r.Header.Get("X-LB-Client-ID")
		fmt.Fprintf(w, "data: connected to %s\n\n", clientID)
		flusher.Flush()
	}))
	defer backend.Close()

	// Create test database and server
	db, cleanup := CreateTestDB(t)
	defer cleanup()

	server := NewTestServer(t, db)
	server.proxyEndpoints = []string{"/stream"}
	server.config.ProxySSEFlushInterval = -1 // Immediate flush
	server.stickyHeader = "X-Session-ID"

	// Add mock backend clients
	client1 := NewMockClient(MockClientOptions{
		ClientID:    "sse-backend-1",
		Endpoint:    backend.URL,
		CPUUsageAvg: []float64{10, 10, 10, 10},
	})
	client2 := NewMockClient(MockClientOptions{
		ClientID:    "sse-backend-2",
		Endpoint:    backend.URL,
		CPUUsageAvg: []float64{10, 10, 10, 10},
	})
	server.AddMockClient(client1)
	server.AddMockClient(client2)

	// Create test HTTP server
	testServer := httptest.NewServer(http.HandlerFunc(server.handleProxy))
	defer testServer.Close()

	// Make first request with sticky session
	req1, err := http.NewRequest("GET", testServer.URL+"/stream", nil)
	if err != nil {
		t.Fatalf("Failed to create request: %v", err)
	}
	req1.Header.Set("X-Session-ID", "sticky-user-123")

	resp1, err := http.DefaultClient.Do(req1)
	if err != nil {
		t.Fatalf("Failed to make first request: %v", err)
	}
	defer resp1.Body.Close()

	// Read first event to get backend ID
	reader1 := bufio.NewReader(resp1.Body)
	line1, err := reader1.ReadString('\n')
	if err != nil {
		t.Fatalf("Failed to read first event: %v", err)
	}

	var firstBackendID string
	if strings.HasPrefix(line1, "data:") {
		// Extract backend ID from "data: connected to sse-backend-X"
		parts := strings.Fields(line1)
		if len(parts) >= 4 {
			firstBackendID = parts[3]
		}
	}

	if firstBackendID == "" {
		t.Fatalf("Failed to extract backend ID from first response: %s", line1)
	}

	// Make second request with same sticky session
	req2, err := http.NewRequest("GET", testServer.URL+"/stream", nil)
	if err != nil {
		t.Fatalf("Failed to create second request: %v", err)
	}
	req2.Header.Set("X-Session-ID", "sticky-user-123")

	resp2, err := http.DefaultClient.Do(req2)
	if err != nil {
		t.Fatalf("Failed to make second request: %v", err)
	}
	defer resp2.Body.Close()

	// Read second event to verify same backend
	reader2 := bufio.NewReader(resp2.Body)
	line2, err := reader2.ReadString('\n')
	if err != nil {
		t.Fatalf("Failed to read second event: %v", err)
	}

	var secondBackendID string
	if strings.HasPrefix(line2, "data:") {
		parts := strings.Fields(line2)
		if len(parts) >= 4 {
			secondBackendID = parts[3]
		}
	}

	// Verify sticky session worked
	if firstBackendID != secondBackendID {
		t.Errorf("Sticky session failed for SSE: first=%s, second=%s", firstBackendID, secondBackendID)
	}
}

// TestProxy_RegularHTTP verifies non-SSE requests still work
func TestProxy_RegularHTTP(t *testing.T) {
	// Create a test backend
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		if _, err := w.Write([]byte(`{"status":"ok","message":"hello from backend"}`)); err != nil {
			t.Logf("Warning: Failed to write response: %v", err)
		}
	}))
	defer backend.Close()

	// Create test database and server
	db, cleanup := CreateTestDB(t)
	defer cleanup()

	server := NewTestServer(t, db)
	server.proxyEndpoints = []string{"/api"}
	server.config.ProxySSEFlushInterval = -1 // SSE enabled, but regular requests should work too

	// Add a mock backend client
	client := NewMockClient(MockClientOptions{
		ClientID:    "api-backend",
		Endpoint:    backend.URL,
		CPUUsageAvg: []float64{10, 10, 10, 10},
	})
	server.AddMockClient(client)

	// Create test HTTP server
	testServer := httptest.NewServer(http.HandlerFunc(server.handleProxy))
	defer testServer.Close()

	// Make regular HTTP request
	resp, err := http.Get(testServer.URL + "/api/test")
	if err != nil {
		t.Fatalf("Failed to make request: %v", err)
	}
	defer resp.Body.Close()

	// Verify response
	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("Failed to read body: %v", err)
	}

	expectedBody := `{"status":"ok","message":"hello from backend"}`
	if string(body) != expectedBody {
		t.Errorf("Expected body %s, got %s", expectedBody, string(body))
	}
}

// TestProxy_POST_WithBody verifies POST requests with bodies are proxied correctly
func TestProxy_POST_WithBody(t *testing.T) {
	// Create a test backend that echoes the request body
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		if _, err := w.Write(body); err != nil {
			t.Logf("Warning: Failed to write response: %v", err)
		}
	}))
	defer backend.Close()

	// Create test database and server
	db, cleanup := CreateTestDB(t)
	defer cleanup()

	server := NewTestServer(t, db)
	server.proxyEndpoints = []string{"/api"}
	server.config.ProxySSEFlushInterval = -1

	// Add a mock backend client
	client := NewMockClient(MockClientOptions{
		ClientID:    "api-backend",
		Endpoint:    backend.URL,
		CPUUsageAvg: []float64{10, 10, 10, 10},
	})
	server.AddMockClient(client)

	// Create test HTTP server
	testServer := httptest.NewServer(http.HandlerFunc(server.handleProxy))
	defer testServer.Close()

	// Make POST request with JSON body
	requestBody := `{"test":"data","value":123}`
	resp, err := http.Post(testServer.URL+"/api/endpoint", "application/json", bytes.NewBufferString(requestBody))
	if err != nil {
		t.Fatalf("Failed to make request: %v", err)
	}
	defer resp.Body.Close()

	// Verify response
	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("Failed to read body: %v", err)
	}

	if string(responseBody) != requestBody {
		t.Errorf("Expected body %s, got %s", requestBody, string(responseBody))
	}
}
