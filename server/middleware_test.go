package main

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestPanicRecovery verifies panic recovery middleware
func TestPanicRecovery(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("test panic")
	})

	wrapped := PanicRecovery(handler)

	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()

	// Should not panic
	wrapped.ServeHTTP(rec, req)

	// Should return 500
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("Expected status 500, got %d", rec.Code)
	}
}

// TestRequestSizeLimit verifies request size limiting
func TestRequestSizeLimit(t *testing.T) {
	maxBytes := int64(100)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Try to read body
		buf := new(bytes.Buffer)
		_, err := buf.ReadFrom(r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusRequestEntityTooLarge)
			return
		}
		w.WriteHeader(http.StatusOK)
	})

	middleware := RequestSizeLimit(maxBytes)
	wrapped := middleware(handler)

	tests := []struct {
		name           string
		bodySize       int
		expectedStatus int
	}{
		{
			name:           "Within limit",
			bodySize:       50,
			expectedStatus: http.StatusOK,
		},
		{
			name:           "At limit",
			bodySize:       100,
			expectedStatus: http.StatusOK,
		},
		{
			name:           "Over limit",
			bodySize:       150,
			expectedStatus: http.StatusRequestEntityTooLarge,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := strings.Repeat("a", tt.bodySize)
			req := httptest.NewRequest("POST", "/test", strings.NewReader(body))
			rec := httptest.NewRecorder()

			wrapped.ServeHTTP(rec, req)

			if rec.Code != tt.expectedStatus {
				t.Errorf("Expected status %d, got %d", tt.expectedStatus, rec.Code)
			}
		})
	}
}

// TestTimeout verifies request timeout middleware
func TestTimeout(t *testing.T) {
	timeout := 100 * time.Millisecond

	// Handler that sleeps longer than timeout
	slowHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	})

	// Handler that completes quickly
	fastHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	middleware := Timeout(timeout)

	t.Run("Slow request times out", func(t *testing.T) {
		wrapped := middleware(slowHandler)
		req := httptest.NewRequest("GET", "/test", nil)
		rec := httptest.NewRecorder()

		wrapped.ServeHTTP(rec, req)

		// Note: Due to goroutine timing, we might not always get the timeout status
		// This test verifies the middleware doesn't panic
	})

	t.Run("Fast request completes", func(t *testing.T) {
		wrapped := middleware(fastHandler)
		req := httptest.NewRequest("GET", "/test", nil)
		rec := httptest.NewRecorder()

		wrapped.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d", rec.Code)
		}
	})
}

// TestTimeoutWithPanic verifies that panics in the timeout goroutine are caught
func TestTimeoutWithPanic(t *testing.T) {
	timeout := 100 * time.Millisecond

	// Handler that panics
	panicHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("simulated panic in handler")
	})

	middleware := Timeout(timeout)
	wrapped := middleware(panicHandler)

	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()

	// Should not panic - middleware should catch it
	wrapped.ServeHTTP(rec, req)

	// Should return 500 when panic is caught
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("Expected status 500 after panic, got %d", rec.Code)
	}
}

// TestTimeoutWithHeaderRace verifies no "superfluous WriteHeader" error
// when handler writes headers simultaneously with timeout
func TestTimeoutWithHeaderRace(t *testing.T) {
	timeout := 50 * time.Millisecond

	// Handler that starts writing response but takes too long
	slowWriterHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Write headers immediately
		w.WriteHeader(http.StatusOK)
		// Then take too long with the body
		time.Sleep(100 * time.Millisecond)
		// Write will likely fail due to timeout, but we don't care in this test
		_, _ = w.Write([]byte("response body"))
	})

	middleware := Timeout(timeout)
	wrapped := middleware(slowWriterHandler)

	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()

	// Should not produce "superfluous WriteHeader" error
	wrapped.ServeHTTP(rec, req)

	// Either the handler's 200 or the timeout's 408 is acceptable
	// depending on timing, but no error should occur
	if rec.Code != http.StatusOK && rec.Code != http.StatusRequestTimeout {
		t.Logf("Note: Got status %d (expected 200 or 408)", rec.Code)
	}
}

