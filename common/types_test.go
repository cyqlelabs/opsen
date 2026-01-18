package common

import (
	"encoding/json"
	"testing"
	"time"
)

// TestTierSpec_JSON verifies TierSpec serialization
func TestTierSpec_JSON(t *testing.T) {
	tier := TierSpec{
		Name:      "pro-standard",
		VCPU:      2,
		MemoryGB:  4.0,
		StorageGB: 20,
	}

	// Marshal to JSON
	data, err := json.Marshal(tier)
	if err != nil {
		t.Fatalf("Failed to marshal TierSpec: %v", err)
	}

	// Unmarshal back
	var decoded TierSpec
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Failed to unmarshal TierSpec: %v", err)
	}

	// Verify fields
	if decoded.Name != tier.Name {
		t.Errorf("Name mismatch: expected %s, got %s", tier.Name, decoded.Name)
	}
	if decoded.VCPU != tier.VCPU {
		t.Errorf("VCPU mismatch: expected %d, got %d", tier.VCPU, decoded.VCPU)
	}
	if decoded.MemoryGB != tier.MemoryGB {
		t.Errorf("MemoryGB mismatch: expected %.1f, got %.1f", tier.MemoryGB, decoded.MemoryGB)
	}
	if decoded.StorageGB != tier.StorageGB {
		t.Errorf("StorageGB mismatch: expected %d, got %d", tier.StorageGB, decoded.StorageGB)
	}
}

// TestResourceStats_JSON verifies ResourceStats serialization
func TestResourceStats_JSON(t *testing.T) {
	stats := ResourceStats{
		ClientID:    "test-client-1",
		Hostname:    "backend-1",
		Timestamp:   time.Now(),
		CPUCores:    8,
		CPUUsageAvg: []float64{10.5, 20.3, 30.1, 40.7, 50.2, 60.8, 70.4, 80.9},
		MemoryTotal: 32.0,
		MemoryUsed:  16.5,
		MemoryAvail: 15.5,
		DiskTotal:   500.0,
		DiskUsed:    250.0,
		DiskAvail:   250.0,
		PublicIP:    "1.2.3.4",
		Latitude:    40.7128,
		Longitude:   -74.0060,
		Country:     "US",
		City:        "New York",
	}

	// Marshal to JSON
	data, err := json.Marshal(stats)
	if err != nil {
		t.Fatalf("Failed to marshal ResourceStats: %v", err)
	}

	// Unmarshal back
	var decoded ResourceStats
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Failed to unmarshal ResourceStats: %v", err)
	}

	// Verify critical fields
	if decoded.ClientID != stats.ClientID {
		t.Errorf("ClientID mismatch")
	}
	if decoded.CPUCores != stats.CPUCores {
		t.Errorf("CPUCores mismatch")
	}
	if len(decoded.CPUUsageAvg) != len(stats.CPUUsageAvg) {
		t.Errorf("CPUUsageAvg length mismatch")
	}
	if decoded.MemoryTotal != stats.MemoryTotal {
		t.Errorf("MemoryTotal mismatch")
	}
}

// TestClientRegistration_JSON verifies ClientRegistration serialization
func TestClientRegistration_JSON(t *testing.T) {
	reg := ClientRegistration{
		ClientID:     "test-backend-1",
		Hostname:     "backend-host-1",
		PublicIP:     "203.0.113.45",
		LocalIP:      "192.168.1.100",
		Latitude:     51.5074,
		Longitude:    -0.1278,
		Country:      "UK",
		City:         "London",
		TotalCPU:     16,
		TotalMemory:  64.0,
		TotalStorage: 1000.0,
		EndpointURL:  "https://backend1.example.com:11000",
	}

	// Marshal to JSON
	data, err := json.Marshal(reg)
	if err != nil {
		t.Fatalf("Failed to marshal ClientRegistration: %v", err)
	}

	// Unmarshal back
	var decoded ClientRegistration
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Failed to unmarshal ClientRegistration: %v", err)
	}

	// Verify fields
	if decoded.ClientID != reg.ClientID {
		t.Errorf("ClientID mismatch")
	}
	if decoded.EndpointURL != reg.EndpointURL {
		t.Errorf("EndpointURL mismatch: expected %s, got %s", reg.EndpointURL, decoded.EndpointURL)
	}
	if decoded.TotalCPU != reg.TotalCPU {
		t.Errorf("TotalCPU mismatch")
	}
}

