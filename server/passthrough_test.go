package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"cyqle.in/opsen/common"
)

// staleTimeout in the test config is StaleMinutes (5) * time.Minute.
func staleClient(id string) *ClientState {
	c := NewMockClient(MockClientOptions{ClientID: id})
	c.LastSeen = time.Now().Add(-10 * time.Minute)
	return c
}

func unhealthyClient(id string) *ClientState {
	c := NewMockClient(MockClientOptions{ClientID: id})
	c.HealthStatus = "unhealthy"
	return c
}

// Untagged (no-tier) traffic must route without reserving session capacity and
// must stick to one backend across requests. This is the availability fix for
// the opsen brownout, where untagged bot traffic forced to "lite" exhausted the
// backend's session slots and 503'd the whole API.
func TestPassthrough_RoutesStickyWithoutReserving(t *testing.T) {
	db, cleanup := CreateTestDB(t)
	defer cleanup()
	server := NewTestServer(t, db)
	for _, id := range []string{"b1", "b2"} {
		// MemoryAvail 0.5 (< lite's 1GB) so a tiered request can't fit.
		server.AddMockClient(NewMockClient(MockClientOptions{ClientID: id, MemoryAvail: 0.5}))
	}

	// A tiered request cannot fit and is refused, reserving nothing.
	lite := server.tierSpecs["lite"]
	AssertNoClient(t, server.selectClientWithStickiness("ip", "lite", lite, 0, 0, "r"))
	if n := server.CountPendingAllocations(); n != 0 {
		t.Fatalf("refused tiered request reserved %d allocations, want 0", n)
	}

	// Passthrough ignores capacity: routes, sticks, and reserves nothing.
	first := server.selectClientPassthrough("ip", 0, 0)
	if first == nil {
		t.Fatal("passthrough returned nil despite healthy backends")
	}
	for i := 0; i < 5; i++ {
		AssertClientSelected(t, server.selectClientPassthrough("ip", 0, 0), first.Registration.ClientID)
	}
	if n := server.CountPendingAllocations(); n != 0 {
		t.Fatalf("passthrough reserved %d pending allocations, want 0", n)
	}
}

// A passthrough request from a sticky_id that already has a real tiered session
// follows that session's backend (affinity) rather than picking freshly.
func TestPassthrough_FollowsExistingTieredSession(t *testing.T) {
	db, cleanup := CreateTestDB(t)
	defer cleanup()
	server := NewTestServer(t, db)
	server.AddMockClient(NewMockClient(MockClientOptions{ClientID: "b1"}))
	server.AddMockClient(NewMockClient(MockClientOptions{ClientID: "b2"}))

	// Establish a real tiered assignment for this sticky_id.
	lite := server.tierSpecs["lite"]
	sess := server.selectClientWithStickiness("ip", "lite", lite, 0, 0, "r")
	if sess == nil {
		t.Fatal("tiered allocation returned nil")
	}
	// Untagged traffic from the same sticky_id follows that backend.
	AssertClientSelected(t, server.selectClientPassthrough("ip", 0, 0), sess.Registration.ClientID)
}

// findAnyHealthyClient skips stale and unhealthy backends, and returns nil when
// none are usable — which is what makes handleProxy answer 503 for passthrough.
func TestFindAnyHealthyClient_SkipsStaleAndUnhealthy(t *testing.T) {
	db, cleanup := CreateTestDB(t)
	defer cleanup()
	server := NewTestServer(t, db)
	server.AddMockClient(staleClient("stale"))
	server.AddMockClient(unhealthyClient("unhealthy"))
	server.AddMockClient(NewMockClient(MockClientOptions{ClientID: "good"}))

	AssertClientSelected(t, server.findAnyHealthyClient(0, 0), "good")

	// Remove the only usable backend → stale + unhealthy remain → nil.
	server.mu.Lock()
	delete(server.clientCache, "good")
	server.mu.Unlock()
	AssertNoClient(t, server.findAnyHealthyClient(0, 0))
}

// findAnyHealthyClient prefers the geographically nearer healthy backend.
func TestFindAnyHealthyClient_PrefersNearer(t *testing.T) {
	db, cleanup := CreateTestDB(t)
	defer cleanup()
	server := NewTestServer(t, db)
	server.AddMockClient(NewMockClient(MockClientOptions{ClientID: "nyc", Latitude: 40.71, Longitude: -74.00}))
	server.AddMockClient(NewMockClient(MockClientOptions{ClientID: "la", Latitude: 34.05, Longitude: -118.24}))

	// Requester near NYC → NYC backend wins on distance.
	AssertClientSelected(t, server.findAnyHealthyClient(40.73, -73.99), "nyc")
}

// With no sticky_id, passthrough still routes but pins nothing.
func TestPassthrough_NoStickyID(t *testing.T) {
	db, cleanup := CreateTestDB(t)
	defer cleanup()
	server := NewTestServer(t, db)
	server.AddMockClient(NewMockClient(MockClientOptions{ClientID: "b1"}))

	AssertClientSelected(t, server.selectClientPassthrough("", 0, 0), "b1")

	server.mu.RLock()
	n := len(server.stickyAssignments)
	server.mu.RUnlock()
	if n != 0 {
		t.Fatalf("empty sticky_id should create no assignment, got %d", n)
	}
}

// A configured default_tier makes untagged requests allocate that tier (opt-in
// to the pre-fix behavior) and be capacity-gated, exercising the default_tier
// branch in handleProxy.
func TestDefaultTier_MakesUntaggedRequestsAllocate(t *testing.T) {
	db, cleanup := CreateTestDB(t)
	defer cleanup()
	server := NewTestServerWithConfig(t, db, func(c *common.ServerConfig) {
		c.DefaultTier = "lite"
	})
	// Dead endpoint: the proxy dial fails fast (502), but the allocation
	// decision — the thing under test — has already happened by then.
	server.AddMockClient(NewMockClient(MockClientOptions{ClientID: "b1", Endpoint: "http://127.0.0.1:1"}))

	req := httptest.NewRequest("GET", "/x", nil) // no tier signal
	req.Header.Set("X-Session-ID", "ip")
	server.handleProxy(httptest.NewRecorder(), req)

	if n := server.CountPendingAllocations(); n != 1 {
		t.Fatalf("default_tier=lite should reserve 1 allocation for an untagged request, got %d", n)
	}
}

// A tiered (X-Tier) request that cannot be allocated still returns 503 — the
// allocation path and its capacity gate are unchanged by the passthrough split.
func TestAllocation_503WhenNoCapacity(t *testing.T) {
	db, cleanup := CreateTestDB(t)
	defer cleanup()
	server := NewTestServer(t, db)
	// MemoryAvail 0.5 < lite's 1GB, so the tier cannot fit.
	server.AddMockClient(NewMockClient(MockClientOptions{ClientID: "b1", MemoryAvail: 0.5}))

	req := httptest.NewRequest("GET", "/x", nil)
	req.Header.Set("X-Session-ID", "ip")
	req.Header.Set("X-Tier", "lite")
	rec := httptest.NewRecorder()
	server.handleProxy(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 for an un-allocatable tiered request, got %d", rec.Code)
	}
}