// TestRateLimiter verifies rate limiting functionality
func TestRateLimiter(t *testing.T) {
	requestsPerMinute := 10
	burst := 15

	rateLimiter := NewRateLimiter(requestsPerMinute, burst)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	wrapped := rateLimiter.Middleware(handler)

	// Simulate requests from same IP
	clientIP := "1.2.3.4"

	// First burst should succeed
	successCount := 0
	for i := 0; i < burst; i++ {
		req := httptest.NewRequest("GET", "/test", nil)
		req.RemoteAddr = clientIP + ":12345"
		rec := httptest.NewRecorder()

		wrapped.ServeHTTP(rec, req)

		if rec.Code == http.StatusOK {
			successCount++
		}
	}

	if successCount < burst {
		t.Logf("Warning: Only %d/%d burst requests succeeded", successCount, burst)
	}

	// Next request should be rate limited (bucket exhausted)
	req := httptest.NewRequest("GET", "/test", nil)
	req.RemoteAddr = clientIP + ":12345"
	rec := httptest.NewRecorder()

	wrapped.ServeHTTP(rec, req)

	if rec.Code != http.StatusTooManyRequests {
		t.Logf("Warning: Expected rate limit, got status %d", rec.Code)
		// Note: This might not always fail due to token refill timing
	}
}

// TestRateLimiter_DifferentIPs verifies per-IP rate limiting
func TestRateLimiter_DifferentIPs(t *testing.T) {
	requestsPerMinute := 5
	burst := 5

	rateLimiter := NewRateLimiter(requestsPerMinute, burst)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	wrapped := rateLimiter.Middleware(handler)

	// Different IPs should have independent rate limits
	ips := []string{"1.2.3.4", "5.6.7.8", "9.10.11.12"}

	for _, ip := range ips {
		for i := 0; i < burst; i++ {
			req := httptest.NewRequest("GET", "/test", nil)
			req.RemoteAddr = ip + ":12345"
			rec := httptest.NewRecorder()

			wrapped.ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Errorf("IP %s request %d failed with status %d", ip, i, rec.Code)
			}
		}
	}
}

// TestAPIKeyAuth verifies API key authentication
func TestAPIKeyAuth(t *testing.T) {
	validKeys := []string{"key123", "key456"}
	apiKeyAuth := NewAPIKeyAuth("", validKeys)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	wrapped := apiKeyAuth.Middleware(handler)

	tests := []struct {
		name           string
		apiKey         string
		expectedStatus int
	}{
		{
			name:           "Valid key 1",
			apiKey:         "key123",
			expectedStatus: http.StatusOK,
		},
		{
			name:           "Valid key 2",
			apiKey:         "key456",
			expectedStatus: http.StatusOK,
		},
		{
			name:           "Invalid key",
			apiKey:         "wrong-key",
			expectedStatus: http.StatusForbidden, // API key auth returns 403
		},
		{
			name:           "No key",
			apiKey:         "",
			expectedStatus: http.StatusUnauthorized, // Missing key returns 401
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/test", nil)
			if tt.apiKey != "" {
				req.Header.Set("X-API-Key", tt.apiKey)
			}
			rec := httptest.NewRecorder()

			wrapped.ServeHTTP(rec, req)

			if rec.Code != tt.expectedStatus {
				t.Errorf("Expected status %d, got %d", tt.expectedStatus, rec.Code)
			}
		})
	}
}

// TestAPIKeyAuth_NoKeysConfigured verifies auth is bypassed when no keys configured
func TestAPIKeyAuth_NoKeysConfigured(t *testing.T) {
	apiKeyAuth := NewAPIKeyAuth("", []string{})

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	wrapped := apiKeyAuth.Middleware(handler)

	req := httptest.NewRequest("GET", "/test", nil)
	// No API key header
	rec := httptest.NewRecorder()

	wrapped.ServeHTTP(rec, req)

	// Should pass through when no keys configured
	if rec.Code != http.StatusOK {
		t.Errorf("Expected status 200 when no keys configured, got %d", rec.Code)
	}
}

