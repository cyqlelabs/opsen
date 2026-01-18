package main

import (
	"fmt"
	"runtime"
	"sync"
	"testing"
	"time"

	"cyqle.in/opsen/common"
)

// ========================================
// ROUTING LATENCY BENCHMARKS
// ========================================

// BenchmarkRoutingLatency_SingleClient measures routing decision time with 1 client
func BenchmarkRoutingLatency_SingleClient(b *testing.B) {
	db, cleanup := CreateTestDB(&testing.T{})
	defer cleanup()

	server := NewTestServer(&testing.T{}, db)

	// Add one client
	client := NewMockClient(MockClientOptions{
		ClientID:    "backend-1",
		Latitude:    40.7128,
		Longitude:   -74.0060,
		CPUUsageAvg: []float64{10, 10, 10, 10, 10, 10, 10, 10},
		MemoryAvail: 50.0,
	})
	server.AddMockClient(client)

	tierSpec := server.tierSpecs["lite"]

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_ = server.findBestClient(tierSpec, 40.7128, -74.0060)
	}
}

// BenchmarkRoutingLatency_10Clients measures routing decision time with 10 clients
func BenchmarkRoutingLatency_10Clients(b *testing.B) {
	db, cleanup := CreateTestDB(&testing.T{})
	defer cleanup()

	server := NewTestServer(&testing.T{}, db)

	// Add 10 clients with varying resources and locations
	for i := 0; i < 10; i++ {
		client := NewMockClient(MockClientOptions{
			ClientID:    fmt.Sprintf("backend-%d", i),
			Latitude:    40.0 + float64(i)*0.5,
			Longitude:   -74.0 + float64(i)*0.3,
			CPUUsageAvg: []float64{10 + float64(i)*5, 10 + float64(i)*5, 10, 10, 10, 10, 10, 10},
			MemoryAvail: 50.0 - float64(i)*2,
		})
		server.AddMockClient(client)
	}

	tierSpec := server.tierSpecs["lite"]

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_ = server.findBestClient(tierSpec, 40.7128, -74.0060)
	}
}

// BenchmarkRoutingLatency_100Clients measures routing decision time with 100 clients
func BenchmarkRoutingLatency_100Clients(b *testing.B) {
	db, cleanup := CreateTestDB(&testing.T{})
	defer cleanup()

	server := NewTestServer(&testing.T{}, db)

	// Add 100 clients
	for i := 0; i < 100; i++ {
		client := NewMockClient(MockClientOptions{
			ClientID:    fmt.Sprintf("backend-%d", i),
			Latitude:    40.0 + float64(i%20)*0.5,
			Longitude:   -74.0 + float64(i%20)*0.3,
			CPUUsageAvg: []float64{10 + float64(i%50), 15 + float64(i%50), 10, 10, 10, 10, 10, 10},
			MemoryAvail: 50.0 - float64(i%30),
		})
		server.AddMockClient(client)
	}

	tierSpec := server.tierSpecs["lite"]

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_ = server.findBestClient(tierSpec, 40.7128, -74.0060)
	}
}

// BenchmarkRoutingLatency_WithStickySessions measures routing with sticky session lookup
func BenchmarkRoutingLatency_WithStickySessions(b *testing.B) {
	db, cleanup := CreateTestDB(&testing.T{})
	defer cleanup()

	server := NewTestServer(&testing.T{}, db)

	// Add 10 clients
	for i := 0; i < 10; i++ {
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
	stickyID := "user-session-benchmark"

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_ = server.selectClientWithStickiness(stickyID, "lite", tierSpec, 40.7128, -74.0060, fmt.Sprintf("req-%d", i))
	}
}

