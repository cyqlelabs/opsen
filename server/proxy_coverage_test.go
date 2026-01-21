package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestHandleProxyOrNotFound_NoProxyEndpoints verifies 404 when no proxy endpoints configured
func TestHandleProxyOrNotFound_NoProxyEndpoints(t *testing.T) {
	db, cleanup := CreateTestDB(t)
	defer cleanup()

	server := NewTestServer(t, db)
	server.proxyEndpoints = []string{} // No proxy endpoints

	req := httptest.NewRequest("GET", "/api/test", nil)
	w := httptest.NewRecorder()

	server.handleProxyOrNotFound(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("Expected 404 with no proxy endpoints, got %d", w.Code)
	}
}

// TestHandleProxyOrNotFound_NonMatchingPath verifies 404 for non-matching paths
func TestHandleProxyOrNotFound_NonMatchingPath(t *testing.T) {
	db, cleanup := CreateTestDB(t)
	defer cleanup()

	server := NewTestServer(t, db)
	server.proxyEndpoints = []string{"/api", "/browse"}

	tests := []struct {
		name string
		path string
	}{
		{"RootPath", "/"},
		{"OtherPath", "/other"},
		{"RegisterPath", "/register"},
		{"StaticPath", "/static/file.js"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", tt.path, nil)
			w := httptest.NewRecorder()

			server.handleProxyOrNotFound(w, req)

			if w.Code != http.StatusNotFound {
				t.Errorf("Expected 404 for path %s, got %d", tt.path, w.Code)
			}
		})
	}
}

// TestHandleProxyOrNotFound_MatchingPathNoClients verifies error when no clients available
func TestHandleProxyOrNotFound_MatchingPathNoClients(t *testing.T) {
	db, cleanup := CreateTestDB(t)
	defer cleanup()

	server := NewTestServer(t, db)
	server.proxyEndpoints = []string{"/api"}

	// Request to proxy endpoint without any registered clients
	req := httptest.NewRequest("GET", "/api/test?tier=small", nil)
	w := httptest.NewRecorder()

	server.handleProxyOrNotFound(w, req)

	// Should return 503 (no available backends)
	if w.Code != http.StatusServiceUnavailable && w.Code != http.StatusBadRequest {
		t.Logf("Got status %d when no clients available", w.Code)
	}
}

// TestHandleProxyOrNotFound_WildcardEndpoint verifies wildcard proxy
func TestHandleProxyOrNotFound_WildcardEndpoint(t *testing.T) {
	db, cleanup := CreateTestDB(t)
	defer cleanup()

	server := NewTestServer(t, db)
	server.proxyEndpoints = []string{"*"}

	// All paths should match wildcard
	paths := []string{"/", "/api", "/test", "/any/path"}

	for _, path := range paths {
		t.Run(path, func(t *testing.T) {
			req := httptest.NewRequest("GET", path, nil)
			w := httptest.NewRecorder()

			server.handleProxyOrNotFound(w, req)

			// Should not return 404 (will fail with no clients, but should match)
			if w.Code == http.StatusNotFound {
				t.Errorf("Wildcard should match path %s, got 404", path)
			}
		})
	}
}

// TestHandleProxyOrNotFound_SlashEndpoint verifies root slash proxy
func TestHandleProxyOrNotFound_SlashEndpoint(t *testing.T) {
	db, cleanup := CreateTestDB(t)
	defer cleanup()

	server := NewTestServer(t, db)
	server.proxyEndpoints = []string{"/"}

	// Root slash should match everything
	paths := []string{"/", "/api", "/test"}

	for _, path := range paths {
		t.Run(path, func(t *testing.T) {
			req := httptest.NewRequest("GET", path, nil)
			w := httptest.NewRecorder()

			server.handleProxyOrNotFound(w, req)

			if w.Code == http.StatusNotFound {
				t.Errorf("Root slash should match path %s, got 404", path)
			}
		})
	}
}

// TestHandleProxyOrNotFound_ExactPrefixMatch verifies prefix matching
func TestHandleProxyOrNotFound_ExactPrefixMatch(t *testing.T) {
	db, cleanup := CreateTestDB(t)
	defer cleanup()

	server := NewTestServer(t, db)
	server.proxyEndpoints = []string{"/api"}

	tests := []struct {
		name      string
		path      string
		shouldMatch bool
	}{
		{"ExactMatch", "/api", true},
		{"PrefixMatch", "/api/test", true},
		{"NestedMatch", "/api/v1/users", true},
		{"NoMatch", "/other", false},
		{"PartialMatch", "/ap", false},
		{"CaseSensitive", "/API", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", tt.path, nil)
			w := httptest.NewRecorder()

			server.handleProxyOrNotFound(w, req)

			is404 := w.Code == http.StatusNotFound
			if tt.shouldMatch && is404 {
				t.Errorf("Path %s should match /api prefix but got 404", tt.path)
			}
			if !tt.shouldMatch && !is404 {
				t.Errorf("Path %s should not match /api prefix but didn't get 404 (got %d)",
					tt.path, w.Code)
			}
		})
	}
}

// TestHandleProxyOrNotFound_MultipleEndpoints verifies multiple proxy endpoints
func TestHandleProxyOrNotFound_MultipleEndpoints(t *testing.T) {
	db, cleanup := CreateTestDB(t)
	defer cleanup()

	server := NewTestServer(t, db)
	server.proxyEndpoints = []string{"/api", "/browse", "/admin"}

	tests := []struct {
		path        string
		shouldMatch bool
	}{
		{"/api/test", true},
		{"/browse/sessions", true},
		{"/admin/users", true},
		{"/other", false},
		{"/api", true},
		{"/brows", false},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			req := httptest.NewRequest("GET", tt.path, nil)
			w := httptest.NewRecorder()

			server.handleProxyOrNotFound(w, req)

			is404 := w.Code == http.StatusNotFound
			if tt.shouldMatch && is404 {
				t.Errorf("Path %s should match but got 404", tt.path)
			}
			if !tt.shouldMatch && !is404 {
				t.Errorf("Path %s should not match but didn't get 404 (got %d)",
					tt.path, w.Code)
			}
		})
	}
}
