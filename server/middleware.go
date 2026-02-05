package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"runtime/debug"
	"strings"
	"sync"
	"time"
)

// PanicRecovery middleware recovers from panics and returns 500 error
func PanicRecovery(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if err := recover(); err != nil {
				// http.ErrAbortHandler is intentionally used by httputil.ReverseProxy
				// to abort when clients disconnect during streaming (SSE, etc.)
				// Don't log or try to write a response - just re-panic to let http.Server handle it
				if err == http.ErrAbortHandler {
					panic(err) // Re-panic to let http.Server silently discard
				}

				stack := debug.Stack()
				log.Printf("PANIC RECOVERED: %v\n%s", err, stack)
				http.Error(w, "Internal server error", http.StatusInternalServerError)
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// RequestSizeLimit middleware limits request body size to prevent memory exhaustion
func RequestSizeLimit(maxBytes int64) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			r.Body = http.MaxBytesReader(w, r.Body, maxBytes)
			next.ServeHTTP(w, r)
		})
	}
}

// timeoutWriter wraps http.ResponseWriter to prevent concurrent writes after timeout
type timeoutWriter struct {
	w          http.ResponseWriter
	mu         sync.Mutex
	timedOut   bool
	wroteHeader bool
}

func (tw *timeoutWriter) Header() http.Header {
	return tw.w.Header()
}

func (tw *timeoutWriter) Write(b []byte) (int, error) {
	tw.mu.Lock()
	defer tw.mu.Unlock()
	if tw.timedOut {
		return 0, http.ErrHandlerTimeout
	}
	tw.wroteHeader = true
	return tw.w.Write(b)
}

// Flush implements http.Flusher for SSE support
func (tw *timeoutWriter) Flush() {
	tw.mu.Lock()
	defer tw.mu.Unlock()
	if !tw.timedOut {
		if flusher, ok := tw.w.(http.Flusher); ok {
			flusher.Flush()
		}
	}
}

func (tw *timeoutWriter) WriteHeader(code int) {
	tw.mu.Lock()
	defer tw.mu.Unlock()
	if tw.timedOut || tw.wroteHeader {
		return
	}
	tw.wroteHeader = true
	tw.w.WriteHeader(code)
}

func (tw *timeoutWriter) setTimedOut() {
	tw.mu.Lock()
	defer tw.mu.Unlock()
	tw.timedOut = true
}

// Timeout middleware enforces request timeout
func Timeout(timeout time.Duration) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Skip timeout for SSE/EventSource connections
			// Detect by Accept header containing text/event-stream
			accept := r.Header.Get("Accept")
			if strings.Contains(accept, "text/event-stream") {
				// No timeout for SSE connections
				next.ServeHTTP(w, r)
				return
			}

			ctx, cancel := context.WithTimeout(r.Context(), timeout)
			defer cancel()

			r = r.WithContext(ctx)

			tw := &timeoutWriter{w: w}
			done := make(chan struct{})
			panicChan := make(chan interface{}, 1)

			go func() {
				defer func() {
					if err := recover(); err != nil {
						panicChan <- err
					}
				}()
				next.ServeHTTP(tw, r)
				close(done)
			}()

			select {
			case <-done:
				return
			case err := <-panicChan:
				// http.ErrAbortHandler is intentional - re-panic to let http.Server handle it
				if err == http.ErrAbortHandler {
					panic(err)
				}

				stack := debug.Stack()
				log.Printf("PANIC in timeout goroutine: %v\n%s", err, stack)
				tw.setTimedOut()
				// Only write error if handler hasn't already written headers
				tw.mu.Lock()
				alreadyWrote := tw.wroteHeader
				tw.mu.Unlock()
				if !alreadyWrote {
					http.Error(w, "Internal server error", http.StatusInternalServerError)
				}
			case <-ctx.Done():
				tw.setTimedOut()
				// Only write timeout error if handler hasn't already written headers
				tw.mu.Lock()
				alreadyWrote := tw.wroteHeader
				tw.mu.Unlock()
				if !alreadyWrote && ctx.Err() == context.DeadlineExceeded {
					http.Error(w, "Request timeout", http.StatusRequestTimeout)
				}
			}
		})
	}
}

// RateLimiter implements token bucket rate limiting per IP
type RateLimiter struct {
	mu       sync.RWMutex
	buckets  map[string]*TokenBucket
	rate     int           // Requests per minute
	burst    int           // Burst capacity
	cleanupInterval time.Duration
}

type TokenBucket struct {
	tokens    float64
	capacity  float64
	rate      float64
	lastCheck time.Time
	mu        sync.Mutex
}

