package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/oschwald/geoip2-golang"
	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/disk"
	"github.com/shirou/gopsutil/v3/mem"
	"cyqle.in/opsen/common"
)

type Config struct {
	ServerURL       string
	ClientID        string
	Hostname        string
	WindowMinutes   int
	ReportInterval  int
	DiskPath        string
	EndpointURL     string
	Endpoints       []common.EndpointConfig
	GeoIPDBPath     string
	SkipGeolocation bool
	InsecureTLS     bool
	ServerKey       string
}

type MetricsCollector struct {
	config          Config
	httpClient      *http.Client
	mu              sync.RWMutex // guards the sample buffers and sampleIndex
	cpuSamples      [][]float64  // [sample_index][core_index]
	memorySamples   []float64
	diskSamples     []float64
	gpuCollector    *GPUCollector // GPU metrics collector
	sampleIndex     int
	maxSamples      int
	circuitBreaker  *CircuitBreaker
	retryConfig     RetryConfig
}

// clientFlags holds the command-line overrides for the client configuration.
type clientFlags struct {
	configFile     string
	serverURL      string
	windowMinutes  int
	reportInterval int
	diskPath       string
	clientID       string
}

// parseClientFlags parses command-line arguments into clientFlags.
func parseClientFlags(args []string) (*clientFlags, error) {
	fs := flag.NewFlagSet("opsen-client", flag.ContinueOnError)

	flags := &clientFlags{}
	fs.StringVar(&flags.configFile, "config", "", "Path to YAML configuration file")
	fs.StringVar(&flags.serverURL, "server", "", "Load balancer server URL")
	fs.IntVar(&flags.windowMinutes, "window", 0, "Time window for averaging metrics (minutes)")
	fs.IntVar(&flags.reportInterval, "interval", 0, "Report interval in seconds")
	fs.StringVar(&flags.diskPath, "disk", "", "Disk path to monitor")
	fs.StringVar(&flags.clientID, "id", "", "Client ID (auto-generated if empty)")

	if err := fs.Parse(args); err != nil {
		return nil, err
	}
	return flags, nil
}

// applyClientFlagOverrides applies non-empty command-line overrides on top of the
// YAML configuration.
func applyClientFlagOverrides(yamlConfig *common.ClientConfig, flags *clientFlags) {
	if flags == nil {
		return
	}
	if flags.serverURL != "" {
		yamlConfig.ServerURL = flags.serverURL
	}
	if flags.windowMinutes > 0 {
		yamlConfig.WindowMinutes = flags.windowMinutes
	}
	if flags.reportInterval > 0 {
		yamlConfig.ReportInterval = flags.reportInterval
	}
	if flags.diskPath != "" {
		yamlConfig.DiskPath = flags.diskPath
	}
	if flags.clientID != "" {
		yamlConfig.ClientID = flags.clientID
	}
}

// loadClientRuntimeConfig resolves the effective client configuration from the
// YAML file, the command-line overrides and the local environment.
func loadClientRuntimeConfig(flags *clientFlags) (Config, error) {
	configFile := ""
	if flags != nil {
		configFile = flags.configFile
	}

	yamlConfig, err := common.LoadClientConfig(configFile)
	if err != nil {
		return Config{}, fmt.Errorf("failed to load config: %w", err)
	}

	applyClientFlagOverrides(yamlConfig, flags)

	// Auto-generate client ID if not set
	if yamlConfig.ClientID == "" {
		yamlConfig.ClientID = uuid.New().String()
	}

	// Get hostname if not set
	hostname := yamlConfig.Hostname
	if hostname == "" {
		hostname, err = os.Hostname()
		if err != nil {
			return Config{}, fmt.Errorf("failed to get hostname: %w", err)
		}
	}

	return Config{
		ServerURL:       yamlConfig.ServerURL,
		ClientID:        yamlConfig.ClientID,
		Hostname:        hostname,
		WindowMinutes:   yamlConfig.WindowMinutes,
		ReportInterval:  yamlConfig.ReportInterval,
		DiskPath:        yamlConfig.DiskPath,
		EndpointURL:     yamlConfig.EndpointURL,
		Endpoints:       yamlConfig.Endpoints,
		GeoIPDBPath:     yamlConfig.GeoIPDBPath,
		SkipGeolocation: yamlConfig.SkipGeolocation,
		InsecureTLS:     yamlConfig.InsecureTLS,
		ServerKey:       yamlConfig.ServerKey,
	}, nil
}