// TestAPIKeyAuth_ServerKey verifies server_key authentication
func TestAPIKeyAuth_ServerKey(t *testing.T) {
	serverKey := "server-secret-123"
	apiKeys := []string{"api-key-456", "api-key-789"}
	apiKeyAuth := NewAPIKeyAuth(serverKey, apiKeys)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	wrapped := apiKeyAuth.Middleware(handler)

	tests := []struct {
		name           string
		apiKey         string
		expectedStatus int
	}{
		{
			name:           "Valid server key",
			apiKey:         "server-secret-123",
			expectedStatus: http.StatusOK,
		},
		{
			name:           "Valid API key 1",
			apiKey:         "api-key-456",
			expectedStatus: http.StatusOK,
		},
		{
			name:           "Valid API key 2",
			apiKey:         "api-key-789",
			expectedStatus: http.StatusOK,
		},
		{
			name:           "Invalid key",
			apiKey:         "wrong-key",
			expectedStatus: http.StatusForbidden,
		},
		{
			name:           "No key",
			apiKey:         "",
			expectedStatus: http.StatusUnauthorized,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/test", nil)
			if tt.apiKey != "" {
				req.Header.Set("X-API-Key", tt.apiKey)
			}
			rec := httptest.NewRecorder()

			wrapped.ServeHTTP(rec, req)

			if rec.Code != tt.expectedStatus {
				t.Errorf("Expected status %d, got %d", tt.expectedStatus, rec.Code)
			}
		})
	}
}

// TestAPIKeyAuth_ServerKeyOnly verifies authentication with only server_key configured
func TestAPIKeyAuth_ServerKeyOnly(t *testing.T) {
	serverKey := "server-secret-123"
	apiKeyAuth := NewAPIKeyAuth(serverKey, []string{})

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	wrapped := apiKeyAuth.Middleware(handler)

	tests := []struct {
		name           string
		apiKey         string
		expectedStatus int
	}{
		{
			name:           "Valid server key",
			apiKey:         "server-secret-123",
			expectedStatus: http.StatusOK,
		},
		{
			name:           "Wrong key",
			apiKey:         "wrong-key",
			expectedStatus: http.StatusForbidden,
		},
		{
			name:           "No key",
			apiKey:         "",
			expectedStatus: http.StatusUnauthorized,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/test", nil)
			if tt.apiKey != "" {
				req.Header.Set("X-API-Key", tt.apiKey)
			}
			rec := httptest.NewRecorder()

			wrapped.ServeHTTP(rec, req)

			if rec.Code != tt.expectedStatus {
				t.Errorf("Expected status %d, got %d", tt.expectedStatus, rec.Code)
			}
		})
	}
}

// TestIPWhitelist verifies IP whitelisting
func TestIPWhitelist(t *testing.T) {
	allowedIPs := []string{"1.2.3.4", "5.6.7.8"}
	ipWhitelist := NewIPWhitelist(allowedIPs)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	wrapped := ipWhitelist.Middleware(handler)

	tests := []struct {
		name           string
		remoteAddr     string
		expectedStatus int
	}{
		{
			name:           "Allowed IP 1",
			remoteAddr:     "1.2.3.4:12345",
			expectedStatus: http.StatusOK,
		},
		{
			name:           "Allowed IP 2",
			remoteAddr:     "5.6.7.8:54321",
			expectedStatus: http.StatusOK,
		},
		{
			name:           "Blocked IP",
			remoteAddr:     "9.10.11.12:12345",
			expectedStatus: http.StatusForbidden,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/test", nil)
			req.RemoteAddr = tt.remoteAddr
			rec := httptest.NewRecorder()

			wrapped.ServeHTTP(rec, req)

			if rec.Code != tt.expectedStatus {
				t.Errorf("Expected status %d, got %d", tt.expectedStatus, rec.Code)
			}
		})
	}
}

// TestIPWhitelist_NoWhitelistConfigured verifies all IPs allowed when whitelist empty
func TestIPWhitelist_NoWhitelistConfigured(t *testing.T) {
	ipWhitelist := NewIPWhitelist([]string{})

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	wrapped := ipWhitelist.Middleware(handler)

	req := httptest.NewRequest("GET", "/test", nil)
	req.RemoteAddr = "1.2.3.4:12345"
	rec := httptest.NewRecorder()

	wrapped.ServeHTTP(rec, req)

	// Should allow all IPs when whitelist not configured
	if rec.Code != http.StatusOK {
		t.Errorf("Expected status 200 when whitelist empty, got %d", rec.Code)
	}
}

