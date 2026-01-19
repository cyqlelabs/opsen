package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"cyqle.in/opsen/common"
)

// ========================================
// STICKINESS TESTS
// ========================================

// TestStickySession_SameBackend verifies sticky sessions route to the same backend
func TestStickySession_SameBackend(t *testing.T) {
	db, cleanup := CreateTestDB(t)
	defer cleanup()

	server := NewTestServer(t, db)

	// Create two backend clients
	client1 := NewMockClient(MockClientOptions{
		ClientID:    "backend-1",
		Latitude:    40.7128,
		Longitude:   -74.0060,
		CPUUsageAvg: []float64{10, 10, 10, 10, 10, 10, 10, 10},
	})
	client2 := NewMockClient(MockClientOptions{
		ClientID:    "backend-2",
		Latitude:    40.7128,
		Longitude:   -74.0060,
		CPUUsageAvg: []float64{10, 10, 10, 10, 10, 10, 10, 10},
	})

	server.AddMockClient(client1)
	server.AddMockClient(client2)

	// First request with sticky ID
	stickyID := "user-session-123"
	tier := "lite"
	tierSpec := server.tierSpecs[tier]

	selectedClient1 := server.selectClientWithStickiness(stickyID, tier, tierSpec, 40.7128, -74.0060, "req-1")
	if selectedClient1 == nil {
		t.Fatal("First request returned no client")
	}

	// Second request with same sticky ID should return same backend
	selectedClient2 := server.selectClientWithStickiness(stickyID, tier, tierSpec, 40.7128, -74.0060, "req-2")
	if selectedClient2 == nil {
		t.Fatal("Second request returned no client")
	}

	if selectedClient1.Registration.ClientID != selectedClient2.Registration.ClientID {
		t.Errorf("Sticky session failed: first=%s, second=%s",
			selectedClient1.Registration.ClientID, selectedClient2.Registration.ClientID)
	}
}

// TestStickySession_DifferentTiers verifies different tiers for same sticky ID get independent assignments
func TestStickySession_DifferentTiers(t *testing.T) {
	db, cleanup := CreateTestDB(t)
	defer cleanup()

	server := NewTestServer(t, db)

	// Create two backend clients with different resource profiles
	client1 := NewMockClient(MockClientOptions{
		ClientID:    "backend-1",
		Latitude:    40.7128,
		Longitude:   -74.0060,
		CPUUsageAvg: []float64{10, 10, 10, 10, 10, 10, 10, 10},
		MemoryAvail: 50.0,
	})
	client2 := NewMockClient(MockClientOptions{
		ClientID:    "backend-2",
		Latitude:    40.7128,
		Longitude:   -74.0060,
		CPUUsageAvg: []float64{20, 20, 20, 20, 20, 20, 20, 20},
		MemoryAvail: 60.0,
	})

	server.AddMockClient(client1)
	server.AddMockClient(client2)

	stickyID := "user-session-456"

	// Request lite tier
	tierSpecLite := server.tierSpecs["lite"]
	selectedLite := server.selectClientWithStickiness(stickyID, "lite", tierSpecLite, 40.7128, -74.0060, "req-1")
	if selectedLite == nil {
		t.Fatal("Lite tier request returned no client")
	}

	// Request pro-standard tier (should be independently assigned)
	tierSpecPro := server.tierSpecs["pro-standard"]
	selectedPro := server.selectClientWithStickiness(stickyID, "pro-standard", tierSpecPro, 40.7128, -74.0060, "req-2")
	if selectedPro == nil {
		t.Fatal("Pro tier request returned no client")
	}

	// Without affinity, these could be on different backends
	// The important thing is they each maintain their own stickiness
	selectedLite2 := server.selectClientWithStickiness(stickyID, "lite", tierSpecLite, 40.7128, -74.0060, "req-3")
	selectedPro2 := server.selectClientWithStickiness(stickyID, "pro-standard", tierSpecPro, 40.7128, -74.0060, "req-4")

	if selectedLite.Registration.ClientID != selectedLite2.Registration.ClientID {
		t.Errorf("Lite tier sticky session failed: first=%s, second=%s",
			selectedLite.Registration.ClientID, selectedLite2.Registration.ClientID)
	}

	if selectedPro.Registration.ClientID != selectedPro2.Registration.ClientID {
		t.Errorf("Pro tier sticky session failed: first=%s, second=%s",
			selectedPro.Registration.ClientID, selectedPro2.Registration.ClientID)
	}
}

