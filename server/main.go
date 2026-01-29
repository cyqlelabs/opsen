package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"math"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"github.com/oschwald/geoip2-golang"
	"cyqle.in/opsen/common"
)

type Server struct {
	db                    *sql.DB
	mu                    sync.RWMutex
	clientCache           map[string]*ClientState
	stickyAssignments     map[string]map[string]string // sticky_id → (tier → client_id)
	pendingAllocations    map[string][]PendingAllocation // client_id → pending allocations
	stickyHeader          string                        // Header name to use for stickiness
	stickyAffinityEnabled bool                          // Whether to prefer same server across tiers
	staleTimeout          time.Duration
	cleanupInterval       time.Duration
	proxyEndpoints        []string                   // Endpoint prefixes to proxy
	geoIPDBPath           string
	tierSpecs             map[string]common.TierSpec // Tier name -> resource requirements
	config                *common.ServerConfig        // Full server configuration
}

type ClientState struct {
	Registration common.ClientRegistration
	Stats        common.ResourceStats
	LastSeen     time.Time
	Endpoint     string // e.g., "http://148.227.74.48:11000"

	// Health check state
	HealthStatus         string    // "healthy", "unhealthy", "unknown"
	LatencyMs            float64   // EWMA of probe latency in milliseconds
	LastHealthCheck      time.Time // When last health check was performed
	ConsecutiveFailures  int       // Consecutive health check failures
	ConsecutiveSuccesses int       // Consecutive health check successes
}

// PendingAllocation tracks resources reserved for in-flight session creations
type PendingAllocation struct {
	StickyID  string             // Identifier from sticky header (for deduplication)
	Tier      string             // Tier name
	TierSpec  common.TierSpec    // Resource requirements
	Timestamp time.Time          // When allocation was made
	RequestID string             // Unique request identifier (for logging)
}

func main() {
	configFile := flag.String("config", "", "Path to YAML configuration file")
	port := flag.Int("port", 0, "Server port")
	dbPath := flag.String("db", "", "SQLite database path")
	staleMinutes := flag.Int("stale", 0, "Client stale timeout (minutes)")
	cleanupInterval := flag.Int("cleanup-interval", 0, "Cleanup interval (seconds)")
	host := flag.String("host", "", "Host to bind to")
	flag.Parse()

	// Load configuration from YAML file
	yamlConfig, err := common.LoadServerConfig(*configFile)
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	// Override with command-line flags if provided
	if *port > 0 {
		yamlConfig.Port = *port
	}
	if *dbPath != "" {
		yamlConfig.Database = *dbPath
	}
	if *staleMinutes > 0 {
		yamlConfig.StaleMinutes = *staleMinutes
	}
	if *cleanupInterval > 0 {
		yamlConfig.CleanupIntervalSecs = *cleanupInterval
	}
	if *host != "" {
		yamlConfig.Host = *host
	}

	// Initialize logger
	InitLogger(yamlConfig.LogLevel, yamlConfig.JSONLogging, "lb-server")
	LogInfo("Load balancer server initializing...")

	// Initialize database with connection pooling
	db, err := initDatabase(yamlConfig.Database)
	if err != nil {
		LogFatal(fmt.Sprintf("Failed to initialize database: %v", err))
	}
	defer db.Close()

	// Configure database connection pool
	db.SetMaxOpenConns(yamlConfig.DBMaxOpenConns)
	db.SetMaxIdleConns(yamlConfig.DBMaxIdleConns)
	db.SetConnMaxLifetime(time.Duration(yamlConfig.DBConnMaxLifetime) * time.Second)
	LogInfoWithData("Database connection pool configured", map[string]interface{}{
		"max_open_conns":    yamlConfig.DBMaxOpenConns,
		"max_idle_conns":    yamlConfig.DBMaxIdleConns,
		"conn_max_lifetime": yamlConfig.DBConnMaxLifetime,
	})

	// Build tier specs map from config
	tierSpecs := make(map[string]common.TierSpec)
	for _, tier := range yamlConfig.Tiers {
		tierSpecs[tier.Name] = tier
	}

	server := &Server{
		db:                    db,
		clientCache:           make(map[string]*ClientState),
		stickyAssignments:     make(map[string]map[string]string),
		pendingAllocations:    make(map[string][]PendingAllocation),
		stickyHeader:          yamlConfig.StickyHeader,
		stickyAffinityEnabled: yamlConfig.StickyAffinityEnabled,
		staleTimeout:          time.Duration(yamlConfig.StaleMinutes) * time.Minute,
		cleanupInterval:       time.Duration(yamlConfig.CleanupIntervalSecs) * time.Second,
		proxyEndpoints:        yamlConfig.ProxyEndpoints,
		geoIPDBPath:           yamlConfig.GeoIPDBPath,
		tierSpecs:             tierSpecs,
		config:                yamlConfig,
	}

	LogInfoWithData("Loaded tier specifications", map[string]interface{}{
		"count": len(tierSpecs),
	})
	for name, spec := range tierSpecs {
		LogInfoWithData(fmt.Sprintf("Tier: %s", name), map[string]interface{}{
			"vcpu":       spec.VCPU,
			"memory_gb":  spec.MemoryGB,
			"storage_gb": spec.StorageGB,
		})
	}

	// Log sticky session configuration
	LogInfoWithData("Sticky session configuration", map[string]interface{}{
		"enabled":          yamlConfig.StickyHeader != "",
		"header":           yamlConfig.StickyHeader,
		"affinity_enabled": yamlConfig.StickyAffinityEnabled,
	})

	// Load existing clients from database
	if err := server.loadClients(); err != nil {
		LogWarn(fmt.Sprintf("Failed to load clients from database: %v", err))
	}

	// Load sticky assignments if sticky sessions enabled
	if yamlConfig.StickyHeader != "" {
		if err := server.loadStickyAssignments(); err != nil {
			LogWarn(fmt.Sprintf("Failed to load sticky assignments: %v", err))
		}
	}

	// Create context for graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start cleanup goroutine
	go server.cleanupStaleClients(ctx)

	// Start health check goroutine
	go server.runHealthChecks(ctx)

	// Initialize middlewares
	rateLimiter := NewRateLimiter(yamlConfig.RateLimitPerMinute, yamlConfig.RateLimitBurst)
	apiKeyAuth := NewAPIKeyAuth(yamlConfig.ServerKey, yamlConfig.APIKeys)
	ipWhitelist := NewIPWhitelist(yamlConfig.WhitelistedIPs)
	inputValidator := &InputValidator{}

	LogInfoWithData("Security configuration", map[string]interface{}{
		"server_key_enabled":   yamlConfig.ServerKey != "",
		"api_keys_enabled":     len(yamlConfig.APIKeys) > 0,
		"ip_whitelist_enabled": len(yamlConfig.WhitelistedIPs) > 0,
		"rate_limit_per_min":   yamlConfig.RateLimitPerMinute,
		"rate_limit_burst":     yamlConfig.RateLimitBurst,
		"max_request_bytes":    yamlConfig.MaxRequestBodyBytes,
		"request_timeout":      yamlConfig.RequestTimeout,
		"cors_enabled":         yamlConfig.EnableCORS,
	})

	// Build middleware chain
	mux := http.NewServeMux()

	// Register management endpoints
	LogInfo("Registering load balancer management endpoints:")
	LogInfo("  - /register (client registration)")
	LogInfo("  - /stats (metrics reporting)")
	LogInfo("  - /route (routing decisions)")
	LogInfo("  - /health (health checks)")
	LogInfo("  - /clients (list active clients)")
	LogInfo("  - /clients/purge (purge stale clients)")

	// Management endpoint middlewares (require auth if configured)
	managementMiddlewares := []func(http.Handler) http.Handler{
		PanicRecovery,
		SecurityHeaders,
		RequestLogger,
		RequestSizeLimit(yamlConfig.MaxRequestBodyBytes),
		Timeout(time.Duration(yamlConfig.RequestTimeout) * time.Second),
		inputValidator.Middleware,
		apiKeyAuth.Middleware,
		ipWhitelist.Middleware,
		rateLimiter.Middleware,
	}

	// Proxy endpoint middlewares (NO auth - these are for end users)
	proxyMiddlewares := []func(http.Handler) http.Handler{
		PanicRecovery,
		SecurityHeaders,
		RequestLogger,
		RequestSizeLimit(yamlConfig.MaxRequestBodyBytes),
		Timeout(time.Duration(yamlConfig.RequestTimeout) * time.Second),
		rateLimiter.Middleware,
	}

	// Add CORS if enabled
	if yamlConfig.EnableCORS {
		corsConfig := CORSConfig{
			AllowedOrigins: yamlConfig.CORSAllowedOrigins,
			AllowedMethods: []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
			AllowedHeaders: []string{"Content-Type", "X-API-Key", "Authorization"},
		}
		managementMiddlewares = append([]func(http.Handler) http.Handler{CORS(corsConfig)}, managementMiddlewares...)
		proxyMiddlewares = append([]func(http.Handler) http.Handler{CORS(corsConfig)}, proxyMiddlewares...)
	}

	mux.Handle("/register", ChainMiddleware(http.HandlerFunc(server.handleRegister), managementMiddlewares...))
	mux.Handle("/stats", ChainMiddleware(http.HandlerFunc(server.handleStats), managementMiddlewares...))
	mux.Handle("/route", ChainMiddleware(http.HandlerFunc(server.handleRoute), managementMiddlewares...))
	mux.Handle("/clients", ChainMiddleware(http.HandlerFunc(server.handleListClients), managementMiddlewares...))
	mux.Handle("/clients/purge", ChainMiddleware(http.HandlerFunc(server.handlePurgeStaleClients), managementMiddlewares...))
	mux.Handle("/clients/purge-pending", ChainMiddleware(http.HandlerFunc(server.handlePurgePendingAllocations), managementMiddlewares...))

	// Health check - minimal middleware (no auth, no rate limiting)
	healthMiddlewares := []func(http.Handler) http.Handler{
		PanicRecovery,
		SecurityHeaders,
	}
	mux.Handle("/health", ChainMiddleware(http.HandlerFunc(server.handleHealth), healthMiddlewares...))

	// Register proxy handlers with prefix matching
	if len(server.proxyEndpoints) > 0 {
		LogInfo("Registering proxy endpoint prefix(es):")
		hasWildcard := false
		for _, prefix := range server.proxyEndpoints {
			if prefix == "/" || prefix == "*" {
				hasWildcard = true
				LogWarn(fmt.Sprintf("  - %s (matches ALL paths, management endpoints excluded)", prefix))
			} else {
				LogInfo(fmt.Sprintf("  - %s (matches %s*)", prefix, prefix))
			}
		}

		if hasWildcard {
			LogWarn("Wildcard proxy enabled - all non-management paths will be proxied to backends")
		}

		mux.Handle("/", ChainMiddleware(http.HandlerFunc(server.handleProxyOrNotFound), proxyMiddlewares...))
	}

	addr := fmt.Sprintf("%s:%d", yamlConfig.Host, yamlConfig.Port)

	// Create HTTP server with security timeouts
	httpServer := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadTimeout:       time.Duration(yamlConfig.RequestTimeout) * time.Second,
		WriteTimeout:      time.Duration(yamlConfig.RequestTimeout) * time.Second,
		IdleTimeout:       120 * time.Second,
		ReadHeaderTimeout: time.Duration(yamlConfig.ReadHeaderTimeout) * time.Second,
		MaxHeaderBytes:    1 << 20, // 1MB
	}

	// Handle graceful shutdown
	go func() {
		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
		<-sigChan

		LogInfo("Shutdown signal received, initiating graceful shutdown...")

		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(),
			time.Duration(yamlConfig.ShutdownTimeout)*time.Second)
		defer shutdownCancel()

		if err := httpServer.Shutdown(shutdownCtx); err != nil {
			LogError(fmt.Sprintf("Server shutdown error: %v", err))
		}

		cancel() // Cancel cleanup goroutine context
		LogInfo("Server stopped")
	}()

	LogInfoWithData("Load balancer server starting", map[string]interface{}{
		"address":          addr,
		"database":         yamlConfig.Database,
		"stale_timeout":    fmt.Sprintf("%dm", yamlConfig.StaleMinutes),
		"cleanup_interval": fmt.Sprintf("%ds", yamlConfig.CleanupIntervalSecs),
		"tls_enabled":      yamlConfig.TLSCertFile != "" && yamlConfig.TLSKeyFile != "",
	})

	// Start server with TLS if configured
	if yamlConfig.TLSCertFile != "" && yamlConfig.TLSKeyFile != "" {
		LogInfo(fmt.Sprintf("Starting HTTPS server with TLS cert: %s", yamlConfig.TLSCertFile))
		if err := httpServer.ListenAndServeTLS(yamlConfig.TLSCertFile, yamlConfig.TLSKeyFile); err != nil && err != http.ErrServerClosed {
			LogFatal(fmt.Sprintf("Server error: %v", err))
		}
	} else {
		LogWarn("Starting HTTP server (TLS not configured)")
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			LogFatal(fmt.Sprintf("Server error: %v", err))
		}
	}
}