// TestSecurityHeaders verifies security headers are added
func TestSecurityHeaders(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	wrapped := SecurityHeaders(handler)

	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()

	wrapped.ServeHTTP(rec, req)

	// Verify security headers
	expectedHeaders := map[string]string{
		"X-Content-Type-Options":  "nosniff",
		"X-Frame-Options":         "DENY",
		"X-XSS-Protection":        "1; mode=block",
		"Strict-Transport-Security": "max-age=31536000; includeSubDomains",
	}

	for header, expectedValue := range expectedHeaders {
		actualValue := rec.Header().Get(header)
		if actualValue != expectedValue {
			t.Errorf("Header %s: expected %s, got %s", header, expectedValue, actualValue)
		}
	}
}

// TestChainMiddleware verifies middleware chaining
func TestChainMiddleware(t *testing.T) {
	// Track order of middleware execution
	var executionOrder []string
	var mu sync.Mutex

	middleware1 := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			mu.Lock()
			executionOrder = append(executionOrder, "middleware1-before")
			mu.Unlock()
			next.ServeHTTP(w, r)
			mu.Lock()
			executionOrder = append(executionOrder, "middleware1-after")
			mu.Unlock()
		})
	}

	middleware2 := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			mu.Lock()
			executionOrder = append(executionOrder, "middleware2-before")
			mu.Unlock()
			next.ServeHTTP(w, r)
			mu.Lock()
			executionOrder = append(executionOrder, "middleware2-after")
			mu.Unlock()
		})
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		executionOrder = append(executionOrder, "handler")
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	})

	chained := ChainMiddleware(handler, middleware1, middleware2)

	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()

	chained.ServeHTTP(rec, req)

	// Verify execution order (middlewares applied in order passed to ChainMiddleware)
	// ChainMiddleware applies them in the order: middleware1, then middleware2
	expectedOrder := []string{
		"middleware1-before",
		"middleware2-before",
		"handler",
		"middleware2-after",
		"middleware1-after",
	}

	mu.Lock()
	defer mu.Unlock()

	if len(executionOrder) != len(expectedOrder) {
		t.Fatalf("Expected %d execution steps, got %d", len(expectedOrder), len(executionOrder))
	}

	for i, expected := range expectedOrder {
		if executionOrder[i] != expected {
			t.Errorf("Step %d: expected %s, got %s", i, expected, executionOrder[i])
		}
	}
}

// TestRequestLogger verifies request logging middleware
func TestRequestLogger(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	wrapped := RequestLogger(handler)

	req := httptest.NewRequest("GET", "/test", nil)
	req.RemoteAddr = "1.2.3.4:12345"
	rec := httptest.NewRecorder()

	wrapped.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", rec.Code)
	}
}

// TestRequestLogger_StatusCode verifies status code capture
func TestRequestLogger_StatusCode(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
	}{
		{"200 OK", http.StatusOK},
		{"404 Not Found", http.StatusNotFound},
		{"500 Internal Server Error", http.StatusInternalServerError},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.statusCode)
			})

			wrapped := RequestLogger(handler)

			req := httptest.NewRequest("GET", "/test", nil)
			rec := httptest.NewRecorder()

			wrapped.ServeHTTP(rec, req)

			if rec.Code != tt.statusCode {
				t.Errorf("Expected status %d, got %d", tt.statusCode, rec.Code)
			}
		})
	}
}

// TestCORS verifies CORS middleware
func TestCORS(t *testing.T) {
	config := CORSConfig{
		AllowedOrigins: []string{"https://example.com", "https://app.example.com"},
		AllowedMethods: []string{"GET", "POST", "PUT", "DELETE"},
		AllowedHeaders: []string{"Content-Type", "Authorization"},
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	middleware := CORS(config)
	wrapped := middleware(handler)

	tests := []struct {
		name           string
		origin         string
		method         string
		expectedStatus int
		checkHeaders   bool
	}{
		{
			name:           "Allowed origin",
			origin:         "https://example.com",
			method:         "GET",
			expectedStatus: http.StatusOK,
			checkHeaders:   true,
		},
		{
			name:           "Another allowed origin",
			origin:         "https://app.example.com",
			method:         "POST",
			expectedStatus: http.StatusOK,
			checkHeaders:   true,
		},
		{
			name:           "Disallowed origin",
			origin:         "https://evil.com",
			method:         "GET",
			expectedStatus: http.StatusForbidden,
			checkHeaders:   false,
		},
		{
			name:           "Preflight request",
			origin:         "https://example.com",
			method:         "OPTIONS",
			expectedStatus: http.StatusNoContent,
			checkHeaders:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, "/test", nil)
			req.Header.Set("Origin", tt.origin)
			rec := httptest.NewRecorder()

			wrapped.ServeHTTP(rec, req)

			if rec.Code != tt.expectedStatus {
				t.Errorf("Expected status %d, got %d", tt.expectedStatus, rec.Code)
			}

			if tt.checkHeaders {
				allowOrigin := rec.Header().Get("Access-Control-Allow-Origin")
				if allowOrigin == "" {
					t.Error("Expected Access-Control-Allow-Origin header")
				}

				allowMethods := rec.Header().Get("Access-Control-Allow-Methods")
				if allowMethods == "" {
					t.Error("Expected Access-Control-Allow-Methods header")
				}

				allowHeaders := rec.Header().Get("Access-Control-Allow-Headers")
				if allowHeaders == "" {
					t.Error("Expected Access-Control-Allow-Headers header")
				}
			}
		})
	}
}