// newHTTPClient builds the HTTP client used for registration and stats reporting.
func newHTTPClient(config Config) *http.Client {
	httpClient := &http.Client{
		Timeout: 30 * time.Second,
	}
	if config.InsecureTLS {
		log.Printf("Warning: TLS certificate verification disabled (insecure_tls: true)")
		httpClient.Transport = &http.Transport{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: true,
			},
		}
	}
	return httpClient
}

// newMetricsCollector wires up a collector with its sample buffers, circuit
// breaker and GPU collector. The caller owns the returned collector and must
// call Close on its GPU collector when done.
func newMetricsCollector(config Config, httpClient *http.Client) *MetricsCollector {
	// Calculate samples per window (1 sample per second)
	samplesPerWindow := config.WindowMinutes * 60

	return &MetricsCollector{
		config:        config,
		httpClient:    httpClient,
		cpuSamples:    make([][]float64, samplesPerWindow),
		memorySamples: make([]float64, samplesPerWindow),
		diskSamples:   make([]float64, samplesPerWindow),
		// Initialize GPU collector (gracefully disabled if no GPUs present)
		gpuCollector: NewGPUCollector(samplesPerWindow),
		maxSamples:   samplesPerWindow,
		// Circuit breaker: max 5 failures, 30 second reset timeout
		circuitBreaker: NewCircuitBreaker(5, 30*time.Second),
		retryConfig:    DefaultRetryConfig(),
	}
}

// runClient registers with the load balancer and runs the collection and
// reporting loops until ctx is cancelled.
func runClient(ctx context.Context, config Config) error {
	LogInfo("Load balancer client initializing...")

	collector := newMetricsCollector(config, newHTTPClient(config))
	defer collector.gpuCollector.Close()

	return runCollector(ctx, collector)
}

// runCollector registers the collector with the load balancer, then samples and
// reports metrics until ctx is cancelled.
func runCollector(ctx context.Context, collector *MetricsCollector) error {
	config := collector.config

	// Register with server (with retry logic)
	if err := RetryWithBackoff(collector.retryConfig, func() error {
		return collector.register()
	}); err != nil {
		return fmt.Errorf("failed to register with server after retries: %w", err)
	}

	LogInfoWithData("Client registered successfully", map[string]interface{}{
		"client_id":       config.ClientID,
		"hostname":        config.Hostname,
		"window_minutes":  config.WindowMinutes,
		"report_interval": config.ReportInterval,
	})

	// Start metrics collection goroutine (1 sample/sec) with panic recovery
	go func() {
		defer func() {
			if r := recover(); r != nil {
				LogErrorWithData("Metrics collection goroutine panic", map[string]interface{}{
					"panic": fmt.Sprintf("%v", r),
				})
			}
		}()
		collector.collectMetrics(ctx)
	}()

	// Report to server periodically
	ticker := time.NewTicker(time.Duration(config.ReportInterval) * time.Second)
	defer ticker.Stop()

	LogInfo(fmt.Sprintf("Starting stats reporting loop (every %d seconds)", config.ReportInterval))

	for {
		select {
		case <-ctx.Done():
			LogInfo("Stats reporting loop stopping...")
			return nil
		case <-ticker.C:
			collector.reportStatsWithCircuitBreaker()
		}
	}
}

// reportStatsWithCircuitBreaker reports stats once, logging (but not returning)
// any failure so that the reporting loop keeps running.
func (c *MetricsCollector) reportStatsWithCircuitBreaker() {
	err := c.circuitBreaker.Call(func() error {
		return c.reportStats()
	})
	if err == nil {
		return
	}

	if err == ErrCircuitOpen {
		LogWarn("Circuit breaker is open, skipping stats report")
		return
	}

	LogErrorWithData("Failed to report stats", map[string]interface{}{
		"error":         err.Error(),
		"circuit_state": c.circuitBreaker.GetState().String(),
		"failures":      c.circuitBreaker.GetFailures(),
	})
}