func initDatabase(dbPath string) (*sql.DB, error) {
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return nil, err
	}

	schema := `
	CREATE TABLE IF NOT EXISTS clients (
		client_id TEXT PRIMARY KEY,
		hostname TEXT,
		public_ip TEXT,
		local_ip TEXT,
		latitude REAL,
		longitude REAL,
		country TEXT,
		city TEXT,
		total_cpu INTEGER,
		total_memory REAL,
		total_storage REAL,
		total_gpus INTEGER DEFAULT 0,
		gpu_models TEXT,
		endpoint TEXT,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		last_seen TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS stats (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		client_id TEXT,
		timestamp TIMESTAMP,
		cpu_cores INTEGER,
		cpu_usage_json TEXT,
		memory_total REAL,
		memory_used REAL,
		memory_avail REAL,
		disk_total REAL,
		disk_used REAL,
		disk_avail REAL,
		gpu_stats_json TEXT,
		FOREIGN KEY (client_id) REFERENCES clients(client_id)
	);

	CREATE TABLE IF NOT EXISTS sticky_assignments (
		sticky_id TEXT NOT NULL,
		tier TEXT NOT NULL,
		client_id TEXT NOT NULL,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		last_used TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		PRIMARY KEY (sticky_id, tier),
		FOREIGN KEY (client_id) REFERENCES clients(client_id)
	);

	CREATE INDEX IF NOT EXISTS idx_stats_client_time ON stats(client_id, timestamp DESC);
	CREATE INDEX IF NOT EXISTS idx_clients_last_seen ON clients(last_seen);
	CREATE INDEX IF NOT EXISTS idx_sticky_last_used ON sticky_assignments(last_used);
	CREATE INDEX IF NOT EXISTS idx_sticky_client ON sticky_assignments(client_id);
	CREATE INDEX IF NOT EXISTS idx_sticky_id ON sticky_assignments(sticky_id);
	`

	_, err = db.Exec(schema)
	return db, err
}