func NewRateLimiter(requestsPerMinute, burst int) *RateLimiter {
	rl := &RateLimiter{
		buckets:         make(map[string]*TokenBucket),
		rate:            requestsPerMinute,
		burst:           burst,
		cleanupInterval: 10 * time.Minute,
	}

	// Start cleanup goroutine to remove old buckets
	go rl.cleanup()

	return rl
}

func (rl *RateLimiter) cleanup() {
	ticker := time.NewTicker(rl.cleanupInterval)
	defer ticker.Stop()

	for range ticker.C {
		rl.mu.Lock()
		now := time.Now()
		for ip, bucket := range rl.buckets {
			bucket.mu.Lock()
			// Remove buckets inactive for > 30 minutes
			if now.Sub(bucket.lastCheck) > 30*time.Minute {
				delete(rl.buckets, ip)
			}
			bucket.mu.Unlock()
		}
		rl.mu.Unlock()
	}
}

func (rl *RateLimiter) Allow(ip string) bool {
	rl.mu.RLock()
	bucket, exists := rl.buckets[ip]
	rl.mu.RUnlock()

	if !exists {
		rl.mu.Lock()
		// Double-check after acquiring write lock
		if bucket, exists = rl.buckets[ip]; !exists {
			bucket = &TokenBucket{
				tokens:    float64(rl.burst),
				capacity:  float64(rl.burst),
				rate:      float64(rl.rate) / 60.0, // Convert to per-second
				lastCheck: time.Now(),
			}
			rl.buckets[ip] = bucket
		}
		rl.mu.Unlock()
	}

	return bucket.Take()
}

func (tb *TokenBucket) Take() bool {
	tb.mu.Lock()
	defer tb.mu.Unlock()

	now := time.Now()
	elapsed := now.Sub(tb.lastCheck).Seconds()
	tb.lastCheck = now

	// Refill tokens based on elapsed time
	tb.tokens += elapsed * tb.rate
	if tb.tokens > tb.capacity {
		tb.tokens = tb.capacity
	}

	// Check if we have a token available
	if tb.tokens >= 1.0 {
		tb.tokens -= 1.0
		return true
	}

	return false
}

func (rl *RateLimiter) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip := getClientIP(r)

		if !rl.Allow(ip) {
			log.Printf("Rate limit exceeded for IP: %s (path: %s)", ip, r.URL.Path)
			http.Error(w, "Rate limit exceeded. Please try again later.", http.StatusTooManyRequests)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// APIKeyAuth middleware validates API key in X-API-Key header
// Supports both a primary server_key (for clients) and additional api_keys (for other integrations)
type APIKeyAuth struct {
	serverKey string
	apiKeys   map[string]bool
	enabled   bool
}

func NewAPIKeyAuth(serverKey string, apiKeys []string) *APIKeyAuth {
	keyMap := make(map[string]bool)
	for _, key := range apiKeys {
		if key != "" {
			keyMap[key] = true
		}
	}

	// Auth is enabled if either server_key or api_keys are configured
	enabled := serverKey != "" || len(keyMap) > 0

	return &APIKeyAuth{
		serverKey: serverKey,
		apiKeys:   keyMap,
		enabled:   enabled,
	}
}

func (a *APIKeyAuth) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Skip auth if not enabled
		if !a.enabled {
			next.ServeHTTP(w, r)
			return
		}

		apiKey := r.Header.Get("X-API-Key")
		if apiKey == "" {
			log.Printf("Missing API key from IP: %s (path: %s)", getClientIP(r), r.URL.Path)
			http.Error(w, "Missing X-API-Key header", http.StatusUnauthorized)
			return
		}

		// Check server_key first (primary authentication for clients)
		if a.serverKey != "" && apiKey == a.serverKey {
			next.ServeHTTP(w, r)
			return
		}

		// Check additional api_keys (for other integrations)
		if a.apiKeys[apiKey] {
			next.ServeHTTP(w, r)
			return
		}

		log.Printf("Invalid API key from IP: %s (path: %s)", getClientIP(r), r.URL.Path)
		http.Error(w, "Invalid API key", http.StatusForbidden)
	})
}

// InputValidator middleware validates and sanitizes input
type InputValidator struct{}

func (v *InputValidator) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Validate content type for POST requests
		if r.Method == http.MethodPost || r.Method == http.MethodPut {
			contentType := r.Header.Get("Content-Type")
			if contentType != "" && !strings.HasPrefix(contentType, "application/json") {
				http.Error(w, "Content-Type must be application/json", http.StatusUnsupportedMediaType)
				return
			}
		}

		// Sanitize path to prevent directory traversal
		if strings.Contains(r.URL.Path, "..") || strings.Contains(r.URL.Path, "//") {
			http.Error(w, "Invalid path", http.StatusBadRequest)
			return
		}

		// Validate host header to prevent host header injection
		if r.Host == "" {
			http.Error(w, "Missing Host header", http.StatusBadRequest)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// SecurityHeaders middleware adds security headers
func SecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("X-XSS-Protection", "1; mode=block")
		w.Header().Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		next.ServeHTTP(w, r)
	})
}