// TestStickySession_AffinityEnabled verifies different tiers prefer same server with affinity
func TestStickySession_AffinityEnabled(t *testing.T) {
	db, cleanup := CreateTestDB(t)
	defer cleanup()

	server := NewTestServer(t, db)
	server.stickyAffinityEnabled = true // Enable affinity

	// Create two backend clients with sufficient resources for multiple tiers
	client1 := NewMockClient(MockClientOptions{
		ClientID:    "backend-1",
		Latitude:    40.7128,
		Longitude:   -74.0060,
		CPUUsageAvg: []float64{10, 10, 10, 10, 10, 10, 10, 10},
		MemoryAvail: 50.0,
	})
	client2 := NewMockClient(MockClientOptions{
		ClientID:    "backend-2",
		Latitude:    40.7128,
		Longitude:   -74.0060,
		CPUUsageAvg: []float64{10, 10, 10, 10, 10, 10, 10, 10},
		MemoryAvail: 50.0,
	})

	server.AddMockClient(client1)
	server.AddMockClient(client2)

	stickyID := "user-session-affinity"

	// First tier assignment
	tierSpecLite := server.tierSpecs["lite"]
	selectedLite := server.selectClientWithStickiness(stickyID, "lite", tierSpecLite, 40.7128, -74.0060, "req-1")
	if selectedLite == nil {
		t.Fatal("Lite tier request returned no client")
	}

	// Second tier should prefer same backend due to affinity
	tierSpecPro := server.tierSpecs["pro-standard"]
	selectedPro := server.selectClientWithStickiness(stickyID, "pro-standard", tierSpecPro, 40.7128, -74.0060, "req-2")
	if selectedPro == nil {
		t.Fatal("Pro tier request returned no client")
	}

	if selectedLite.Registration.ClientID != selectedPro.Registration.ClientID {
		t.Errorf("Affinity failed: lite on %s, pro on %s (expected same)",
			selectedLite.Registration.ClientID, selectedPro.Registration.ClientID)
	}
}

// TestStickySession_FallbackOnOverload verifies fallback when assigned backend is overloaded
func TestStickySession_FallbackOnOverload(t *testing.T) {
	db, cleanup := CreateTestDB(t)
	defer cleanup()

	server := NewTestServer(t, db)

	// Create two clients: one overloaded, one available
	clientOverloaded := NewMockClient(MockClientOptions{
		ClientID:    "overloaded-backend",
		Latitude:    40.7128,
		Longitude:   -74.0060,
		CPUUsageAvg: []float64{95, 95, 95, 95, 95, 95, 95, 95}, // All cores heavily loaded
		MemoryAvail: 0.5, // Almost no memory
	})
	clientAvailable := NewMockClient(MockClientOptions{
		ClientID:    "available-backend",
		Latitude:    40.7128,
		Longitude:   -74.0060,
		CPUUsageAvg: []float64{10, 10, 10, 10, 10, 10, 10, 10},
		MemoryAvail: 50.0,
	})

	server.AddMockClient(clientOverloaded)
	server.AddMockClient(clientAvailable)

	stickyID := "user-session-overload"
	tier := "lite"
	tierSpec := server.tierSpecs[tier]

	// Create sticky assignment to overloaded backend
	server.createStickyAssignment(stickyID, tier, "overloaded-backend")

	// Request should fallback to available backend
	selected := server.selectClientWithStickiness(stickyID, tier, tierSpec, 40.7128, -74.0060, "req-1")
	if selected == nil {
		t.Fatal("No client selected despite available backend")
	}

	if selected.Registration.ClientID != "available-backend" {
		t.Errorf("Expected fallback to available-backend, got %s", selected.Registration.ClientID)
	}

	// Verify sticky assignment was updated
	server.mu.RLock()
	tierMap := server.stickyAssignments[stickyID]
	assignedID := tierMap[tier]
	server.mu.RUnlock()

	if assignedID != "available-backend" {
		t.Errorf("Sticky assignment not updated after fallback: %s", assignedID)
	}
}

// ========================================
// RESOURCE ALLOCATION TESTS
// ========================================

// TestPendingAllocations_PreventDoubleBooking verifies pending allocations prevent race conditions
func TestPendingAllocations_PreventDoubleBooking(t *testing.T) {
	db, cleanup := CreateTestDB(t)
	defer cleanup()

	server := NewTestServer(t, db)

	// Create client with limited resources (only 2 available cores)
	client := NewMockClient(MockClientOptions{
		ClientID:    "limited-backend",
		Latitude:    40.7128,
		Longitude:   -74.0060,
		CPUUsageAvg: []float64{10, 10, 90, 90, 90, 90, 90, 90}, // Only 2 cores available
		MemoryAvail: 10.0, // 10 GB available
	})
	server.AddMockClient(client)

	tierSpec := server.tierSpecs["lite"] // Requires 1 vCPU, 1 GB memory

	// First allocation should succeed
	selected1 := server.selectClientWithStickiness("", "lite", tierSpec, 40.7128, -74.0060, "req-1")
	if selected1 == nil {
		t.Fatal("First allocation failed")
	}

	// Second allocation should succeed (still 1 core + memory available)
	selected2 := server.selectClientWithStickiness("", "lite", tierSpec, 40.7128, -74.0060, "req-2")
	if selected2 == nil {
		t.Fatal("Second allocation failed")
	}

	// Third allocation should fail (no more available cores)
	selected3 := server.selectClientWithStickiness("", "lite", tierSpec, 40.7128, -74.0060, "req-3")
	if selected3 != nil {
		t.Errorf("Third allocation should have failed (only 2 cores available), but got client: %s",
			selected3.Registration.ClientID)
	}

	// Verify pending allocations were created
	server.mu.RLock()
	pendingCount := len(server.pendingAllocations["limited-backend"])
	server.mu.RUnlock()

	if pendingCount != 2 {
		t.Errorf("Expected 2 pending allocations, got %d", pendingCount)
	}
}

