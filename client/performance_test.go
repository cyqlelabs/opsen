package main

import (
	"runtime"
	"testing"
	"time"

	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/disk"
	"github.com/shirou/gopsutil/v3/mem"
)

// ========================================
// CLIENT OVERHEAD BENCHMARKS
// ========================================

// BenchmarkCPUSampling measures CPU sampling overhead
func BenchmarkCPUSampling(b *testing.B) {
	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, _ = cpu.Percent(0, true) // Per-CPU sampling
	}
}

// BenchmarkMemorySampling measures memory sampling overhead
func BenchmarkMemorySampling(b *testing.B) {
	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, _ = mem.VirtualMemory()
	}
}

// BenchmarkDiskSampling measures disk sampling overhead
func BenchmarkDiskSampling(b *testing.B) {
	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, _ = disk.Usage("/")
	}
}

// BenchmarkGPUSampling measures GPU sampling overhead (if available)
func BenchmarkGPUSampling(b *testing.B) {
	collector := NewGPUCollector(60)

	if !collector.IsEnabled() {
		b.Skip("No NVIDIA GPUs available, skipping GPU sampling benchmark")
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_ = collector.CollectSample()
	}
}

// BenchmarkFullMetricsCollection measures complete metrics collection cycle
func BenchmarkFullMetricsCollection(b *testing.B) {
	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		// CPU sampling
		_, _ = cpu.Percent(0, true)

		// Memory sampling
		_, _ = mem.VirtualMemory()

		// Disk sampling
		_, _ = disk.Usage("/")
	}
}

// ========================================
// CLIENT OVERHEAD TESTS
// ========================================

// TestClientOverhead_CPUSampling measures actual CPU overhead of sampling
func TestClientOverhead_CPUSampling(t *testing.T) {
	// Sample 100 times (simulating ~100 seconds at 1 sample/sec)
	start := time.Now()

	for i := 0; i < 100; i++ {
		_, err := cpu.Percent(0, true)
		if err != nil {
			t.Fatalf("CPU sampling failed: %v", err)
		}
	}

	duration := time.Since(start)
	avgSampleTime := duration / 100

	t.Logf("✓ Completed 100 CPU samples in %v", duration)
	t.Logf("  Average sample time: %v", avgSampleTime)
	t.Logf("  Estimated overhead: ~%.2f%% (assuming 1 sample/second)", float64(avgSampleTime.Microseconds())/10000.0)

	if avgSampleTime > 10*time.Millisecond {
		t.Logf("  WARNING: Sample time (%v) exceeds 10ms", avgSampleTime)
	}
}

// TestClientOverhead_MemorySampling measures memory sampling overhead
func TestClientOverhead_MemorySampling(t *testing.T) {
	start := time.Now()

	for i := 0; i < 100; i++ {
		_, err := mem.VirtualMemory()
		if err != nil {
			t.Fatalf("Memory sampling failed: %v", err)
		}
	}

	duration := time.Since(start)
	avgSampleTime := duration / 100

	t.Logf("✓ Completed 100 memory samples in %v", duration)
	t.Logf("  Average sample time: %v", avgSampleTime)
}

// TestClientOverhead_DiskSampling measures disk sampling overhead
func TestClientOverhead_DiskSampling(t *testing.T) {
	start := time.Now()

	for i := 0; i < 100; i++ {
		_, err := disk.Usage("/")
		if err != nil {
			t.Fatalf("Disk sampling failed: %v", err)
		}
	}

	duration := time.Since(start)
	avgSampleTime := duration / 100

	t.Logf("✓ Completed 100 disk samples in %v", duration)
	t.Logf("  Average sample time: %v", avgSampleTime)
}

// TestClientOverhead_GPUSampling measures GPU sampling overhead
func TestClientOverhead_GPUSampling(t *testing.T) {
	collector := NewGPUCollector(60)

	if !collector.IsEnabled() {
		t.Skip("No NVIDIA GPUs available, skipping GPU overhead test")
	}

	start := time.Now()

	for i := 0; i < 100; i++ {
		err := collector.CollectSample()
		if err != nil {
			t.Logf("Warning: GPU stats collection error: %v", err)
		}
	}

	duration := time.Since(start)
	avgSampleTime := duration / 100

	t.Logf("✓ Completed 100 GPU samples in %v", duration)
	t.Logf("  Average sample time: %v", avgSampleTime)
}

