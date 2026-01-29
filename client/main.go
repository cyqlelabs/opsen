package main

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
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
	GeoIPDBPath     string
	SkipGeolocation bool
	InsecureTLS     bool
	ServerKey       string
}

type MetricsCollector struct {
	config          Config
	httpClient      *http.Client
	cpuSamples      [][]float64 // [sample_index][core_index]
	memorySamples   []float64
	diskSamples     []float64
	gpuCollector    *GPUCollector // GPU metrics collector
	sampleIndex     int
	maxSamples      int
	circuitBreaker  *CircuitBreaker
	retryConfig     RetryConfig
}

func main() {
	configFile := flag.String("config", "", "Path to YAML configuration file")
	serverURL := flag.String("server", "", "Load balancer server URL")
	windowMinutes := flag.Int("window", 0, "Time window for averaging metrics (minutes)")
	reportInterval := flag.Int("interval", 0, "Report interval in seconds")
	diskPath := flag.String("disk", "", "Disk path to monitor")
	clientID := flag.String("id", "", "Client ID (auto-generated if empty)")
	flag.Parse()

	// Load configuration from YAML file
	yamlConfig, err := common.LoadClientConfig(*configFile)
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	// Override with command-line flags if provided
	if *serverURL != "" {
		yamlConfig.ServerURL = *serverURL
	}
	if *windowMinutes > 0 {
		yamlConfig.WindowMinutes = *windowMinutes
	}
	if *reportInterval > 0 {
		yamlConfig.ReportInterval = *reportInterval
	}
	if *diskPath != "" {
		yamlConfig.DiskPath = *diskPath
	}
	if *clientID != "" {
		yamlConfig.ClientID = *clientID
	}

	// Auto-generate client ID if not set
	if yamlConfig.ClientID == "" {
		yamlConfig.ClientID = uuid.New().String()
	}

	// Get hostname if not set
	hostname := yamlConfig.Hostname
	if hostname == "" {
		hostname, err = os.Hostname()
		if err != nil {
			log.Fatalf("Failed to get hostname: %v", err)
		}
	}

	config := Config{
		ServerURL:       yamlConfig.ServerURL,
		ClientID:        yamlConfig.ClientID,
		Hostname:        hostname,
		WindowMinutes:   yamlConfig.WindowMinutes,
		ReportInterval:  yamlConfig.ReportInterval,
		DiskPath:        yamlConfig.DiskPath,
		EndpointURL:     yamlConfig.EndpointURL,
		GeoIPDBPath:     yamlConfig.GeoIPDBPath,
		SkipGeolocation: yamlConfig.SkipGeolocation,
		InsecureTLS:     yamlConfig.InsecureTLS,
		ServerKey:       yamlConfig.ServerKey,
	}

	// Create HTTP client with TLS configuration
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

	// Initialize logger
	InitLogger("info", false, "lb-client")
	LogInfo("Load balancer client initializing...")

	// Calculate samples per window (1 sample per second)
	samplesPerWindow := config.WindowMinutes * 60

	// Create circuit breaker (max 5 failures, 30 second reset timeout)
	circuitBreaker := NewCircuitBreaker(5, 30*time.Second)

	// Initialize GPU collector (gracefully disabled if no GPUs present)
	gpuCollector := NewGPUCollector(samplesPerWindow)
	defer gpuCollector.Close()

	collector := &MetricsCollector{
		config:         config,
		httpClient:     httpClient,
		cpuSamples:     make([][]float64, samplesPerWindow),
		memorySamples:  make([]float64, samplesPerWindow),
		diskSamples:    make([]float64, samplesPerWindow),
		gpuCollector:   gpuCollector,
		maxSamples:     samplesPerWindow,
		circuitBreaker: circuitBreaker,
		retryConfig:    DefaultRetryConfig(),
	}

	// Register with server (with retry logic)
	err = RetryWithBackoff(collector.retryConfig, func() error {
		return collector.register()
	})
	if err != nil {
		LogFatal(fmt.Sprintf("Failed to register with server after retries: %v", err))
	}

	LogInfoWithData("Client registered successfully", map[string]interface{}{
		"client_id": config.ClientID,
		"hostname":  config.Hostname,
		"window_minutes": config.WindowMinutes,
		"report_interval": config.ReportInterval,
	})

	// Add panic recovery wrapper
	defer func() {
		if r := recover(); r != nil {
			LogFatalWithData("Client panic", map[string]interface{}{
				"panic": fmt.Sprintf("%v", r),
			})
		}
	}()

	// Start metrics collection goroutine (1 sample/sec) with panic recovery
	go func() {
		defer func() {
			if r := recover(); r != nil {
				LogErrorWithData("Metrics collection goroutine panic", map[string]interface{}{
					"panic": fmt.Sprintf("%v", r),
				})
			}
		}()
		collector.collectMetrics()
	}()

	// Report to server periodically
	ticker := time.NewTicker(time.Duration(config.ReportInterval) * time.Second)
	defer ticker.Stop()

	LogInfo(fmt.Sprintf("Starting stats reporting loop (every %d seconds)", config.ReportInterval))

	for range ticker.C {
		// Use circuit breaker for stats reporting
		err := collector.circuitBreaker.Call(func() error {
			return collector.reportStats()
		})

		if err != nil {
			if err == ErrCircuitOpen {
				LogWarn("Circuit breaker is open, skipping stats report")
			} else {
				LogErrorWithData("Failed to report stats", map[string]interface{}{
					"error": err.Error(),
					"circuit_state": collector.circuitBreaker.GetState().String(),
					"failures": collector.circuitBreaker.GetFailures(),
				})
			}
		}
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

		// Try GeoIP database lookup
		if _, err := os.Stat(geoIPPath); err == nil {
			log.Printf("Using GeoIP database: %s", geoIPPath)

			// Get public IP first (GeoIP databases only work with public IPs)
			publicIP, err = c.getPublicIP()
			if err != nil {
				log.Printf("Warning: Failed to get public IP: %v", err)
			} else {
				log.Printf("Public IP detected: %s", publicIP)

				// Lookup geolocation using public IP
				geoData, err := c.getGeolocationFromDB(geoIPPath)
				if err != nil {
					log.Printf("Warning: Failed to get geolocation from database: %v", err)
					log.Printf("Continuing without geolocation data")
				} else {
					latitude = geoData["latitude"].(float64)
					longitude = geoData["longitude"].(float64)
					country = geoData["country"].(string)
					city = geoData["city"].(string)
					log.Printf("GeoIP database lookup: City=%s, Country=%s, Coords=(%.4f, %.4f)",
						city, country, latitude, longitude)
				}
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

func (c *MetricsCollector) collectMetrics() {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		// CPU per-core usage
		perCore, err := cpu.Percent(0, true)
		if err == nil && len(perCore) > 0 {
			c.cpuSamples[c.sampleIndex] = perCore
		}

		// Memory usage
		memInfo, err := mem.VirtualMemory()
		if err == nil {
			c.memorySamples[c.sampleIndex] = float64(memInfo.Used) / 1024 / 1024 / 1024
		}

		// Disk usage
		diskInfo, err := disk.Usage(c.config.DiskPath)
		if err == nil {
			c.diskSamples[c.sampleIndex] = float64(diskInfo.Used) / 1024 / 1024 / 1024
		}

		// GPU metrics (if available)
		if c.gpuCollector.IsEnabled() {
			if err := c.gpuCollector.CollectSample(); err != nil {
				LogWarn(fmt.Sprintf("Failed to collect GPU sample: %v", err))
			}
		}

		c.sampleIndex = (c.sampleIndex + 1) % c.maxSamples
	}
}

func (c *MetricsCollector) reportStats() error {
	// Calculate averages over the window
	cpuCoreAvg := c.calculateCPUAverages()
	memoryUsed := c.calculateAverage(c.memorySamples)
	diskUsed := c.calculateAverage(c.diskSamples)

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

func (c *MetricsCollector) getPublicIP() (string, error) {
	// Use a simple, reliable service with high rate limits
	resp, err := c.httpClient.Get("https://api.ipify.org")
	if err != nil {
		return "", fmt.Errorf("failed to fetch public IP: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("API returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response: %w", err)
	}

	return string(body), nil
}

func (c *MetricsCollector) getGeolocationFromDB(dbPath string) (map[string]interface{}, error) {
	// Get public IP for lookup
	publicIP, err := c.getPublicIP()
	if err != nil {
		return nil, fmt.Errorf("failed to get public IP: %w", err)
	}

	db, err := geoip2.Open(dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open GeoIP database: %w", err)
	}
	defer db.Close()

	ip := net.ParseIP(publicIP)
	if ip == nil {
		return nil, fmt.Errorf("invalid IP address: %s", publicIP)
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
		"ip":        publicIP,
		"latitude":  record.Location.Latitude,
		"longitude": record.Location.Longitude,
		"country":   record.Country.IsoCode,
		"city":      cityName,
	}, nil
}

func (c *MetricsCollector) downloadGeoIPDatabase(targetPath string) error {
	downloadURL := "https://cyqle-opsen.s3.us-east-2.amazonaws.com/GeoLite2-City.mmdb"

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