// getClientIP extracts client IP from request, checking X-Forwarded-For and X-Real-IP
func getClientIP(r *http.Request) string {
	// Check X-Forwarded-For header (but validate it)
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		// Take the first IP in the chain (original client)
		ips := strings.Split(xff, ",")
		if len(ips) > 0 {
			ip := strings.TrimSpace(ips[0])
			// Validate it's a valid IP
			if net.ParseIP(ip) != nil {
				return ip
			}
		}
	}

	// Check X-Real-IP header
	if xri := r.Header.Get("X-Real-IP"); xri != "" {
		if net.ParseIP(xri) != nil {
			return xri
		}
	}

	// Fall back to RemoteAddr
	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return ip
}

// RequestLogger logs HTTP requests with details
func RequestLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		// Wrap response writer to capture status code
		rw := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}

		next.ServeHTTP(rw, r)

		duration := time.Since(start)
		log.Printf("%s %s from %s - %d (%s)",
			r.Method,
			r.URL.Path,
			getClientIP(r),
			rw.statusCode,
			duration)
	})
}

type responseWriter struct {
	http.ResponseWriter
	statusCode int
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}

// Flush implements http.Flusher for SSE support
func (rw *responseWriter) Flush() {
	if flusher, ok := rw.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

// ChainMiddleware chains multiple middleware functions
func ChainMiddleware(handler http.Handler, middlewares ...func(http.Handler) http.Handler) http.Handler {
	for i := len(middlewares) - 1; i >= 0; i-- {
		handler = middlewares[i](handler)
	}
	return handler
}

// HealthCheckBypass allows health checks to bypass rate limiting
func HealthCheckBypass(healthPaths []string, next http.Handler) http.Handler {
	pathMap := make(map[string]bool)
	for _, path := range healthPaths {
		pathMap[path] = true
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if pathMap[r.URL.Path] {
			// Bypass middleware chain for health checks
			next.ServeHTTP(w, r)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// CORSConfig stores CORS configuration
type CORSConfig struct {
	AllowedOrigins []string
	AllowedMethods []string
	AllowedHeaders []string
}

// CORS middleware handles Cross-Origin Resource Sharing
func CORS(config CORSConfig) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")

			// Check if origin is allowed
			allowed := false
			for _, allowedOrigin := range config.AllowedOrigins {
				if allowedOrigin == "*" || allowedOrigin == origin {
					allowed = true
					w.Header().Set("Access-Control-Allow-Origin", allowedOrigin)
					break
				}
			}

			if !allowed && len(config.AllowedOrigins) > 0 {
				http.Error(w, "Origin not allowed", http.StatusForbidden)
				return
			}

			w.Header().Set("Access-Control-Allow-Methods", strings.Join(config.AllowedMethods, ", "))
			w.Header().Set("Access-Control-Allow-Headers", strings.Join(config.AllowedHeaders, ", "))

			// Handle preflight request
			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// IPWhitelist middleware allows only whitelisted IPs
type IPWhitelist struct {
	allowedIPs map[string]bool
	enabled    bool
}

func NewIPWhitelist(ips []string) *IPWhitelist {
	ipMap := make(map[string]bool)
	for _, ip := range ips {
		if ip != "" {
			ipMap[ip] = true
		}
	}

	return &IPWhitelist{
		allowedIPs: ipMap,
		enabled:    len(ipMap) > 0,
	}
}

func (ipw *IPWhitelist) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !ipw.enabled {
			next.ServeHTTP(w, r)
			return
		}

		clientIP := getClientIP(r)

		if !ipw.allowedIPs[clientIP] {
			log.Printf("IP not whitelisted: %s (path: %s)", clientIP, r.URL.Path)
			http.Error(w, "Access denied", http.StatusForbidden)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// CircuitBreakerState represents circuit breaker states
type CircuitBreakerState int

const (
	StateClosed CircuitBreakerState = iota
	StateOpen
	StateHalfOpen
)

func (s CircuitBreakerState) String() string {
	switch s {
	case StateClosed:
		return "closed"
	case StateOpen:
		return "open"
	case StateHalfOpen:
		return "half-open"
	default:
		return fmt.Sprintf("unknown(%d)", s)
	}
}
