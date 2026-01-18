package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"cyqle.in/opsen/common"
)

// CreateTestDB creates a temporary test database and returns cleanup function
func CreateTestDB(t *testing.T) (*sql.DB, func()) {
	t.Helper()

	// Create temporary database file
	tmpFile, err := os.CreateTemp("", "test-lb-*.db")
	if err != nil {
		t.Fatalf("Failed to create temp db: %v", err)
	}
	tmpFile.Close()

	db, err := initDatabase(tmpFile.Name())
	if err != nil {
		os.Remove(tmpFile.Name())
		t.Fatalf("Failed to init database: %v", err)
	}

	cleanup := func() {
		db.Close()
		os.Remove(tmpFile.Name())
	}

	return db, cleanup
}

// NewTestServer creates a server instance for testing
func NewTestServer(t *testing.T, db *sql.DB) *Server {
	t.Helper()
	return NewTestServerWithConfig(t, db, nil)
}

// NewTestServerWithConfig creates a server instance with custom configuration
func NewTestServerWithConfig(t *testing.T, db *sql.DB, configModifier func(*common.ServerConfig)) *Server {
	t.Helper()

	config := &common.ServerConfig{
		Port:                         8080,
		StaleMinutes:                 5,
		CleanupIntervalSecs:          60,
		StickyHeader:                 "X-Session-ID",
		StickyAffinityEnabled:        true,
		PendingAllocationTimeoutSecs: 120,
		TierFieldName:                "tier",   // Default tier field name
		TierHeader:                   "X-Tier", // Default tier header

		// Health check defaults
		HealthCheckEnabled:            true,
		HealthCheckIntervalSecs:       10,
		HealthCheckTimeoutSecs:        2,
		HealthCheckType:               "tcp",
		HealthCheckPath:               "/health",
		HealthCheckUnhealthyThreshold: 3,
		HealthCheckHealthyThreshold:   2,

		Tiers: []common.TierSpec{
			{Name: "free", VCPU: 1, MemoryGB: 1.0, StorageGB: 0},
			{Name: "lite", VCPU: 1, MemoryGB: 1.0, StorageGB: 5},
			{Name: "pro-standard", VCPU: 2, MemoryGB: 4.0, StorageGB: 20},
			{Name: "pro-turbo", VCPU: 4, MemoryGB: 8.0, StorageGB: 30},
			{Name: "pro-max", VCPU: 8, MemoryGB: 16.0, StorageGB: 40},
		},
	}

	// Apply custom configuration if provided
	if configModifier != nil {
		configModifier(config)
	}

	tierSpecs := make(map[string]common.TierSpec)
	for _, tier := range config.Tiers {
		tierSpecs[tier.Name] = tier
	}

	return &Server{
		db:                    db,
		clientCache:           make(map[string]*ClientState),
		stickyAssignments:     make(map[string]map[string]string),
		pendingAllocations:    make(map[string][]PendingAllocation),
		stickyHeader:          config.StickyHeader,
		stickyAffinityEnabled: config.StickyAffinityEnabled,
		staleTimeout:          time.Duration(config.StaleMinutes) * time.Minute,
		cleanupInterval:       time.Duration(config.CleanupIntervalSecs) * time.Second,
		tierSpecs:             tierSpecs,
		config:                config,
	}
}

// MockClient creates a test client with specified resources
type MockClientOptions struct {
	ClientID      string
	Hostname      string
	Latitude      float64
	Longitude     float64
	TotalCPU      int
	TotalMemory   float64
	TotalStorage  float64
	TotalGPUs     int               // Number of GPUs
	GPUModels     []string          // GPU model names
	CPUUsageAvg   []float64         // Per-core usage (0-100)
	MemoryUsed    float64
	MemoryAvail   float64
	DiskUsed      float64
	DiskAvail     float64
	GPUs          []common.GPUStats // GPU metrics
	LastSeen      time.Time
	Endpoint      string
}

