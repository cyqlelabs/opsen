package main

import (
	"context"
	"strings"
	"testing"
	"time"
)

// TestCleanupStaleClients verifies stale client cleanup process
func TestCleanupStaleClients(t *testing.T) {
	db, cleanup := CreateTestDB(t)
	defer cleanup()

	server := NewTestServer(t, db)
	server.staleTimeout = 100 * time.Millisecond
	server.cleanupInterval = 50 * time.Millisecond

	// Add a fresh client
	freshClient := NewMockClient(MockClientOptions{
		ClientID:     "fresh-client",
		TotalCPU:     8,
		TotalMemory:  16.0,
		TotalStorage: 100.0,
	})
	server.mu.Lock()
	server.clientCache["fresh-client"] = freshClient
	server.mu.Unlock()

	// Add a stale client (last seen > 3x staleTimeout ago)
	staleClient := NewMockClient(MockClientOptions{
		ClientID:     "stale-client",
		TotalCPU:     8,
		TotalMemory:  16.0,
		TotalStorage: 100.0,
		LastSeen:     time.Now().Add(-400 * time.Millisecond), // > 3x staleTimeout
	})
	server.mu.Lock()
	server.clientCache["stale-client"] = staleClient
	server.mu.Unlock()

	// Insert into database as well
	_, err := db.Exec(`
		INSERT INTO clients (client_id, hostname, public_ip, total_cpu, total_memory, total_storage, endpoint, last_seen)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`, "stale-client", "stale-host", "1.2.3.4", 8, 16.0, 100.0, "http://stale:11000", time.Now().Add(-400*time.Millisecond).Format("2006-01-02 15:04:05"))
	if err != nil {
		t.Fatalf("Failed to insert stale client: %v", err)
	}

	// Start cleanup in background
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	go server.cleanupStaleClients(ctx)

	// Wait for cleanup to run
	time.Sleep(200 * time.Millisecond)

	// Verify stale client removed from cache
	server.mu.RLock()
	_, staleExists := server.clientCache["stale-client"]
	_, freshExists := server.clientCache["fresh-client"]
	server.mu.RUnlock()

	if staleExists {
		t.Error("Stale client should have been removed from cache")
	}
	if !freshExists {
		t.Error("Fresh client should still exist in cache")
	}

	// Verify stale client removed from database
	var count int
	err = db.QueryRow("SELECT COUNT(*) FROM clients WHERE client_id = ?", "stale-client").Scan(&count)
	if err != nil {
		t.Fatalf("Failed to query database: %v", err)
	}
	if count > 0 {
		t.Error("Stale client should have been removed from database")
	}

	// Wait for context to cancel
	<-ctx.Done()
}

// TestPurgeInvalidClients verifies cleanup of invalid client data
func TestPurgeInvalidClients(t *testing.T) {
	db, cleanup := CreateTestDB(t)
	defer cleanup()

	server := NewTestServer(t, db)

	// Insert client with NULL last_seen
	_, err := db.Exec(`
		INSERT INTO clients (client_id, hostname, public_ip, total_cpu, total_memory, total_storage, endpoint, last_seen)
		VALUES (?, ?, ?, ?, ?, ?, ?, NULL)
	`, "null-timestamp", "host1", "1.2.3.4", 8, 16.0, 100.0, "http://host1:11000")
	if err != nil {
		t.Fatalf("Failed to insert null timestamp client: %v", err)
	}

	// Insert client with very old timestamp (>30 days)
	oldTime := time.Now().Add(-35 * 24 * time.Hour).Format("2006-01-02 15:04:05")
	_, err = db.Exec(`
		INSERT INTO clients (client_id, hostname, public_ip, total_cpu, total_memory, total_storage, endpoint, last_seen)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`, "old-client", "host2", "1.2.3.5", 8, 16.0, 100.0, "http://host2:11000", oldTime)
	if err != nil {
		t.Fatalf("Failed to insert old client: %v", err)
	}

	// Insert valid recent client
	recentTime := time.Now().Add(-1 * time.Hour).Format("2006-01-02 15:04:05")
	_, err = db.Exec(`
		INSERT INTO clients (client_id, hostname, public_ip, total_cpu, total_memory, total_storage, endpoint, last_seen)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`, "recent-client", "host3", "1.2.3.6", 8, 16.0, 100.0, "http://host3:11000", recentTime)
	if err != nil {
		t.Fatalf("Failed to insert recent client: %v", err)
	}

	// Run purge
	server.purgeInvalidClients()

	// Verify invalid clients removed
	var count int
	err = db.QueryRow("SELECT COUNT(*) FROM clients WHERE client_id IN (?, ?)", "null-timestamp", "old-client").Scan(&count)
	if err != nil {
		t.Fatalf("Failed to query database: %v", err)
	}
	if count > 0 {
		t.Errorf("Expected invalid clients to be purged, but found %d", count)
	}

	// Verify recent client still exists
	err = db.QueryRow("SELECT COUNT(*) FROM clients WHERE client_id = ?", "recent-client").Scan(&count)
	if err != nil {
		t.Fatalf("Failed to query database: %v", err)
	}
	if count != 1 {
		t.Error("Recent client should still exist")
	}
}

