package main

import (
	"testing"

	"github.com/NVIDIA/go-nvml/pkg/nvml"
)

// fakeDevice implements the handful of nvml.Device methods the collector calls.
// The embedded interface satisfies the remaining (unused) methods; calling any of
// them would panic, which is exactly the signal we want if the collector grows a
// new NVML dependency without a matching fake.
type fakeDevice struct {
	nvml.Device

	name    string
	nameRet nvml.Return

	util    nvml.Utilization
	utilRet nvml.Return

	mem    nvml.Memory
	memRet nvml.Return

	temp    uint32
	tempRet nvml.Return

	power    uint32
	powerRet nvml.Return
}

func (d *fakeDevice) GetName() (string, nvml.Return) { return d.name, d.nameRet }

func (d *fakeDevice) GetUtilizationRates() (nvml.Utilization, nvml.Return) {
	return d.util, d.utilRet
}

func (d *fakeDevice) GetMemoryInfo() (nvml.Memory, nvml.Return) { return d.mem, d.memRet }

func (d *fakeDevice) GetTemperature(nvml.TemperatureSensors) (uint32, nvml.Return) {
	return d.temp, d.tempRet
}

func (d *fakeDevice) GetPowerUsage() (uint32, nvml.Return) { return d.power, d.powerRet }

// newFakeDevice returns a device where every metric read succeeds.
func newFakeDevice(name string) *fakeDevice {
	return &fakeDevice{
		name:     name,
		nameRet:  nvml.SUCCESS,
		util:     nvml.Utilization{Gpu: 50, Memory: 25},
		utilRet:  nvml.SUCCESS,
		mem:      nvml.Memory{Total: 8 * gib, Used: 2 * gib, Free: 6 * gib},
		memRet:   nvml.SUCCESS,
		temp:     65,
		tempRet:  nvml.SUCCESS,
		power:    150000, // milliwatts
		powerRet: nvml.SUCCESS,
	}
}

const gib = 1024 * 1024 * 1024

// nvmlStub describes the fake NVML library behaviour for a single test.
type nvmlStub struct {
	initRet     nvml.Return
	count       int
	countRet    nvml.Return
	devices     []*fakeDevice
	handleRets  []nvml.Return
	shutdownRet nvml.Return

	initCalls     int
	shutdownCalls int
}

// install swaps the package-level NVML hooks for the stub and restores them
// when the test finishes.
func (s *nvmlStub) install(t *testing.T) {
	t.Helper()

	origInit, origShutdown := nvmlInit, nvmlShutdown
	origCount, origHandle := nvmlDeviceGetCount, nvmlDeviceGetHandleByIndex
	t.Cleanup(func() {
		nvmlInit, nvmlShutdown = origInit, origShutdown
		nvmlDeviceGetCount, nvmlDeviceGetHandleByIndex = origCount, origHandle
	})

	nvmlInit = func() nvml.Return {
		s.initCalls++
		return s.initRet
	}
	nvmlShutdown = func() nvml.Return {
		s.shutdownCalls++
		return s.shutdownRet
	}
	nvmlDeviceGetCount = func() (int, nvml.Return) {
		return s.count, s.countRet
	}
	nvmlDeviceGetHandleByIndex = func(i int) (nvml.Device, nvml.Return) {
		ret := nvml.SUCCESS
		if i < len(s.handleRets) {
			ret = s.handleRets[i]
		}
		if ret != nvml.SUCCESS {
			return nil, ret
		}
		if i >= len(s.devices) {
			return nil, nvml.ERROR_INVALID_ARGUMENT
		}
		return s.devices[i], nvml.SUCCESS
	}
}

// stubWithDevices builds a fully working stub exposing the named devices.
func stubWithDevices(names ...string) *nvmlStub {
	stub := &nvmlStub{
		initRet:     nvml.SUCCESS,
		count:       len(names),
		countRet:    nvml.SUCCESS,
		shutdownRet: nvml.SUCCESS,
	}
	for _, name := range names {
		stub.devices = append(stub.devices, newFakeDevice(name))
	}
	return stub
}