// BenchmarkRoutingLatency_GPUTier measures routing for GPU-enabled tiers
func BenchmarkRoutingLatency_GPUTier(b *testing.B) {
	db, cleanup := CreateTestDB(&testing.T{})
	defer cleanup()

	server := NewTestServer(&testing.T{}, db)

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

	// Add 10 GPU-enabled clients
	for i := 0; i < 10; i++ {
		client := NewMockClient(MockClientOptions{
			ClientID:    fmt.Sprintf("gpu-backend-%d", i),
			Latitude:    40.0 + float64(i)*0.5,
			Longitude:   -74.0,
			TotalGPUs:   2,
			GPUModels:   []string{"NVIDIA A100", "NVIDIA A100"},
			CPUUsageAvg: []float64{10, 10, 10, 10, 10, 10, 10, 10},
			MemoryAvail: 100.0,
			GPUs: []common.GPUStats{
				{DeviceID: 0, Name: "NVIDIA A100", UtilizationPct: 10 + float64(i)*5, MemoryUsedGB: 10, MemoryTotalGB: 80},
				{DeviceID: 1, Name: "NVIDIA A100", UtilizationPct: 15 + float64(i)*3, MemoryUsedGB: 20, MemoryTotalGB: 80},
			},
		})
		server.AddMockClient(client)
	}

	tierSpec := server.tierSpecs["gpu-inference"]

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_ = server.findBestClient(tierSpec, 40.7128, -74.0060)
	}
}

// ========================================
// CONCURRENT ROUTING BENCHMARKS
// ========================================

// BenchmarkConcurrentRouting_10Goroutines measures concurrent routing with 10 goroutines
func BenchmarkConcurrentRouting_10Goroutines(b *testing.B) {
	db, cleanup := CreateTestDB(&testing.T{})
	defer cleanup()

	server := NewTestServer(&testing.T{}, db)

	// Add 20 clients (more than goroutines to avoid contention)
	for i := 0; i < 20; i++ {
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

	b.ResetTimer()
	b.ReportAllocs()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_ = server.findBestClient(tierSpec, 40.7128, -74.0060)
		}
	})
}

// BenchmarkConcurrentRouting_100Goroutines measures concurrent routing with 100 goroutines
func BenchmarkConcurrentRouting_100Goroutines(b *testing.B) {
	db, cleanup := CreateTestDB(&testing.T{})
	defer cleanup()

	server := NewTestServer(&testing.T{}, db)

	// Add 100 clients
	for i := 0; i < 100; i++ {
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

	b.ResetTimer()
	b.ReportAllocs()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_ = server.findBestClient(tierSpec, 40.7128, -74.0060)
		}
	})
}

// ========================================
// SCALABILITY TESTS (NOT BENCHMARKS)
// ========================================

// TestScalability_100Clients verifies server can handle 100+ clients
func TestScalability_100Clients(t *testing.T) {
	db, cleanup := CreateTestDB(t)
	defer cleanup()

	server := NewTestServer(t, db)

	// Add 150 clients to test beyond 100
	for i := 0; i < 150; i++ {
		client := NewMockClient(MockClientOptions{
			ClientID:    fmt.Sprintf("backend-%d", i),
			Latitude:    40.0 + float64(i%20)*0.5,
			Longitude:   -74.0 + float64(i%20)*0.3,
			CPUUsageAvg: []float64{10 + float64(i%50), 10, 10, 10, 10, 10, 10, 10},
			MemoryAvail: 50.0,
		})
		server.AddMockClient(client)
	}

	tierSpec := server.tierSpecs["lite"]

	// Perform 1000 routing decisions
	start := time.Now()
	for i := 0; i < 1000; i++ {
		selected := server.findBestClient(tierSpec, 40.7128, -74.0060)
		if selected == nil {
			t.Fatalf("Routing failed at iteration %d", i)
		}
	}
	duration := time.Since(start)

	avgLatency := duration / 1000
	t.Logf("✓ Successfully routed 1000 requests across 150 clients")
	t.Logf("  Average routing latency: %v", avgLatency)

	if avgLatency > 10*time.Millisecond {
		t.Logf("  WARNING: Average latency (%v) exceeds 10ms target", avgLatency)
	}
}