// TestLoadClients verifies loading clients from database on startup
func TestLoadClients(t *testing.T) {
	db, cleanup := CreateTestDB(t)
	defer cleanup()

	server := NewTestServer(t, db)

	// Insert test clients into database
	clients := []struct {
		id        string
		hostname  string
		publicIP  string
		localIP   string
		cpu       int
		memory    float64
		storage   float64
		gpus      int
		gpuModels string
		endpoint  string
	}{
		{"client-1", "host1", "1.2.3.4", "192.168.1.10", 8, 16.0, 100.0, 0, "", "http://192.168.1.10:11000"},
		{"client-2", "host2", "1.2.3.5", "192.168.1.11", 16, 32.0, 200.0, 2, `["RTX 3090", "RTX 4090"]`, "http://192.168.1.11:11000"},
	}

	for _, c := range clients {
		_, err := db.Exec(`
			INSERT INTO clients (client_id, hostname, public_ip, local_ip, latitude, longitude, country, city,
			                     total_cpu, total_memory, total_storage, total_gpus, gpu_models, endpoint, last_seen)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`, c.id, c.hostname, c.publicIP, c.localIP, 40.7128, -74.0060, "US", "New York",
			c.cpu, c.memory, c.storage, c.gpus, c.gpuModels, c.endpoint, time.Now().Format("2006-01-02 15:04:05"))
		if err != nil {
			t.Fatalf("Failed to insert client: %v", err)
		}
	}

	// Clear cache and load from database
	server.mu.Lock()
	server.clientCache = make(map[string]*ClientState)
	server.mu.Unlock()

	err := server.loadClients()
	if err != nil {
		t.Fatalf("Failed to load clients: %v", err)
	}

	// Verify clients loaded into cache
	server.mu.RLock()
	defer server.mu.RUnlock()

	if len(server.clientCache) != 2 {
		t.Errorf("Expected 2 clients in cache, got %d", len(server.clientCache))
	}

	// Verify client-1 data
	client1 := server.clientCache["client-1"]
	if client1 == nil {
		t.Fatal("client-1 not found in cache")
	}
	if client1.Registration.TotalCPU != 8 {
		t.Errorf("client-1: expected TotalCPU=8, got %d", client1.Registration.TotalCPU)
	}
	if client1.Registration.TotalMemory != 16.0 {
		t.Errorf("client-1: expected TotalMemory=16.0, got %.1f", client1.Registration.TotalMemory)
	}

	// Verify client-2 GPU data
	client2 := server.clientCache["client-2"]
	if client2 == nil {
		t.Fatal("client-2 not found in cache")
	}
	if client2.Registration.TotalGPUs != 2 {
		t.Errorf("client-2: expected TotalGPUs=2, got %d", client2.Registration.TotalGPUs)
	}
	if len(client2.Registration.GPUModels) != 2 {
		t.Errorf("client-2: expected 2 GPU models, got %d", len(client2.Registration.GPUModels))
	}
}