func TestNewGPUCollector_InitFailureDisablesMonitoring(t *testing.T) {
	stub := &nvmlStub{initRet: nvml.ERROR_LIBRARY_NOT_FOUND}
	stub.install(t)

	gc := NewGPUCollector(5)

	if gc.IsEnabled() {
		t.Error("collector should be disabled when NVML init fails")
	}
	if stub.shutdownCalls != 0 {
		t.Errorf("shutdown should not be called when init failed, got %d calls", stub.shutdownCalls)
	}
	if gc.GetDeviceCount() != 0 {
		t.Errorf("expected 0 devices, got %d", gc.GetDeviceCount())
	}
	if len(gc.GetDeviceModels()) != 0 {
		t.Errorf("expected no device models, got %v", gc.GetDeviceModels())
	}
}

func TestNewGPUCollector_DeviceCountFailure(t *testing.T) {
	stub := &nvmlStub{
		initRet:     nvml.SUCCESS,
		countRet:    nvml.ERROR_UNKNOWN,
		shutdownRet: nvml.SUCCESS,
	}
	stub.install(t)

	gc := NewGPUCollector(5)

	if gc.IsEnabled() {
		t.Error("collector should be disabled when device count fails")
	}
	if stub.shutdownCalls != 1 {
		t.Errorf("expected NVML shutdown after count failure, got %d calls", stub.shutdownCalls)
	}
}

func TestNewGPUCollector_DeviceCountFailureWithFailingShutdown(t *testing.T) {
	stub := &nvmlStub{
		initRet:     nvml.SUCCESS,
		countRet:    nvml.ERROR_UNKNOWN,
		shutdownRet: nvml.ERROR_UNINITIALIZED,
	}
	stub.install(t)

	gc := NewGPUCollector(5)

	if gc.IsEnabled() {
		t.Error("collector should be disabled when device count fails")
	}
	if stub.shutdownCalls != 1 {
		t.Errorf("expected one shutdown attempt, got %d", stub.shutdownCalls)
	}
}

func TestNewGPUCollector_ZeroDevices(t *testing.T) {
	stub := &nvmlStub{
		initRet:     nvml.SUCCESS,
		count:       0,
		countRet:    nvml.SUCCESS,
		shutdownRet: nvml.ERROR_UNINITIALIZED,
	}
	stub.install(t)

	gc := NewGPUCollector(5)

	if gc.IsEnabled() {
		t.Error("collector should be disabled when no GPUs are present")
	}
	if stub.shutdownCalls != 1 {
		t.Errorf("expected NVML shutdown when no GPUs found, got %d calls", stub.shutdownCalls)
	}
}

func TestNewGPUCollector_AllHandlesFail(t *testing.T) {
	stub := stubWithDevices("GPU-A", "GPU-B")
	stub.handleRets = []nvml.Return{nvml.ERROR_UNKNOWN, nvml.ERROR_UNKNOWN}
	stub.install(t)

	gc := NewGPUCollector(5)

	if gc.IsEnabled() {
		t.Error("collector should be disabled when no device handle can be acquired")
	}
	if stub.shutdownCalls != 1 {
		t.Errorf("expected NVML shutdown when no usable GPUs, got %d calls", stub.shutdownCalls)
	}
}

func TestNewGPUCollector_SkipsUnusableDevice(t *testing.T) {
	stub := stubWithDevices("GPU-A", "GPU-B", "GPU-C")
	// Middle device cannot be opened and must be skipped without aborting.
	stub.handleRets = []nvml.Return{nvml.SUCCESS, nvml.ERROR_UNKNOWN, nvml.SUCCESS}
	stub.install(t)

	gc := NewGPUCollector(5)

	if !gc.IsEnabled() {
		t.Fatal("collector should be enabled when at least one GPU is usable")
	}
	if got := gc.GetDeviceCount(); got != 2 {
		t.Errorf("expected 2 usable devices, got %d", got)
	}
	models := gc.GetDeviceModels()
	if len(models) != 2 || models[0] != "GPU-A" || models[1] != "GPU-C" {
		t.Errorf("expected [GPU-A GPU-C], got %v", models)
	}
}

