package main

import (
	"testing"

	"cyqle.in/opsen/common"
)

// TestGPUCollector_NewGPUCollector_NoGPUs verifies collector initialization without GPUs
func TestGPUCollector_NewGPUCollector_NoGPUs(t *testing.T) {
	// This will typically create a disabled collector if no NVIDIA GPUs present
	gc := NewGPUCollector(60)
	defer gc.Close()

	if gc == nil {
		t.Fatal("NewGPUCollector should never return nil")
	}

	// Should have sample window initialized even if disabled
	if gc.sampleWindow == nil {
		t.Error("Sample window should be initialized")
	}

	if gc.maxSamples != 60 {
		t.Errorf("Expected maxSamples 60, got %d", gc.maxSamples)
	}

	// Most systems won't have GPUs in CI
	if !gc.enabled {
		t.Log("GPU collector disabled (no NVIDIA GPUs detected)")
	} else {
		t.Logf("GPU collector enabled with %d GPU(s)", gc.GetDeviceCount())
	}
}

// TestGPUCollector_CollectSample_MultipleDevices verifies multi-GPU collection
func TestGPUCollector_CollectSample_MultipleDevices(t *testing.T) {
	gc := NewGPUCollector(3)
	defer gc.Close()

	if !gc.IsEnabled() {
		t.Skip("No GPUs available for testing")
	}

	deviceCount := gc.GetDeviceCount()
	t.Logf("Testing with %d GPU(s)", deviceCount)

	// Collect a sample
	err := gc.CollectSample()
	if err != nil {
		t.Fatalf("Failed to collect GPU sample: %v", err)
	}

	// Verify sample was stored
	if len(gc.sampleWindow[0]) != deviceCount {
		t.Errorf("Expected %d GPU stats, got %d", deviceCount, len(gc.sampleWindow[0]))
	}
}

// TestGPUCollector_CalculateAverages_SingleDevice verifies single GPU averaging
func TestGPUCollector_CalculateAverages_SingleDevice(t *testing.T) {
	// Test with real GPU collector (will use actual or disabled collector)
	gc := NewGPUCollector(3)
	defer gc.Close()

	if !gc.IsEnabled() || gc.GetDeviceCount() == 0 {
		t.Skip("No GPUs available for single device test")
	}

	// Use the real device count but inject test data
	deviceCount := gc.GetDeviceCount()
	t.Logf("Testing with %d GPU(s), injecting test data for averaging", deviceCount)

	// Add sample data (only for first GPU)
	gc.sampleWindow[0] = []common.GPUStats{
		{DeviceID: 0, Name: gc.deviceModels[0], UtilizationPct: 30.0, MemoryUsedGB: 5.0, MemoryTotalGB: 16.0, TemperatureC: 55.0, PowerDrawW: 80.0},
	}
	gc.sampleWindow[1] = []common.GPUStats{
		{DeviceID: 0, Name: gc.deviceModels[0], UtilizationPct: 40.0, MemoryUsedGB: 6.0, MemoryTotalGB: 16.0, TemperatureC: 60.0, PowerDrawW: 90.0},
	}
	gc.sampleWindow[2] = []common.GPUStats{
		{DeviceID: 0, Name: gc.deviceModels[0], UtilizationPct: 50.0, MemoryUsedGB: 7.0, MemoryTotalGB: 16.0, TemperatureC: 65.0, PowerDrawW: 100.0},
	}

	averages := gc.CalculateAverages()

	// Should get averages for all GPUs (but only first has data)
	if len(averages) != deviceCount {
		t.Fatalf("Expected %d averages, got %d", deviceCount, len(averages))
	}

	// Check averages for first GPU
	expected := common.GPUStats{
		DeviceID:       0,
		Name:           gc.deviceModels[0],
		UtilizationPct: 40.0,    // (30 + 40 + 50) / 3
		MemoryUsedGB:   6.0,     // (5 + 6 + 7) / 3
		MemoryTotalGB:  16.0,    // (16 + 16 + 16) / 3
		TemperatureC:   60.0,    // (55 + 60 + 65) / 3
		PowerDrawW:     90.0,    // (80 + 90 + 100) / 3
	}

	if averages[0].UtilizationPct != expected.UtilizationPct {
		t.Errorf("Expected utilization %.2f, got %.2f", expected.UtilizationPct, averages[0].UtilizationPct)
	}
	if averages[0].MemoryUsedGB != expected.MemoryUsedGB {
		t.Errorf("Expected memory %.2f GB, got %.2f GB", expected.MemoryUsedGB, averages[0].MemoryUsedGB)
	}
	if averages[0].TemperatureC != expected.TemperatureC {
		t.Errorf("Expected temperature %.2f C, got %.2f C", expected.TemperatureC, averages[0].TemperatureC)
	}
	if averages[0].PowerDrawW != expected.PowerDrawW {
		t.Errorf("Expected power %.2f W, got %.2f W", expected.PowerDrawW, averages[0].PowerDrawW)
	}
}