func (s *Server) loadClients() error {
	rows, err := s.db.Query(`
		SELECT client_id, hostname, public_ip, local_ip, latitude, longitude, country, city,
		       total_cpu, total_memory, total_storage, total_gpus, gpu_models, endpoint, last_seen
		FROM clients
	`)
	if err != nil {
		return err
	}
	defer rows.Close()

	s.mu.Lock()
	defer s.mu.Unlock()

	for rows.Next() {
		var state ClientState
		var lastSeen string
		var localIP sql.NullString
		var gpuModelsJSON sql.NullString

		err := rows.Scan(
			&state.Registration.ClientID,
			&state.Registration.Hostname,
			&state.Registration.PublicIP,
			&localIP,
			&state.Registration.Latitude,
			&state.Registration.Longitude,
			&state.Registration.Country,
			&state.Registration.City,
			&state.Registration.TotalCPU,
			&state.Registration.TotalMemory,
			&state.Registration.TotalStorage,
			&state.Registration.TotalGPUs,
			&gpuModelsJSON,
			&state.Endpoint,
			&lastSeen,
		)
		if err != nil {
			log.Printf("Error scanning client row: %v", err)
			continue
		}

		if localIP.Valid {
			state.Registration.LocalIP = localIP.String
		}

		if gpuModelsJSON.Valid && gpuModelsJSON.String != "" {
			if err := json.Unmarshal([]byte(gpuModelsJSON.String), &state.Registration.GPUModels); err != nil {
				log.Printf("Warning: Failed to parse GPU models JSON for client %s: %v", state.Registration.ClientID, err)
			}
		}

		state.LastSeen, _ = time.Parse("2006-01-02 15:04:05", lastSeen)
		s.clientCache[state.Registration.ClientID] = &state
	}

	log.Printf("Loaded %d clients from database", len(s.clientCache))
	return nil
}

func (s *Server) handleRegister(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var reg common.ClientRegistration
	if err := json.NewDecoder(r.Body).Decode(&reg); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Determine endpoint: use override if provided, otherwise construct from local IP
	var endpoint string
	if reg.EndpointURL != "" {
		endpoint = reg.EndpointURL
	} else if reg.LocalIP != "" {
		endpoint = fmt.Sprintf("http://%s:11000", reg.LocalIP)
	} else {
		// Fallback to public IP if local IP not available
		endpoint = fmt.Sprintf("http://%s:11000", reg.PublicIP)
	}

	// Check for duplicate clients with same endpoint
	// (same machine can run multiple clients for different services/ports)
	s.mu.Lock()
	duplicateIDs := []string{}
	for id, client := range s.clientCache {
		if id != reg.ClientID && client.Endpoint == endpoint {
			duplicateIDs = append(duplicateIDs, id)
		}
	}

	// Remove duplicates from cache
	for _, id := range duplicateIDs {
		delete(s.clientCache, id)
		log.Printf("Removed duplicate client: %s (same endpoint=%s)", id, endpoint)
	}

	// Add/update current client
	s.clientCache[reg.ClientID] = &ClientState{
		Registration: reg,
		LastSeen:     time.Now(),
		Endpoint:     endpoint,
		HealthStatus: "unknown", // Will be updated by first health check
	}
	s.mu.Unlock()

	// Remove duplicates from database
	if len(duplicateIDs) > 0 {
		for _, id := range duplicateIDs {
			if _, err := s.db.Exec("DELETE FROM clients WHERE client_id = ?", id); err != nil {
				log.Printf("Warning: Failed to delete duplicate client: %v", err)
			}
			if _, err := s.db.Exec("DELETE FROM stats WHERE client_id = ?", id); err != nil {
				log.Printf("Warning: Failed to delete duplicate client stats: %v", err)
			}
		}
	}

	// Persist to database
	gpuModelsJSON, _ := json.Marshal(reg.GPUModels)
	_, err := s.db.Exec(`
		INSERT OR REPLACE INTO clients
		(client_id, hostname, public_ip, local_ip, latitude, longitude, country, city,
		 total_cpu, total_memory, total_storage, total_gpus, gpu_models, endpoint, last_seen)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
	`, reg.ClientID, reg.Hostname, reg.PublicIP, reg.LocalIP, reg.Latitude, reg.Longitude,
		reg.Country, reg.City, reg.TotalCPU, reg.TotalMemory, reg.TotalStorage,
		reg.TotalGPUs, gpuModelsJSON, endpoint)

	if err != nil {
		log.Printf("Error persisting client registration: %v", err)
	}

	log.Printf("Client registered: %s (%s) PublicIP=%s LocalIP=%s (endpoint: %s)",
		reg.ClientID, reg.Hostname, reg.PublicIP, reg.LocalIP, endpoint)
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(map[string]string{"status": "registered"}); err != nil {
		log.Printf("Warning: Failed to encode registration response: %v", err)
	}
}

