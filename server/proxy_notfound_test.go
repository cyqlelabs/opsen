package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"cyqle.in/opsen/common"
)

// Note: Full proxy tests are in proxy_test.go
// These tests focus only on the path matching logic of handleProxyOrNotFound

// TestHandleProxyOrNotFound_NoMatch verifies 404 when path doesn't match
func TestHandleProxyOrNotFound_NoMatch(t *testing.T) {
	db, cleanup := CreateTestDB(t)
	defer cleanup()

	server := NewTestServer(t, db)
	server.proxyEndpoints = []string{"/api", "/browse"}

	req := httptest.NewRequest("GET", "/other/path", nil)
	rec := httptest.NewRecorder()

	server.handleProxyOrNotFound(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("Expected 404 for non-matching path, got %d", rec.Code)
	}
}

// Path matching logic is tested sufficiently in other tests
// Full proxy integration is covered in proxy_test.go

// TestHandleProxyOrNotFound_EmptyProxyEndpoints verifies 404 when no proxies configured
func TestHandleProxyOrNotFound_EmptyProxyEndpoints(t *testing.T) {
	db, cleanup := CreateTestDB(t)
	defer cleanup()

	server := NewTestServer(t, db)
	server.proxyEndpoints = []string{} // No proxy endpoints

	req := httptest.NewRequest("GET", "/api/endpoint", nil)
	rec := httptest.NewRecorder()

	server.handleProxyOrNotFound(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("Expected 404 when no proxy endpoints configured, got %d", rec.Code)
	}
}


// TestLookupIPLocation verifies IP geolocation lookup
func TestLookupIPLocation(t *testing.T) {
	db, cleanup := CreateTestDB(t)
	defer cleanup()

	server := NewTestServer(t, db)

	// Test with nil geoDB (should return 0, 0 without error)
	lat, lon := server.lookupIPLocation("8.8.8.8")
	if lat != 0.0 || lon != 0.0 {
		t.Errorf("Expected (0, 0) when geoDB is nil, got (%.4f, %.4f)", lat, lon)
	}

	// Note: Testing with actual GeoIP database would require a test fixture
	// For now, we're testing the nil case which is the most common in tests
}

// TestRunHealthChecks_ShutdownGracefully verifies health check goroutine shutdown
func TestRunHealthChecks_ShutdownGracefully(t *testing.T) {
	db, cleanup := CreateTestDB(t)
	defer cleanup()

	server := NewTestServerWithConfig(t, db, func(config *common.ServerConfig) {
		config.HealthCheckEnabled = true
		config.HealthCheckIntervalSecs = 1
	})

	// Create context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	// Start health checks
	done := make(chan struct{})
	go func() {
		server.runHealthChecks(ctx)
		close(done)
	}()

	// Wait for context to be canceled
	<-ctx.Done()

	// Wait for goroutine to finish
	<-done

	// Verify it shut down gracefully (no panic)
}