func main() {
	flags, err := parseClientFlags(os.Args[1:])
	if err != nil {
		log.Fatalf("Failed to parse flags: %v", err)
	}

	config, err := loadClientRuntimeConfig(flags)
	if err != nil {
		log.Fatalf("%v", err)
	}

	// Initialize logger
	InitLogger("info", false, "lb-client")

	// Add panic recovery wrapper
	defer func() {
		if r := recover(); r != nil {
			LogFatalWithData("Client panic", map[string]interface{}{
				"panic": fmt.Sprintf("%v", r),
			})
		}
	}()

	if err := runClient(context.Background(), config); err != nil {
		LogFatal(err.Error())
	}
}

func (c *MetricsCollector) register() error {
	// Get local IP address
	localIP, err := c.getLocalIP()
	if err != nil {
		log.Printf("Warning: Failed to get local IP: %v", err)
		localIP = "127.0.0.1"
	}
	log.Printf("Detected local IP: %s", localIP)

	// Default geolocation values
	publicIP := "unknown"
	latitude := 0.0
	longitude := 0.0
	country := "unknown"
	city := "unknown"

	// Get geolocation (skip if configured)
	if !c.config.SkipGeolocation {
		// Default GeoIP database path if not configured
		geoIPPath := c.config.GeoIPDBPath
		if geoIPPath == "" {
			geoIPPath = "./GeoLite2-City.mmdb"
		}

		// Try to download GeoIP database if it doesn't exist
		if _, err := os.Stat(geoIPPath); os.IsNotExist(err) {
			log.Printf("GeoIP database not found at %s, attempting to download...", geoIPPath)
			if err := c.downloadGeoIPDatabase(geoIPPath); err != nil {
				log.Printf("Warning: Failed to download GeoIP database: %v", err)
				log.Printf("Continuing without geolocation data")
			} else {
				log.Printf("GeoIP database downloaded successfully to %s", geoIPPath)
			}
		}

		// Try GeoIP database lookup using local IP
		if _, err := os.Stat(geoIPPath); err == nil {
			log.Printf("Using GeoIP database: %s", geoIPPath)
			log.Printf("Note: Geolocation uses local IP. For accurate location behind NAT, configure endpoint_url with public IP")

			// Try to lookup geolocation using local IP
			// Note: This will only work if the client has a public IP address
			// For clients behind NAT, geolocation will use the local IP which may not be accurate
			geoData, err := c.getGeolocationFromIP(geoIPPath, localIP)
			if err != nil {
				log.Printf("Warning: Failed to get geolocation from database: %v", err)
				log.Printf("Continuing without geolocation data")
			} else {
				publicIP = localIP // Use local IP as public IP
				latitude = geoData["latitude"].(float64)
				longitude = geoData["longitude"].(float64)
				country = geoData["country"].(string)
				city = geoData["city"].(string)
				log.Printf("GeoIP database lookup: City=%s, Country=%s, Coords=(%.4f, %.4f)",
					city, country, latitude, longitude)
			}
		}
	} else {
		log.Printf("Geolocation skipped (skip_geolocation: true)")
	}

	log.Printf("Location: PublicIP=%s, LocalIP=%s, City=%s, Country=%s, Coords=(%.4f, %.4f)",
		publicIP, localIP, city, country, latitude, longitude)

	// Get total resources
	cpuCount, _ := cpu.Counts(true)
	memInfo, _ := mem.VirtualMemory()
	diskInfo, _ := disk.Usage(c.config.DiskPath)

	// Get GPU info if available
	totalGPUs := c.gpuCollector.GetDeviceCount()
	gpuModels := c.gpuCollector.GetDeviceModels()

	registration := common.ClientRegistration{
		ClientID:     c.config.ClientID,
		Hostname:     c.config.Hostname,
		PublicIP:     publicIP,
		LocalIP:      localIP,
		Latitude:     latitude,
		Longitude:    longitude,
		Country:      country,
		City:         city,
		TotalCPU:     cpuCount,
		TotalMemory:  float64(memInfo.Total) / 1024 / 1024 / 1024,
		TotalStorage: float64(diskInfo.Total) / 1024 / 1024 / 1024,
		TotalGPUs:    totalGPUs,
		GPUModels:    gpuModels,
		EndpointURL:  c.config.EndpointURL,
		Endpoints:    c.config.Endpoints,
	}

	if totalGPUs > 0 {
		log.Printf("Registering with %d GPU(s): %v", totalGPUs, gpuModels)
	}

	body, _ := json.Marshal(registration)

	// Create request with server key header if configured
	req, err := http.NewRequest("POST", c.config.ServerURL+"/register", bytes.NewBuffer(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.config.ServerKey != "" {
		req.Header.Set("X-API-Key", c.config.ServerKey)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("registration failed: %s", resp.Status)
	}

	return nil
}

// collectMetrics samples system metrics once per second until ctx is cancelled.
func (c *MetricsCollector) collectMetrics(ctx context.Context) {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			LogInfo("Metrics collection stopping...")
			return
		case <-ticker.C:
			c.collectSample()
		}
	}
}