func (s *Server) handleStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var stats common.ResourceStats
	if err := json.NewDecoder(r.Body).Decode(&stats); err != nil {
		http.Error(w, fmt.Sprintf("Invalid JSON body: %v. Expected ResourceStats format.", err), http.StatusBadRequest)
		return
	}

	// Validate required fields
	if stats.ClientID == "" {
		http.Error(w, "Missing required field: client_id", http.StatusBadRequest)
		return
	}

	s.mu.Lock()
	if client, ok := s.clientCache[stats.ClientID]; ok {
		client.Stats = stats
		client.LastSeen = time.Now()
	}
	s.mu.Unlock()

	// Persist to database
	cpuJSON, _ := json.Marshal(stats.CPUUsageAvg)
	gpuJSON, _ := json.Marshal(stats.GPUs)
	_, err := s.db.Exec(`
		INSERT INTO stats
		(client_id, timestamp, cpu_cores, cpu_usage_json, memory_total, memory_used,
		 memory_avail, disk_total, disk_used, disk_avail, gpu_stats_json)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, stats.ClientID, stats.Timestamp, stats.CPUCores, cpuJSON,
		stats.MemoryTotal, stats.MemoryUsed, stats.MemoryAvail,
		stats.DiskTotal, stats.DiskUsed, stats.DiskAvail, gpuJSON)

	if err != nil {
		log.Printf("Error persisting stats: %v", err)
	}

	// Update last_seen in clients table
	if _, err := s.db.Exec("UPDATE clients SET last_seen = CURRENT_TIMESTAMP WHERE client_id = ?", stats.ClientID); err != nil {
		log.Printf("Warning: Failed to update client last_seen: %v", err)
	}

	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(map[string]string{"status": "received"}); err != nil {
		log.Printf("Warning: Failed to encode stats response: %v", err)
	}
}

func (s *Server) handleRoute(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req common.RoutingRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Get tier spec from configured tiers
	tierSpec, ok := s.tierSpecs[req.Tier]
	if !ok {
		http.Error(w, fmt.Sprintf("Unknown tier: %s", req.Tier), http.StatusBadRequest)
		return
	}

	// Extract sticky ID from configured header
	stickyID := ""
	if s.stickyHeader != "" {
		stickyID = r.Header.Get(s.stickyHeader)
	}

	// Resolve client coordinates
	clientLat := req.ClientLat
	clientLon := req.ClientLon

	// If coordinates not provided, lookup IP location
	if clientLat == 0 && clientLon == 0 && req.ClientIP != "" {
		log.Printf("Client coordinates not provided, attempting GeoIP lookup for IP: %s", req.ClientIP)
		clientLat, clientLon = s.lookupIPLocation(req.ClientIP)
		if clientLat != 0 || clientLon != 0 {
			log.Printf("Resolved IP %s to location: %.4f, %.4f", req.ClientIP, clientLat, clientLon)
		} else {
			log.Printf("GeoIP lookup failed for %s (returned 0,0) - distance will not be factored into routing", req.ClientIP)
		}
	}

	// Generate unique request ID for resource tracking
	requestID := fmt.Sprintf("%d-%s", time.Now().UnixNano(), stickyID)

	// Select client with stickiness support and resource reservation
	client := s.selectClientWithStickiness(stickyID, req.Tier, tierSpec, clientLat, clientLon, requestID)
	if client == nil {
		http.Error(w, "No available clients with sufficient resources", http.StatusServiceUnavailable)
		return
	}

	distance := 0.0
	if clientLat != 0 && clientLon != 0 &&
	   client.Registration.Latitude != 0 && client.Registration.Longitude != 0 {
		distance = haversineDistance(
			clientLat, clientLon,
			client.Registration.Latitude, client.Registration.Longitude,
		)
	}

	response := common.RoutingResponse{
		ClientID: client.Registration.ClientID,
		Endpoint: client.Endpoint,
		Hostname: client.Registration.Hostname,
		Distance: distance,
	}

	LogInfoWithData("Routed request", map[string]interface{}{
		"tier":      req.Tier,
		"client_id": client.Registration.ClientID,
		"hostname":  client.Registration.Hostname,
		"distance":  fmt.Sprintf("%.0f km", distance),
		"sticky_id": stickyID,
	})

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		log.Printf("Warning: Failed to encode routing response: %v", err)
	}
}

func (s *Server) findBestClient(tier common.TierSpec, clientLat, clientLon float64) *ClientState {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var bestClient *ClientState
	bestScore := math.Inf(1)

	for _, client := range s.clientCache {
		// Skip stale clients
		if time.Since(client.LastSeen) > s.staleTimeout {
			continue
		}

		// Skip unhealthy clients (if health checks enabled)
		if s.config.HealthCheckEnabled && client.HealthStatus == "unhealthy" {
			continue
		}

		// Check if client has sufficient resources (lock already held)
		if !s.hasResourcesLocked(client, tier) {
			continue
		}

		// Calculate score (lower is better)
		// Score = distance_km + (cpu_usage_penalty * 100) + (memory_usage_penalty * 100)
		distance := 0.0
		// Only calculate distance if both client and backend have valid geolocation
		if clientLat != 0 && clientLon != 0 &&
		   client.Registration.Latitude != 0 && client.Registration.Longitude != 0 {
			distance = haversineDistance(
				clientLat, clientLon,
				client.Registration.Latitude, client.Registration.Longitude,
			)
		}

		// Calculate CPU usage for the cores that would be allocated
		// Use the N least-loaded cores (where N = tier.VCPU)
		avgCPU := s.calculateAllocatedCoresUsage(client.Stats.CPUUsageAvg, tier.VCPU)

		memoryUsagePct := (client.Stats.MemoryUsed / client.Stats.MemoryTotal) * 100

		// Calculate GPU utilization if tier requires GPUs
		gpuUtilPct := 0.0
		if tier.GPU > 0 && len(client.Stats.GPUs) > 0 {
			totalUtil := 0.0
			for _, gpu := range client.Stats.GPUs {
				totalUtil += gpu.UtilizationPct
			}
			gpuUtilPct = totalUtil / float64(len(client.Stats.GPUs))
		}

		// Weighted score: distance (km) + CPU penalty + memory penalty + GPU penalty + latency
		// GPU gets higher weight (1.5) as GPU workloads are more sensitive to contention
		// Latency adds milliseconds directly to score (e.g., 50ms latency = +50 to score)
		score := distance + (avgCPU * 1.0) + (memoryUsagePct * 1.0) + (gpuUtilPct * 1.5) + client.LatencyMs

		if score < bestScore {
			bestScore = score
			bestClient = client
		}
	}

	return bestClient
}

func (s *Server) hasResources(client *ClientState, tier common.TierSpec) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.hasResourcesLocked(client, tier)
}

// hasResourcesLocked checks resource availability with lock already held
func (s *Server) hasResourcesLocked(client *ClientState, tier common.TierSpec) bool {
	// Calculate pending resource reservations for this client
	pendingList := s.pendingAllocations[client.Registration.ClientID]

	// Sum up pending allocations
	var pendingVCPU int
	var pendingMemoryGB float64
	var pendingStorageGB int
	var pendingGPUs int
	var pendingGPUMemoryGB float64

	for _, pending := range pendingList {
		pendingVCPU += pending.TierSpec.VCPU
		pendingMemoryGB += pending.TierSpec.MemoryGB
		pendingStorageGB += pending.TierSpec.StorageGB
		pendingGPUs += pending.TierSpec.GPU
		pendingGPUMemoryGB += pending.TierSpec.GPUMemoryGB
	}

	// Count available CPU cores (cores with <80% usage)
	availableCores := 0
	for _, usage := range client.Stats.CPUUsageAvg {
		if usage < 80.0 {
			availableCores++
		}
	}

	// Subtract pending CPU allocations
	availableCores -= pendingVCPU

	// Check CPU availability
	if availableCores < tier.VCPU {
		return false
	}

	// Check memory availability (account for pending allocations)
	availableMemory := client.Stats.MemoryAvail - pendingMemoryGB
	if availableMemory < tier.MemoryGB {
		return false
	}

	// Check disk availability (account for pending allocations)
	availableDisk := client.Stats.DiskAvail - float64(pendingStorageGB)
	if availableDisk < float64(tier.StorageGB) {
		return false
	}

	// Check GPU availability if tier requires GPUs
	if tier.GPU > 0 {
		// Check GPU count
		availableGPUs := client.Registration.TotalGPUs - pendingGPUs
		if availableGPUs < tier.GPU {
			return false
		}

		// Check GPU memory if specified
		if tier.GPUMemoryGB > 0 && len(client.Stats.GPUs) > 0 {
			totalAvailableVRAM := 0.0
			for _, gpu := range client.Stats.GPUs {
				availableVRAM := gpu.MemoryTotalGB - gpu.MemoryUsedGB
				totalAvailableVRAM += availableVRAM
			}
			totalAvailableVRAM -= pendingGPUMemoryGB

			if totalAvailableVRAM < tier.GPUMemoryGB {
				return false
			}
		}
	}

	return true
}

// calculateAllocatedCoresUsage calculates the average CPU usage of the N least-loaded cores
// This represents the actual load the new session would experience
func (s *Server) calculateAllocatedCoresUsage(cpuUsageAvg []float64, vcpuRequired int) float64 {
	if len(cpuUsageAvg) == 0 {
		return 0.0
	}

	// If requesting more cores than available, use all cores
	if vcpuRequired >= len(cpuUsageAvg) {
		sum := 0.0
		for _, usage := range cpuUsageAvg {
			sum += usage
		}
		return sum / float64(len(cpuUsageAvg))
	}

	// Copy and sort cores by usage (ascending)
	sortedCores := make([]float64, len(cpuUsageAvg))
	copy(sortedCores, cpuUsageAvg)

	// Simple bubble sort (efficient for small arrays)
	for i := 0; i < len(sortedCores); i++ {
		for j := i + 1; j < len(sortedCores); j++ {
			if sortedCores[i] > sortedCores[j] {
				sortedCores[i], sortedCores[j] = sortedCores[j], sortedCores[i]
			}
		}
	}

	// Calculate average of the N least-loaded cores
	sum := 0.0
	for i := 0; i < vcpuRequired; i++ {
		sum += sortedCores[i]
	}

	return sum / float64(vcpuRequired)
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	totalClients := len(s.clientCache)
	activeClients := 0
	for _, client := range s.clientCache {
		if time.Since(client.LastSeen) <= s.staleTimeout {
			activeClients++
		}
	}
	s.mu.RUnlock()

	response := common.HealthCheckResponse{
		Status:        "ok",
		Timestamp:     time.Now(),
		TotalClients:  totalClients,
		ActiveClients: activeClients,
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		log.Printf("Warning: Failed to encode health response: %v", err)
	}
}

func (s *Server) handleListClients(w http.ResponseWriter, r *http.Request) {
	// Check for active_only query parameter
	activeOnly := r.URL.Query().Get("active_only") == "true"

	s.mu.RLock()
	clients := make([]map[string]interface{}, 0, len(s.clientCache))
	for _, client := range s.clientCache {
		isActive := time.Since(client.LastSeen) <= s.staleTimeout

		// Skip inactive clients if active_only is set
		if activeOnly && !isActive {
			continue
		}

		// Format per-core CPU usage
		cpuUsage := make([]string, len(client.Stats.CPUUsageAvg))
		for i, usage := range client.Stats.CPUUsageAvg {
			cpuUsage[i] = fmt.Sprintf("%.1f%%", usage)
		}

		// Format GPU stats if available
		gpuStats := make([]map[string]interface{}, 0, len(client.Stats.GPUs))
		for _, gpu := range client.Stats.GPUs {
			gpuInfo := map[string]interface{}{
				"device_id":    gpu.DeviceID,
				"name":         gpu.Name,
				"utilization":  fmt.Sprintf("%.1f%%", gpu.UtilizationPct),
				"vram":         fmt.Sprintf("%.1f/%.1f GB", gpu.MemoryUsedGB, gpu.MemoryTotalGB),
				"temperature":  fmt.Sprintf("%.0f°C", gpu.TemperatureC),
			}
			if gpu.PowerDrawW > 0 {
				gpuInfo["power_draw"] = fmt.Sprintf("%.0fW", gpu.PowerDrawW)
			}
			gpuStats = append(gpuStats, gpuInfo)
		}

		clientInfo := map[string]interface{}{
			"client_id":        client.Registration.ClientID,
			"hostname":         client.Registration.Hostname,
			"endpoint":         client.Endpoint,
			"location":         fmt.Sprintf("%s, %s", client.Registration.City, client.Registration.Country),
			"cpu_cores":        client.Stats.CPUCores,
			"cpu_usage":        cpuUsage, // Per-core usage percentages
			"memory_gb":        fmt.Sprintf("%.1f/%.1f", client.Stats.MemoryUsed, client.Stats.MemoryTotal),
			"disk_gb":          fmt.Sprintf("%.1f/%.1f", client.Stats.DiskUsed, client.Stats.DiskTotal),
			"last_seen":        client.LastSeen.Format(time.RFC3339),
			"is_active":        isActive,
			"health_status":    client.HealthStatus,
			"latency_ms":       fmt.Sprintf("%.1f", client.LatencyMs),
			"last_health_check": client.LastHealthCheck.Format(time.RFC3339),
		}

		// Add GPU fields if client has GPUs
		if client.Registration.TotalGPUs > 0 {
			clientInfo["total_gpus"] = client.Registration.TotalGPUs
			clientInfo["gpu_models"] = client.Registration.GPUModels
			if len(gpuStats) > 0 {
				clientInfo["gpu_stats"] = gpuStats
			}
		}

		clients = append(clients, clientInfo)
	}
	s.mu.RUnlock()

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(clients); err != nil {
		log.Printf("Warning: Failed to encode clients list response: %v", err)
	}
}

func (s *Server) handlePurgeStaleClients(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost && r.Method != http.MethodDelete {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	s.mu.Lock()
	purged := 0
	staleIDs := []string{}

	// Find and remove stale clients from cache
	for id, client := range s.clientCache {
		if time.Since(client.LastSeen) > s.staleTimeout {
			staleIDs = append(staleIDs, id)
			delete(s.clientCache, id)
			purged++
		}
	}
	s.mu.Unlock()

	// Remove from database
	for _, id := range staleIDs {
		if _, err := s.db.Exec("DELETE FROM clients WHERE client_id = ?", id); err != nil {
			log.Printf("Warning: Failed to delete stale client: %v", err)
		}
		if _, err := s.db.Exec("DELETE FROM stats WHERE client_id = ?", id); err != nil {
			log.Printf("Warning: Failed to delete stale client stats: %v", err)
		}
	}

	// Also purge invalid/old clients
	result, err := s.db.Exec(`
		DELETE FROM clients
		WHERE last_seen IS NULL
		   OR last_seen = ''
		   OR last_seen < datetime('now', '-30 days')
	`)

	dbPurged := int64(0)
	if err == nil {
		dbPurged, _ = result.RowsAffected()
	}

	totalPurged := purged + int(dbPurged)

	log.Printf("Purged %d stale clients (%d from cache, %d from database)", totalPurged, purged, dbPurged)

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]interface{}{
		"status":        "success",
		"purged":        totalPurged,
		"cache_purged":  purged,
		"db_purged":     dbPurged,
		"timestamp":     time.Now(),
	}); err != nil {
		log.Printf("Warning: Failed to encode purge response: %v", err)
	}
}

func (s *Server) handlePurgePendingAllocations(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost && r.Method != http.MethodDelete {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	s.mu.Lock()
	totalRemoved := 0
	for clientID, allocations := range s.pendingAllocations {
		totalRemoved += len(allocations)
		delete(s.pendingAllocations, clientID)
	}
	s.mu.Unlock()

	LogInfoWithData("Manually purged all pending allocations", map[string]interface{}{
		"removed": totalRemoved,
	})

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]interface{}{
		"status":    "success",
		"purged":    totalRemoved,
		"timestamp": time.Now(),
	}); err != nil {
		log.Printf("Warning: Failed to encode purge pending response: %v", err)
	}
}

func (s *Server) cleanupStaleClients(ctx context.Context) {
	ticker := time.NewTicker(s.cleanupInterval)
	defer ticker.Stop()

	LogInfoWithData("Cleanup goroutine started", map[string]interface{}{
		"interval":         s.cleanupInterval.String(),
		"stale_threshold":  (s.staleTimeout * 3).String(),
	})

	for {
		select {
		case <-ctx.Done():
			LogInfo("Cleanup goroutine stopping...")
			return
		case <-ticker.C:
			s.mu.Lock()
			staleIDs := []string{}
			for id, client := range s.clientCache {
				// Remove from cache if stale for 3x the timeout period
				if time.Since(client.LastSeen) > s.staleTimeout*3 {
					staleIDs = append(staleIDs, id)
					delete(s.clientCache, id)
				}
			}
			s.mu.Unlock()

			// Also remove from database
			if len(staleIDs) > 0 {
				for _, id := range staleIDs {
					if _, err := s.db.Exec("DELETE FROM clients WHERE client_id = ?", id); err != nil {
						log.Printf("Warning: Failed to delete stale client: %v", err)
					}
					if _, err := s.db.Exec("DELETE FROM stats WHERE client_id = ?", id); err != nil {
						log.Printf("Warning: Failed to delete stale client stats: %v", err)
					}
					LogInfoWithData("Purged stale client from database", map[string]interface{}{
						"client_id": id,
					})
				}
			}

			// Purge clients with invalid timestamps (zero value)
			s.purgeInvalidClients()

			// Cleanup stale pending allocations
			s.cleanupStalePendingAllocations()
		}
	}
}

func (s *Server) purgeInvalidClients() {
	// Remove clients with zero timestamp or very old data
	result, err := s.db.Exec(`
		DELETE FROM clients
		WHERE last_seen IS NULL
		   OR last_seen = ''
		   OR last_seen < datetime('now', '-30 days')
	`)
	if err != nil {
		log.Printf("Error purging invalid clients: %v", err)
		return
	}

	affected, _ := result.RowsAffected()
	if affected > 0 {
		log.Printf("Purged %d invalid/old clients from database", affected)
	}
}

// handleProxyOrNotFound checks if the request path matches any proxy prefix
// If yes, proxies the request. If no, returns 404.
// This is registered as a catch-all "/" handler and runs AFTER specific handlers.
func (s *Server) handleProxyOrNotFound(w http.ResponseWriter, r *http.Request) {
	// Check if path matches any configured proxy prefix
	for _, prefix := range s.proxyEndpoints {
		// Wildcard: match all paths
		if prefix == "/" || prefix == "*" {
			s.handleProxy(w, r)
			return
		}

		// Prefix match
		if strings.HasPrefix(r.URL.Path, prefix) {
			s.handleProxy(w, r)
			return
		}
	}

	// No matching prefix found
	http.NotFound(w, r)
}

func (s *Server) handleProxy(w http.ResponseWriter, r *http.Request) {
	// Read the request body
	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Failed to read request body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	// Extract sticky ID from configured header
	stickyID := ""
	if s.stickyHeader != "" {
		stickyID = r.Header.Get(s.stickyHeader)
	}

	// Extract routing parameters from JSON body (if provided)
	var tier string
	var clientLat, clientLon float64

	// Try to parse JSON body, but allow empty or non-JSON bodies
	if len(bodyBytes) > 0 {
		var payload map[string]interface{}
		if err := json.Unmarshal(bodyBytes, &payload); err == nil {
			// Successfully parsed JSON - use configured field name
			tier, _ = payload[s.config.TierFieldName].(string)
			clientLat, _ = payload["client_lat"].(float64)
			clientLon, _ = payload["client_lon"].(float64)
		}
		// If JSON parsing fails, continue with defaults (no error)
	}

	// Extract client IP from headers or connection
	clientIP := r.Header.Get("X-Forwarded-For")
	if clientIP == "" {
		clientIP = r.Header.Get("X-Real-IP")
	}
	if clientIP == "" {
		clientIP = r.RemoteAddr
	}

	// Use tier from query parameter if not in body
	if tier == "" {
		tier = r.URL.Query().Get(s.config.TierFieldName)
	}

	// Use tier from header if not in body or query
	if tier == "" {
		tier = r.Header.Get(s.config.TierHeader)
	}

	// Default tier if still not specified
	if tier == "" {
		tier = "lite"
	}

	// Get tier spec from configured tiers
	tierSpec, ok := s.tierSpecs[tier]
	if !ok {
		http.Error(w, fmt.Sprintf("Unknown tier: %s", tier), http.StatusBadRequest)
		return
	}

	// Generate unique request ID for resource tracking
	requestID := fmt.Sprintf("%d-%s", time.Now().UnixNano(), stickyID)

	// Select client with stickiness support and resource reservation
	client := s.selectClientWithStickiness(stickyID, tier, tierSpec, clientLat, clientLon, requestID)
	if client == nil {
		http.Error(w, "No available backends with sufficient resources", http.StatusServiceUnavailable)
		return
	}

	// Parse backend URL from endpoint
	targetURL, err := url.Parse(client.Endpoint)
	if err != nil {
		log.Printf("Invalid backend URL: %s", client.Endpoint)
		http.Error(w, "Invalid backend configuration", http.StatusInternalServerError)
		return
	}

	// Create reverse proxy with SSE support
	proxy := &httputil.ReverseProxy{
		Director: func(req *http.Request) {
			req.URL.Scheme = targetURL.Scheme
			req.URL.Host = targetURL.Host
			req.Host = targetURL.Host
			req.URL.Path = r.URL.Path        // Preserve original path
			req.URL.RawQuery = r.URL.RawQuery // Preserve query parameters

			// Restore the original request body
			req.Body = io.NopCloser(bytes.NewReader(bodyBytes))
			req.ContentLength = int64(len(bodyBytes))

			// Add headers to track routing
			req.Header.Set("X-LB-Client-ID", client.Registration.ClientID)
			req.Header.Set("X-LB-Hostname", client.Registration.Hostname)
			req.Header.Set("X-Forwarded-For", clientIP)
		},
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: s.config.TLSInsecureSkipVerify,
			},
		},
		// FlushInterval enables SSE/streaming support
		// -1 = flush immediately (best for SSE), 0 = no flush, >0 = flush at interval
		FlushInterval: time.Duration(s.config.ProxySSEFlushInterval) * time.Millisecond,
	}

	distance := 0.0
	if clientLat != 0 && clientLon != 0 {
		distance = haversineDistance(
			clientLat, clientLon,
			client.Registration.Latitude, client.Registration.Longitude,
		)
	}

	LogInfoWithData("Proxying request", map[string]interface{}{
		"method":     r.Method,
		"path":       r.URL.Path,
		"tier":       tier,
		"endpoint":   client.Endpoint,
		"distance":   fmt.Sprintf("%.0f km", distance),
		"sticky_id":  stickyID,
		"client_id":  client.Registration.ClientID,
	})

	// Proxy the request
	proxy.ServeHTTP(w, r)
}

// haversineDistance calculates the distance between two points on Earth (in km)
func haversineDistance(lat1, lon1, lat2, lon2 float64) float64 {
	const R = 6371 // Earth radius in kilometers

	dLat := (lat2 - lat1) * math.Pi / 180
	dLon := (lon2 - lon1) * math.Pi / 180

	lat1Rad := lat1 * math.Pi / 180
	lat2Rad := lat2 * math.Pi / 180

	a := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(lat1Rad)*math.Cos(lat2Rad)*
			math.Sin(dLon/2)*math.Sin(dLon/2)

	c := 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))

	return R * c
}

// lookupIPLocation performs GeoIP lookup for an IP address
// Returns latitude, longitude, or 0,0 if lookup fails or DB not configured
func (s *Server) lookupIPLocation(ipAddr string) (float64, float64) {
	// If no GeoIP database configured, return 0,0
	if s.geoIPDBPath == "" {
		log.Printf("GeoIP database not configured (geoip_db_path is empty)")
		return 0, 0
	}

	db, err := geoip2.Open(s.geoIPDBPath)
	if err != nil {
		log.Printf("Warning: Failed to open GeoIP database at %s: %v", s.geoIPDBPath, err)
		return 0, 0
	}
	defer db.Close()

	ip := net.ParseIP(ipAddr)
	if ip == nil {
		log.Printf("Warning: Invalid IP address: %s", ipAddr)
		return 0, 0
	}

	record, err := db.City(ip)
	if err != nil {
		log.Printf("Warning: Failed to lookup IP %s in database: %v", ipAddr, err)
		return 0, 0
	}

	log.Printf("GeoIP database lookup successful for %s: %.4f, %.4f (%s, %s)",
		ipAddr, record.Location.Latitude, record.Location.Longitude,
		record.City.Names["en"], record.Country.IsoCode)

	return record.Location.Latitude, record.Location.Longitude
}

// loadStickyAssignments loads sticky session mappings from database on startup
func (s *Server) loadStickyAssignments() error {
	rows, err := s.db.Query("SELECT sticky_id, tier, client_id FROM sticky_assignments")
	if err != nil {
		return err
	}
	defer rows.Close()

	s.mu.Lock()
	defer s.mu.Unlock()

	count := 0
	for rows.Next() {
		var stickyID, tier, clientID string
		if err := rows.Scan(&stickyID, &tier, &clientID); err != nil {
			continue
		}

		if s.stickyAssignments[stickyID] == nil {
			s.stickyAssignments[stickyID] = make(map[string]string)
		}
		s.stickyAssignments[stickyID][tier] = clientID
		count++
	}

	LogInfoWithData("Loaded sticky assignments", map[string]interface{}{
		"count":  count,
		"header": s.stickyHeader,
	})
	return nil
}

// selectClientWithStickiness implements sticky routing logic with fallback
// Includes resource reservation to prevent race conditions with concurrent requests
func (s *Server) selectClientWithStickiness(stickyID, tier string, tierSpec common.TierSpec,
	clientLat, clientLon float64, requestID string) *ClientState {

	// If no sticky header configured or no sticky ID provided, use standard routing
	if s.stickyHeader == "" || stickyID == "" {
		client := s.findBestClient(tierSpec, clientLat, clientLon)
		if client != nil {
			// Reserve resources even for non-sticky requests to prevent race conditions
			s.addPendingAllocation(client.Registration.ClientID, stickyID, tier, tierSpec, requestID)
		}
		return client
	}

	// Try to use existing assignment for this sticky_id + tier
	client := s.findStickyAssignment(stickyID, tier, tierSpec)
	if client != nil {
		LogInfoWithData("Using sticky assignment", map[string]interface{}{
			"sticky_id": stickyID,
			"tier":      tier,
			"client_id": client.Registration.ClientID,
		})
		return client
	}

	// No assignment or backend unavailable/overloaded
	// If affinity enabled, prefer servers where this sticky_id already has sessions
	var selectedClient *ClientState
	if s.stickyAffinityEnabled {
		selectedClient = s.findBestClientWithAffinity(stickyID, tierSpec, clientLat, clientLon)
	} else {
		selectedClient = s.findBestClient(tierSpec, clientLat, clientLon)
	}

	// If we found a client, create the sticky assignment and reserve resources
	if selectedClient != nil {
		// Create assignment (may return different client ID if another goroutine won the race)
		assignedClientID := s.createStickyAssignment(stickyID, tier, selectedClient.Registration.ClientID)

		// If another goroutine created a different assignment, use that instead
		if assignedClientID != selectedClient.Registration.ClientID {
			s.mu.RLock()
			selectedClient = s.clientCache[assignedClientID]
			s.mu.RUnlock()

			if selectedClient == nil {
				// Assigned client disappeared, clear assignment and retry would be ideal,
				// but for now just return nil to avoid routing to a dead client
				s.removeStickyAssignment(stickyID, tier)
				return nil
			}
		}

		s.addPendingAllocation(selectedClient.Registration.ClientID, stickyID, tier, tierSpec, requestID)

		LogInfoWithData("Created sticky assignment", map[string]interface{}{
			"sticky_id": stickyID,
			"tier":      tier,
			"client_id": selectedClient.Registration.ClientID,
		})
	}

	return selectedClient
}

// findStickyAssignment checks if sticky_id+tier has an assigned backend with capacity
func (s *Server) findStickyAssignment(stickyID, tier string, tierSpec common.TierSpec) *ClientState {
	s.mu.RLock()
	tierMap, exists := s.stickyAssignments[stickyID]
	if !exists {
		s.mu.RUnlock()
		return nil
	}

	clientID, exists := tierMap[tier]
	s.mu.RUnlock()

	if !exists {
		return nil
	}

	s.mu.RLock()
	client, exists := s.clientCache[clientID]
	s.mu.RUnlock()

	if !exists || time.Since(client.LastSeen) > s.staleTimeout {
		// Backend offline/stale
		s.removeStickyAssignment(stickyID, tier)
		LogWarn(fmt.Sprintf("Sticky assignment stale: sticky_id=%s tier=%s client=%s",
			stickyID, tier, clientID))
		return nil
	}

	// Check if backend is unhealthy
	if s.config.HealthCheckEnabled && client.HealthStatus == "unhealthy" {
		LogWarn(fmt.Sprintf("Sticky assignment backend unhealthy, will reassign: sticky_id=%s tier=%s client=%s",
			stickyID, tier, clientID))
		s.removeStickyAssignment(stickyID, tier)
		return nil
	}

	// Check if backend still has resources
	if !s.hasResources(client, tierSpec) {
		LogWarn(fmt.Sprintf("Sticky assignment backend overloaded, will reassign: sticky_id=%s tier=%s client=%s",
			stickyID, tier, clientID))
		s.removeStickyAssignment(stickyID, tier)
		return nil
	}

	// Update last_used timestamp
	if _, err := s.db.Exec(`UPDATE sticky_assignments SET last_used = CURRENT_TIMESTAMP
                WHERE sticky_id = ? AND tier = ?`, stickyID, tier); err != nil {
		log.Printf("Warning: Failed to update sticky assignment timestamp: %v", err)
	}

	return client
}

// findBestClientWithAffinity prefers servers where sticky_id already has sessions
// Falls back to standard selection if affinity server has no capacity
func (s *Server) findBestClientWithAffinity(stickyID string, tierSpec common.TierSpec,
	clientLat, clientLon float64) *ClientState {

	s.mu.RLock()
	tierMap, hasAssignments := s.stickyAssignments[stickyID]
	s.mu.RUnlock()

	// Check if sticky_id has existing assignments on any server
	if hasAssignments && len(tierMap) > 0 {
		// Try each server where this sticky_id has sessions
		for existingTier, assignedClientID := range tierMap {
			s.mu.RLock()
			client, exists := s.clientCache[assignedClientID]
			s.mu.RUnlock()

			if exists &&
				time.Since(client.LastSeen) <= s.staleTimeout &&
				(!s.config.HealthCheckEnabled || client.HealthStatus != "unhealthy") &&
				s.hasResources(client, tierSpec) {
				LogInfoWithData("Using affinity server", map[string]interface{}{
					"sticky_id":      stickyID,
					"existing_tier":  existingTier,
					"requested_tier": tierSpec.Name,
					"client_id":      assignedClientID,
				})
				return client
			}
		}
	}

	// No affinity server available with capacity, use standard selection
	LogInfoWithData("No affinity server available, using best available", map[string]interface{}{
		"sticky_id": stickyID,
		"tier":      tierSpec.Name,
	})
	return s.findBestClient(tierSpec, clientLat, clientLon)
}

// createStickyAssignment creates a new sticky_id+tier → backend mapping
// Returns the actual client ID assigned (which may differ if another goroutine won the race)
func (s *Server) createStickyAssignment(stickyID, tier, clientID string) string {
	s.mu.Lock()

	// Check if assignment already exists (race condition protection)
	if tierMap, exists := s.stickyAssignments[stickyID]; exists {
		if existingClientID, exists := tierMap[tier]; exists {
			// Another goroutine already created this assignment
			s.mu.Unlock()
			return existingClientID
		}
	}

	// Create new assignment
	if s.stickyAssignments[stickyID] == nil {
		s.stickyAssignments[stickyID] = make(map[string]string)
	}
	s.stickyAssignments[stickyID][tier] = clientID
	s.mu.Unlock()

	_, err := s.db.Exec(`
		INSERT OR REPLACE INTO sticky_assignments (sticky_id, tier, client_id, last_used)
		VALUES (?, ?, ?, CURRENT_TIMESTAMP)
	`, stickyID, tier, clientID)

	if err != nil {
		LogError(fmt.Sprintf("Failed to save sticky assignment: %v", err))
	}

	return clientID
}

// removeStickyAssignment clears a sticky_id+tier assignment
func (s *Server) removeStickyAssignment(stickyID, tier string) {
	s.mu.Lock()
	if tierMap, exists := s.stickyAssignments[stickyID]; exists {
		delete(tierMap, tier)
		if len(tierMap) == 0 {
			delete(s.stickyAssignments, stickyID)
		}
	}
	s.mu.Unlock()

	if _, err := s.db.Exec("DELETE FROM sticky_assignments WHERE sticky_id = ? AND tier = ?", stickyID, tier); err != nil {
		log.Printf("Warning: Failed to delete sticky assignment: %v", err)
	}
}

// addPendingAllocation reserves resources on a client for an in-flight session creation
// This prevents race conditions where multiple concurrent requests all select the same server
func (s *Server) addPendingAllocation(clientID, stickyID, tier string, tierSpec common.TierSpec, requestID string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// If this sticky_id+tier already has a pending allocation on this client, remove it first
	// (Assume the previous request completed successfully)
	if stickyID != "" && tier != "" {
		s.removePendingAllocationForStickyTierLocked(clientID, stickyID, tier)
	}

	allocation := PendingAllocation{
		StickyID:  stickyID,
		Tier:      tier,
		TierSpec:  tierSpec,
		Timestamp: time.Now(),
		RequestID: requestID,
	}

	s.pendingAllocations[clientID] = append(s.pendingAllocations[clientID], allocation)

	LogInfoWithData("Added pending allocation", map[string]interface{}{
		"client_id":  clientID,
		"sticky_id":  stickyID,
		"tier":       tier,
		"vcpu":       tierSpec.VCPU,
		"memory_gb":  tierSpec.MemoryGB,
		"request_id": requestID,
	})
}

// removePendingAllocationForStickyTierLocked removes pending allocation for specific sticky_id+tier
// Must be called with s.mu held
func (s *Server) removePendingAllocationForStickyTierLocked(clientID, stickyID, tier string) {
	allocations := s.pendingAllocations[clientID]
	if allocations == nil {
		return
	}

	// Filter out allocations matching sticky_id+tier
	filtered := make([]PendingAllocation, 0, len(allocations))
	removed := 0
	for _, alloc := range allocations {
		if alloc.StickyID == stickyID && alloc.Tier == tier {
			removed++
			continue
		}
		filtered = append(filtered, alloc)
	}

	if removed > 0 {
		s.pendingAllocations[clientID] = filtered
		if len(filtered) == 0 {
			delete(s.pendingAllocations, clientID)
		}
	}
}

// cleanupStalePendingAllocations removes allocations older than a threshold
// Called periodically by cleanup goroutine
func (s *Server) cleanupStalePendingAllocations() {
	// Use configured timeout (default: 2 minutes)
	threshold := time.Duration(s.config.PendingAllocationTimeoutSecs) * time.Second

	s.mu.Lock()
	defer s.mu.Unlock()

	totalRemoved := 0
	for clientID, allocations := range s.pendingAllocations {
		filtered := make([]PendingAllocation, 0, len(allocations))
		removed := 0

		for _, alloc := range allocations {
			if time.Since(alloc.Timestamp) > threshold {
				removed++
				LogWarn(fmt.Sprintf("Removing stale pending allocation: client=%s sticky_id=%s tier=%s age=%s",
					clientID, alloc.StickyID, alloc.Tier, time.Since(alloc.Timestamp)))
				continue
			}
			filtered = append(filtered, alloc)
		}

		if removed > 0 {
			totalRemoved += removed
			if len(filtered) == 0 {
				delete(s.pendingAllocations, clientID)
			} else {
				s.pendingAllocations[clientID] = filtered
			}
		}
	}

	if totalRemoved > 0 {
		LogInfoWithData("Cleaned up stale pending allocations", map[string]interface{}{
			"removed": totalRemoved,
		})
	}
}

// runHealthChecks periodically probes backends to verify they're reachable
func (s *Server) runHealthChecks(ctx context.Context) {
	if !s.config.HealthCheckEnabled {
		LogInfo("Health checks disabled")
		return
	}

	interval := time.Duration(s.config.HealthCheckIntervalSecs) * time.Second
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	LogInfoWithData("Health check goroutine started", map[string]interface{}{
		"interval": interval.String(),
		"type":     s.config.HealthCheckType,
		"timeout":  fmt.Sprintf("%ds", s.config.HealthCheckTimeoutSecs),
	})

	for {
		select {
		case <-ctx.Done():
			LogInfo("Health check goroutine stopping...")
			return
		case <-ticker.C:
			s.performHealthChecks()
		}
	}
}

// performHealthChecks probes all registered clients
func (s *Server) performHealthChecks() {
	s.mu.RLock()
	clients := make([]*ClientState, 0, len(s.clientCache))
	for _, client := range s.clientCache {
		clients = append(clients, client)
	}
	s.mu.RUnlock()

	// Probe clients concurrently
	var wg sync.WaitGroup
	for _, client := range clients {
		wg.Add(1)
		go func(c *ClientState) {
			defer wg.Done()
			s.probeClient(c)
		}(client)
	}
	wg.Wait()
}

// probeClient performs a single health check probe
func (s *Server) probeClient(client *ClientState) {
	timeout := time.Duration(s.config.HealthCheckTimeoutSecs) * time.Second

	var success bool
	var latency time.Duration

	switch s.config.HealthCheckType {
	case "http":
		success, latency = s.probeHTTP(client.Endpoint, s.config.HealthCheckPath, timeout)
	case "tcp":
		fallthrough
	default:
		success, latency = s.probeTCP(client.Endpoint, timeout)
	}

	s.updateHealthStatus(client, success, latency)
}

// probeTCP performs a TCP connection check
func (s *Server) probeTCP(endpoint string, timeout time.Duration) (bool, time.Duration) {
	// Parse endpoint to get host:port
	parsed, err := url.Parse(endpoint)
	if err != nil {
		LogWarn(fmt.Sprintf("Failed to parse endpoint for health check: %s", endpoint))
		return false, 0
	}

	addr := parsed.Host
	start := time.Now()

	conn, err := net.DialTimeout("tcp", addr, timeout)
	latency := time.Since(start)

	if err != nil {
		return false, latency
	}

	conn.Close()
	return true, latency
}

// probeHTTP performs an HTTP health check
func (s *Server) probeHTTP(endpoint, path string, timeout time.Duration) (bool, time.Duration) {
	client := &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: s.config.TLSInsecureSkipVerify,
			},
		},
	}

	healthURL := fmt.Sprintf("%s%s", endpoint, path)
	start := time.Now()

	resp, err := client.Get(healthURL)
	latency := time.Since(start)

	if err != nil {
		return false, latency
	}
	defer resp.Body.Close()

	// Consider 2xx and 3xx as healthy
	success := resp.StatusCode >= 200 && resp.StatusCode < 400
	return success, latency
}

// updateHealthStatus updates client health status based on probe result
func (s *Server) updateHealthStatus(client *ClientState, success bool, latency time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()

	client.LastHealthCheck = time.Now()
	latencyMs := float64(latency.Nanoseconds()) / 1000000.0 // Convert nanoseconds to milliseconds (float)

	// Update latency using exponential weighted moving average (EWMA)
	// Alpha = 0.3 gives more weight to recent measurements
	if client.LatencyMs == 0 {
		client.LatencyMs = latencyMs
	} else {
		alpha := 0.3
		client.LatencyMs = alpha*latencyMs + (1-alpha)*client.LatencyMs
	}

	previousStatus := client.HealthStatus

	if success {
		client.ConsecutiveSuccesses++
		client.ConsecutiveFailures = 0

		// Become healthy after threshold consecutive successes
		if client.ConsecutiveSuccesses >= s.config.HealthCheckHealthyThreshold {
			client.HealthStatus = "healthy"
		}
	} else {
		client.ConsecutiveFailures++
		client.ConsecutiveSuccesses = 0

		// Become unhealthy after threshold consecutive failures
		if client.ConsecutiveFailures >= s.config.HealthCheckUnhealthyThreshold {
			client.HealthStatus = "unhealthy"

			// Remove sticky assignments for unhealthy backends
			s.removeStickyAssignmentsForClientLocked(client.Registration.ClientID)
		}
	}

	// Log status changes
	if previousStatus != client.HealthStatus && client.HealthStatus != "unknown" {
		LogInfoWithData("Health status changed", map[string]interface{}{
			"client_id":      client.Registration.ClientID,
			"hostname":       client.Registration.Hostname,
			"previous":       previousStatus,
			"current":        client.HealthStatus,
			"latency_ms":     fmt.Sprintf("%.1f", client.LatencyMs),
			"failures":       client.ConsecutiveFailures,
			"successes":      client.ConsecutiveSuccesses,
		})
	}
}

// removeStickyAssignmentsForClientLocked removes all sticky assignments for a client
// Must be called with s.mu held
func (s *Server) removeStickyAssignmentsForClientLocked(clientID string) {
	removed := 0
	for stickyID, tierMap := range s.stickyAssignments {
		for tier, assignedClientID := range tierMap {
			if assignedClientID == clientID {
				delete(tierMap, tier)
				removed++

				// Clean up database (in background, capture loop variables)
				go func(sid, t string) {
					if _, err := s.db.Exec("DELETE FROM sticky_assignments WHERE sticky_id = ? AND tier = ?", sid, t); err != nil {
						log.Printf("Warning: Failed to delete sticky assignment in background: %v", err)
					}
				}(stickyID, tier)
			}
		}
		// Clean up empty tier maps
		if len(tierMap) == 0 {
			delete(s.stickyAssignments, stickyID)
		}
	}

	if removed > 0 {
		LogInfoWithData("Removed sticky assignments for unhealthy backend", map[string]interface{}{
			"client_id": clientID,
			"removed":   removed,
		})
	}
}
