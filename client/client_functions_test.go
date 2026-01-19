package main

import (
	"testing"
)

// TestCalculateCPUAverages verifies CPU averaging across cores
func TestCalculateCPUAverages(t *testing.T) {
	collector := &MetricsCollector{
		maxSamples: 3,
		cpuSamples: make([][]float64, 3),
	}

	// Add CPU samples
	collector.cpuSamples[0] = []float64{10.0, 20.0, 30.0, 40.0}
	collector.cpuSamples[1] = []float64{15.0, 25.0, 35.0, 45.0}
	collector.cpuSamples[2] = []float64{20.0, 30.0, 40.0, 50.0}
	collector.sampleIndex = 3

	averages := collector.calculateCPUAverages()

	if len(averages) != 4 {
		t.Fatalf("Expected 4 core averages, got %d", len(averages))
	}

	// Core 0: (10 + 15 + 20) / 3 = 15
	if averages[0] != 15.0 {
		t.Errorf("Core 0: expected 15.0, got %.2f", averages[0])
	}

	// Core 1: (20 + 25 + 30) / 3 = 25
	if averages[1] != 25.0 {
		t.Errorf("Core 1: expected 25.0, got %.2f", averages[1])
	}

	// Core 2: (30 + 35 + 40) / 3 = 35
	if averages[2] != 35.0 {
		t.Errorf("Core 2: expected 35.0, got %.2f", averages[2])
	}

	// Core 3: (40 + 45 + 50) / 3 = 45
	if averages[3] != 45.0 {
		t.Errorf("Core 3: expected 45.0, got %.2f", averages[3])
	}
}

// TestCalculateCPUAverages_EmptySamples verifies handling of empty samples
func TestCalculateCPUAverages_EmptySamples(t *testing.T) {
	collector := &MetricsCollector{
		maxSamples: 3,
		cpuSamples: make([][]float64, 3),
		sampleIndex: 0,
	}

	averages := collector.calculateCPUAverages()

	if len(averages) != 0 {
		t.Errorf("Expected empty averages, got %d cores", len(averages))
	}
}

// TestCalculateCPUAverages_PartialSamples verifies handling of partial samples
func TestCalculateCPUAverages_PartialSamples(t *testing.T) {
	collector := &MetricsCollector{
		maxSamples: 3,
		cpuSamples: make([][]float64, 3),
	}

	// Only first sample has data
	collector.cpuSamples[0] = []float64{30.0, 40.0}
	collector.sampleIndex = 1

	averages := collector.calculateCPUAverages()

	if len(averages) != 2 {
		t.Fatalf("Expected 2 core averages, got %d", len(averages))
	}

	if averages[0] != 30.0 {
		t.Errorf("Core 0: expected 30.0, got %.2f", averages[0])
	}

	if averages[1] != 40.0 {
		t.Errorf("Core 1: expected 40.0, got %.2f", averages[1])
	}
}

// TestCalculateCPUAverages_VariableCoreCounts verifies averaging with different core counts
// The function uses the FIRST non-empty sample's core count
func TestCalculateCPUAverages_VariableCoreCounts(t *testing.T) {
	collector := &MetricsCollector{
		maxSamples: 3,
		cpuSamples: make([][]float64, 3),
	}

	// Different number of cores per sample (uses first sample's count)
	collector.cpuSamples[0] = []float64{10.0, 20.0}
	collector.cpuSamples[1] = []float64{15.0, 25.0, 35.0}
	collector.cpuSamples[2] = []float64{20.0, 30.0}
	collector.sampleIndex = 3

	averages := collector.calculateCPUAverages()

	// Should have 2 cores (from first sample)
	if len(averages) != 2 {
		t.Fatalf("Expected 2 core averages, got %d", len(averages))
	}

	// Core 0: (10 + 15 + 20) / 3 = 15
	if averages[0] != 15.0 {
		t.Errorf("Core 0: expected 15.0, got %.2f", averages[0])
	}

	// Core 1: (20 + 25 + 30) / 3 = 25
	if averages[1] != 25.0 {
		t.Errorf("Core 1: expected 25.0, got %.2f", averages[1])
	}
}