// TestClientOverhead_CombinedSampling measures total overhead of all sampling
func TestClientOverhead_CombinedSampling(t *testing.T) {
	gpuCollector := NewGPUCollector(60)
	hasGPU := gpuCollector.IsEnabled()

	start := time.Now()

	for i := 0; i < 100; i++ {
		// CPU
		_, _ = cpu.Percent(0, true)

		// Memory
		_, _ = mem.VirtualMemory()

		// Disk
		_, _ = disk.Usage("/")

		// GPU (if available)
		if hasGPU {
			_ = gpuCollector.CollectSample()
		}
	}

	duration := time.Since(start)
	avgSampleTime := duration / 100
	overheadPct := float64(avgSampleTime.Microseconds()) / 10000.0 // Assuming 1 sample/sec

	t.Logf("✓ Completed 100 combined metric samples in %v", duration)
	t.Logf("  Average sample time: %v", avgSampleTime)
	t.Logf("  GPU sampling: %v", hasGPU)
	t.Logf("  Estimated CPU overhead: ~%.3f%% (at 1 sample/second)", overheadPct)

	if avgSampleTime > 100*time.Millisecond {
		t.Logf("  WARNING: Combined sample time (%v) exceeds 100ms", avgSampleTime)
	}
}

// TestClientOverhead_ReportInterval measures HTTP reporting overhead
func TestClientOverhead_ReportInterval(t *testing.T) {
	// Test shows time to prepare report (not including network I/O)
	gpuCollector := NewGPUCollector(60)

	// Collect some sample data first
	cpuSamples := make([][]float64, 60)
	for i := 0; i < 60; i++ {
		cpuUsage, err := cpu.Percent(0, true)
		if err != nil {
			t.Fatalf("CPU sampling failed: %v", err)
		}
		cpuSamples[i] = cpuUsage
	}

	memorySamples := make([]float64, 60)
	for i := 0; i < 60; i++ {
		vmem, err := mem.VirtualMemory()
		if err != nil {
			t.Fatalf("Memory sampling failed: %v", err)
		}
		memorySamples[i] = float64(vmem.Available) / 1024 / 1024 / 1024 // GB
	}

	// Measure report preparation time
	start := time.Now()

	// Simulate calculating averages (what happens before HTTP POST)
	avgCPU := make([]float64, len(cpuSamples[0]))
	for i := range avgCPU {
		sum := 0.0
		count := 0
		for j := range cpuSamples {
			if len(cpuSamples[j]) > i {
				sum += cpuSamples[j][i]
				count++
			}
		}
		if count > 0 {
			avgCPU[i] = sum / float64(count)
		}
	}

	// Calculate average memory (used for report preparation test)
	avgMemory := 0.0
	for _, v := range memorySamples {
		avgMemory += v
	}
	if len(memorySamples) > 0 {
		avgMemory /= float64(len(memorySamples))
	}
	_ = avgMemory // Mark as used for benchmark purposes

	// Collect GPU stats
	_ = gpuCollector.CollectSample()

	duration := time.Since(start)

	t.Logf("✓ Report preparation time: %v", duration)
	t.Logf("  Sample window: 60 samples")
	t.Logf("  CPU cores: %d", len(avgCPU))

	if duration > 10*time.Millisecond {
		t.Logf("  WARNING: Report preparation (%v) exceeds 10ms", duration)
	}
}

// ========================================
// MEMORY USAGE TESTS
// ========================================

