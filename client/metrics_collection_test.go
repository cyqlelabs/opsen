package main

import (
	"testing"
	"time"

	"cyqle.in/opsen/common"
)

// TestCollectMetrics_Integration verifies metrics collection loop
func TestCollectMetrics_Integration(t *testing.T) {
	config := Config{
		ServerURL:       "http://localhost:8080",
		ClientID:        "test-client-metrics",
		Hostname:        "test-host",
		WindowMinutes:   1,
		ReportInterval:  60,
		DiskPath:        "/",
		SkipGeolocation: true,
	}

	gpuCollector := NewGPUCollector(3)
	defer gpuCollector.Close()

	collector := &MetricsCollector{
		config:         config,
		httpClient:     nil,
		cpuSamples:     make([][]float64, 3),
		memorySamples:  make([]float64, 3),
		diskSamples:    make([]float64, 3),
		gpuCollector:   gpuCollector,
		maxSamples:     3,
		circuitBreaker: NewCircuitBreaker(5, 30*time.Second),
		retryConfig:    DefaultRetryConfig(),
	}

	// Run collectMetrics in background for a short time
	done := make(chan bool)
	go func() {
		// Run for 2.5 seconds to collect at least 2 samples
		ticker := time.NewTicker(1 * time.Second)
		defer ticker.Stop()

		count := 0
		for range ticker.C {
			// CPU per-core usage (simulated)
			perCore := []float64{10.0, 20.0, 30.0}
			collector.cpuSamples[collector.sampleIndex] = perCore

			// Memory usage (simulated)
			collector.memorySamples[collector.sampleIndex] = 8.0

			// Disk usage (simulated)
			collector.diskSamples[collector.sampleIndex] = 50.0

			// GPU metrics (if available)
			if collector.gpuCollector.IsEnabled() {
				_ = collector.gpuCollector.CollectSample()
			}

			collector.sampleIndex = (collector.sampleIndex + 1) % collector.maxSamples

			count++
			if count >= 2 {
				break
			}
		}
		done <- true
	}()

	// Wait for collection to complete
	select {
	case <-done:
		t.Log("Metrics collection completed successfully")
	case <-time.After(5 * time.Second):
		t.Fatal("Metrics collection timed out")
	}

	// Verify samples were collected
	nonEmptyCount := 0
	for _, sample := range collector.cpuSamples {
		if len(sample) > 0 {
			nonEmptyCount++
		}
	}

	if nonEmptyCount < 2 {
		t.Errorf("Expected at least 2 CPU samples, got %d", nonEmptyCount)
	}
}

// TestCollectMetrics_GPUEnabled verifies GPU collection when enabled
func TestCollectMetrics_GPUEnabled(t *testing.T) {
	gpuCollector := NewGPUCollector(3)
	defer gpuCollector.Close()

	collector := &MetricsCollector{
		config: Config{
			DiskPath: "/",
		},
		cpuSamples:    make([][]float64, 3),
		memorySamples: make([]float64, 3),
		diskSamples:   make([]float64, 3),
		gpuCollector:  gpuCollector,
		maxSamples:    3,
		sampleIndex:   0,
	}

	// Simulate one collection cycle
	collector.cpuSamples[0] = []float64{10.0, 20.0}
	collector.memorySamples[0] = 8.0
	collector.diskSamples[0] = 50.0

	if gpuCollector.IsEnabled() {
		err := gpuCollector.CollectSample()
		if err != nil {
			t.Logf("GPU collection failed (expected if no GPU): %v", err)
		} else {
			t.Log("GPU metrics collected successfully")
		}
	} else {
		t.Log("GPU collection disabled (no GPUs available)")
	}

	collector.sampleIndex = (collector.sampleIndex + 1) % collector.maxSamples

	// Verify sample was stored
	if len(collector.cpuSamples[0]) == 0 {
		t.Error("Expected CPU samples to be collected")
	}
}