func TestNewGPUCollector_UnknownDeviceName(t *testing.T) {
	stub := stubWithDevices("ignored")
	stub.devices[0].nameRet = nvml.ERROR_NOT_SUPPORTED
	stub.install(t)

	gc := NewGPUCollector(5)

	if !gc.IsEnabled() {
		t.Fatal("collector should still enable when the device name is unavailable")
	}
	if models := gc.GetDeviceModels(); len(models) != 1 || models[0] != "Unknown GPU" {
		t.Errorf("expected [Unknown GPU], got %v", models)
	}
}

func TestGPUCollector_CollectSampleWithDevices(t *testing.T) {
	stub := stubWithDevices("GPU-0", "GPU-1")
	stub.install(t)

	gc := NewGPUCollector(3)
	if !gc.IsEnabled() {
		t.Fatal("expected enabled collector")
	}

	if err := gc.CollectSample(); err != nil {
		t.Fatalf("CollectSample returned error: %v", err)
	}

	sample := gc.sampleWindow[0]
	if len(sample) != 2 {
		t.Fatalf("expected 2 device samples, got %d", len(sample))
	}
	if sample[0].UtilizationPct != 50 {
		t.Errorf("expected 50%% utilization, got %.1f", sample[0].UtilizationPct)
	}
	if sample[0].MemoryTotalGB != 8 {
		t.Errorf("expected 8GB total memory, got %.2f", sample[0].MemoryTotalGB)
	}
	if sample[0].MemoryUsedGB != 2 {
		t.Errorf("expected 2GB used memory, got %.2f", sample[0].MemoryUsedGB)
	}
	if sample[0].TemperatureC != 65 {
		t.Errorf("expected 65C, got %.1f", sample[0].TemperatureC)
	}
	if sample[0].PowerDrawW != 150 {
		t.Errorf("expected 150W, got %.1f", sample[0].PowerDrawW)
	}
	if sample[1].Name != "GPU-1" {
		t.Errorf("expected second device name GPU-1, got %s", sample[1].Name)
	}
	if gc.sampleIndex != 1 {
		t.Errorf("expected sample index to advance to 1, got %d", gc.sampleIndex)
	}
}

func TestGPUCollector_CollectSampleUnsupportedMetrics(t *testing.T) {
	stub := stubWithDevices("GPU-0")
	dev := stub.devices[0]
	dev.utilRet = nvml.ERROR_NOT_SUPPORTED
	dev.memRet = nvml.ERROR_NOT_SUPPORTED
	dev.tempRet = nvml.ERROR_NOT_SUPPORTED
	dev.powerRet = nvml.ERROR_NOT_SUPPORTED
	stub.install(t)

	gc := NewGPUCollector(2)
	if err := gc.CollectSample(); err != nil {
		t.Fatalf("CollectSample returned error: %v", err)
	}

	sample := gc.sampleWindow[0]
	if len(sample) != 1 {
		t.Fatalf("expected 1 device sample, got %d", len(sample))
	}
	got := sample[0]
	if got.UtilizationPct != 0 || got.MemoryUsedGB != 0 || got.MemoryTotalGB != 0 ||
		got.TemperatureC != 0 || got.PowerDrawW != 0 {
		t.Errorf("unsupported metrics should default to zero, got %+v", got)
	}
}

func TestGPUCollector_CollectSampleWrapsAroundWindow(t *testing.T) {
	stub := stubWithDevices("GPU-0")
	stub.install(t)

	gc := NewGPUCollector(2)
	for i := 0; i < 5; i++ {
		if err := gc.CollectSample(); err != nil {
			t.Fatalf("CollectSample %d returned error: %v", i, err)
		}
	}

	if gc.sampleIndex != 1 {
		t.Errorf("expected sample index 1 after 5 samples over a window of 2, got %d", gc.sampleIndex)
	}
}