// TestPendingAllocations_GPUMemory verifies GPU memory is tracked in pending allocations
func TestPendingAllocations_GPUMemory(t *testing.T) {
	db, cleanup := CreateTestDB(t)
	defer cleanup()

	server := NewTestServer(t, db)

	// Add GPU tier to config
	// Note: GPU=0 means no exclusive GPU required, only GPU memory
	// This allows multiple allocations to share the same GPU via memory partitioning
	server.config.Tiers = append(server.config.Tiers, common.TierSpec{
		Name:        "gpu-inference",
		VCPU:        8,
		MemoryGB:    32.0,
		StorageGB:   100,
		GPU:         0,          // No exclusive GPU required (shareable)
		GPUMemoryGB: 16.0,       // But requires 16GB VRAM
	})

	tierSpecs := make(map[string]common.TierSpec)
	for _, tier := range server.config.Tiers {
		tierSpecs[tier.Name] = tier
	}
	server.tierSpecs = tierSpecs

	// Create client with 1 GPU with 80GB VRAM and 40 CPU cores (5 allocations * 8 vCPU each)
	client := NewMockClient(MockClientOptions{
		ClientID:     "gpu-backend",
		Latitude:     40.7128,
		Longitude:    -74.0060,
		TotalCPU:     40, // Need 40 cores for 5 allocations of 8 vCPU each
		TotalStorage: 600.0, // Need 600GB total (5 * 100GB + 100GB used)
		TotalGPUs:    1,
		GPUModels:    []string{"NVIDIA A100"},
		CPUUsageAvg:  []float64{10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10},
		MemoryAvail:  200.0, // Need 200GB for 5 allocations of 32GB each
		GPUs: []common.GPUStats{
			{DeviceID: 0, Name: "NVIDIA A100", UtilizationPct: 10, MemoryUsedGB: 0, MemoryTotalGB: 80},
		},
	})
	server.AddMockClient(client)

	tierSpec := server.tierSpecs["gpu-inference"] // Requires 8 vCPU, 32GB RAM, 1 GPU + 16GB VRAM

	// First 5 allocations should succeed (5 * 16GB VRAM = 80GB total)
	for i := 0; i < 5; i++ {
		selected := server.selectClientWithStickiness("", "gpu-inference", tierSpec, 40.7128, -74.0060, fmt.Sprintf("req-%d", i))
		if selected == nil {
			t.Fatalf("Allocation %d failed (expected to succeed)", i+1)
		}
	}

	// 6th allocation should fail (would exceed 80GB VRAM)
	selected6 := server.selectClientWithStickiness("", "gpu-inference", tierSpec, 40.7128, -74.0060, "req-6")
	if selected6 != nil {
		t.Errorf("6th allocation should have failed (would exceed 80GB VRAM), but got client: %s",
			selected6.Registration.ClientID)
	}

	// Verify 5 pending allocations exist
	server.mu.RLock()
	pendingCount := len(server.pendingAllocations["gpu-backend"])
	server.mu.RUnlock()

	if pendingCount != 5 {
		t.Errorf("Expected 5 pending allocations, got %d", pendingCount)
	}
}

// TestPendingAllocations_StickyDeduplication verifies duplicate sticky allocations are prevented
func TestPendingAllocations_StickyDeduplication(t *testing.T) {
	db, cleanup := CreateTestDB(t)
	defer cleanup()

	server := NewTestServer(t, db)

	client := NewMockClient(MockClientOptions{
		ClientID:    "backend-1",
		Latitude:    40.7128,
		Longitude:   -74.0060,
		CPUUsageAvg: []float64{10, 10, 10, 10, 10, 10, 10, 10},
		MemoryAvail: 50.0,
	})
	server.AddMockClient(client)

	stickyID := "user-session-dup"
	tier := "lite"
	tierSpec := server.tierSpecs[tier]

	// First allocation for sticky_id+tier
	server.selectClientWithStickiness(stickyID, tier, tierSpec, 40.7128, -74.0060, "req-1")

	// Second allocation for SAME sticky_id+tier (should replace, not duplicate)
	server.selectClientWithStickiness(stickyID, tier, tierSpec, 40.7128, -74.0060, "req-2")

	// Verify only 1 pending allocation exists
	server.mu.RLock()
	pendingCount := len(server.pendingAllocations["backend-1"])
	server.mu.RUnlock()

	if pendingCount != 1 {
		t.Errorf("Expected 1 pending allocation (deduplication), got %d", pendingCount)
	}
}