// TestScalability_ConcurrentRequests verifies server handles concurrent load
func TestScalability_ConcurrentRequests(t *testing.T) {
	db, cleanup := CreateTestDB(t)
	defer cleanup()

	server := NewTestServer(t, db)

	// Add 100 clients
	for i := 0; i < 100; i++ {
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

	// Simulate 1000 concurrent requests
	numRequests := 1000
	var wg sync.WaitGroup
	successCount := 0
	var mu sync.Mutex

	start := time.Now()

	for i := 0; i < numRequests; i++ {
		wg.Add(1)
		go func(reqNum int) {
			defer wg.Done()

			selected := server.findBestClient(tierSpec, 40.7128, -74.0060)
			if selected != nil {
				mu.Lock()
				successCount++
				mu.Unlock()
			}
		}(i)
	}

	wg.Wait()
	duration := time.Since(start)

	avgLatency := duration / time.Duration(numRequests)
	t.Logf("✓ Completed %d concurrent requests in %v", numRequests, duration)
	t.Logf("  Success rate: %d/%d (%.1f%%)", successCount, numRequests, float64(successCount)/float64(numRequests)*100)
	t.Logf("  Average latency: %v", avgLatency)

	if successCount != numRequests {
		t.Errorf("Expected all %d requests to succeed, got %d", numRequests, successCount)
	}

	if avgLatency > 10*time.Millisecond {
		t.Logf("  WARNING: Average latency (%v) exceeds 10ms target", avgLatency)
	}
}

// ========================================
// MEMORY USAGE BENCHMARKS
// ========================================

// BenchmarkMemoryUsage_ServerWithClients measures memory allocation for server with clients
func BenchmarkMemoryUsage_ServerWithClients(b *testing.B) {
	var m1, m2 runtime.MemStats

	// Measure baseline memory
	runtime.GC()
	runtime.ReadMemStats(&m1)

	db, cleanup := CreateTestDB(&testing.T{})
	defer cleanup()

	server := NewTestServer(&testing.T{}, db)

	// Add 100 clients (typical scenario)
	for i := 0; i < 100; i++ {
		client := NewMockClient(MockClientOptions{
			ClientID:    fmt.Sprintf("backend-%d", i),
			Latitude:    40.7128,
			Longitude:   -74.0060,
			CPUUsageAvg: []float64{10, 10, 10, 10, 10, 10, 10, 10},
			MemoryAvail: 50.0,
		})
		server.AddMockClient(client)
	}

	// Force GC and measure
	runtime.GC()
	runtime.ReadMemStats(&m2)

	allocatedMB := float64(m2.Alloc-m1.Alloc) / 1024 / 1024
	b.Logf("Server with 100 clients memory usage: %.2f MB", allocatedMB)
	b.Logf("  Heap objects: %d", m2.HeapObjects-m1.HeapObjects)
	b.Logf("  Total allocated: %.2f MB", float64(m2.TotalAlloc-m1.TotalAlloc)/1024/1024)

	b.ResetTimer()

	// Benchmark allocation overhead during routing
	for i := 0; i < b.N; i++ {
		_ = server.findBestClient(server.tierSpecs["lite"], 40.7128, -74.0060)
	}
}

// TestMemoryUsage_ServerBaseline measures baseline server memory
func TestMemoryUsage_ServerBaseline(t *testing.T) {
	var m1, m2 runtime.MemStats

	// Force GC and measure baseline
	runtime.GC()
	runtime.ReadMemStats(&m1)

	db, cleanup := CreateTestDB(t)
	defer cleanup()

	_ = NewTestServer(t, db)

	// Force GC again to get accurate measurement
	runtime.GC()
	runtime.ReadMemStats(&m2)

	// Calculate the difference correctly
	var allocatedMB float64
	if m2.Alloc >= m1.Alloc {
		allocatedMB = float64(m2.Alloc-m1.Alloc) / 1024 / 1024
	} else {
		allocatedMB = 0.0
	}

	t.Logf("✓ Server baseline memory usage: %.2f MB", allocatedMB)
	t.Logf("  Current heap alloc: %.2f MB", float64(m2.Alloc)/1024/1024)
	t.Logf("  Heap objects: %d", m2.HeapObjects)
	t.Logf("  Sys memory: %.2f MB", float64(m2.Sys)/1024/1024)

	if allocatedMB > 20 {
		t.Logf("  INFO: Memory usage includes test overhead (target: ~10MB for production server)")
	}
}

// TestMemoryUsage_With100Clients measures memory with 100 clients
func TestMemoryUsage_With100Clients(t *testing.T) {
	var m1, m2 runtime.MemStats

	db, cleanup := CreateTestDB(t)
	defer cleanup()

	server := NewTestServer(t, db)

	// Measure before adding clients
	runtime.GC()
	runtime.ReadMemStats(&m1)

	// Add 100 clients
	for i := 0; i < 100; i++ {
		client := NewMockClient(MockClientOptions{
			ClientID:    fmt.Sprintf("backend-%d", i),
			Latitude:    40.7128,
			Longitude:   -74.0060,
			CPUUsageAvg: []float64{10, 10, 10, 10, 10, 10, 10, 10},
			MemoryAvail: 50.0,
		})
		server.AddMockClient(client)
	}

	// Measure after adding clients
	runtime.GC()
	runtime.ReadMemStats(&m2)

	var clientMemoryMB float64
	if m2.Alloc >= m1.Alloc {
		clientMemoryMB = float64(m2.Alloc-m1.Alloc) / 1024 / 1024
	} else {
		clientMemoryMB = 0.0
	}
	memoryPerClient := clientMemoryMB / 100.0

	t.Logf("✓ Memory for 100 clients: %.2f MB", clientMemoryMB)
	t.Logf("  Memory per client: %.3f MB", memoryPerClient)
	t.Logf("  Total heap alloc: %.2f MB", float64(m2.Alloc)/1024/1024)
	t.Logf("  Heap objects: %d", m2.HeapObjects)
}

// ========================================
// PENDING ALLOCATION BENCHMARKS
// ========================================

// BenchmarkPendingAllocation_Create measures pending allocation creation overhead
func BenchmarkPendingAllocation_Create(b *testing.B) {
	db, cleanup := CreateTestDB(&testing.T{})
	defer cleanup()

	server := NewTestServer(&testing.T{}, db)

	client := NewMockClient(MockClientOptions{
		ClientID:    "backend-1",
		Latitude:    40.7128,
		Longitude:   -74.0060,
		CPUUsageAvg: []float64{10, 10, 10, 10, 10, 10, 10, 10},
		MemoryAvail: 50.0,
	})
	server.AddMockClient(client)

	tierSpec := server.tierSpecs["lite"]

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_ = server.selectClientWithStickiness("", "lite", tierSpec, 40.7128, -74.0060, fmt.Sprintf("req-%d", i))
	}
}

// BenchmarkPendingAllocation_Cleanup measures cleanup performance
func BenchmarkPendingAllocation_Cleanup(b *testing.B) {
	db, cleanup := CreateTestDB(&testing.T{})
	defer cleanup()

	server := NewTestServer(&testing.T{}, db)
	server.config.PendingAllocationTimeoutSecs = 1

	client := NewMockClient(MockClientOptions{
		ClientID:    "backend-1",
		Latitude:    40.7128,
		Longitude:   -74.0060,
		CPUUsageAvg: []float64{10, 10, 10, 10, 10, 10, 10, 10},
		MemoryAvail: 50.0,
	})
	server.AddMockClient(client)

	tierSpec := server.tierSpecs["lite"]

	// Create some stale allocations
	for i := 0; i < 100; i++ {
		server.selectClientWithStickiness("", "lite", tierSpec, 40.7128, -74.0060, fmt.Sprintf("req-%d", i))
	}

	time.Sleep(2 * time.Second) // Make them stale

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		server.cleanupStalePendingAllocations()
	}
}
