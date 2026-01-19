package main

import (
	"fmt"
	"log"

	"github.com/NVIDIA/go-nvml/pkg/nvml"
	"cyqle.in/opsen/common"
)

// GPUCollector manages GPU metrics collection
type GPUCollector struct {
	enabled      bool
	devices      []nvml.Device
	deviceModels []string
	sampleWindow [][]common.GPUStats // [sample_index][device_index]
	sampleIndex  int
	maxSamples   int
}

// NewGPUCollector initializes GPU monitoring with graceful degradation
// Returns a disabled collector if GPUs are not available or NVML fails to initialize
func NewGPUCollector(samplesPerWindow int) *GPUCollector {
	collector := &GPUCollector{
		enabled:      false,
		sampleWindow: make([][]common.GPUStats, samplesPerWindow),
		maxSamples:   samplesPerWindow,
	}

	// Attempt to initialize NVML
	ret := nvml.Init()
	if ret != nvml.SUCCESS {
		log.Printf("GPU monitoring disabled: NVML init failed (%v). This is normal if no NVIDIA GPU is present.", nvml.ErrorString(ret))
		return collector
	}

	// Get GPU device count
	count, ret := nvml.DeviceGetCount()
	if ret != nvml.SUCCESS {
		log.Printf("GPU monitoring disabled: Failed to get device count (%v)", nvml.ErrorString(ret))
		if shutdownRet := nvml.Shutdown(); shutdownRet != nvml.SUCCESS {
			log.Printf("Warning: NVML shutdown failed: %v", nvml.ErrorString(shutdownRet))
		}
		return collector
	}

	if count == 0 {
		log.Printf("GPU monitoring disabled: No NVIDIA GPUs detected")
		if shutdownRet := nvml.Shutdown(); shutdownRet != nvml.SUCCESS {
			log.Printf("Warning: NVML shutdown failed: %v", nvml.ErrorString(shutdownRet))
		}
		return collector
	}

	// Initialize device handles
	devices := make([]nvml.Device, 0, count)
	deviceModels := make([]string, 0, count)

	for i := 0; i < count; i++ {
		device, ret := nvml.DeviceGetHandleByIndex(i)
		if ret != nvml.SUCCESS {
			log.Printf("Warning: Failed to get GPU %d handle (%v), skipping", i, nvml.ErrorString(ret))
			continue
		}

		// Get device name
		name, ret := device.GetName()
		if ret != nvml.SUCCESS {
			name = "Unknown GPU"
		}

		devices = append(devices, device)
		deviceModels = append(deviceModels, name)
		log.Printf("GPU %d detected: %s", i, name)
	}

	if len(devices) == 0 {
		log.Printf("GPU monitoring disabled: No usable NVIDIA GPUs found")
		if shutdownRet := nvml.Shutdown(); shutdownRet != nvml.SUCCESS {
			log.Printf("Warning: NVML shutdown failed: %v", nvml.ErrorString(shutdownRet))
		}
		return collector
	}

	collector.enabled = true
	collector.devices = devices
	collector.deviceModels = deviceModels

	log.Printf("GPU monitoring enabled: %d NVIDIA GPU(s) detected", len(devices))
	return collector
}

// IsEnabled returns whether GPU monitoring is active
func (gc *GPUCollector) IsEnabled() bool {
	return gc.enabled
}

// GetDeviceCount returns the number of GPUs being monitored
func (gc *GPUCollector) GetDeviceCount() int {
	if !gc.enabled {
		return 0
	}
	return len(gc.devices)
}

// GetDeviceModels returns the GPU model names
func (gc *GPUCollector) GetDeviceModels() []string {
	if !gc.enabled {
		return []string{}
	}
	return gc.deviceModels
}