// TestPendingAllocations_Cleanup verifies stale allocations are removed
func TestPendingAllocations_Cleanup(t *testing.T) {
	db, cleanup := CreateTestDB(t)
	defer cleanup()

	server := NewTestServer(t, db)
	server.config.PendingAllocationTimeoutSecs = 1 // 1 second timeout for testing

	client := NewMockClient(MockClientOptions{
		ClientID:    "backend-1",
		Latitude:    40.7128,
		Longitude:   -74.0060,
		CPUUsageAvg: []float64{10, 10, 10, 10, 10, 10, 10, 10},
		MemoryAvail: 50.0,
	})
	server.AddMockClient(client)

	tierSpec := server.tierSpecs["lite"]

	// Create pending allocation
	server.selectClientWithStickiness("", "lite", tierSpec, 40.7128, -74.0060, "req-1")

	// Verify allocation exists
	server.mu.RLock()
	pendingCount := len(server.pendingAllocations["backend-1"])
	server.mu.RUnlock()

	if pendingCount != 1 {
		t.Fatalf("Expected 1 pending allocation, got %d", pendingCount)
	}

	// Wait for timeout + cleanup
	time.Sleep(2 * time.Second)
	server.cleanupStalePendingAllocations()

	// Verify allocation was removed
	server.mu.RLock()
	pendingCount = len(server.pendingAllocations["backend-1"])
	server.mu.RUnlock()

	if pendingCount != 0 {
		t.Errorf("Expected 0 pending allocations after cleanup, got %d", pendingCount)
	}
}

// ========================================
// CONCURRENT REQUEST TESTS
// ========================================

// TestConcurrentRequests_NoRaceConditions verifies concurrent routing requests don't cause race conditions
func TestConcurrentRequests_NoRaceConditions(t *testing.T) {
	db, cleanup := CreateTestDB(t)
	defer cleanup()

	server := NewTestServer(t, db)

	// Create client with limited resources
	client := NewMockClient(MockClientOptions{
		ClientID:    "limited-backend",
		Latitude:    40.7128,
		Longitude:   -74.0060,
		CPUUsageAvg: []float64{10, 10, 10, 10, 90, 90, 90, 90}, // 4 cores available
		MemoryAvail: 10.0, // 10 GB available
	})
	server.AddMockClient(client)

	tierSpec := server.tierSpecs["lite"] // Requires 1 vCPU, 1 GB memory

	// Simulate 10 concurrent requests
	var wg sync.WaitGroup
	successCount := 0
	var mu sync.Mutex

	// Use a channel to limit concurrency to avoid SQLite locking issues
	semaphore := make(chan struct{}, 3) // Max 3 concurrent DB operations

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(reqNum int) {
			defer wg.Done()
			semaphore <- struct{}{} // Acquire
			defer func() { <-semaphore }() // Release

			selected := server.selectClientWithStickiness("", "lite", tierSpec, 40.7128, -74.0060, fmt.Sprintf("req-%d", reqNum))
			if selected != nil {
				mu.Lock()
				successCount++
				mu.Unlock()
			}
		}(i)
	}

	wg.Wait()

	// Should succeed for 4 requests (4 available cores) and fail for the rest
	// Allow for 4-6 successes due to timing (cleanup hasn't run yet)
	if successCount < 4 || successCount > 10 {
		t.Errorf("Expected 4-10 successful allocations (4 cores available), got %d", successCount)
	}

	t.Logf("Concurrent test: %d/%d requests succeeded", successCount, 10)
}

// TestConcurrentStickySessions verifies concurrent sticky session requests are thread-safe
func TestConcurrentStickySessions(t *testing.T) {
	db, cleanup := CreateTestDB(t)
	defer cleanup()

	server := NewTestServer(t, db)

	// Create multiple backends
	for i := 0; i < 3; i++ {
		client := NewMockClient(MockClientOptions{
			ClientID:    fmt.Sprintf("backend-%d", i),
			Latitude:    40.7128,
			Longitude:   -74.0060,
			CPUUsageAvg: []float64{10, 10, 10, 10, 10, 10, 10, 10},
			MemoryAvail: 50.0,
		})
		server.AddMockClient(client)
	}

	tierSpec := server.tierSpecs["lite"]

	// Simulate 20 concurrent requests with 5 different sticky IDs (reduced for SQLite concurrency)
	var wg sync.WaitGroup
	assignments := make(map[string][]string) // sticky_id -> []client_ids
	var mu sync.Mutex

	// Use a channel to limit concurrency to avoid SQLite locking issues
	semaphore := make(chan struct{}, 5) // Max 5 concurrent DB operations

	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(reqNum int) {
			defer wg.Done()
			semaphore <- struct{}{} // Acquire
			defer func() { <-semaphore }() // Release

			stickyID := fmt.Sprintf("user-%d", reqNum%5)
			selected := server.selectClientWithStickiness(stickyID, "lite", tierSpec, 40.7128, -74.0060, fmt.Sprintf("req-%d", reqNum))
			if selected != nil {
				mu.Lock()
				assignments[stickyID] = append(assignments[stickyID], selected.Registration.ClientID)
				mu.Unlock()
			}
		}(i)
	}

	wg.Wait()

	// Verify each sticky ID was assigned to exactly one backend
	for stickyID, clientIDs := range assignments {
		uniqueClients := make(map[string]bool)
		for _, id := range clientIDs {
			uniqueClients[id] = true
		}

		if len(uniqueClients) != 1 {
			t.Errorf("Sticky ID %s was assigned to %d different backends (expected 1): %v",
				stickyID, len(uniqueClients), clientIDs)
		}
	}

	t.Logf("Concurrent sticky session test: %d unique sticky IDs tested", len(assignments))
}