// collectSample takes a single sample of CPU, memory, disk and GPU usage and
// stores it in the rolling window.
func (c *MetricsCollector) collectSample() {
	// CPU per-core usage
	perCore, cpuErr := cpu.Percent(0, true)

	// Memory usage
	memInfo, memErr := mem.VirtualMemory()

	// Disk usage
	diskInfo, diskErr := disk.Usage(c.config.DiskPath)

	c.mu.Lock()
	if cpuErr == nil && len(perCore) > 0 {
		c.cpuSamples[c.sampleIndex] = perCore
	}
	if memErr == nil {
		c.memorySamples[c.sampleIndex] = float64(memInfo.Used) / 1024 / 1024 / 1024
	}
	if diskErr == nil {
		c.diskSamples[c.sampleIndex] = float64(diskInfo.Used) / 1024 / 1024 / 1024
	}
	c.sampleIndex = (c.sampleIndex + 1) % c.maxSamples
	c.mu.Unlock()

	// GPU metrics (if available)
	if c.gpuCollector.IsEnabled() {
		if err := c.gpuCollector.CollectSample(); err != nil {
			LogWarn(fmt.Sprintf("Failed to collect GPU sample: %v", err))
		}
	}
}

func (c *MetricsCollector) reportStats() error {
	// Calculate averages over the window (single read lock for a consistent view)
	c.mu.RLock()
	cpuCoreAvg := c.calculateCPUAveragesLocked()
	memoryUsed := c.calculateAverage(c.memorySamples)
	diskUsed := c.calculateAverage(c.diskSamples)
	c.mu.RUnlock()

	// Get current total resources
	memInfo, _ := mem.VirtualMemory()
	diskInfo, _ := disk.Usage(c.config.DiskPath)

	// Get GPU averages if available
	gpuStats := c.gpuCollector.CalculateAverages()

	stats := common.ResourceStats{
		ClientID:    c.config.ClientID,
		Hostname:    c.config.Hostname,
		Timestamp:   time.Now(),
		CPUCores:    len(cpuCoreAvg),
		CPUUsageAvg: cpuCoreAvg,
		MemoryTotal: float64(memInfo.Total) / 1024 / 1024 / 1024,
		MemoryUsed:  memoryUsed,
		MemoryAvail: float64(memInfo.Total)/1024/1024/1024 - memoryUsed,
		DiskTotal:   float64(diskInfo.Total) / 1024 / 1024 / 1024,
		DiskUsed:    diskUsed,
		DiskAvail:   float64(diskInfo.Total)/1024/1024/1024 - diskUsed,
		GPUs:        gpuStats,
	}

	body, _ := json.Marshal(stats)

	// Create request with server key header if configured
	req, err := http.NewRequest("POST", c.config.ServerURL+"/stats", bytes.NewBuffer(body))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if c.config.ServerKey != "" {
		req.Header.Set("X-API-Key", c.config.ServerKey)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("http request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("stats report failed: status=%s, body=%s", resp.Status, string(bodyBytes))
	}

	logData := map[string]interface{}{
		"cpu_cores":    stats.CPUCores,
		"memory_used":  fmt.Sprintf("%.1fGB", stats.MemoryUsed),
		"memory_total": fmt.Sprintf("%.1fGB", stats.MemoryTotal),
		"disk_used":    fmt.Sprintf("%.1fGB", stats.DiskUsed),
		"disk_total":   fmt.Sprintf("%.1fGB", stats.DiskTotal),
	}

	if len(gpuStats) > 0 {
		logData["gpu_count"] = len(gpuStats)
		for i, gpu := range gpuStats {
			logData[fmt.Sprintf("gpu_%d_util", i)] = fmt.Sprintf("%.1f%%", gpu.UtilizationPct)
			logData[fmt.Sprintf("gpu_%d_mem", i)] = fmt.Sprintf("%.1f/%.1fGB", gpu.MemoryUsedGB, gpu.MemoryTotalGB)
		}
	}

	LogDebugWithData("Stats reported successfully", logData)

	return nil
}

func (c *MetricsCollector) calculateCPUAverages() []float64 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.calculateCPUAveragesLocked()
}

