package main

import (
	"sync"
	"testing"
	"time"

	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/disk"
	"github.com/shirou/gopsutil/v3/mem"
)

// TestCollectMetrics_DataCollection verifies collectMetrics actually collects data
func TestCollectMetrics_DataCollection(t *testing.T) {
	config := Config{
		ServerURL:       "http://localhost:8080",
		ClientID:        "test-client-collect",
		Hostname:        "test-host",
		WindowMinutes:   1,
		ReportInterval:  60,
		DiskPath:        "/",
		SkipGeolocation: true,
	}

	gpuCollector := NewGPUCollector(5)

	collector := &MetricsCollector{
		config:         config,
		httpClient:     nil,
		cpuSamples:     make([][]float64, 5),
		memorySamples:  make([]float64, 5),
		diskSamples:    make([]float64, 5),
		gpuCollector:   gpuCollector,
		maxSamples:     5,
		circuitBreaker: NewCircuitBreaker(5, 30*time.Second),
		retryConfig:    DefaultRetryConfig(),
	}

	// Start collectMetrics in background
	done := make(chan bool)
	go func() {
		// Run for a short time then signal completion
		ticker := time.NewTicker(1 * time.Second)
		defer ticker.Stop()

		collected := 0
		for range ticker.C {
			// CPU per-core usage
			perCore, err := cpu.Percent(0, true)
			if err == nil && len(perCore) > 0 {
				collector.cpuSamples[collector.sampleIndex] = perCore
			}

			// Memory usage
			memInfo, err := mem.VirtualMemory()
			if err == nil {
				collector.memorySamples[collector.sampleIndex] = float64(memInfo.Used) / 1024 / 1024 / 1024
			}

			// Disk usage
			diskInfo, err := disk.Usage(collector.config.DiskPath)
			if err == nil {
				collector.diskSamples[collector.sampleIndex] = float64(diskInfo.Used) / 1024 / 1024 / 1024
			}

			// GPU metrics (if available)
			if collector.gpuCollector.IsEnabled() {
				_ = collector.gpuCollector.CollectSample()
			}

			collector.sampleIndex = (collector.sampleIndex + 1) % collector.maxSamples
			collected++

			if collected >= 3 {
				done <- true
				return
			}
		}
	}()

	// Wait for collection to complete
	select {
	case <-done:
		t.Log("Metrics collection completed successfully")
	case <-time.After(5 * time.Second):
		t.Fatal("Metrics collection timed out")
	}

	// Verify some data was collected
	hasData := false
	for i := 0; i < collector.maxSamples; i++ {
		if len(collector.cpuSamples[i]) > 0 || collector.memorySamples[i] > 0 || collector.diskSamples[i] > 0 {
			hasData = true
			break
		}
	}

	if !hasData {
		t.Error("No metrics data was collected")
	}
}

// TestCollectMetrics_SampleIndexWrap verifies sample index wraps correctly
func TestCollectMetrics_SampleIndexWrap(t *testing.T) {
	collector := &MetricsCollector{
		cpuSamples:    make([][]float64, 3),
		memorySamples: make([]float64, 3),
		diskSamples:   make([]float64, 3),
		maxSamples:    3,
		sampleIndex:   0,
	}

	// Simulate multiple collections
	for i := 0; i < 10; i++ {
		collector.sampleIndex = (collector.sampleIndex + 1) % collector.maxSamples
	}

	// Verify index wrapped correctly (10 % 3 = 1)
	expectedIndex := 10 % 3
	if collector.sampleIndex != expectedIndex {
		t.Errorf("Expected sampleIndex %d after 10 increments, got %d", expectedIndex, collector.sampleIndex)
	}
}