// CollectSample collects current GPU metrics and stores in sample window
func (gc *GPUCollector) CollectSample() error {
	if !gc.enabled {
		return nil
	}

	stats := make([]common.GPUStats, 0, len(gc.devices))

	for i, device := range gc.devices {
		// Get utilization rates
		util, ret := device.GetUtilizationRates()
		gpuUtil := 0.0
		if ret == nvml.SUCCESS {
			gpuUtil = float64(util.Gpu)
		}

		// Get memory info
		memInfo, ret := device.GetMemoryInfo()
		memUsedGB := 0.0
		memTotalGB := 0.0
		if ret == nvml.SUCCESS {
			memUsedGB = float64(memInfo.Used) / 1024 / 1024 / 1024
			memTotalGB = float64(memInfo.Total) / 1024 / 1024 / 1024
		}

		// Get temperature
		temp, ret := device.GetTemperature(nvml.TEMPERATURE_GPU)
		tempC := 0.0
		if ret == nvml.SUCCESS {
			tempC = float64(temp)
		}

		// Get power draw (optional, may not be supported on all GPUs)
		power, ret := device.GetPowerUsage()
		powerW := 0.0
		if ret == nvml.SUCCESS {
			powerW = float64(power) / 1000.0 // Convert milliwatts to watts
		}

		stats = append(stats, common.GPUStats{
			DeviceID:       i,
			Name:           gc.deviceModels[i],
			UtilizationPct: gpuUtil,
			MemoryUsedGB:   memUsedGB,
			MemoryTotalGB:  memTotalGB,
			TemperatureC:   tempC,
			PowerDrawW:     powerW,
		})
	}

	gc.sampleWindow[gc.sampleIndex] = stats
	gc.sampleIndex = (gc.sampleIndex + 1) % gc.maxSamples

	return nil
}

// CalculateAverages computes averaged GPU metrics over the sample window
func (gc *GPUCollector) CalculateAverages() []common.GPUStats {
	if !gc.enabled || len(gc.devices) == 0 {
		return []common.GPUStats{}
	}

	numDevices := len(gc.devices)
	averages := make([]common.GPUStats, numDevices)
	counts := make([]int, numDevices)

	// Initialize with device info
	for i := 0; i < numDevices; i++ {
		averages[i] = common.GPUStats{
			DeviceID: i,
			Name:     gc.deviceModels[i],
		}
	}

	// Sum all samples
	for _, sample := range gc.sampleWindow {
		if len(sample) == 0 {
			continue
		}

		for _, gpuStat := range sample {
			if gpuStat.DeviceID < numDevices {
				idx := gpuStat.DeviceID
				averages[idx].UtilizationPct += gpuStat.UtilizationPct
				averages[idx].MemoryUsedGB += gpuStat.MemoryUsedGB
				averages[idx].MemoryTotalGB += gpuStat.MemoryTotalGB
				averages[idx].TemperatureC += gpuStat.TemperatureC
				averages[idx].PowerDrawW += gpuStat.PowerDrawW
				counts[idx]++
			}
		}
	}

	// Calculate averages
	for i := 0; i < numDevices; i++ {
		if counts[i] > 0 {
			averages[i].UtilizationPct /= float64(counts[i])
			averages[i].MemoryUsedGB /= float64(counts[i])
			averages[i].MemoryTotalGB /= float64(counts[i])
			averages[i].TemperatureC /= float64(counts[i])
			averages[i].PowerDrawW /= float64(counts[i])
		}
	}

	return averages
}

// Close shuts down NVML gracefully
func (gc *GPUCollector) Close() {
	if gc.enabled {
		ret := nvml.Shutdown()
		if ret != nvml.SUCCESS {
			log.Printf("Warning: NVML shutdown returned error: %v", nvml.ErrorString(ret))
		} else {
			log.Printf("GPU monitoring shut down successfully")
		}
	}
}

// GetInstantMetrics returns current GPU metrics without averaging (for registration)
func (gc *GPUCollector) GetInstantMetrics() ([]common.GPUStats, error) {
	if !gc.enabled {
		return []common.GPUStats{}, nil
	}

	stats := make([]common.GPUStats, 0, len(gc.devices))

	for i, device := range gc.devices {
		// Get memory info for total VRAM
		memInfo, ret := device.GetMemoryInfo()
		if ret != nvml.SUCCESS {
			return nil, fmt.Errorf("failed to get memory info for GPU %d: %v", i, nvml.ErrorString(ret))
		}

		stats = append(stats, common.GPUStats{
			DeviceID:      i,
			Name:          gc.deviceModels[i],
			MemoryTotalGB: float64(memInfo.Total) / 1024 / 1024 / 1024,
		})
	}

	return stats, nil
}