// calculateCPUAveragesLocked computes per-core averages. Callers must hold at
// least a read lock on c.mu.
func (c *MetricsCollector) calculateCPUAveragesLocked() []float64 {
	if len(c.cpuSamples) == 0 {
		return []float64{}
	}

	// Find the first non-empty sample to determine number of cores
	var numCores int
	for _, sample := range c.cpuSamples {
		if len(sample) > 0 {
			numCores = len(sample)
			break
		}
	}

	if numCores == 0 {
		return []float64{}
	}

	averages := make([]float64, numCores)
	counts := make([]int, numCores)

	for _, sample := range c.cpuSamples {
		if len(sample) == 0 {
			continue
		}
		for core := 0; core < len(sample) && core < numCores; core++ {
			averages[core] += sample[core]
			counts[core]++
		}
	}

	for core := 0; core < numCores; core++ {
		if counts[core] > 0 {
			averages[core] /= float64(counts[core])
		}
	}

	return averages
}

func (c *MetricsCollector) calculateAverage(samples []float64) float64 {
	sum := 0.0
	count := 0

	for _, sample := range samples {
		if sample > 0 {
			sum += sample
			count++
		}
	}

	if count == 0 {
		return 0
	}

	return sum / float64(count)
}

// Helper functions for safe type conversion from map[string]interface{}
func getStringOrDefault(m map[string]interface{}, key, defaultValue string) string {
	if val, ok := m[key]; ok && val != nil {
		if str, ok := val.(string); ok {
			return str
		}
	}
	return defaultValue
}

func getFloatOrDefault(m map[string]interface{}, key string, defaultValue float64) float64 {
	if val, ok := m[key]; ok && val != nil {
		switch v := val.(type) {
		case float64:
			return v
		case float32:
			return float64(v)
		case int:
			return float64(v)
		case int64:
			return float64(v)
		}
	}
	return defaultValue
}

func (c *MetricsCollector) getLocalIP() (string, error) {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return "", err
	}

	for _, addr := range addrs {
		if ipnet, ok := addr.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
			if ipnet.IP.To4() != nil {
				return ipnet.IP.String(), nil
			}
		}
	}

	return "", fmt.Errorf("no local IP address found")
}

func (c *MetricsCollector) getGeolocationFromIP(dbPath, ipAddress string) (map[string]interface{}, error) {
	db, err := geoip2.Open(dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open GeoIP database: %w", err)
	}
	defer db.Close()

	ip := net.ParseIP(ipAddress)
	if ip == nil {
		return nil, fmt.Errorf("invalid IP address: %s", ipAddress)
	}

	record, err := db.City(ip)
	if err != nil {
		return nil, fmt.Errorf("failed to lookup IP in database: %w", err)
	}

	cityName := ""
	if name, ok := record.City.Names["en"]; ok {
		cityName = name
	}

	return map[string]interface{}{
		"ip":        ipAddress,
		"latitude":  record.Location.Latitude,
		"longitude": record.Location.Longitude,
		"country":   record.Country.IsoCode,
		"city":      cityName,
	}, nil
}

// geoIPDownloadURL is the source for the GeoLite2 city database. It is a
// variable so tests can point the download at a local server.
var geoIPDownloadURL = "https://cyqle-opsen.s3.us-east-2.amazonaws.com/GeoLite2-City.mmdb"

func (c *MetricsCollector) downloadGeoIPDatabase(targetPath string) error {
	downloadURL := geoIPDownloadURL

	log.Printf("Downloading GeoIP database from %s", downloadURL)

	resp, err := c.httpClient.Get(downloadURL)
	if err != nil {
		return fmt.Errorf("failed to download database: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download failed with status %d", resp.StatusCode)
	}

	// Create target file
	file, err := os.Create(targetPath)
	if err != nil {
		return fmt.Errorf("failed to create file: %w", err)
	}
	defer file.Close()

	// Copy downloaded content to file
	_, err = io.Copy(file, resp.Body)
	if err != nil {
		os.Remove(targetPath) // Clean up partial file
		return fmt.Errorf("failed to write database file: %w", err)
	}

	return nil
}