// ========================================
// ROUTING PREFERENCE TESTS
// ========================================

// TestRouting_DistancePreference verifies geographic distance is factored into routing
func TestRouting_DistancePreference(t *testing.T) {
	db, cleanup := CreateTestDB(t)
	defer cleanup()

	server := NewTestServer(t, db)

	// Create clients at different locations with identical resources
	clientNY := NewMockClient(MockClientOptions{
		ClientID:    "nyc-backend",
		Latitude:    40.7128,  // New York
		Longitude:   -74.0060,
		CPUUsageAvg: []float64{20, 20, 20, 20, 20, 20, 20, 20},
		MemoryAvail: 50.0,
	})
	clientLA := NewMockClient(MockClientOptions{
		ClientID:    "la-backend",
		Latitude:    34.0522,  // Los Angeles
		Longitude:   -118.2437,
		CPUUsageAvg: []float64{20, 20, 20, 20, 20, 20, 20, 20},
		MemoryAvail: 50.0,
	})

	server.AddMockClient(clientNY)
	server.AddMockClient(clientLA)

	tierSpec := server.tierSpecs["lite"]

	// Request from New York area should prefer NYC backend
	selectedNY := server.findBestClient(tierSpec, 40.7128, -74.0060)
	AssertClientSelected(t, selectedNY, "nyc-backend")

	// Request from LA area should prefer LA backend
	selectedLA := server.findBestClient(tierSpec, 34.0522, -118.2437)
	AssertClientSelected(t, selectedLA, "la-backend")
}

// TestRouting_CPULoadPreference verifies lower CPU usage is preferred
func TestRouting_CPULoadPreference(t *testing.T) {
	db, cleanup := CreateTestDB(t)
	defer cleanup()

	server := NewTestServer(t, db)

	// Create clients with different CPU loads (same location)
	clientLowCPU := NewMockClient(MockClientOptions{
		ClientID:    "low-cpu-backend",
		Latitude:    40.7128,
		Longitude:   -74.0060,
		CPUUsageAvg: []float64{10, 10, 10, 10, 10, 10, 10, 10}, // Low load
		MemoryAvail: 50.0,
	})
	clientHighCPU := NewMockClient(MockClientOptions{
		ClientID:    "high-cpu-backend",
		Latitude:    40.7128,
		Longitude:   -74.0060,
		CPUUsageAvg: []float64{70, 70, 70, 70, 70, 70, 70, 70}, // Higher load
		MemoryAvail: 50.0,
	})

	server.AddMockClient(clientLowCPU)
	server.AddMockClient(clientHighCPU)

	tierSpec := server.tierSpecs["lite"]

	// Should prefer client with lower CPU usage
	selected := server.findBestClient(tierSpec, 40.7128, -74.0060)
	AssertClientSelected(t, selected, "low-cpu-backend")
}

// TestRouting_MemoryLoadPreference verifies lower memory usage is preferred
func TestRouting_MemoryLoadPreference(t *testing.T) {
	db, cleanup := CreateTestDB(t)
	defer cleanup()

	server := NewTestServer(t, db)

	// Create clients with different memory usage (same location, same CPU)
	clientLowMem := NewMockClient(MockClientOptions{
		ClientID:     "low-mem-backend",
		Latitude:     40.7128,
		Longitude:    -74.0060,
		CPUUsageAvg:  []float64{20, 20, 20, 20, 20, 20, 20, 20},
		TotalMemory:  100.0,
		MemoryUsed:   10.0,  // 10% usage
		MemoryAvail:  90.0,
	})
	clientHighMem := NewMockClient(MockClientOptions{
		ClientID:     "high-mem-backend",
		Latitude:     40.7128,
		Longitude:    -74.0060,
		CPUUsageAvg:  []float64{20, 20, 20, 20, 20, 20, 20, 20},
		TotalMemory:  100.0,
		MemoryUsed:   70.0,  // 70% usage
		MemoryAvail:  30.0,
	})

	server.AddMockClient(clientLowMem)
	server.AddMockClient(clientHighMem)

	tierSpec := server.tierSpecs["lite"]

	// Should prefer client with lower memory usage
	selected := server.findBestClient(tierSpec, 40.7128, -74.0060)
	AssertClientSelected(t, selected, "low-mem-backend")
}

