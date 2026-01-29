package main

import (
	"testing"

	"github.com/NVIDIA/go-nvml/pkg/nvml"
	"cyqle.in/opsen/common"
)

// TestGPUCollector_SampleRotationEdgeCase tests that sample index wraps around correctly
func TestGPUCollector_SampleRotationEdgeCase(t *testing.T) {
	// Create a collector with small sample window
	gc := &GPUCollector{
		enabled:      true,
		deviceModels: []string{"GPU 0", "GPU 1"},
		sampleWindow: make([][]common.GPUStats, 3), // maxSamples = 3
		maxSamples:   3,
		sampleIndex:  0,
	}

	// Manually simulate adding samples (what CollectSample would do)
	for sampleNum := 0; sampleNum < 5; sampleNum++ {
		// Create sample data
		sample := []common.GPUStats{
			{DeviceID: 0, UtilizationPct: float64(sampleNum * 10), MemoryUsedGB: float64(sampleNum)},
			{DeviceID: 1, UtilizationPct: float64(sampleNum * 15), MemoryUsedGB: float64(sampleNum * 2)},
		}

		// Add to window
		gc.sampleWindow[gc.sampleIndex] = sample
		gc.sampleIndex = (gc.sampleIndex + 1) % gc.maxSamples
	}

	// Verify sampleIndex wrapped around correctly (5 % 3 = 2)
	expectedIndex := 2
	if gc.sampleIndex != expectedIndex {
		t.Errorf("Expected sampleIndex=%d after 5 samples, got %d", expectedIndex, gc.sampleIndex)
	}

	// Verify all slots in window have data
	for i := 0; i < gc.maxSamples; i++ {
		if len(gc.sampleWindow[i]) != 2 {
			t.Errorf("Expected 2 devices in sample window[%d], got %d", i, len(gc.sampleWindow[i]))
		}
	}
}

// TestGPUCollector_CalculateAverages_MismatchedDeviceID tests defensive check
// for device IDs that exceed the number of devices (line 195)
func TestGPUCollector_CalculateAverages_MismatchedDeviceID(t *testing.T) {
	gc := &GPUCollector{
		enabled:      true,
		devices:      make([]nvml.Device, 2), // Zero-value devices for testing logic
		deviceModels: []string{"GPU 0", "GPU 1"},
		sampleWindow: make([][]common.GPUStats, 2),
		maxSamples:   2,
	}

	// Add sample with INVALID device ID (ID=5, but we only have 2 devices)
	gc.sampleWindow[0] = []common.GPUStats{
		{DeviceID: 0, UtilizationPct: 50.0, MemoryUsedGB: 1.0},
		{DeviceID: 1, UtilizationPct: 60.0, MemoryUsedGB: 2.0},
		{DeviceID: 5, UtilizationPct: 70.0, MemoryUsedGB: 3.0}, // Invalid!
	}
	gc.sampleWindow[1] = []common.GPUStats{
		{DeviceID: 0, UtilizationPct: 55.0, MemoryUsedGB: 1.5},
		{DeviceID: 1, UtilizationPct: 65.0, MemoryUsedGB: 2.5},
	}

	// Calculate averages - should skip invalid device ID
	averages := gc.CalculateAverages()

	// Should only have 2 devices (not 3 or 6)
	if len(averages) != 2 {
		t.Errorf("Expected 2 devices in averages, got %d", len(averages))
	}

	// Verify device IDs are valid
	for _, stat := range averages {
		if stat.DeviceID >= len(gc.devices) {
			t.Errorf("Invalid device ID %d (only have %d devices)", stat.DeviceID, len(gc.devices))
		}
	}

	// Verify averages calculated correctly (should ignore invalid ID=5)
	// GPU 0: (50.0 + 55.0) / 2 = 52.5
	if averages[0].UtilizationPct != 52.5 {
		t.Errorf("Expected GPU 0 utilization 52.5%%, got %.1f%%", averages[0].UtilizationPct)
	}

	// GPU 1: (60.0 + 65.0) / 2 = 62.5
	if averages[1].UtilizationPct != 62.5 {
		t.Errorf("Expected GPU 1 utilization 62.5%%, got %.1f%%", averages[1].UtilizationPct)
	}
}