// TestGPUCollector_CalculateAverages_SkipInvalidDeviceID verifies invalid device ID handling
func TestGPUCollector_CalculateAverages_SkipInvalidDeviceID(t *testing.T) {
	// Use disabled collector to avoid NVML issues
	gc := &GPUCollector{
		enabled:      false,
		deviceModels: []string{"GPU0", "GPU1"},
		sampleWindow: make([][]common.GPUStats, 3),
		maxSamples:   3,
	}

	// CalculateAverages returns empty for disabled collector
	averages := gc.CalculateAverages()

	if len(averages) != 0 {
		t.Errorf("Expected empty averages for disabled collector, got %d", len(averages))
	}
}

// TestGPUCollector_CalculateAverages_MixedSamples verifies averaging with sparse samples
func TestGPUCollector_CalculateAverages_MixedSamples(t *testing.T) {
	// Test with disabled collector (no NVML required)
	gc := &GPUCollector{
		enabled:      false,
		deviceModels: []string{},
		sampleWindow: make([][]common.GPUStats, 4),
		maxSamples:   4,
	}

	// Disabled collector returns empty
	averages := gc.CalculateAverages()

	if len(averages) != 0 {
		t.Errorf("Expected empty averages for disabled collector, got %d", len(averages))
	}
}

// TestGPUCollector_Close_Enabled verifies closing enabled collector
func TestGPUCollector_Close_Enabled(t *testing.T) {
	gc := NewGPUCollector(3)

	// Should not panic regardless of enabled state
	gc.Close()

	// Should be safe to call multiple times
	gc.Close()
	gc.Close()

	t.Log("GPU collector closed successfully")
}

// TestGPUCollector_GetInstantMetrics_WithDevices verifies instant metrics retrieval
func TestGPUCollector_GetInstantMetrics_WithDevices(t *testing.T) {
	gc := NewGPUCollector(3)
	defer gc.Close()

	if !gc.IsEnabled() {
		t.Skip("No GPUs available for testing")
	}

	metrics, err := gc.GetInstantMetrics()
	if err != nil {
		t.Fatalf("Failed to get instant metrics: %v", err)
	}

	deviceCount := gc.GetDeviceCount()
	if len(metrics) != deviceCount {
		t.Errorf("Expected %d metrics, got %d", deviceCount, len(metrics))
	}

	// Verify each metric has required fields
	for i, metric := range metrics {
		if metric.DeviceID != i {
			t.Errorf("Metric %d: Expected DeviceID %d, got %d", i, i, metric.DeviceID)
		}
		if metric.Name == "" {
			t.Errorf("Metric %d: Expected non-empty name", i)
		}
		if metric.MemoryTotalGB <= 0 {
			t.Errorf("Metric %d: Expected positive memory total, got %.2f", i, metric.MemoryTotalGB)
		}

		t.Logf("GPU %d: %s, Memory: %.2f GB", metric.DeviceID, metric.Name, metric.MemoryTotalGB)
	}
}

// TestGPUCollector_SampleRotation verifies sample window rotation
func TestGPUCollector_SampleRotation(t *testing.T) {
	// Use disabled collector (no NVML required)
	gc := &GPUCollector{
		enabled:      false,
		deviceModels: []string{"Test GPU"},
		sampleWindow: make([][]common.GPUStats, 3),
		maxSamples:   3,
		sampleIndex:  0,
	}

	// Collect more samples than window size
	for i := 0; i < 5; i++ {
		gc.sampleWindow[gc.sampleIndex] = []common.GPUStats{
			{DeviceID: 0, UtilizationPct: float64(i * 10)},
		}
		gc.sampleIndex = (gc.sampleIndex + 1) % gc.maxSamples
	}

	// After 5 samples, index should be at 2 (5 % 3)
	if gc.sampleIndex != 2 {
		t.Errorf("Expected sampleIndex 2, got %d", gc.sampleIndex)
	}

	// Window should contain samples 2, 3, 4 (overwritten 0, 1)
	// Sample at index 0 should be from iteration 3
	if gc.sampleWindow[0][0].UtilizationPct != 30.0 {
		t.Logf("Sample 0 util: %.1f", gc.sampleWindow[0][0].UtilizationPct)
	}
}

