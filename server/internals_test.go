package main

import (
	"bufio"
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"cyqle.in/opsen/common"
)

func TestMatchWildcard_EmptyInputs(t *testing.T) {
	cases := []struct {
		path    string
		pattern string
		want    bool
	}{
		{"", "", true},
		{"/api", "", false},
		{"", "*", true},
		{"", "/*", true},
		{"", "/api", false},
		{"/a", "?", false},
		{"/a", "??", true},
		{"/api/v1", "/???/v?", true},
		{"/api/v1", "/api/??", true},
		{"/api/v1", "/api/?", false},
	}

	for _, tc := range cases {
		if got := matchWildcard(tc.path, tc.pattern); got != tc.want {
			t.Errorf("matchWildcard(%q, %q) = %v, want %v", tc.path, tc.pattern, got, tc.want)
		}
	}
}

func TestPerformHealthChecks_ProbesEveryClient(t *testing.T) {
	db, cleanup := CreateTestDB(t)
	defer cleanup()

	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer backend.Close()

	server := NewTestServerWithConfig(t, db, func(c *common.ServerConfig) {
		c.HealthCheckEnabled = true
		c.HealthCheckType = "tcp"
		c.HealthCheckTimeoutSecs = 2
		c.HealthCheckHealthyThreshold = 1
		c.HealthCheckUnhealthyThreshold = 1
	})

	reachable := NewMockClient(MockClientOptions{ClientID: "reachable", Endpoint: backend.URL})
	// Port 1 on the loopback interface is reliably closed.
	unreachable := NewMockClient(MockClientOptions{ClientID: "unreachable", Endpoint: "http://127.0.0.1:1"})
	server.AddMockClient(reachable)
	server.AddMockClient(unreachable)

	server.performHealthChecks()

	server.mu.RLock()
	reachableStatus := server.clientCache["reachable"].HealthStatus
	unreachableStatus := server.clientCache["unreachable"].HealthStatus
	server.mu.RUnlock()

	if reachableStatus != "healthy" {
		t.Errorf("expected the reachable backend to be healthy, got %q", reachableStatus)
	}
	if unreachableStatus != "unhealthy" {
		t.Errorf("expected the unreachable backend to be unhealthy, got %q", unreachableStatus)
	}
}

func TestPerformHealthChecks_NoClients(t *testing.T) {
	db, cleanup := CreateTestDB(t)
	defer cleanup()

	server := NewTestServer(t, db)

	// Must be a no-op rather than a panic when nothing is registered.
	server.performHealthChecks()
}

func TestProbeTCP_UnparseableEndpoint(t *testing.T) {
	db, cleanup := CreateTestDB(t)
	defer cleanup()

	server := NewTestServer(t, db)

	ok, latency := server.probeTCP("://not a url", time.Second)
	if ok {
		t.Error("expected the probe to fail for an unparseable endpoint")
	}
	if latency != 0 {
		t.Errorf("expected zero latency for a parse failure, got %s", latency)
	}
}

func TestRunHealthChecks_StopsOnContextCancel(t *testing.T) {
	db, cleanup := CreateTestDB(t)
	defer cleanup()

	server := NewTestServerWithConfig(t, db, func(c *common.ServerConfig) {
		c.HealthCheckEnabled = true
		c.HealthCheckIntervalSecs = 1
		c.HealthCheckTimeoutSecs = 1
	})

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		server.runHealthChecks(ctx)
	}()

	// Let the ticker fire at least once before shutting down.
	time.Sleep(1200 * time.Millisecond)
	cancel()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("runHealthChecks did not stop after context cancellation")
	}
}