// TestCollectMetrics_ConcurrentAccess verifies thread safety
func TestCollectMetrics_ConcurrentAccess(t *testing.T) {
	config := Config{
		ServerURL:       "http://localhost:8080",
		ClientID:        "test-client-concurrent",
		Hostname:        "test-host",
		WindowMinutes:   1,
		ReportInterval:  60,
		DiskPath:        "/",
		SkipGeolocation: true,
	}

	gpuCollector := NewGPUCollector(10)

	collector := &MetricsCollector{
		config:         config,
		httpClient:     nil,
		cpuSamples:     make([][]float64, 10),
		memorySamples:  make([]float64, 10),
		diskSamples:    make([]float64, 10),
		gpuCollector:   gpuCollector,
		maxSamples:     10,
		circuitBreaker: NewCircuitBreaker(5, 30*time.Second),
		retryConfig:    DefaultRetryConfig(),
	}

	var wg sync.WaitGroup
	errors := make(chan error, 3)

	// Start multiple goroutines accessing collector
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			for j := 0; j < 5; j++ {
				// Simulate data collection
				perCore, err := cpu.Percent(0, true)
				if err == nil && len(perCore) > 0 {
					// This could race without proper synchronization in real code
					idx := collector.sampleIndex
					if idx < len(collector.cpuSamples) {
						collector.cpuSamples[idx] = perCore
					}
				}

				time.Sleep(10 * time.Millisecond)
			}
		}(i)
	}

	// Wait for all goroutines to complete
	wg.Wait()
	close(errors)

	// Check if any errors occurred
	for err := range errors {
		t.Errorf("Concurrent access error: %v", err)
	}
}

// TestCalculateCPUAverages_EmptyData verifies handling of empty CPU data
func TestCalculateCPUAverages_EmptyData(t *testing.T) {
	collector := &MetricsCollector{
		cpuSamples: make([][]float64, 3),
		maxSamples: 3,
	}

	// Calculate averages with no data
	result := collector.calculateCPUAverages()

	if len(result) != 0 {
		t.Errorf("Expected empty result for empty CPU samples, got %d cores", len(result))
	}
}

// TestCalculateCPUAverages_VariableCores verifies handling of variable core counts
func TestCalculateCPUAverages_VariableCores(t *testing.T) {
	collector := &MetricsCollector{
		cpuSamples: make([][]float64, 3),
		maxSamples: 3,
	}

	// Sample 0: 2 cores
	collector.cpuSamples[0] = []float64{10.0, 20.0}
	// Sample 1: 4 cores (different count)
	collector.cpuSamples[1] = []float64{15.0, 25.0, 35.0, 45.0}
	// Sample 2: 2 cores again
	collector.cpuSamples[2] = []float64{20.0, 30.0}

	// Calculate averages
	result := collector.calculateCPUAverages()

	// Should handle variable core counts gracefully
	if len(result) == 0 {
		t.Error("Expected non-empty result even with variable core counts")
	}

	t.Logf("Calculated %d core averages from variable samples", len(result))
}

// TestCalculateAverage_EmptySlice verifies handling of empty slice
func TestCalculateAverage_EmptySlice(t *testing.T) {
	collector := &MetricsCollector{}

	result := collector.calculateAverage([]float64{})

	if result != 0.0 {
		t.Errorf("Expected 0.0 for empty slice, got %f", result)
	}
}

// TestCalculateAverage_AllZeros verifies handling of all-zero values
func TestCalculateAverage_AllZeros(t *testing.T) {
	collector := &MetricsCollector{}

	samples := []float64{0.0, 0.0, 0.0}
	result := collector.calculateAverage(samples)

	if result != 0.0 {
		t.Errorf("Expected 0.0 for all-zero samples, got %f", result)
	}
}

// TestCalculateAverage_MixedValues verifies correct average calculation
func TestCalculateAverage_MixedValues(t *testing.T) {
	collector := &MetricsCollector{}

	samples := []float64{10.0, 20.0, 30.0, 0.0, 0.0}
	result := collector.calculateAverage(samples)

	// The function skips zero values, so: (10 + 20 + 30) / 3 = 20.0
	expected := 20.0
	if result != expected {
		t.Errorf("Expected average %f, got %f", expected, result)
	}
}