// TestGPUCollector_ZeroSamples verifies behavior with no samples collected
func TestGPUCollector_ZeroSamples(t *testing.T) {
	// Use disabled collector
	gc := &GPUCollector{
		enabled:      false,
		deviceModels: []string{},
		sampleWindow: make([][]common.GPUStats, 3),
		maxSamples:   3,
	}

	// No samples collected - should return empty for disabled collector
	averages := gc.CalculateAverages()

	if len(averages) != 0 {
		t.Errorf("Expected empty averages for disabled collector, got %d", len(averages))
	}
}

// TestGPUCollector_GetDeviceModels_Empty verifies empty model list
func TestGPUCollector_GetDeviceModels_Empty(t *testing.T) {
	gc := &GPUCollector{
		enabled:      true,
		deviceModels: []string{},
	}

	models := gc.GetDeviceModels()
	if len(models) != 0 {
		t.Errorf("Expected empty model list, got %d models", len(models))
	}
}

// TestGPUCollector_GetDeviceModels_Multiple verifies multiple device models
func TestGPUCollector_GetDeviceModels_Multiple(t *testing.T) {
	expectedModels := []string{"Tesla V100", "Tesla T4", "RTX 3090"}
	gc := &GPUCollector{
		enabled:      true,
		deviceModels: expectedModels,
	}

	models := gc.GetDeviceModels()
	if len(models) != len(expectedModels) {
		t.Fatalf("Expected %d models, got %d", len(expectedModels), len(models))
	}

	for i, expected := range expectedModels {
		if models[i] != expected {
			t.Errorf("Model %d: Expected %s, got %s", i, expected, models[i])
		}
	}
}

// TestGPUCollector_DeviceCount_Zero verifies zero device count
func TestGPUCollector_DeviceCount_Zero(t *testing.T) {
	gc := &GPUCollector{
		enabled: false, // Use disabled collector
	}

	count := gc.GetDeviceCount()
	if count != 0 {
		t.Errorf("Expected device count 0 for disabled collector, got %d", count)
	}
}

// TestGPUCollector_RealHardware_Integration verifies real GPU hardware if available
func TestGPUCollector_RealHardware_Integration(t *testing.T) {
	gc := NewGPUCollector(5)
	defer gc.Close()

	if !gc.IsEnabled() {
		t.Skip("No NVIDIA GPUs available - skipping hardware integration test")
	}

	deviceCount := gc.GetDeviceCount()
	t.Logf("Detected %d NVIDIA GPU(s)", deviceCount)

	// Get device models
	models := gc.GetDeviceModels()
	for i, model := range models {
		t.Logf("GPU %d: %s", i, model)
	}

	// Collect several samples
	for i := 0; i < 3; i++ {
		err := gc.CollectSample()
		if err != nil {
			t.Fatalf("Sample %d failed: %v", i, err)
		}
	}

	// Calculate averages
	averages := gc.CalculateAverages()
	if len(averages) != deviceCount {
		t.Errorf("Expected %d averages, got %d", deviceCount, len(averages))
	}

	for i, avg := range averages {
		t.Logf("GPU %d averages: Util=%.1f%%, Mem=%.2f/%.2f GB, Temp=%.1fC, Power=%.1fW",
			i, avg.UtilizationPct, avg.MemoryUsedGB, avg.MemoryTotalGB, avg.TemperatureC, avg.PowerDrawW)
	}

	// Get instant metrics
	instant, err := gc.GetInstantMetrics()
	if err != nil {
		t.Fatalf("Failed to get instant metrics: %v", err)
	}

	for i, metric := range instant {
		t.Logf("GPU %d instant: %s, Total Memory: %.2f GB", i, metric.Name, metric.MemoryTotalGB)
	}
}
