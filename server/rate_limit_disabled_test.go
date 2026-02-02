package main

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"cyqle.in/opsen/common"
)

// TestRateLimitDisabled_IntegrationWithMiddleware verifies rate limit disabled works with full middleware chain
func TestRateLimitDisabled_IntegrationWithMiddleware(t *testing.T) {
	_, cleanup := CreateTestDB(t)
	defer cleanup()

	// Config with rate limiting disabled
	config := &common.ServerConfig{
		RateLimitPerMinute:  0, // Disabled
		RateLimitBurst:      0,
		MaxRequestBodyBytes: 10 * 1024 * 1024,
		RequestTimeout:      30,
	}

	// Initialize middlewares (same as main.go)
	var rateLimiter *RateLimiter
	if config.RateLimitPerMinute > 0 {
		rateLimiter = NewRateLimiter(config.RateLimitPerMinute, config.RateLimitBurst)
	}

	// Verify rate limiter is nil when disabled
	if rateLimiter != nil {
		t.Fatal("Rate limiter should be nil when RateLimitPerMinute is 0")
	}

	// Build middleware chain (similar to main.go)
	middlewares := []func(http.Handler) http.Handler{
		PanicRecovery,
		RequestLogger,
	}

	// Only add rate limiter if not nil
	if rateLimiter != nil {
		middlewares = append(middlewares, rateLimiter.Middleware)
	}

	// Create handler with middleware
	handler := ChainMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("OK"))
	}), middlewares...)

	// Make many rapid requests
	for i := 0; i < 300; i++ {
		req := httptest.NewRequest("GET", "/test", nil)
		req.RemoteAddr = "10.0.0.1:12345"
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("Request %d failed with status %d, expected 200 (rate limiting should be disabled)",
				i+1, rec.Code)
		}
	}

	t.Log("All 300 requests succeeded with rate limiting disabled")
}

// TestRateLimitEnabled_Comparison verifies rate limiting works when enabled
func TestRateLimitEnabled_Comparison(t *testing.T) {
	_, cleanup := CreateTestDB(t)
	defer cleanup()

	// Config with rate limiting ENABLED (very restrictive for testing)
	config := &common.ServerConfig{
		RateLimitPerMinute:  60, // 1 req/sec
		RateLimitBurst:      5,  // Only 5 burst requests
		MaxRequestBodyBytes: 10 * 1024 * 1024,
		RequestTimeout:      30,
	}

	// Initialize rate limiter (ENABLED this time)
	var rateLimiter *RateLimiter
	if config.RateLimitPerMinute > 0 {
		rateLimiter = NewRateLimiter(config.RateLimitPerMinute, config.RateLimitBurst)
	}

	// Verify rate limiter is NOT nil when enabled
	if rateLimiter == nil {
		t.Fatal("Rate limiter should NOT be nil when RateLimitPerMinute > 0")
	}

	// Build middleware chain
	middlewares := []func(http.Handler) http.Handler{
		PanicRecovery,
		RequestLogger,
	}

	// Add rate limiter
	if rateLimiter != nil {
		middlewares = append(middlewares, rateLimiter.Middleware)
	}

	handler := ChainMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("OK"))
	}), middlewares...)

	// Make rapid requests - should hit rate limit
	successCount := 0
	rateLimitedCount := 0

	for i := 0; i < 20; i++ {
		req := httptest.NewRequest("GET", "/test", nil)
		req.RemoteAddr = "10.0.0.1:12345"
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		if rec.Code == http.StatusOK {
			successCount++
		} else if rec.Code == http.StatusTooManyRequests {
			rateLimitedCount++
		}
	}

	// Should have hit rate limit (only 5 burst allowed)
	if rateLimitedCount == 0 {
		t.Error("Expected some requests to be rate limited, but none were")
	}

	if successCount > config.RateLimitBurst+2 { // Allow small buffer for timing
		t.Errorf("Expected ~%d successful requests (burst limit), got %d",
			config.RateLimitBurst, successCount)
	}

	t.Logf("Rate limiting working: %d succeeded, %d rate limited", successCount, rateLimitedCount)
}

// TestRateLimitConfig_DefaultValues verifies config defaults
func TestRateLimitConfig_DefaultValues(t *testing.T) {
	// Load default config
	config, err := common.LoadServerConfig("")
	if err != nil {
		t.Fatalf("Failed to load default config: %v", err)
	}

	// Verify default rate limit values
	if config.RateLimitPerMinute != 60 {
		t.Errorf("Expected default RateLimitPerMinute=60, got %d", config.RateLimitPerMinute)
	}

	if config.RateLimitBurst != 120 {
		t.Errorf("Expected default RateLimitBurst=120, got %d", config.RateLimitBurst)
	}
}

// TestRateLimitConfig_ZeroDisables verifies that 0 means disabled
func TestRateLimitConfig_ZeroDisables(t *testing.T) {
	// Create config with rate limiting disabled
	config := &common.ServerConfig{
		RateLimitPerMinute: 0,
		RateLimitBurst:     0,
	}

	// Initialize rate limiter as done in main.go
	var rateLimiter *RateLimiter
	if config.RateLimitPerMinute > 0 {
		rateLimiter = NewRateLimiter(config.RateLimitPerMinute, config.RateLimitBurst)
	}

	// Should be nil (disabled)
	if rateLimiter != nil {
		t.Error("Rate limiter should be nil when RateLimitPerMinute is 0")
	}
}