// TestClientMemoryUsage_Baseline measures baseline client memory
func TestClientMemoryUsage_Baseline(t *testing.T) {
	var m1, m2 runtime.MemStats

	runtime.GC()
	runtime.ReadMemStats(&m1)

	// Create metrics collector (without starting goroutines)
	gpuCollector := NewGPUCollector(60)

	config := Config{
		ServerURL:       "http://localhost:8080",
		ClientID:        "test-client",
		Hostname:        "test-host",
		WindowMinutes:   15,
		ReportInterval:  60,
		DiskPath:        "/",
		SkipGeolocation: true,
	}

	collector := &MetricsCollector{
		config:         config,
		cpuSamples:     make([][]float64, 900), // 15 minutes * 60 samples/min
		memorySamples:  make([]float64, 900),
		diskSamples:    make([]float64, 900),
		gpuCollector:   gpuCollector,
		maxSamples:     900,
		circuitBreaker: NewCircuitBreaker(5, 30*time.Second),
		retryConfig:    DefaultRetryConfig(),
	}

	runtime.GC()
	runtime.ReadMemStats(&m2)

	allocatedMB := float64(m2.Alloc-m1.Alloc) / 1024 / 1024
	t.Logf("✓ Client baseline memory usage: %.2f MB", allocatedMB)
	t.Logf("  Heap objects: %d", m2.HeapObjects-m1.HeapObjects)
	t.Logf("  Sys memory: %.2f MB", float64(m2.Sys-m1.Sys)/1024/1024)
	t.Logf("  Sample window: %d minutes (%d samples)", config.WindowMinutes, collector.maxSamples)

	// Note: This includes test overhead, actual client should be lower
	if allocatedMB > 10 {
		t.Logf("  INFO: Memory (%.2f MB) includes test overhead (target: ~5MB for actual client)", allocatedMB)
	}
}

// TestClientMemoryUsage_WithSamples measures memory after collecting samples
func TestClientMemoryUsage_WithSamples(t *testing.T) {
	var m1, m2 runtime.MemStats

	gpuCollector := NewGPUCollector(900)

	config := Config{
		ServerURL:       "http://localhost:8080",
		ClientID:        "test-client",
		Hostname:        "test-host",
		WindowMinutes:   15,
		ReportInterval:  60,
		DiskPath:        "/",
		SkipGeolocation: true,
	}

	collector := &MetricsCollector{
		config:         config,
		cpuSamples:     make([][]float64, 900),
		memorySamples:  make([]float64, 900),
		diskSamples:    make([]float64, 900),
		gpuCollector:   gpuCollector,
		maxSamples:     900,
		circuitBreaker: NewCircuitBreaker(5, 30*time.Second),
		retryConfig:    DefaultRetryConfig(),
	}

	runtime.GC()
	runtime.ReadMemStats(&m1)

	// Fill sample buffers
	numCPUs := 8 // Typical CPU count
	for i := 0; i < 900; i++ {
		collector.cpuSamples[i] = make([]float64, numCPUs)
		for j := 0; j < numCPUs; j++ {
			collector.cpuSamples[i][j] = 10.0 + float64(i%50)
		}
		collector.memorySamples[i] = 16.0
		collector.diskSamples[i] = 100.0
	}

	runtime.GC()
	runtime.ReadMemStats(&m2)

	sampleMemoryMB := float64(m2.Alloc-m1.Alloc) / 1024 / 1024
	t.Logf("✓ Memory for 900 samples (15 min window): %.2f MB", sampleMemoryMB)
	t.Logf("  CPU cores tracked: %d", numCPUs)
	t.Logf("  Total data points: %d (CPU) + %d (memory) + %d (disk) = %d",
		900*numCPUs, 900, 900, 900*numCPUs+1800)
	t.Logf("  Bytes per sample: %.2f", float64(m2.Alloc-m1.Alloc)/float64(900*numCPUs+1800))
}

// ========================================
// CIRCUIT BREAKER BENCHMARKS
// ========================================

// BenchmarkCircuitBreaker_Closed measures overhead when circuit is closed
func BenchmarkCircuitBreaker_Closed(b *testing.B) {
	cb := NewCircuitBreaker(5, 30*time.Second)

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_ = cb.Call(func() error {
			return nil // Always succeed
		})
	}
}

// BenchmarkCircuitBreaker_Open measures overhead when circuit is open
func BenchmarkCircuitBreaker_Open(b *testing.B) {
	cb := NewCircuitBreaker(3, 30*time.Second)

	// Open the circuit
	for i := 0; i < 3; i++ {
		_ = cb.Call(func() error {
			return ErrCircuitOpen
		})
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_ = cb.Call(func() error {
			b.Fatal("Should not be called when circuit is open")
			return nil
		})
	}
}
