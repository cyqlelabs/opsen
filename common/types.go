package common

import "time"

// TierSpec defines resource requirements for a subscription tier
type TierSpec struct {
	Name        string  `json:"name" yaml:"name"`
	VCPU        int     `json:"vcpu" yaml:"vcpu"`
	MemoryGB    float64 `json:"memory_gb" yaml:"memory_gb"`
	StorageGB   int     `json:"storage_gb" yaml:"storage_gb"`
	GPU         int     `json:"gpu,omitempty" yaml:"gpu,omitempty"`               // Number of GPUs required (optional)
	GPUMemoryGB float64 `json:"gpu_memory_gb,omitempty" yaml:"gpu_memory_gb,omitempty"` // GPU VRAM required in GB (optional)
}

// TierSpecs maps tier names to their resource requirements
var TierSpecs = map[string]TierSpec{
	"free": {
		Name:      "free",
		VCPU:      1,
		MemoryGB:  1.0,
		StorageGB: 0,
	},
	"lite": {
		Name:      "lite",
		VCPU:      1,
		MemoryGB:  1.0,
		StorageGB: 5,
	},
	"pro-standard": {
		Name:      "pro-standard",
		VCPU:      2,
		MemoryGB:  4.0,
		StorageGB: 20,
	},
	"pro-turbo": {
		Name:      "pro-turbo",
		VCPU:      4,
		MemoryGB:  8.0,
		StorageGB: 30,
	},
	"pro-max": {
		Name:      "pro-max",
		VCPU:      8,
		MemoryGB:  16.0,
		StorageGB: 40,
	},
}

// GPUStats represents GPU metrics for a single GPU device
type GPUStats struct {
	DeviceID       int     `json:"device_id"`           // GPU device index
	Name           string  `json:"name"`                // GPU model name
	UtilizationPct float64 `json:"utilization_pct"`     // GPU core utilization (0-100)
	MemoryUsedGB   float64 `json:"memory_used_gb"`      // GPU VRAM used in GB
	MemoryTotalGB  float64 `json:"memory_total_gb"`     // Total GPU VRAM in GB
	TemperatureC   float64 `json:"temperature_c"`       // GPU temperature in Celsius
	PowerDrawW     float64 `json:"power_draw_w,omitempty"` // Power draw in Watts (optional)
}

// ResourceStats represents the current resource usage of a client machine
type ResourceStats struct {
	ClientID      string    `json:"client_id"`
	Hostname      string    `json:"hostname"`
	Timestamp     time.Time `json:"timestamp"`

	// CPU metrics (per-core averages over time window)
	CPUCores      int       `json:"cpu_cores"`
	CPUUsageAvg   []float64 `json:"cpu_usage_avg"` // Per-core usage percentage (0-100)

	// Memory metrics (GB)
	MemoryTotal   float64   `json:"memory_total_gb"`
	MemoryUsed    float64   `json:"memory_used_gb"`
	MemoryAvail   float64   `json:"memory_avail_gb"`

	// Disk metrics (GB)
	DiskTotal     float64   `json:"disk_total_gb"`
	DiskUsed      float64   `json:"disk_used_gb"`
	DiskAvail     float64   `json:"disk_avail_gb"`

	// GPU metrics (optional, empty if no GPUs available)
	GPUs          []GPUStats `json:"gpus,omitempty"`

	// Network info
	PublicIP      string    `json:"public_ip"`
	Latitude      float64   `json:"latitude"`
	Longitude     float64   `json:"longitude"`
	Country       string    `json:"country"`
	City          string    `json:"city"`
}

// ClientRegistration is sent when a client first connects
type ClientRegistration struct {
	ClientID     string   `json:"client_id"`
	Hostname     string   `json:"hostname"`
	PublicIP     string   `json:"public_ip"`
	LocalIP      string   `json:"local_ip"`      // Local/private IP address
	Latitude     float64  `json:"latitude"`
	Longitude    float64  `json:"longitude"`
	Country      string   `json:"country"`
	City         string   `json:"city"`
	TotalCPU     int      `json:"total_cpu"`
	TotalMemory  float64  `json:"total_memory_gb"`
	TotalStorage float64  `json:"total_storage_gb"`
	TotalGPUs    int      `json:"total_gpus,omitempty"`    // Number of GPUs (optional)
	GPUModels    []string `json:"gpu_models,omitempty"`    // GPU model names (optional)
	EndpointURL  string   `json:"endpoint_url,omitempty"`  // Optional: Override endpoint URL
}

// RoutingRequest is sent from Caddy to determine which backend to use
type RoutingRequest struct {
	Tier         string  `json:"tier"`
	ClientIP     string  `json:"client_ip"`
	ClientLat    float64 `json:"client_lat,omitempty"`
	ClientLon    float64 `json:"client_lon,omitempty"`
}

// RoutingResponse returns the selected backend endpoint
type RoutingResponse struct {
	ClientID     string  `json:"client_id"`
	Endpoint     string  `json:"endpoint"`
	Hostname     string  `json:"hostname"`
	Distance     float64 `json:"distance_km,omitempty"`
}

// HealthCheck request/response
type HealthCheckResponse struct {
	Status        string    `json:"status"`
	Timestamp     time.Time `json:"timestamp"`
	TotalClients  int       `json:"total_clients"`
	ActiveClients int       `json:"active_clients"`
}