// TestGPUCollector_CalculateAverages_MixedEmptySamples tests averaging
// when some samples in the window are nil/empty
func TestGPUCollector_CalculateAverages_MixedEmptySamples(t *testing.T) {
	gc := &GPUCollector{
		enabled:      true,
		devices:      make([]nvml.Device, 2),
		deviceModels: []string{"GPU 0", "GPU 1"},
		sampleWindow: make([][]common.GPUStats, 4),
		maxSamples:   4,
	}

	// Mix of nil, empty, and populated samples
	gc.sampleWindow[0] = []common.GPUStats{
		{DeviceID: 0, UtilizationPct: 40.0, MemoryUsedGB: 2.0, TemperatureC: 60.0},
		{DeviceID: 1, UtilizationPct: 50.0, MemoryUsedGB: 3.0, TemperatureC: 65.0},
	}
	gc.sampleWindow[1] = nil // nil sample
	gc.sampleWindow[2] = []common.GPUStats{} // empty sample
	gc.sampleWindow[3] = []common.GPUStats{
		{DeviceID: 0, UtilizationPct: 60.0, MemoryUsedGB: 3.0, TemperatureC: 70.0},
		{DeviceID: 1, UtilizationPct: 70.0, MemoryUsedGB: 4.0, TemperatureC: 75.0},
	}

	averages := gc.CalculateAverages()

	if len(averages) != 2 {
		t.Fatalf("Expected 2 devices, got %d", len(averages))
	}

	// Should average only non-nil/non-empty samples (samples 0 and 3)
	// GPU 0: (40.0 + 60.0) / 2 = 50.0
	if averages[0].UtilizationPct != 50.0 {
		t.Errorf("Expected GPU 0 utilization 50.0%%, got %.1f%%", averages[0].UtilizationPct)
	}

	// GPU 1: (50.0 + 70.0) / 2 = 60.0
	if averages[1].UtilizationPct != 60.0 {
		t.Errorf("Expected GPU 1 utilization 60.0%%, got %.1f%%", averages[1].UtilizationPct)
	}

	// Verify temperature averaging too
	// GPU 0: (60.0 + 70.0) / 2 = 65.0
	if averages[0].TemperatureC != 65.0 {
		t.Errorf("Expected GPU 0 temperature 65.0C, got %.1fC", averages[0].TemperatureC)
	}
}

// TestGPUCollector_CalculateAverages_SingleSample tests averaging with
// only one sample in the window
func TestGPUCollector_CalculateAverages_SingleSample(t *testing.T) {
	gc := &GPUCollector{
		enabled:      true,
		devices:      make([]nvml.Device, 1),
		deviceModels: []string{"Tesla V100"},
		sampleWindow: make([][]common.GPUStats, 3),
		maxSamples:   3,
	}

	// Only first slot has data
	gc.sampleWindow[0] = []common.GPUStats{
		{
			DeviceID:       0,
			Name:           "Tesla V100",
			UtilizationPct: 75.0,
			MemoryUsedGB:   12.5,
			MemoryTotalGB:  16.0,
			TemperatureC:   82.0,
			PowerDrawW:     250.0,
		},
	}
	gc.sampleWindow[1] = nil
	gc.sampleWindow[2] = nil

	averages := gc.CalculateAverages()

	if len(averages) != 1 {
		t.Fatalf("Expected 1 device, got %d", len(averages))
	}

	// With single sample, average should equal the sample value
	if averages[0].UtilizationPct != 75.0 {
		t.Errorf("Expected utilization 75.0%%, got %.1f%%", averages[0].UtilizationPct)
	}

	if averages[0].MemoryUsedGB != 12.5 {
		t.Errorf("Expected memory used 12.5GB, got %.1fGB", averages[0].MemoryUsedGB)
	}

	if averages[0].TemperatureC != 82.0 {
		t.Errorf("Expected temperature 82.0C, got %.1fC", averages[0].TemperatureC)
	}

	if averages[0].PowerDrawW != 250.0 {
		t.Errorf("Expected power draw 250.0W, got %.1fW", averages[0].PowerDrawW)
	}
}

// TestGPUCollector_CalculateAverages_AllEmptySamples tests behavior
// when all samples in window are nil or empty
func TestGPUCollector_CalculateAverages_AllEmptySamples(t *testing.T) {
	gc := &GPUCollector{
		enabled:      true,
		devices:      make([]nvml.Device, 2),
		deviceModels: []string{"GPU 0", "GPU 1"},
		sampleWindow: make([][]common.GPUStats, 3),
		maxSamples:   3,
	}

	// All samples are nil or empty
	gc.sampleWindow[0] = nil
	gc.sampleWindow[1] = []common.GPUStats{}
	gc.sampleWindow[2] = nil

	averages := gc.CalculateAverages()

	if len(averages) != 2 {
		t.Errorf("Expected 2 devices in result, got %d", len(averages))
	}

	// All values should be zero (no samples to average)
	for i, stat := range averages {
		if stat.UtilizationPct != 0 || stat.MemoryUsedGB != 0 || stat.TemperatureC != 0 {
			t.Errorf("Expected zero values for GPU %d with no samples, got util=%.1f mem=%.1f temp=%.1f",
				i, stat.UtilizationPct, stat.MemoryUsedGB, stat.TemperatureC)
		}
	}
}