// TestRoutingRequest_JSON verifies RoutingRequest serialization
func TestRoutingRequest_JSON(t *testing.T) {
	req := RoutingRequest{
		Tier:      "pro-turbo",
		ClientIP:  "203.0.113.45",
		ClientLat: 40.7128,
		ClientLon: -74.0060,
	}

	// Marshal to JSON
	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("Failed to marshal RoutingRequest: %v", err)
	}

	// Unmarshal back
	var decoded RoutingRequest
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Failed to unmarshal RoutingRequest: %v", err)
	}

	// Verify fields
	if decoded.Tier != req.Tier {
		t.Errorf("Tier mismatch")
	}
	if decoded.ClientIP != req.ClientIP {
		t.Errorf("ClientIP mismatch")
	}
	if decoded.ClientLat != req.ClientLat {
		t.Errorf("ClientLat mismatch")
	}
	if decoded.ClientLon != req.ClientLon {
		t.Errorf("ClientLon mismatch")
	}
}

// TestRoutingResponse_JSON verifies RoutingResponse serialization
func TestRoutingResponse_JSON(t *testing.T) {
	resp := RoutingResponse{
		ClientID: "backend-123",
		Endpoint: "https://backend123.example.com:11000",
		Hostname: "backend-host-123",
		Distance: 3944.5,
	}

	// Marshal to JSON
	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("Failed to marshal RoutingResponse: %v", err)
	}

	// Unmarshal back
	var decoded RoutingResponse
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Failed to unmarshal RoutingResponse: %v", err)
	}

	// Verify fields
	if decoded.ClientID != resp.ClientID {
		t.Errorf("ClientID mismatch")
	}
	if decoded.Endpoint != resp.Endpoint {
		t.Errorf("Endpoint mismatch")
	}
	if decoded.Distance != resp.Distance {
		t.Errorf("Distance mismatch: expected %.1f, got %.1f", resp.Distance, decoded.Distance)
	}
}

// TestHealthCheckResponse_JSON verifies HealthCheckResponse serialization
func TestHealthCheckResponse_JSON(t *testing.T) {
	resp := HealthCheckResponse{
		Status:        "ok",
		Timestamp:     time.Now(),
		TotalClients:  10,
		ActiveClients: 8,
	}

	// Marshal to JSON
	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("Failed to marshal HealthCheckResponse: %v", err)
	}

	// Unmarshal back
	var decoded HealthCheckResponse
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Failed to unmarshal HealthCheckResponse: %v", err)
	}

	// Verify fields
	if decoded.Status != resp.Status {
		t.Errorf("Status mismatch")
	}
	if decoded.TotalClients != resp.TotalClients {
		t.Errorf("TotalClients mismatch")
	}
	if decoded.ActiveClients != resp.ActiveClients {
		t.Errorf("ActiveClients mismatch")
	}
}

// TestTierSpecs_DefaultValues verifies default tier specifications
func TestTierSpecs_DefaultValues(t *testing.T) {
	expectedTiers := map[string]struct {
		vcpu      int
		memoryGB  float64
		storageGB int
	}{
		"free":         {vcpu: 1, memoryGB: 1.0, storageGB: 0},
		"lite":         {vcpu: 1, memoryGB: 1.0, storageGB: 5},
		"pro-standard": {vcpu: 2, memoryGB: 4.0, storageGB: 20},
		"pro-turbo":    {vcpu: 4, memoryGB: 8.0, storageGB: 30},
		"pro-max":      {vcpu: 8, memoryGB: 16.0, storageGB: 40},
	}

	for tierName, expected := range expectedTiers {
		tier, exists := TierSpecs[tierName]
		if !exists {
			t.Errorf("Missing tier: %s", tierName)
			continue
		}

		if tier.VCPU != expected.vcpu {
			t.Errorf("Tier %s: expected VCPU=%d, got %d", tierName, expected.vcpu, tier.VCPU)
		}
		if tier.MemoryGB != expected.memoryGB {
			t.Errorf("Tier %s: expected MemoryGB=%.1f, got %.1f", tierName, expected.memoryGB, tier.MemoryGB)
		}
		if tier.StorageGB != expected.storageGB {
			t.Errorf("Tier %s: expected StorageGB=%d, got %d", tierName, expected.storageGB, tier.StorageGB)
		}
	}
}

// TestResourceStats_PerCoreUsage verifies per-core CPU usage handling
func TestResourceStats_PerCoreUsage(t *testing.T) {
	stats := ResourceStats{
		ClientID:    "test-client",
		CPUCores:    4,
		CPUUsageAvg: []float64{25.5, 50.0, 75.2, 90.8},
	}

	if len(stats.CPUUsageAvg) != stats.CPUCores {
		t.Errorf("CPUUsageAvg length (%d) doesn't match CPUCores (%d)",
			len(stats.CPUUsageAvg), stats.CPUCores)
	}

	// Verify all values are percentages (0-100)
	for i, usage := range stats.CPUUsageAvg {
		if usage < 0 || usage > 100 {
			t.Errorf("Core %d usage %.2f%% is out of range [0-100]", i, usage)
		}
	}
}
