package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// TestRateLimiter_TokenRefill verifies that tokens refill over time
func TestRateLimiter_TokenRefill(t *testing.T) {
	// Configure rate limiter: 60 requests/min = 1 request/second, burst of 2
	requestsPerMinute := 60 // 1 token per second
	burst := 2

	rateLimiter := NewRateLimiter(requestsPerMinute, burst)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	wrapped := rateLimiter.Middleware(handler)
	clientIP := "192.168.1.100"

	// Step 1: Exhaust the burst capacity (2 requests)
	for i := 0; i < burst; i++ {
		req := httptest.NewRequest("GET", "/test", nil)
		req.RemoteAddr = clientIP + ":12345"
		rec := httptest.NewRecorder()
		wrapped.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("Request %d should succeed (burst), got status %d", i+1, rec.Code)
		}
	}

	// Step 2: Immediate next request should be rate limited (no tokens left)
	req := httptest.NewRequest("GET", "/test", nil)
	req.RemoteAddr = clientIP + ":12345"
	rec := httptest.NewRecorder()
	wrapped.ServeHTTP(rec, req)

	if rec.Code != http.StatusTooManyRequests {
		t.Errorf("Request should be rate limited immediately after burst, got status %d", rec.Code)
	}

	// Step 3: Wait for tokens to refill (1.5 seconds = ~1.5 tokens)
	t.Log("Waiting 1.5 seconds for token refill...")
	time.Sleep(1500 * time.Millisecond)

	// Step 4: Request should now succeed (tokens refilled)
	req = httptest.NewRequest("GET", "/test", nil)
	req.RemoteAddr = clientIP + ":12345"
	rec = httptest.NewRecorder()
	wrapped.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Request should succeed after token refill, got status %d", rec.Code)
	}

	// Step 5: Immediate next request should be rate limited again
	req = httptest.NewRequest("GET", "/test", nil)
	req.RemoteAddr = clientIP + ":12345"
	rec = httptest.NewRecorder()
	wrapped.ServeHTTP(rec, req)

	if rec.Code != http.StatusTooManyRequests {
		t.Errorf("Request should be rate limited after consuming refilled token, got status %d", rec.Code)
	}
}

// TestRateLimiter_TokenRefillRate verifies exact refill rate
func TestRateLimiter_TokenRefillRate(t *testing.T) {
	// Configure: 120 requests/min = 2 requests/second, burst of 1
	requestsPerMinute := 120
	burst := 1

	rateLimiter := NewRateLimiter(requestsPerMinute, burst)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	wrapped := rateLimiter.Middleware(handler)
	clientIP := "10.0.0.1"

	// Exhaust burst
	req := httptest.NewRequest("GET", "/test", nil)
	req.RemoteAddr = clientIP + ":12345"
	rec := httptest.NewRecorder()
	wrapped.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("First request should succeed, got status %d", rec.Code)
	}

	// Verify rate limited
	req = httptest.NewRequest("GET", "/test", nil)
	req.RemoteAddr = clientIP + ":12345"
	rec = httptest.NewRecorder()
	wrapped.ServeHTTP(rec, req)

	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("Second request should be rate limited, got status %d", rec.Code)
	}

	// Wait 500ms (should get ~1 token at 2 tokens/sec)
	time.Sleep(550 * time.Millisecond) // Add 50ms buffer for timing variations

	// Should succeed
	req = httptest.NewRequest("GET", "/test", nil)
	req.RemoteAddr = clientIP + ":12345"
	rec = httptest.NewRecorder()
	wrapped.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Request should succeed after 500ms refill (2 req/sec), got status %d", rec.Code)
	}
}

// TestRateLimiter_BurstCapacityCap verifies tokens don't exceed capacity
func TestRateLimiter_BurstCapacityCap(t *testing.T) {
	requestsPerMinute := 60 // 1 token/sec
	burst := 3

	rateLimiter := NewRateLimiter(requestsPerMinute, burst)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	wrapped := rateLimiter.Middleware(handler)
	clientIP := "172.16.0.1"

	// Make one request to initialize bucket
	req := httptest.NewRequest("GET", "/test", nil)
	req.RemoteAddr = clientIP + ":12345"
	rec := httptest.NewRecorder()
	wrapped.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("First request should succeed, got status %d", rec.Code)
	}

	// Wait 10 seconds (should refill 10 tokens, but cap at burst=3)
	t.Log("Waiting 10 seconds to verify capacity cap...")
	time.Sleep(10 * time.Second)

	// Should only be able to make 3 requests (burst capacity), not 10+
	successCount := 0
	for i := 0; i < 5; i++ {
		req = httptest.NewRequest("GET", "/test", nil)
		req.RemoteAddr = clientIP + ":12345"
		rec = httptest.NewRecorder()
		wrapped.ServeHTTP(rec, req)

		if rec.Code == http.StatusOK {
			successCount++
		}
	}

	// Should succeed exactly 3 times (we already consumed 1 token earlier, so 2 remaining + refilled to cap of 3)
	if successCount != 3 {
		t.Errorf("Expected exactly 3 successful requests (burst cap), got %d", successCount)
	}
}