func NewMockClient(opts MockClientOptions) *ClientState {
	// Apply defaults
	if opts.ClientID == "" {
		opts.ClientID = fmt.Sprintf("client-%d", rand.Intn(10000))
	}
	if opts.Hostname == "" {
		opts.Hostname = opts.ClientID
	}
	if opts.TotalCPU == 0 {
		opts.TotalCPU = 8
	}
	if opts.TotalMemory == 0 {
		opts.TotalMemory = 32.0
	}
	if opts.TotalStorage == 0 {
		opts.TotalStorage = 500.0
	}
	if opts.LastSeen.IsZero() {
		opts.LastSeen = time.Now()
	}
	if opts.Endpoint == "" {
		opts.Endpoint = fmt.Sprintf("http://localhost:11000")
	}

	// Generate default CPU usage if not provided
	if len(opts.CPUUsageAvg) == 0 {
		opts.CPUUsageAvg = make([]float64, opts.TotalCPU)
		for i := 0; i < opts.TotalCPU; i++ {
			opts.CPUUsageAvg[i] = 20.0 // Low usage by default
		}
	}

	// Calculate memory if not specified
	if opts.MemoryUsed == 0 && opts.MemoryAvail == 0 {
		opts.MemoryUsed = 8.0
		opts.MemoryAvail = opts.TotalMemory - opts.MemoryUsed
	}

	// Calculate disk if not specified
	if opts.DiskUsed == 0 && opts.DiskAvail == 0 {
		opts.DiskUsed = 100.0
		opts.DiskAvail = opts.TotalStorage - opts.DiskUsed
	}

	return &ClientState{
		Registration: common.ClientRegistration{
			ClientID:     opts.ClientID,
			Hostname:     opts.Hostname,
			Latitude:     opts.Latitude,
			Longitude:    opts.Longitude,
			TotalCPU:     opts.TotalCPU,
			TotalMemory:  opts.TotalMemory,
			TotalStorage: opts.TotalStorage,
			TotalGPUs:    opts.TotalGPUs,
			GPUModels:    opts.GPUModels,
		},
		Stats: common.ResourceStats{
			ClientID:    opts.ClientID,
			Hostname:    opts.Hostname,
			CPUCores:    opts.TotalCPU,
			CPUUsageAvg: opts.CPUUsageAvg,
			MemoryTotal: opts.TotalMemory,
			MemoryUsed:  opts.MemoryUsed,
			MemoryAvail: opts.MemoryAvail,
			DiskTotal:   opts.TotalStorage,
			DiskUsed:    opts.DiskUsed,
			DiskAvail:   opts.DiskAvail,
			GPUs:        opts.GPUs,
		},
		LastSeen:     opts.LastSeen,
		Endpoint:     opts.Endpoint,
		HealthStatus: "unknown", // Default health status
	}
}

// AddMockClient adds a mock client to the server's cache
func (s *Server) AddMockClient(client *ClientState) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.clientCache[client.Registration.ClientID] = client
}

// RegisterMockClientInDB registers a mock client in the database
func RegisterMockClientInDB(t *testing.T, db *sql.DB, client *ClientState) {
	t.Helper()

	gpuModelsJSON, _ := json.Marshal(client.Registration.GPUModels)
	_, err := db.Exec(`
		INSERT OR REPLACE INTO clients
		(client_id, hostname, public_ip, local_ip, latitude, longitude, country, city,
		 total_cpu, total_memory, total_storage, total_gpus, gpu_models, endpoint, last_seen)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, client.Registration.ClientID, client.Registration.Hostname,
		"1.2.3.4", "192.168.1.1",
		client.Registration.Latitude, client.Registration.Longitude,
		"US", "TestCity",
		client.Registration.TotalCPU, client.Registration.TotalMemory,
		client.Registration.TotalStorage, client.Registration.TotalGPUs,
		gpuModelsJSON, client.Endpoint,
		client.LastSeen.Format("2006-01-02 15:04:05"))

	if err != nil {
		t.Fatalf("Failed to register mock client in DB: %v", err)
	}

	// Also add stats
	cpuJSON, _ := json.Marshal(client.Stats.CPUUsageAvg)
	gpuJSON, _ := json.Marshal(client.Stats.GPUs)
	_, err = db.Exec(`
		INSERT INTO stats
		(client_id, timestamp, cpu_cores, cpu_usage_json, memory_total, memory_used,
		 memory_avail, disk_total, disk_used, disk_avail, gpu_stats_json)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, client.Stats.ClientID, time.Now().Format("2006-01-02 15:04:05"),
		client.Stats.CPUCores, cpuJSON,
		client.Stats.MemoryTotal, client.Stats.MemoryUsed, client.Stats.MemoryAvail,
		client.Stats.DiskTotal, client.Stats.DiskUsed, client.Stats.DiskAvail, gpuJSON)

	if err != nil {
		t.Fatalf("Failed to insert stats: %v", err)
	}
}

// AssertClientSelected verifies the selected client matches expected ID
func AssertClientSelected(t *testing.T, client *ClientState, expectedID string) {
	t.Helper()
	if client == nil {
		t.Fatalf("Expected client %s but got nil", expectedID)
	}
	if client.Registration.ClientID != expectedID {
		t.Errorf("Expected client %s but got %s", expectedID, client.Registration.ClientID)
	}
}

// AssertNoClient verifies no client was selected
func AssertNoClient(t *testing.T, client *ClientState) {
	t.Helper()
	if client != nil {
		t.Errorf("Expected no client but got %s", client.Registration.ClientID)
	}
}

// CountPendingAllocations returns the total number of pending allocations across all clients
func (s *Server) CountPendingAllocations() int {
	s.mu.RLock()
	defer s.mu.RUnlock()

	count := 0
	for _, allocations := range s.pendingAllocations {
		count += len(allocations)
	}
	return count
}

// GetPendingAllocationsForClient returns pending allocations for a specific client
func (s *Server) GetPendingAllocationsForClient(clientID string) []PendingAllocation {
	s.mu.RLock()
	defer s.mu.RUnlock()

	allocations := s.pendingAllocations[clientID]
	result := make([]PendingAllocation, len(allocations))
	copy(result, allocations)
	return result
}

// ClearPendingAllocations clears all pending allocations (for testing)
func (s *Server) ClearPendingAllocations() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pendingAllocations = make(map[string][]PendingAllocation)
}