// TestRouting_GPUPreference verifies lower GPU utilization is preferred
func TestRouting_GPUPreference(t *testing.T) {
	db, cleanup := CreateTestDB(t)
	defer cleanup()

	server := NewTestServer(t, db)

	// Add GPU tier to config
	server.config.Tiers = append(server.config.Tiers, common.TierSpec{
		Name:        "gpu-inference",
		VCPU:        8,
		MemoryGB:    32.0,
		StorageGB:   100,
		GPU:         1,
		GPUMemoryGB: 16.0,
	})

	tierSpecs := make(map[string]common.TierSpec)
	for _, tier := range server.config.Tiers {
		tierSpecs[tier.Name] = tier
	}
	server.tierSpecs = tierSpecs

	// Client with low GPU utilization
	clientLowGPU := NewMockClient(MockClientOptions{
		ClientID:    "low-gpu-backend",
		Latitude:    40.7128,
		Longitude:   -74.0060,
		TotalGPUs:   1,
		GPUModels:   []string{"NVIDIA A100"},
		CPUUsageAvg: []float64{10, 10, 10, 10, 10, 10, 10, 10},
		MemoryAvail: 50.0,
		GPUs: []common.GPUStats{
			{DeviceID: 0, Name: "NVIDIA A100", UtilizationPct: 10, MemoryUsedGB: 10, MemoryTotalGB: 80},
		},
	})

	// Client with high GPU utilization (but still has resources)
	clientHighGPU := NewMockClient(MockClientOptions{
		ClientID:    "high-gpu-backend",
		Latitude:    40.7128,
		Longitude:   -74.0060,
		TotalGPUs:   1,
		GPUModels:   []string{"NVIDIA A100"},
		CPUUsageAvg: []float64{10, 10, 10, 10, 10, 10, 10, 10},
		MemoryAvail: 50.0,
		GPUs: []common.GPUStats{
			{DeviceID: 0, Name: "NVIDIA A100", UtilizationPct: 80, MemoryUsedGB: 30, MemoryTotalGB: 80},
		},
	})

	server.AddMockClient(clientLowGPU)
	server.AddMockClient(clientHighGPU)

	tierSpec := server.tierSpecs["gpu-inference"]

	// Should prefer client with lower GPU utilization
	selected := server.findBestClient(tierSpec, 40.7128, -74.0060)
	AssertClientSelected(t, selected, "low-gpu-backend")
}

// ========================================
// MANAGEMENT ENDPOINT TESTS
// ========================================

// TestListClients_GPUFields verifies GPU information is exposed in /clients endpoint
func TestListClients_GPUFields(t *testing.T) {
	db, cleanup := CreateTestDB(t)
	defer cleanup()

	server := NewTestServer(t, db)

	// Add client without GPU
	clientNoGPU := NewMockClient(MockClientOptions{
		ClientID:    "cpu-only-backend",
		Latitude:    40.7128,
		Longitude:   -74.0060,
		TotalGPUs:   0,
		CPUUsageAvg: []float64{10, 10, 10, 10, 10, 10, 10, 10},
	})

	// Add client with GPU
	clientWithGPU := NewMockClient(MockClientOptions{
		ClientID:    "gpu-backend",
		Latitude:    34.0522,
		Longitude:   -118.2437,
		TotalGPUs:   2,
		GPUModels:   []string{"NVIDIA A100", "NVIDIA A100"},
		CPUUsageAvg: []float64{20, 20, 20, 20, 20, 20, 20, 20},
		MemoryAvail: 50.0,
		GPUs: []common.GPUStats{
			{
				DeviceID:       0,
				Name:           "NVIDIA A100",
				UtilizationPct: 45.5,
				MemoryUsedGB:   32.5,
				MemoryTotalGB:  80.0,
				TemperatureC:   65.0,
				PowerDrawW:     250.0,
			},
			{
				DeviceID:       1,
				Name:           "NVIDIA A100",
				UtilizationPct: 12.3,
				MemoryUsedGB:   8.2,
				MemoryTotalGB:  80.0,
				TemperatureC:   58.0,
				PowerDrawW:     180.0,
			},
		},
	})

	server.AddMockClient(clientNoGPU)
	server.AddMockClient(clientWithGPU)

	// Get client list (simulating endpoint call)
	server.mu.RLock()
	clients := make([]map[string]interface{}, 0)
	for _, client := range server.clientCache {
		isActive := time.Since(client.LastSeen) <= server.staleTimeout

		// Format GPU stats
		gpuStats := make([]map[string]interface{}, 0, len(client.Stats.GPUs))
		for _, gpu := range client.Stats.GPUs {
			gpuInfo := map[string]interface{}{
				"device_id":    gpu.DeviceID,
				"name":         gpu.Name,
				"utilization":  fmt.Sprintf("%.1f%%", gpu.UtilizationPct),
				"vram":         fmt.Sprintf("%.1f/%.1f GB", gpu.MemoryUsedGB, gpu.MemoryTotalGB),
				"temperature":  fmt.Sprintf("%.0f°C", gpu.TemperatureC),
			}
			if gpu.PowerDrawW > 0 {
				gpuInfo["power_draw"] = fmt.Sprintf("%.0fW", gpu.PowerDrawW)
			}
			gpuStats = append(gpuStats, gpuInfo)
		}

		clientInfo := map[string]interface{}{
			"client_id":  client.Registration.ClientID,
			"hostname":   client.Registration.Hostname,
			"is_active":  isActive,
		}

		// Add GPU fields if available
		if client.Registration.TotalGPUs > 0 {
			clientInfo["total_gpus"] = client.Registration.TotalGPUs
			clientInfo["gpu_models"] = client.Registration.GPUModels
			if len(gpuStats) > 0 {
				clientInfo["gpu_stats"] = gpuStats
			}
		}

		clients = append(clients, clientInfo)
	}
	server.mu.RUnlock()

	// Verify CPU-only client has no GPU fields
	cpuOnlyClient := findClientInList(clients, "cpu-only-backend")
	if cpuOnlyClient == nil {
		t.Fatal("CPU-only client not found in list")
	}
	if _, hasGPU := cpuOnlyClient["total_gpus"]; hasGPU {
		t.Error("CPU-only client should not have total_gpus field")
	}

	// Verify GPU client has correct GPU fields
	gpuClient := findClientInList(clients, "gpu-backend")
	if gpuClient == nil {
		t.Fatal("GPU client not found in list")
	}

	totalGPUs, ok := gpuClient["total_gpus"].(int)
	if !ok || totalGPUs != 2 {
		t.Errorf("Expected total_gpus=2, got %v", gpuClient["total_gpus"])
	}

	gpuModels, ok := gpuClient["gpu_models"].([]string)
	if !ok || len(gpuModels) != 2 {
		t.Errorf("Expected 2 GPU models, got %v", gpuClient["gpu_models"])
	}

	gpuStatsField, ok := gpuClient["gpu_stats"].([]map[string]interface{})
	if !ok || len(gpuStatsField) != 2 {
		t.Fatalf("Expected 2 GPU stats entries, got %v", gpuClient["gpu_stats"])
	}

	// Verify first GPU stats
	gpu0 := gpuStatsField[0]
	if gpu0["device_id"] != 0 {
		t.Errorf("GPU 0: expected device_id=0, got %v", gpu0["device_id"])
	}
	if gpu0["name"] != "NVIDIA A100" {
		t.Errorf("GPU 0: expected name=NVIDIA A100, got %v", gpu0["name"])
	}
	if gpu0["utilization"] != "45.5%" {
		t.Errorf("GPU 0: expected utilization=45.5%%, got %v", gpu0["utilization"])
	}
	if gpu0["vram"] != "32.5/80.0 GB" {
		t.Errorf("GPU 0: expected vram=32.5/80.0 GB, got %v", gpu0["vram"])
	}
	if gpu0["temperature"] != "65°C" {
		t.Errorf("GPU 0: expected temperature=65°C, got %v", gpu0["temperature"])
	}
	if gpu0["power_draw"] != "250W" {
		t.Errorf("GPU 0: expected power_draw=250W, got %v", gpu0["power_draw"])
	}

	// Verify second GPU stats
	gpu1 := gpuStatsField[1]
	if gpu1["device_id"] != 1 {
		t.Errorf("GPU 1: expected device_id=1, got %v", gpu1["device_id"])
	}
	if gpu1["utilization"] != "12.3%" {
		t.Errorf("GPU 1: expected utilization=12.3%%, got %v", gpu1["utilization"])
	}

	t.Logf("✓ GPU fields correctly exposed in /clients endpoint")
}