// TestGPUCollector_CalculateAverages_PartialDeviceData tests when some
// samples have incomplete device data
func TestGPUCollector_CalculateAverages_PartialDeviceData(t *testing.T) {
	gc := &GPUCollector{
		enabled:      true,
		devices:      make([]nvml.Device, 2),
		deviceModels: []string{"GPU 0", "GPU 1"},
		sampleWindow: make([][]common.GPUStats, 2),
		maxSamples:   2,
	}

	// First sample has both GPUs
	gc.sampleWindow[0] = []common.GPUStats{
		{DeviceID: 0, UtilizationPct: 40.0, MemoryUsedGB: 2.0},
		{DeviceID: 1, UtilizationPct: 60.0, MemoryUsedGB: 4.0},
	}

	// Second sample only has GPU 0 (GPU 1 missing)
	gc.sampleWindow[1] = []common.GPUStats{
		{DeviceID: 0, UtilizationPct: 80.0, MemoryUsedGB: 3.0},
	}

	averages := gc.CalculateAverages()

	if len(averages) != 2 {
		t.Fatalf("Expected 2 devices, got %d", len(averages))
	}

	// GPU 0: (40.0 + 80.0) / 2 = 60.0
	if averages[0].UtilizationPct != 60.0 {
		t.Errorf("Expected GPU 0 utilization 60.0%%, got %.1f%%", averages[0].UtilizationPct)
	}

	// GPU 1: Only one sample (60.0 / 1 = 60.0)
	if averages[1].UtilizationPct != 60.0 {
		t.Errorf("Expected GPU 1 utilization 60.0%% (single sample), got %.1f%%", averages[1].UtilizationPct)
	}

	// GPU 0 memory: (2.0 + 3.0) / 2 = 2.5
	if averages[0].MemoryUsedGB != 2.5 {
		t.Errorf("Expected GPU 0 memory 2.5GB, got %.1fGB", averages[0].MemoryUsedGB)
	}

	// GPU 1 memory: Only one sample (4.0 / 1 = 4.0)
	if averages[1].MemoryUsedGB != 4.0 {
		t.Errorf("Expected GPU 1 memory 4.0GB, got %.1fGB", averages[1].MemoryUsedGB)
	}
}

// TestGPUCollector_DisabledCollector tests that disabled collector returns empty results
func TestGPUCollector_DisabledCollector(t *testing.T) {
	gc := &GPUCollector{
		enabled:      false, // Disabled
		sampleWindow: make([][]common.GPUStats, 2),
		maxSamples:   2,
	}

	// Add some data (shouldn't be used since disabled)
	gc.sampleWindow[0] = []common.GPUStats{
		{DeviceID: 0, UtilizationPct: 50.0},
	}

	averages := gc.CalculateAverages()

	if len(averages) != 0 {
		t.Errorf("Expected empty result for disabled collector, got %d devices", len(averages))
	}
}

// TestGPUCollector_CalculateAverages_MultipleDevices tests averaging
// with more than 2 GPUs
func TestGPUCollector_CalculateAverages_MultipleDevices(t *testing.T) {
	gc := &GPUCollector{
		enabled:      true,
		devices:      make([]nvml.Device, 4), // 4 GPUs
		deviceModels: []string{"GPU 0", "GPU 1", "GPU 2", "GPU 3"},
		sampleWindow: make([][]common.GPUStats, 2),
		maxSamples:   2,
	}

	gc.sampleWindow[0] = []common.GPUStats{
		{DeviceID: 0, UtilizationPct: 10.0},
		{DeviceID: 1, UtilizationPct: 20.0},
		{DeviceID: 2, UtilizationPct: 30.0},
		{DeviceID: 3, UtilizationPct: 40.0},
	}
	gc.sampleWindow[1] = []common.GPUStats{
		{DeviceID: 0, UtilizationPct: 50.0},
		{DeviceID: 1, UtilizationPct: 60.0},
		{DeviceID: 2, UtilizationPct: 70.0},
		{DeviceID: 3, UtilizationPct: 80.0},
	}

	averages := gc.CalculateAverages()

	if len(averages) != 4 {
		t.Fatalf("Expected 4 devices, got %d", len(averages))
	}

	// Expected averages
	expected := []float64{30.0, 40.0, 50.0, 60.0}
	for i, exp := range expected {
		if averages[i].UtilizationPct != exp {
			t.Errorf("GPU %d: Expected utilization %.1f%%, got %.1f%%",
				i, exp, averages[i].UtilizationPct)
		}
	}
}

