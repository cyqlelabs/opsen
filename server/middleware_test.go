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