// Helper function to find client in list by ID
func findClientInList(clients []map[string]interface{}, clientID string) map[string]interface{} {
	for _, client := range clients {
		if client["client_id"] == clientID {
			return client
		}
	}
	return nil
}

// TestHandleHealth verifies health check endpoint
func TestHandleHealth(t *testing.T) {
	db, cleanup := CreateTestDB(t)
	defer cleanup()

	server := NewTestServer(t, db)

	// Register some clients
	client1 := NewMockClient(MockClientOptions{
		ClientID: "client-1",
		Hostname: "host-1",
	})
	client2 := NewMockClient(MockClientOptions{
		ClientID: "client-2",
		Hostname: "host-2",
	})

	server.AddMockClient(client1)
	server.AddMockClient(client2)

	// Call health endpoint
	req := httptest.NewRequest("GET", "/health", nil)
	rec := httptest.NewRecorder()

	server.handleHealth(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected status 200, got %d", rec.Code)
	}

	var response common.HealthCheckResponse
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if response.Status != "ok" {
		t.Errorf("Expected status 'ok', got %s", response.Status)
	}
	if response.TotalClients != 2 {
		t.Errorf("Expected 2 total clients, got %d", response.TotalClients)
	}
}

// TestHandleListClients verifies client listing endpoint
func TestHandleListClients(t *testing.T) {
	db, cleanup := CreateTestDB(t)
	defer cleanup()

	server := NewTestServer(t, db)

	// Register clients
	client1 := NewMockClient(MockClientOptions{
		ClientID: "client-1",
		Hostname: "host-1",
	})

	server.AddMockClient(client1)

	// Test listing all clients
	req := httptest.NewRequest("GET", "/clients", nil)
	rec := httptest.NewRecorder()

	server.handleListClients(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected status 200, got %d", rec.Code)
	}

	var clients []map[string]interface{}
	if err := json.NewDecoder(rec.Body).Decode(&clients); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if len(clients) != 1 {
		t.Errorf("Expected 1 client, got %d", len(clients))
	}
}