func TestGPUCollector_CalculateAveragesWithDevices(t *testing.T) {
	stub := stubWithDevices("GPU-0")
	stub.install(t)

	gc := NewGPUCollector(4)

	// Two samples with different utilization values: 50 then 100 -> average 75.
	if err := gc.CollectSample(); err != nil {
		t.Fatalf("CollectSample returned error: %v", err)
	}
	stub.devices[0].util = nvml.Utilization{Gpu: 100}
	if err := gc.CollectSample(); err != nil {
		t.Fatalf("CollectSample returned error: %v", err)
	}

	averages := gc.CalculateAverages()
	if len(averages) != 1 {
		t.Fatalf("expected averages for 1 device, got %d", len(averages))
	}
	if averages[0].UtilizationPct != 75 {
		t.Errorf("expected 75%% average utilization, got %.1f", averages[0].UtilizationPct)
	}
	if averages[0].Name != "GPU-0" {
		t.Errorf("expected name GPU-0, got %s", averages[0].Name)
	}
	if averages[0].MemoryTotalGB != 8 {
		t.Errorf("expected 8GB average total memory, got %.2f", averages[0].MemoryTotalGB)
	}
}

func TestGPUCollector_GetInstantMetricsWithDevices(t *testing.T) {
	stub := stubWithDevices("GPU-0", "GPU-1")
	stub.devices[1].mem = nvml.Memory{Total: 16 * gib}
	stub.install(t)

	gc := NewGPUCollector(2)

	stats, err := gc.GetInstantMetrics()
	if err != nil {
		t.Fatalf("GetInstantMetrics returned error: %v", err)
	}
	if len(stats) != 2 {
		t.Fatalf("expected 2 device stats, got %d", len(stats))
	}
	if stats[0].MemoryTotalGB != 8 {
		t.Errorf("expected 8GB for GPU-0, got %.2f", stats[0].MemoryTotalGB)
	}
	if stats[1].MemoryTotalGB != 16 {
		t.Errorf("expected 16GB for GPU-1, got %.2f", stats[1].MemoryTotalGB)
	}
	if stats[1].DeviceID != 1 {
		t.Errorf("expected device id 1, got %d", stats[1].DeviceID)
	}
}

func TestGPUCollector_GetInstantMetricsMemoryError(t *testing.T) {
	stub := stubWithDevices("GPU-0")
	stub.install(t)

	gc := NewGPUCollector(2)
	stub.devices[0].memRet = nvml.ERROR_UNKNOWN

	stats, err := gc.GetInstantMetrics()
	if err == nil {
		t.Fatal("expected an error when memory info cannot be read")
	}
	if stats != nil {
		t.Errorf("expected nil stats on error, got %+v", stats)
	}
}

func TestGPUCollector_CloseShutsDownNVML(t *testing.T) {
	stub := stubWithDevices("GPU-0")
	stub.install(t)

	gc := NewGPUCollector(2)
	before := stub.shutdownCalls

	gc.Close()

	if stub.shutdownCalls != before+1 {
		t.Errorf("expected Close to shut down NVML, calls went %d -> %d", before, stub.shutdownCalls)
	}
}

func TestGPUCollector_CloseReportsShutdownFailure(t *testing.T) {
	stub := stubWithDevices("GPU-0")
	stub.install(t)

	gc := NewGPUCollector(2)
	stub.shutdownRet = nvml.ERROR_UNINITIALIZED

	gc.Close() // must not panic even though NVML reports a failure

	if stub.shutdownCalls != 1 {
		t.Errorf("expected exactly one shutdown call, got %d", stub.shutdownCalls)
	}
}

func TestGPUCollector_DisabledCollectorIsInert(t *testing.T) {
	stub := &nvmlStub{initRet: nvml.ERROR_LIBRARY_NOT_FOUND}
	stub.install(t)

	gc := NewGPUCollector(2)

	if err := gc.CollectSample(); err != nil {
		t.Errorf("CollectSample on disabled collector should be a no-op, got %v", err)
	}
	if got := gc.CalculateAverages(); len(got) != 0 {
		t.Errorf("expected no averages from disabled collector, got %v", got)
	}
	stats, err := gc.GetInstantMetrics()
	if err != nil || len(stats) != 0 {
		t.Errorf("expected empty instant metrics from disabled collector, got %v / %v", stats, err)
	}
	gc.Close() // no shutdown expected
	if stub.shutdownCalls != 0 {
		t.Errorf("disabled collector should not shut down NVML, got %d calls", stub.shutdownCalls)
	}
}