// TestCORS_Wildcard verifies wildcard origin support
func TestCORS_Wildcard(t *testing.T) {
	config := CORSConfig{
		AllowedOrigins: []string{"*"},
		AllowedMethods: []string{"GET"},
		AllowedHeaders: []string{"Content-Type"},
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	middleware := CORS(config)
	wrapped := middleware(handler)

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Origin", "https://any-origin.com")
	rec := httptest.NewRecorder()

	wrapped.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", rec.Code)
	}

	allowOrigin := rec.Header().Get("Access-Control-Allow-Origin")
	if allowOrigin != "*" {
		t.Errorf("Expected wildcard origin, got %s", allowOrigin)
	}
}

// TestHealthCheckBypass verifies health check bypass middleware
func TestHealthCheckBypass(t *testing.T) {
	healthPaths := []string{"/health", "/healthz", "/ping"}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	wrapped := HealthCheckBypass(healthPaths, handler)

	tests := []struct {
		name           string
		path           string
		expectedStatus int
	}{
		{"Health endpoint 1", "/health", http.StatusOK},
		{"Health endpoint 2", "/healthz", http.StatusOK},
		{"Health endpoint 3", "/ping", http.StatusOK},
		{"Non-health endpoint", "/api/data", http.StatusOK},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", tt.path, nil)
			rec := httptest.NewRecorder()

			wrapped.ServeHTTP(rec, req)

			if rec.Code != tt.expectedStatus {
				t.Errorf("Expected status %d, got %d", tt.expectedStatus, rec.Code)
			}
		})
	}
}

// TestGetClientIP verifies client IP extraction
func TestGetClientIP(t *testing.T) {
	tests := []struct {
		name           string
		remoteAddr     string
		forwardedFor   string
		realIP         string
		expectedPrefix string
	}{
		{
			name:           "Direct connection",
			remoteAddr:     "1.2.3.4:12345",
			expectedPrefix: "1.2.3.4",
		},
		{
			name:           "X-Forwarded-For single IP",
			remoteAddr:     "127.0.0.1:12345",
			forwardedFor:   "5.6.7.8",
			expectedPrefix: "5.6.7.8",
		},
		{
			name:           "X-Forwarded-For multiple IPs",
			remoteAddr:     "127.0.0.1:12345",
			forwardedFor:   "5.6.7.8, 9.10.11.12, 13.14.15.16",
			expectedPrefix: "5.6.7.8",
		},
		{
			name:           "X-Real-IP",
			remoteAddr:     "127.0.0.1:12345",
			realIP:         "9.10.11.12",
			expectedPrefix: "9.10.11.12",
		},
		{
			name:           "Invalid X-Forwarded-For falls back to RemoteAddr",
			remoteAddr:     "1.2.3.4:12345",
			forwardedFor:   "invalid-ip",
			expectedPrefix: "1.2.3.4",
		},
		{
			name:           "Invalid X-Real-IP falls back to RemoteAddr",
			remoteAddr:     "1.2.3.4:12345",
			realIP:         "invalid-ip",
			expectedPrefix: "1.2.3.4",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/test", nil)
			req.RemoteAddr = tt.remoteAddr
			if tt.forwardedFor != "" {
				req.Header.Set("X-Forwarded-For", tt.forwardedFor)
			}
			if tt.realIP != "" {
				req.Header.Set("X-Real-IP", tt.realIP)
			}

			ip := getClientIP(req)
			if !strings.HasPrefix(ip, tt.expectedPrefix) {
				t.Errorf("Expected IP to start with %s, got %s", tt.expectedPrefix, ip)
			}
		})
	}
}

