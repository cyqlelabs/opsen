package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"cyqle.in/opsen/common"
)

func TestHandleRoute_RejectsGET(t *testing.T) {
	db, cleanup := CreateTestDB(t)
	defer cleanup()

	server := NewTestServer(t, db)

	rec := httptest.NewRecorder()
	server.handleRoute(rec, httptest.NewRequest(http.MethodGet, "/route", nil))

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", rec.Code)
	}
}

func TestHandleRoute_MalformedBody(t *testing.T) {
	db, cleanup := CreateTestDB(t)
	defer cleanup()

	server := NewTestServer(t, db)

	req := httptest.NewRequest(http.MethodPost, "/route", bytes.NewBufferString("{not json"))
	rec := httptest.NewRecorder()
	server.handleRoute(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rec.Code)
	}
}

func TestHandleRoute_UnknownTier(t *testing.T) {
	db, cleanup := CreateTestDB(t)
	defer cleanup()

	server := NewTestServer(t, db)

	body, err := json.Marshal(common.RoutingRequest{Tier: "does-not-exist"})
	if err != nil {
		t.Fatalf("failed to marshal request: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/route", bytes.NewBuffer(body))
	rec := httptest.NewRecorder()
	server.handleRoute(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rec.Code)
	}
}

func TestHandleRoute_NoCapacityReturns503(t *testing.T) {
	db, cleanup := CreateTestDB(t)
	defer cleanup()

	server := NewTestServer(t, db)

	body, err := json.Marshal(common.RoutingRequest{Tier: "pro-max"})
	if err != nil {
		t.Fatalf("failed to marshal request: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/route", bytes.NewBuffer(body))
	rec := httptest.NewRecorder()
	server.handleRoute(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503 with no registered backends, got %d", rec.Code)
	}
}

func TestHandleRoute_StickyByIPAndGeoIPFallback(t *testing.T) {
	db, cleanup := CreateTestDB(t)
	defer cleanup()

	server := NewTestServerWithConfig(t, db, func(c *common.ServerConfig) {
		c.StickyHeader = ""
		c.StickyByIP = true
	})
	// No GeoIP database is configured, so the lookup returns 0,0.
	server.geoIPDBPath = ""

	server.AddMockClient(NewMockClient(MockClientOptions{
		ClientID:  "backend-1",
		Latitude:  40.7,
		Longitude: -74.0,
	}))

	body, err := json.Marshal(common.RoutingRequest{Tier: "free", ClientIP: "8.8.8.8"})
	if err != nil {
		t.Fatalf("failed to marshal request: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/route", bytes.NewBuffer(body))
	req.RemoteAddr = "203.0.113.5:4321"
	rec := httptest.NewRecorder()
	server.handleRoute(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}

	var resp common.RoutingResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp.ClientID != "backend-1" {
		t.Errorf("expected backend-1, got %s", resp.ClientID)
	}
	if resp.Distance != 0 {
		t.Errorf("expected no distance without client coordinates, got %f", resp.Distance)
	}

	// The client IP must have been used as the sticky key.
	server.mu.RLock()
	assigned := server.stickyAssignments["203.0.113.5"]["free"]
	server.mu.RUnlock()
	if assigned != "backend-1" {
		t.Errorf("expected a sticky assignment keyed by client IP, got %q", assigned)
	}
}

func TestHandleRegisterAndStats_SurviveDatabaseFailure(t *testing.T) {
	db, cleanup := CreateTestDB(t)
	defer cleanup()

	server := NewTestServer(t, db)

	// Registration and stats must still succeed for the caller even when the
	// database is unavailable; persistence errors are logged, not fatal.
	closeDB(t, db)

	registration := common.ClientRegistration{
		ClientID:     "db-down",
		Hostname:     "db-down-host",
		PublicIP:     "1.2.3.4",
		LocalIP:      "10.0.0.5",
		TotalCPU:     4,
		TotalMemory:  8,
		TotalStorage: 100,
		EndpointURL:  "http://10.0.0.5:3000",
	}
	body, err := json.Marshal(registration)
	if err != nil {
		t.Fatalf("failed to marshal registration: %v", err)
	}
	rec := httptest.NewRecorder()
	server.handleRegister(rec, httptest.NewRequest(http.MethodPost, "/register", bytes.NewBuffer(body)))
	if rec.Code != http.StatusOK {
		t.Errorf("expected registration to succeed, got %d", rec.Code)
	}

	stats := common.ResourceStats{
		ClientID:    "db-down",
		Hostname:    "db-down-host",
		Timestamp:   time.Now(),
		CPUCores:    4,
		CPUUsageAvg: []float64{10, 10, 10, 10},
		MemoryTotal: 8,
		MemoryUsed:  2,
		MemoryAvail: 6,
	}
	statsBody, err := json.Marshal(stats)
	if err != nil {
		t.Fatalf("failed to marshal stats: %v", err)
	}
	rec = httptest.NewRecorder()
	server.handleStats(rec, httptest.NewRequest(http.MethodPost, "/stats", bytes.NewBuffer(statsBody)))
	if rec.Code != http.StatusOK {
		t.Errorf("expected stats to be accepted, got %d", rec.Code)
	}
}

func TestHandlePurgeStaleClients_SurvivesDatabaseFailure(t *testing.T) {
	db, cleanup := CreateTestDB(t)
	defer cleanup()

	server := NewTestServer(t, db)
	server.AddMockClient(NewMockClient(MockClientOptions{
		ClientID: "stale",
		LastSeen: time.Now().Add(-time.Hour),
	}))

	closeDB(t, db)

	rec := httptest.NewRecorder()
	server.handlePurgeStaleClients(rec, httptest.NewRequest(http.MethodPost, "/clients/purge", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp["cache_purged"].(float64) != 1 {
		t.Errorf("expected the stale client to be purged from the cache, got %v", resp["cache_purged"])
	}
}

func TestLoadClients_QueryFailure(t *testing.T) {
	db, cleanup := CreateTestDB(t)
	defer cleanup()

	server := NewTestServer(t, db)
	closeDB(t, db)

	if err := server.loadClients(); err == nil {
		t.Fatal("expected loadClients to fail against a closed database")
	}
}

func TestLoadStickyAssignments_QueryFailure(t *testing.T) {
	db, cleanup := CreateTestDB(t)
	defer cleanup()

	server := NewTestServer(t, db)
	closeDB(t, db)

	if err := server.loadStickyAssignments(); err == nil {
		t.Fatal("expected loadStickyAssignments to fail against a closed database")
	}
}

func TestPurgeInvalidClients_DatabaseFailure(t *testing.T) {
	db, cleanup := CreateTestDB(t)
	defer cleanup()

	server := NewTestServer(t, db)
	closeDB(t, db)

	// Must log and return rather than panic.
	server.purgeInvalidClients()
}

func TestCleanupStaleClients_RemovesExpiredEntries(t *testing.T) {
	db, cleanup := CreateTestDB(t)
	defer cleanup()

	server := NewTestServerWithConfig(t, db, func(c *common.ServerConfig) {
		c.StaleMinutes = 1
		c.CleanupIntervalSecs = 1
	})
	server.staleTimeout = 10 * time.Millisecond
	server.cleanupInterval = 10 * time.Millisecond

	stale := NewMockClient(MockClientOptions{
		ClientID: "expired",
		LastSeen: time.Now().Add(-time.Hour),
	})
	RegisterMockClientInDB(t, db, stale)
	server.AddMockClient(stale)
	server.AddMockClient(NewMockClient(MockClientOptions{ClientID: "fresh"}))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan struct{})
	go func() {
		defer close(done)
		server.cleanupStaleClients(ctx)
	}()

	deadline := time.Now().Add(5 * time.Second)
	for {
		server.mu.RLock()
		_, stalePresent := server.clientCache["expired"]
		_, freshPresent := server.clientCache["fresh"]
		server.mu.RUnlock()

		if !stalePresent {
			if !freshPresent {
				t.Error("the fresh client should not have been purged")
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for the stale client to be purged")
		}
		time.Sleep(10 * time.Millisecond)
	}

	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("cleanupStaleClients did not stop after context cancellation")
	}
}

func TestCleanupStalePendingAllocations_KeepsFreshReservations(t *testing.T) {
	db, cleanup := CreateTestDB(t)
	defer cleanup()

	server := NewTestServerWithConfig(t, db, func(c *common.ServerConfig) {
		c.PendingAllocationTimeoutSecs = 60
	})

	tierSpec := server.tierSpecs["free"]
	server.mu.Lock()
	server.pendingAllocations["client-1"] = []PendingAllocation{
		{StickyID: "old", Tier: "free", TierSpec: tierSpec, Timestamp: time.Now().Add(-2 * time.Hour), RequestID: "r1"},
		{StickyID: "new", Tier: "free", TierSpec: tierSpec, Timestamp: time.Now(), RequestID: "r2"},
	}
	server.pendingAllocations["client-2"] = []PendingAllocation{
		{StickyID: "old-only", Tier: "free", TierSpec: tierSpec, Timestamp: time.Now().Add(-2 * time.Hour), RequestID: "r3"},
	}
	server.mu.Unlock()

	server.cleanupStalePendingAllocations()

	remaining := server.GetPendingAllocationsForClient("client-1")
	if len(remaining) != 1 || remaining[0].StickyID != "new" {
		t.Errorf("expected only the fresh allocation to remain, got %+v", remaining)
	}

	server.mu.RLock()
	_, stillTracked := server.pendingAllocations["client-2"]
	server.mu.RUnlock()
	if stillTracked {
		t.Error("expected the client with only stale allocations to be dropped entirely")
	}
}

func TestRemovePendingAllocationForStickyTier(t *testing.T) {
	db, cleanup := CreateTestDB(t)
	defer cleanup()

	server := NewTestServer(t, db)
	tierSpec := server.tierSpecs["free"]

	server.mu.Lock()
	server.pendingAllocations["client-1"] = []PendingAllocation{
		{StickyID: "session-a", Tier: "free", TierSpec: tierSpec, Timestamp: time.Now()},
		{StickyID: "session-b", Tier: "free", TierSpec: tierSpec, Timestamp: time.Now()},
	}
	server.removePendingAllocationForStickyTierLocked("client-1", "session-a", "free")
	server.mu.Unlock()

	remaining := server.GetPendingAllocationsForClient("client-1")
	if len(remaining) != 1 || remaining[0].StickyID != "session-b" {
		t.Fatalf("expected only session-b to remain, got %+v", remaining)
	}

	// Removing the last allocation drops the client key entirely.
	server.mu.Lock()
	server.removePendingAllocationForStickyTierLocked("client-1", "session-b", "free")
	server.mu.Unlock()

	server.mu.RLock()
	_, stillTracked := server.pendingAllocations["client-1"]
	server.mu.RUnlock()
	if stillTracked {
		t.Error("expected the client entry to be removed once empty")
	}

	// Unknown clients and non-matching keys are no-ops.
	server.mu.Lock()
	server.removePendingAllocationForStickyTierLocked("unknown-client", "session-a", "free")
	server.mu.Unlock()
}

func TestFindStickyAssignment_ReassignsUnhealthyBackend(t *testing.T) {
	db, cleanup := CreateTestDB(t)
	defer cleanup()

	server := NewTestServerWithConfig(t, db, func(c *common.ServerConfig) {
		c.HealthCheckEnabled = true
	})

	client := NewMockClient(MockClientOptions{ClientID: "sick"})
	client.HealthStatus = "unhealthy"
	server.AddMockClient(client)

	server.createStickyAssignment("session-1", "free", "sick")

	if got := server.findStickyAssignment("session-1", "free", server.tierSpecs["free"]); got != nil {
		t.Errorf("expected no assignment for an unhealthy backend, got %s", got.Registration.ClientID)
	}

	server.mu.RLock()
	_, stillAssigned := server.stickyAssignments["session-1"]["free"]
	server.mu.RUnlock()
	if stillAssigned {
		t.Error("expected the sticky assignment to be cleared")
	}
}

func TestFindStickyAssignment_ReassignsOverloadedBackend(t *testing.T) {
	db, cleanup := CreateTestDB(t)
	defer cleanup()

	server := NewTestServer(t, db)

	// A backend with no free memory cannot satisfy the tier.
	exhausted := NewMockClient(MockClientOptions{
		ClientID:    "full",
		TotalMemory: 8,
		MemoryUsed:  8,
		MemoryAvail: 0,
	})
	server.AddMockClient(exhausted)
	server.createStickyAssignment("session-2", "pro-max", "full")

	if got := server.findStickyAssignment("session-2", "pro-max", server.tierSpecs["pro-max"]); got != nil {
		t.Errorf("expected no assignment for an overloaded backend, got %s", got.Registration.ClientID)
	}

	server.mu.RLock()
	_, stillAssigned := server.stickyAssignments["session-2"]["pro-max"]
	server.mu.RUnlock()
	if stillAssigned {
		t.Error("expected the sticky assignment to be cleared")
	}
}

func TestFindStickyAssignment_UpdatesTimestampWithFailingDatabase(t *testing.T) {
	db, cleanup := CreateTestDB(t)
	defer cleanup()

	server := NewTestServer(t, db)
	server.AddMockClient(NewMockClient(MockClientOptions{ClientID: "healthy"}))
	server.createStickyAssignment("session-3", "free", "healthy")

	closeDB(t, db)

	// The timestamp update fails but the assignment is still usable.
	got := server.findStickyAssignment("session-3", "free", server.tierSpecs["free"])
	if got == nil || got.Registration.ClientID != "healthy" {
		t.Fatalf("expected the assignment to be honoured, got %v", got)
	}
}

func TestSelectClientWithStickiness_HonoursConcurrentAssignment(t *testing.T) {
	db, cleanup := CreateTestDB(t)
	defer cleanup()

	server := NewTestServer(t, db)

	// The winner of the race is already recorded for this sticky_id + tier.
	winner := NewMockClient(MockClientOptions{ClientID: "winner"})
	server.AddMockClient(winner)
	server.mu.Lock()
	server.stickyAssignments["session-race"] = map[string]string{"free": "winner"}
	server.mu.Unlock()

	selected := server.selectClientWithStickiness("session-race", "free",
		server.tierSpecs["free"], 0, 0, "req-1")

	if selected == nil || selected.Registration.ClientID != "winner" {
		t.Fatalf("expected the existing assignment to win, got %v", selected)
	}
}

func TestCreateStickyAssignment_DatabaseFailureStillCaches(t *testing.T) {
	db, cleanup := CreateTestDB(t)
	defer cleanup()

	server := NewTestServer(t, db)
	closeDB(t, db)

	assigned := server.createStickyAssignment("session-4", "free", "client-x")
	if assigned != "client-x" {
		t.Errorf("expected client-x, got %s", assigned)
	}

	server.mu.RLock()
	cached := server.stickyAssignments["session-4"]["free"]
	server.mu.RUnlock()
	if cached != "client-x" {
		t.Errorf("expected the in-memory assignment to be kept, got %q", cached)
	}
}

func TestRemoveStickyAssignment_DatabaseFailure(t *testing.T) {
	db, cleanup := CreateTestDB(t)
	defer cleanup()

	server := NewTestServer(t, db)
	server.createStickyAssignment("session-5", "free", "client-y")
	closeDB(t, db)

	server.removeStickyAssignment("session-5", "free")

	server.mu.RLock()
	_, present := server.stickyAssignments["session-5"]
	server.mu.RUnlock()
	if present {
		t.Error("expected the assignment to be removed from memory even when the delete fails")
	}
}

func TestHasResources_GPURequirements(t *testing.T) {
	db, cleanup := CreateTestDB(t)
	defer cleanup()

	server := NewTestServer(t, db)

	gpuTier := common.TierSpec{Name: "gpu", VCPU: 1, MemoryGB: 1, StorageGB: 1, GPU: 1, GPUMemoryGB: 8}

	withGPU := NewMockClient(MockClientOptions{
		ClientID:  "gpu-node",
		TotalGPUs: 1,
		GPUModels: []string{"Tesla-T4"},
		GPUs: []common.GPUStats{
			{DeviceID: 0, Name: "Tesla-T4", MemoryTotalGB: 16, MemoryUsedGB: 2},
		},
	})
	if !server.hasResources(withGPU, gpuTier) {
		t.Error("expected a GPU node with free VRAM to satisfy the tier")
	}

	noGPU := NewMockClient(MockClientOptions{ClientID: "cpu-node"})
	if server.hasResources(noGPU, gpuTier) {
		t.Error("expected a node without GPUs to be rejected")
	}

	lowVRAM := NewMockClient(MockClientOptions{
		ClientID:  "gpu-busy",
		TotalGPUs: 1,
		GPUModels: []string{"Tesla-T4"},
		GPUs: []common.GPUStats{
			{DeviceID: 0, Name: "Tesla-T4", MemoryTotalGB: 16, MemoryUsedGB: 15},
		},
	})
	if server.hasResources(lowVRAM, gpuTier) {
		t.Error("expected a node without enough free VRAM to be rejected")
	}

	tightDisk := NewMockClient(MockClientOptions{
		ClientID:     "small-disk",
		TotalStorage: 10,
		DiskUsed:     9,
		DiskAvail:    1,
	})
	if server.hasResources(tightDisk, server.tierSpecs["pro-max"]) {
		t.Error("expected a node without enough disk to be rejected")
	}
}