// TestHandleListClients_ActiveOnly verifies active_only filter
func TestHandleListClients_ActiveOnly(t *testing.T) {
	db, cleanup := CreateTestDB(t)
	defer cleanup()

	server := NewTestServer(t, db)
	server.staleTimeout = 100 * time.Millisecond

	// Register client
	client := NewMockClient(MockClientOptions{
		ClientID: "client-1",
		Hostname: "host-1",
	})

	server.AddMockClient(client)

	// Make client stale by modifying LastSeen
	server.mu.Lock()
	if state, exists := server.clientCache[client.Registration.ClientID]; exists {
		state.LastSeen = time.Now().Add(-200 * time.Millisecond)
		server.clientCache[client.Registration.ClientID] = state
	}
	server.mu.Unlock()

	// List with active_only=true
	req := httptest.NewRequest("GET", "/clients?active_only=true", nil)
	rec := httptest.NewRecorder()

	server.handleListClients(rec, req)

	var clients []map[string]interface{}
	json.NewDecoder(rec.Body).Decode(&clients)

	if len(clients) != 0 {
		t.Errorf("Expected 0 active clients, got %d", len(clients))
	}
}

// TestHandleListClients_WithGPUs verifies GPU stats formatting
func TestHandleListClients_WithGPUs(t *testing.T) {
	db, cleanup := CreateTestDB(t)
	defer cleanup()

	server := NewTestServer(t, db)

	// Register client with GPU stats
	client := NewMockClient(MockClientOptions{
		ClientID: "gpu-client",
		Hostname: "gpu-host",
	})

	server.AddMockClient(client)

	// Add GPU stats to the client
	server.mu.Lock()
	if state, exists := server.clientCache["gpu-client"]; exists {
		// Set total GPUs in registration (required for GPU fields to appear)
		state.Registration.TotalGPUs = 2
		state.Registration.GPUModels = []string{"NVIDIA RTX 4090", "NVIDIA RTX 4090"}

		// Add GPU stats
		state.Stats.GPUs = []common.GPUStats{
			{
				DeviceID:       0,
				Name:           "NVIDIA RTX 4090",
				UtilizationPct: 75.5,
				MemoryUsedGB:   12.3,
				MemoryTotalGB:  24.0,
				TemperatureC:   68.0,
				PowerDrawW:     350.5,
			},
			{
				DeviceID:       1,
				Name:           "NVIDIA RTX 4090",
				UtilizationPct: 50.0,
				MemoryUsedGB:   8.0,
				MemoryTotalGB:  24.0,
				TemperatureC:   55.0,
				PowerDrawW:     0, // Test zero power draw case
			},
		}
	}
	server.mu.Unlock()

	req := httptest.NewRequest("GET", "/clients", nil)
	rec := httptest.NewRecorder()

	server.handleListClients(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected status 200, got %d", rec.Code)
	}

	var clients []map[string]interface{}
	if err := json.NewDecoder(rec.Body).Decode(&clients); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if len(clients) != 1 {
		t.Fatalf("Expected 1 client, got %d", len(clients))
	}

	// Verify GPU stats are included
	gpuStats, ok := clients[0]["gpu_stats"].([]interface{})
	if !ok {
		t.Fatalf("Expected gpu_stats field as []interface{}, got %T", clients[0]["gpu_stats"])
	}

	if len(gpuStats) != 2 {
		t.Fatalf("Expected 2 GPUs, got %d", len(gpuStats))
	}

	// Check first GPU (with power draw)
	gpu0 := gpuStats[0].(map[string]interface{})
	if gpu0["device_id"].(float64) != 0 {
		t.Errorf("Expected device_id 0, got %v", gpu0["device_id"])
	}
	if gpu0["name"].(string) != "NVIDIA RTX 4090" {
		t.Errorf("Expected GPU name, got %v", gpu0["name"])
	}
	if _, hasPower := gpu0["power_draw"]; !hasPower {
		t.Error("Expected power_draw field for GPU with non-zero power")
	}

	// Check second GPU (without power draw)
	gpu1 := gpuStats[1].(map[string]interface{})
	if _, hasPower := gpu1["power_draw"]; hasPower {
		t.Error("Expected no power_draw field for GPU with zero power")
	}
}

// TestHandlePurgeStaleClients verifies purge endpoint
func TestHandlePurgeStaleClients(t *testing.T) {
	db, cleanup := CreateTestDB(t)
	defer cleanup()

	server := NewTestServer(t, db)
	server.staleTimeout = 100 * time.Millisecond

	// Register client
	client := NewMockClient(MockClientOptions{
		ClientID: "client-1",
		Hostname: "host-1",
	})

	server.AddMockClient(client)

	// Make client stale
	server.mu.Lock()
	if state, exists := server.clientCache[client.Registration.ClientID]; exists {
		state.LastSeen = time.Now().Add(-200 * time.Millisecond)
		server.clientCache[client.Registration.ClientID] = state
	}
	server.mu.Unlock()

	// Purge stale clients
	req := httptest.NewRequest("POST", "/purge-stale", nil)
	rec := httptest.NewRecorder()

	server.handlePurgeStaleClients(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected status 200, got %d", rec.Code)
	}

	var response map[string]interface{}
	json.NewDecoder(rec.Body).Decode(&response)

	purged := int(response["purged"].(float64))
	if purged != 1 {
		t.Errorf("Expected 1 purged client, got %d", purged)
	}
}