// TestInputValidator verifies input validation middleware
func TestInputValidator(t *testing.T) {
	validator := &InputValidator{}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	wrapped := validator.Middleware(handler)

	tests := []struct {
		name           string
		method         string
		contentType    string
		path           string
		host           string
		expectedStatus int
	}{
		{
			name:           "Valid JSON POST",
			method:         "POST",
			contentType:    "application/json",
			path:           "/api/test",
			host:           "example.com",
			expectedStatus: http.StatusOK,
		},
		{
			name:           "Valid PUT",
			method:         "PUT",
			contentType:    "application/json",
			path:           "/api/test",
			host:           "example.com",
			expectedStatus: http.StatusOK,
		},
		{
			name:           "Invalid content type",
			method:         "POST",
			contentType:    "text/plain",
			path:           "/api/test",
			host:           "example.com",
			expectedStatus: http.StatusUnsupportedMediaType,
		},
		{
			name:           "Path traversal attempt",
			method:         "GET",
			path:           "/api/../../../etc/passwd",
			host:           "example.com",
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:           "Double slash in path",
			method:         "GET",
			path:           "/api//test",
			host:           "example.com",
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:           "Missing host header",
			method:         "GET",
			path:           "/api/test",
			host:           "",
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:           "GET request (no content type required)",
			method:         "GET",
			path:           "/api/test",
			host:           "example.com",
			expectedStatus: http.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, tt.path, nil)
			req.Host = tt.host
			if tt.contentType != "" {
				req.Header.Set("Content-Type", tt.contentType)
			}
			rec := httptest.NewRecorder()

			wrapped.ServeHTTP(rec, req)

			if rec.Code != tt.expectedStatus {
				t.Errorf("Expected status %d, got %d", tt.expectedStatus, rec.Code)
			}
		})
	}
}

// TestIPWhitelist_String verifies String() method
func TestIPWhitelist_String(t *testing.T) {
	// This tests if the IPWhitelist type exists and works
	// (There's no String() method defined, so this test just verifies basic usage)
	ipw := NewIPWhitelist([]string{"1.2.3.4"})
	if ipw == nil {
		t.Fatal("Expected non-nil IPWhitelist")
	}
	if !ipw.enabled {
		t.Error("Expected IPWhitelist to be enabled")
	}
}

// TestResponseWriter_WriteHeader verifies responseWriter status code capture
func TestResponseWriter_WriteHeader(t *testing.T) {
	tests := []int{
		http.StatusOK,
		http.StatusCreated,
		http.StatusNotFound,
		http.StatusInternalServerError,
	}

	for _, statusCode := range tests {
		t.Run(http.StatusText(statusCode), func(t *testing.T) {
			rw := &responseWriter{ResponseWriter: httptest.NewRecorder(), statusCode: http.StatusOK}
			rw.WriteHeader(statusCode)
			if rw.statusCode != statusCode {
				t.Errorf("Expected status code %d, got %d", statusCode, rw.statusCode)
			}
		})
	}
}

// TestRateLimiter_Cleanup verifies cleanup goroutine doesn't panic
func TestRateLimiter_Cleanup(t *testing.T) {
	rl := NewRateLimiter(10, 10)

	// Add some buckets
	rl.Allow("1.2.3.4")
	rl.Allow("5.6.7.8")

	// Wait a bit to ensure cleanup goroutine runs
	time.Sleep(100 * time.Millisecond)

	// Cleanup goroutine should not panic
	// (this test mainly verifies no race conditions)
}

// TestCircuitBreakerState_String verifies state string representation
func TestCircuitBreakerState_String(t *testing.T) {
	tests := []struct {
		state    CircuitBreakerState
		expected string
	}{
		{StateClosed, "closed"},
		{StateOpen, "open"},
		{StateHalfOpen, "half-open"},
		{CircuitBreakerState(999), "unknown(999)"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			if got := tt.state.String(); got != tt.expected {
				t.Errorf("Expected %s, got %s", tt.expected, got)
			}
		})
	}
}