// TestRateLimiter_ContinuousLoad verifies sustained rate limiting
func TestRateLimiter_ContinuousLoad(t *testing.T) {
	// 60 requests/min = 1 req/sec, burst of 5
	requestsPerMinute := 60
	burst := 5

	rateLimiter := NewRateLimiter(requestsPerMinute, burst)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	wrapped := rateLimiter.Middleware(handler)
	clientIP := "203.0.113.1"

	// Exhaust burst
	for i := 0; i < burst; i++ {
		req := httptest.NewRequest("GET", "/test", nil)
		req.RemoteAddr = clientIP + ":12345"
		rec := httptest.NewRecorder()
		wrapped.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("Burst request %d should succeed, got %d", i+1, rec.Code)
		}
	}

	// Make requests at ~1 per second for 3 seconds
	successCount := 0
	failCount := 0

	for i := 0; i < 3; i++ {
		time.Sleep(1 * time.Second)

		req := httptest.NewRequest("GET", "/test", nil)
		req.RemoteAddr = clientIP + ":12345"
		rec := httptest.NewRecorder()
		wrapped.ServeHTTP(rec, req)

		if rec.Code == http.StatusOK {
			successCount++
		} else {
			failCount++
		}
	}

	t.Logf("After burst, sustained load: %d succeeded, %d failed", successCount, failCount)

	// Should succeed approximately 3 times (1 token/sec * 3 seconds)
	// Allow for timing variations
	if successCount < 2 || successCount > 3 {
		t.Errorf("Expected ~3 successful requests at sustained rate, got %d", successCount)
	}
}

// TestRateLimiter_IndependentBuckets verifies each IP has independent token bucket
func TestRateLimiter_IndependentBuckets(t *testing.T) {
	requestsPerMinute := 60 // 1 req/sec
	burst := 2

	rateLimiter := NewRateLimiter(requestsPerMinute, burst)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	wrapped := rateLimiter.Middleware(handler)

	// IP1 exhausts its bucket
	ip1 := "1.1.1.1"
	for i := 0; i < burst; i++ {
		req := httptest.NewRequest("GET", "/test", nil)
		req.RemoteAddr = ip1 + ":12345"
		rec := httptest.NewRecorder()
		wrapped.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("IP1 burst request %d failed", i+1)
		}
	}

	// IP1 should now be rate limited
	req := httptest.NewRequest("GET", "/test", nil)
	req.RemoteAddr = ip1 + ":12345"
	rec := httptest.NewRecorder()
	wrapped.ServeHTTP(rec, req)
	if rec.Code != http.StatusTooManyRequests {
		t.Error("IP1 should be rate limited")
	}

	// IP2 should have full burst available (independent bucket)
	ip2 := "2.2.2.2"
	for i := 0; i < burst; i++ {
		req := httptest.NewRequest("GET", "/test", nil)
		req.RemoteAddr = ip2 + ":12345"
		rec := httptest.NewRecorder()
		wrapped.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Errorf("IP2 burst request %d should succeed (independent bucket), got %d", i+1, rec.Code)
		}
	}
}

// TestRateLimiter_DirectBucketTest tests the token bucket directly
func TestRateLimiter_DirectBucketTest(t *testing.T) {
	rl := NewRateLimiter(60, 5) // 1 token/sec, burst 5

	ip := "192.0.2.1"

	// Test initial burst
	for i := 0; i < 5; i++ {
		if !rl.Allow(ip) {
			t.Errorf("Request %d should be allowed (initial burst)", i+1)
		}
	}

	// Next should fail
	if rl.Allow(ip) {
		t.Error("Request should be denied after burst exhausted")
	}

	// Wait and verify refill
	t.Log("Waiting 2 seconds for refill...")
	time.Sleep(2 * time.Second)

	// Should allow 2 more requests (~2 tokens refilled)
	successCount := 0
	for i := 0; i < 3; i++ {
		if rl.Allow(ip) {
			successCount++
		}
	}

	// Should succeed ~2 times
	if successCount < 1 || successCount > 2 {
		t.Errorf("Expected ~2 successful requests after 2sec refill, got %d", successCount)
	}
}

// TestRateLimiter_PrintDebugInfo prints token bucket state for debugging
func TestRateLimiter_PrintDebugInfo(t *testing.T) {
	rl := NewRateLimiter(60, 3) // 1 token/sec, burst 3
	ip := "198.51.100.1"

	printBucketState := func(label string) {
		rl.mu.RLock()
		bucket := rl.buckets[ip]
		rl.mu.RUnlock()

		if bucket != nil {
			bucket.mu.Lock()
			t.Logf("%s: tokens=%.2f, capacity=%.2f, rate=%.4f tokens/sec",
				label, bucket.tokens, bucket.capacity, bucket.rate)
			bucket.mu.Unlock()
		} else {
			t.Logf("%s: bucket not initialized yet", label)
		}
	}

	printBucketState("Initial state")

	// Make 3 requests (exhaust burst)
	for i := 0; i < 3; i++ {
		rl.Allow(ip)
	}
	printBucketState("After burst (3 requests)")

	// Try one more (should fail)
	if rl.Allow(ip) {
		t.Error("Should be rate limited")
	}
	printBucketState("After failed request")

	// Wait 1 second
	time.Sleep(1 * time.Second)
	printBucketState("After 1 second wait")

	// Make another request
	if !rl.Allow(ip) {
		t.Error("Should succeed after refill")
	}
	printBucketState("After successful refill request")
}

// Benchmark rate limiter performance
func BenchmarkRateLimiter_Allow(b *testing.B) {
	rl := NewRateLimiter(1000, 100)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ip := fmt.Sprintf("192.168.1.%d", i%254+1)
		rl.Allow(ip)
	}
}

func BenchmarkRateLimiter_AllowSameIP(b *testing.B) {
	rl := NewRateLimiter(1000000, 100000) // Very high limits to avoid rate limiting
	ip := "192.168.1.1"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rl.Allow(ip)
	}
}