func TestRateLimiterCleanup_RemovesIdleBuckets(t *testing.T) {
	rl := &RateLimiter{
		buckets:         make(map[string]*TokenBucket),
		rate:            60,
		burst:           10,
		cleanupInterval: 5 * time.Millisecond,
		stopCh:          make(chan struct{}),
	}

	rl.buckets["idle"] = &TokenBucket{
		tokens:    1,
		capacity:  10,
		rate:      1,
		lastCheck: time.Now().Add(-time.Hour), // idle for well over 30 minutes
	}
	rl.buckets["active"] = &TokenBucket{
		tokens:    1,
		capacity:  10,
		rate:      1,
		lastCheck: time.Now(),
	}

	go rl.cleanup()
	defer rl.Stop()

	deadline := time.Now().Add(5 * time.Second)
	for {
		rl.mu.RLock()
		_, idlePresent := rl.buckets["idle"]
		_, activePresent := rl.buckets["active"]
		rl.mu.RUnlock()

		if !idlePresent {
			if !activePresent {
				t.Error("the recently used bucket should not have been removed")
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for the idle bucket to be cleaned up")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestRateLimiterStop_EndsCleanupGoroutine(t *testing.T) {
	rl := NewRateLimiter(60, 10)

	// Stop is single-use: it closes the channel the cleanup goroutine selects on.
	rl.Stop()

	if !rl.Allow("1.2.3.4") {
		t.Error("stopping the cleanup goroutine must not affect rate limiting")
	}
}

// flushRecorder records whether Flush reached the underlying writer.
type flushRecorder struct {
	http.ResponseWriter
	flushed int
}

func (f *flushRecorder) Flush() { f.flushed++ }

func TestTimeoutWriter_Flush(t *testing.T) {
	recorder := &flushRecorder{ResponseWriter: httptest.NewRecorder()}
	tw := &timeoutWriter{w: recorder}

	tw.Flush()
	if recorder.flushed != 1 {
		t.Errorf("expected the flush to pass through, got %d flushes", recorder.flushed)
	}

	// After a timeout the writer must stop touching the connection.
	tw.setTimedOut()
	tw.Flush()
	if recorder.flushed != 1 {
		t.Errorf("expected no flush after timeout, got %d flushes", recorder.flushed)
	}
}

func TestTimeoutWriter_FlushOnNonFlusher(t *testing.T) {
	// A writer that does not implement http.Flusher must be tolerated.
	tw := &timeoutWriter{w: nonFlusherWriter{httptest.NewRecorder()}}
	tw.Flush()
}

// nonFlusherWriter hides the Flush method of the embedded recorder.
type nonFlusherWriter struct {
	rec *httptest.ResponseRecorder
}

func (w nonFlusherWriter) Header() http.Header         { return w.rec.Header() }
func (w nonFlusherWriter) Write(b []byte) (int, error) { return w.rec.Write(b) }
func (w nonFlusherWriter) WriteHeader(code int)        { w.rec.WriteHeader(code) }

func TestResponseWriter_Flush(t *testing.T) {
	recorder := &flushRecorder{ResponseWriter: httptest.NewRecorder()}
	rw := &responseWriter{ResponseWriter: recorder, statusCode: http.StatusOK}

	rw.Flush()
	if recorder.flushed != 1 {
		t.Errorf("expected the flush to pass through, got %d flushes", recorder.flushed)
	}

	// Non-flushable writers are silently ignored.
	plain := &responseWriter{ResponseWriter: nonFlusherWriter{httptest.NewRecorder()}, statusCode: http.StatusOK}
	plain.Flush()
}

func TestResponseWriter_HijackUnsupported(t *testing.T) {
	rw := &responseWriter{ResponseWriter: httptest.NewRecorder(), statusCode: http.StatusOK}

	conn, buf, err := rw.Hijack()
	if err == nil {
		t.Fatal("expected an error when the underlying writer cannot be hijacked")
	}
	if conn != nil || buf != nil {
		t.Error("expected nil connection and buffer on failure")
	}
}

// hijackableWriter implements http.Hijacker for the success path.
type hijackableWriter struct {
	http.ResponseWriter
	conn net.Conn
}

func (h *hijackableWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return h.conn, bufio.NewReadWriter(bufio.NewReader(h.conn), bufio.NewWriter(h.conn)), nil
}

func TestResponseWriter_HijackSupported(t *testing.T) {
	client, server := net.Pipe()
	defer func() { _ = client.Close() }()
	defer func() { _ = server.Close() }()

	rw := &responseWriter{
		ResponseWriter: &hijackableWriter{ResponseWriter: httptest.NewRecorder(), conn: server},
		statusCode:     http.StatusOK,
	}

	conn, buf, err := rw.Hijack()
	if err != nil {
		t.Fatalf("Hijack returned error: %v", err)
	}
	if conn == nil || buf == nil {
		t.Fatal("expected a connection and buffered reader/writer")
	}
}

func TestTimeoutWriter_ConcurrentWritesAfterTimeout(t *testing.T) {
	tw := &timeoutWriter{w: httptest.NewRecorder()}

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		tw.setTimedOut()
	}()
	go func() {
		defer wg.Done()
		_, _ = tw.Write([]byte("data"))
		tw.WriteHeader(http.StatusOK)
		tw.Flush()
	}()
	wg.Wait()

	// Once timed out, writes must be rejected with ErrHandlerTimeout.
	if _, err := tw.Write([]byte("more")); err != http.ErrHandlerTimeout {
		t.Errorf("expected ErrHandlerTimeout, got %v", err)
	}
}

func TestGetClientIP_FallsBackToRemoteAddr(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "not-a-host-port"

	if got := getClientIP(req); got != "not-a-host-port" {
		t.Errorf("expected the raw RemoteAddr fallback, got %q", got)
	}
}