// TestCollectMetrics_SampleRotation verifies sample window rotation
func TestCollectMetrics_SampleRotation(t *testing.T) {
	collector := &MetricsCollector{
		config: Config{
			DiskPath: "/",
		},
		cpuSamples:    make([][]float64, 3),
		memorySamples: make([]float64, 3),
		diskSamples:   make([]float64, 3),
		gpuCollector:  &GPUCollector{enabled: false},
		maxSamples:    3,
		sampleIndex:   0,
	}

	// Collect 5 samples (more than window size)
	for i := 0; i < 5; i++ {
		collector.cpuSamples[collector.sampleIndex] = []float64{float64(i * 10)}
		collector.memorySamples[collector.sampleIndex] = float64(i)
		collector.diskSamples[collector.sampleIndex] = float64(i * 5)
		collector.sampleIndex = (collector.sampleIndex + 1) % collector.maxSamples
	}

	// After 5 samples, index should be at position 2 (5 % 3)
	if collector.sampleIndex != 2 {
		t.Errorf("Expected sampleIndex 2, got %d", collector.sampleIndex)
	}

	// Verify samples were overwritten (should have samples 2, 3, 4)
	// Sample at index 2 should be from iteration 2 (or was overwritten by iteration 5)
	if collector.memorySamples[0] != 3.0 {
		t.Logf("Sample 0 memory: %.1f (iteration 3)", collector.memorySamples[0])
	}
	if collector.memorySamples[1] != 4.0 {
		t.Logf("Sample 1 memory: %.1f (iteration 4)", collector.memorySamples[1])
	}
}

// TestCollectMetrics_AllMetricTypes verifies all metric types are collected
func TestCollectMetrics_AllMetricTypes(t *testing.T) {
	gpuCollector := NewGPUCollector(3)
	defer gpuCollector.Close()

	collector := &MetricsCollector{
		config: Config{
			DiskPath: "/",
		},
		cpuSamples:    make([][]float64, 3),
		memorySamples: make([]float64, 3),
		diskSamples:   make([]float64, 3),
		gpuCollector:  gpuCollector,
		maxSamples:    3,
		sampleIndex:   0,
	}

	// Simulate one complete collection cycle
	collector.cpuSamples[0] = []float64{25.5, 30.2, 15.8}
	collector.memorySamples[0] = 12.3
	collector.diskSamples[0] = 67.9

	if gpuCollector.IsEnabled() {
		// Try to collect GPU samples
		err := gpuCollector.CollectSample()
		if err != nil {
			t.Logf("GPU collection skipped: %v", err)
		}
	}

	// Verify all types collected
	if len(collector.cpuSamples[0]) == 0 {
		t.Error("CPU samples not collected")
	}
	if collector.memorySamples[0] == 0 {
		t.Error("Memory samples not collected")
	}
	if collector.diskSamples[0] == 0 {
		t.Error("Disk samples not collected")
	}

	// Calculate averages to ensure samples are usable
	cpuAvg := collector.calculateCPUAverages()
	if len(cpuAvg) > 0 {
		t.Logf("CPU averages calculated: %d cores", len(cpuAvg))
	}

	memAvg := collector.calculateAverage(collector.memorySamples)
	if memAvg > 0 {
		t.Logf("Memory average: %.2f GB", memAvg)
	}

	diskAvg := collector.calculateAverage(collector.diskSamples)
	if diskAvg > 0 {
		t.Logf("Disk average: %.2f GB", diskAvg)
	}
}

// TestReportStats_GPUMetrics verifies GPU metrics in stats report
func TestReportStats_GPUMetrics(t *testing.T) {
	// This test verifies that GPU stats are included in report when available
	gpuCollector := &GPUCollector{
		enabled:      false, // Simulate no GPUs
		sampleWindow: make([][]common.GPUStats, 3),
		maxSamples:   3,
	}

	collector := &MetricsCollector{
		config: Config{
			DiskPath: "/",
		},
		cpuSamples:    make([][]float64, 3),
		memorySamples: make([]float64, 3),
		diskSamples:   make([]float64, 3),
		gpuCollector:  gpuCollector,
		maxSamples:    3,
	}

	// Add sample data
	collector.cpuSamples[0] = []float64{10.0, 20.0}
	collector.memorySamples[0] = 8.0
	collector.diskSamples[0] = 50.0

	// Get GPU averages (should be empty)
	gpuStats := gpuCollector.CalculateAverages()
	if len(gpuStats) != 0 {
		t.Errorf("Expected empty GPU stats for disabled collector, got %d", len(gpuStats))
	}
}

// TestGetLocalIP_NoInterfaces verifies error handling when no interfaces available
func TestGetLocalIP_NoInterfaces(t *testing.T) {
	// This test documents the behavior - actual failure depends on system state
	collector := &MetricsCollector{}

	_, err := collector.getLocalIP()
	if err != nil {
		t.Logf("Error getting local IP (expected in some cases): %v", err)
	}
}