// TestLoadStickyAssignments verifies loading sticky assignments from database
func TestLoadStickyAssignments(t *testing.T) {
	db, cleanup := CreateTestDB(t)
	defer cleanup()

	server := NewTestServer(t, db)
	server.stickyHeader = "X-Session-ID"

	// Insert sticky assignments into database
	assignments := []struct {
		stickyID string
		tier     string
		clientID string
	}{
		{"session-1", "small", "client-1"},
		{"session-1", "medium", "client-2"},
		{"session-2", "small", "client-3"},
	}

	for _, a := range assignments {
		_, err := db.Exec(`
			INSERT INTO sticky_assignments (sticky_id, tier, client_id, created_at)
			VALUES (?, ?, ?, ?)
		`, a.stickyID, a.tier, a.clientID, time.Now().Format("2006-01-02 15:04:05"))
		if err != nil {
			t.Fatalf("Failed to insert sticky assignment: %v", err)
		}
	}

	// Clear in-memory assignments and load from database
	server.mu.Lock()
	server.stickyAssignments = make(map[string]map[string]string)
	server.mu.Unlock()

	err := server.loadStickyAssignments()
	if err != nil {
		t.Fatalf("Failed to load sticky assignments: %v", err)
	}

	// Verify assignments loaded correctly
	server.mu.RLock()
	defer server.mu.RUnlock()

	if len(server.stickyAssignments) != 2 {
		t.Errorf("Expected 2 sticky IDs, got %d", len(server.stickyAssignments))
	}

	// Check session-1 assignments
	session1 := server.stickyAssignments["session-1"]
	if session1 == nil {
		t.Fatal("session-1 not found in sticky assignments")
	}
	if session1["small"] != "client-1" {
		t.Errorf("session-1/small: expected client-1, got %s", session1["small"])
	}
	if session1["medium"] != "client-2" {
		t.Errorf("session-1/medium: expected client-2, got %s", session1["medium"])
	}

	// Check session-2 assignments
	session2 := server.stickyAssignments["session-2"]
	if session2 == nil {
		t.Fatal("session-2 not found in sticky assignments")
	}
	if session2["small"] != "client-3" {
		t.Errorf("session-2/small: expected client-3, got %s", session2["small"])
	}
}

// TestHandleProxyOrNotFound_PathMatching verifies proxy path matching logic
func TestHandleProxyOrNotFound_PathMatching(t *testing.T) {
	tests := []struct {
		name           string
		proxyEndpoints []string
		path           string
		expectMatch    bool
	}{
		{"matching /api prefix", []string{"/api", "/browse"}, "/api/endpoint", true},
		{"matching /browse prefix", []string{"/api", "/browse"}, "/browse/sessions", true},
		{"non-matching path", []string{"/api", "/browse"}, "/other", false},
		{"wildcard match", []string{"/"}, "/anything", true},
		{"wildcard star", []string{"*"}, "/anything", true},
		{"exact path match", []string{"/api"}, "/api", true},
		{"subpath match", []string{"/api"}, "/api/v1/resource", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			matched := false
			for _, prefix := range tt.proxyEndpoints {
				if prefix == "/" || prefix == "*" {
					matched = true
					break
				}
				if strings.HasPrefix(tt.path, prefix) {
					matched = true
					break
				}
			}

			if matched != tt.expectMatch {
				t.Errorf("Path %s with endpoints %v: expected match=%v, got %v",
					tt.path, tt.proxyEndpoints, tt.expectMatch, matched)
			}
		})
	}
}

// TestLoadClients_EmptyDatabase verifies graceful handling of empty database
func TestLoadClients_EmptyDatabase(t *testing.T) {
	db, cleanup := CreateTestDB(t)
	defer cleanup()

	server := NewTestServer(t, db)

	// Clear cache
	server.mu.Lock()
	server.clientCache = make(map[string]*ClientState)
	server.mu.Unlock()

	err := server.loadClients()
	if err != nil {
		t.Fatalf("loadClients should not error on empty database: %v", err)
	}

	server.mu.RLock()
	defer server.mu.RUnlock()

	if len(server.clientCache) != 0 {
		t.Errorf("Expected empty cache, got %d clients", len(server.clientCache))
	}
}

// TestLoadStickyAssignments_EmptyDatabase verifies graceful handling of empty database
func TestLoadStickyAssignments_EmptyDatabase(t *testing.T) {
	db, cleanup := CreateTestDB(t)
	defer cleanup()

	server := NewTestServer(t, db)
	server.stickyHeader = "X-Session-ID"

	// Clear assignments
	server.mu.Lock()
	server.stickyAssignments = make(map[string]map[string]string)
	server.mu.Unlock()

	err := server.loadStickyAssignments()
	if err != nil {
		t.Fatalf("loadStickyAssignments should not error on empty database: %v", err)
	}

	server.mu.RLock()
	defer server.mu.RUnlock()

	if len(server.stickyAssignments) != 0 {
		t.Errorf("Expected empty assignments, got %d", len(server.stickyAssignments))
	}
}
