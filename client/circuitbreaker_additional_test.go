package main

import (
	"errors"
	"testing"
	"time"
)

// TestCircuitBreaker_AllStateTransitions verifies all state transitions in detail
func TestCircuitBreaker_AllStateTransitions(t *testing.T) {
	cb := NewCircuitBreaker(2, 100*time.Millisecond)

	// Initial state should be CLOSED
	if cb.GetState() != StateClosed {
		t.Errorf("Initial state should be CLOSED, got %s", cb.GetState())
	}

	// First failure
	err := cb.Call(func() error {
		return errors.New("failure 1")
	})
	_ = err // Suppress linter warning
	if err == nil {
		t.Error("Expected error from first failure")
	}
	if cb.GetState() != StateClosed {
		t.Error("Should remain CLOSED after first failure")
	}

	// Second failure - should open circuit
	err = cb.Call(func() error {
		return errors.New("failure 2")
	})
	if err == nil {
		t.Error("Expected error from second failure")
	}
	if cb.GetState() != StateOpen {
		t.Errorf("Should transition to OPEN after %d failures, got %s", 2, cb.GetState())
	}

	// Attempt call while OPEN - should fail fast
	err = cb.Call(func() error {
		t.Error("Function should not be called when circuit is OPEN")
		return nil
	})
	if err == nil {
		t.Error("Expected error when circuit is OPEN")
	}
	if err.Error() != "circuit breaker is open" {
		t.Errorf("Expected 'circuit breaker is open' error, got: %v", err)
	}

	// Wait for reset timeout
	time.Sleep(150 * time.Millisecond)

	// Next call should transition to HALF_OPEN
	attemptCount := 0
	err = cb.Call(func() error {
		attemptCount++
		return nil // Success
	})
	if err != nil {
		t.Errorf("Expected success in HALF_OPEN, got error: %v", err)
	}
	if attemptCount != 1 {
		t.Error("Function should have been called once in HALF_OPEN")
	}
	if cb.GetState() != StateClosed {
		t.Errorf("Should transition to CLOSED after success in HALF_OPEN, got %s", cb.GetState())
	}
}

// TestCircuitBreaker_HalfOpenFailure verifies failure in HALF_OPEN returns to OPEN
func TestCircuitBreaker_HalfOpenFailure(t *testing.T) {
	cb := NewCircuitBreaker(1, 50*time.Millisecond)

	// Trigger circuit to open
	_ = cb.Call(func() error {
		return errors.New("failure")
	})

	if cb.GetState() != StateOpen {
		t.Fatal("Circuit should be OPEN")
	}

	// Wait for reset timeout
	time.Sleep(60 * time.Millisecond)

	// Fail in HALF_OPEN - should return to OPEN
	err := cb.Call(func() error {
		return errors.New("failure in half-open")
	})
	if err == nil {
		t.Error("Expected error from failure in HALF_OPEN")
	}
	_ = err // Used above

	if cb.GetState() != StateOpen {
		t.Errorf("Should return to OPEN after failure in HALF_OPEN, got %s", cb.GetState())
	}

	// Verify can't call again immediately
	err = cb.Call(func() error {
		t.Error("Should not execute when OPEN after HALF_OPEN failure")
		return nil
	})
	if err == nil {
		t.Error("Expected circuit breaker error")
	}
}

// TestCircuitBreaker_ConsecutiveSuccesses verifies circuit stays closed with successes
func TestCircuitBreaker_ConsecutiveSuccesses(t *testing.T) {
	cb := NewCircuitBreaker(5, 100*time.Millisecond)

	for i := 0; i < 10; i++ {
		err := cb.Call(func() error {
			return nil
		})
		if err != nil {
			t.Errorf("Iteration %d: Expected success, got error: %v", i, err)
		}
		if cb.GetState() != StateClosed {
			t.Errorf("Iteration %d: Circuit should remain CLOSED, got %s", i, cb.GetState())
		}
	}
}

// TestCircuitBreaker_FailureThresholdEdge verifies behavior at threshold boundary
func TestCircuitBreaker_FailureThresholdEdge(t *testing.T) {
	threshold := uint32(3)
	cb := NewCircuitBreaker(threshold, 100*time.Millisecond)

	// Fail threshold-1 times - should stay closed
	for i := uint32(0); i < threshold-1; i++ {
		_ = cb.Call(func() error {
			return errors.New("failure")
		})
		if cb.GetState() != StateClosed {
			t.Errorf("Should remain CLOSED after %d failures (threshold=%d)", i+1, threshold)
		}
	}

	// One more failure should open it
	_ = cb.Call(func() error {
		return errors.New("failure")
	})
	if cb.GetState() != StateOpen {
		t.Errorf("Should be OPEN after %d failures", threshold)
	}
}

// TestCircuitBreaker_SuccessResetsFailureCount verifies success resets counter
func TestCircuitBreaker_SuccessResetsFailureCount(t *testing.T) {
	cb := NewCircuitBreaker(3, 100*time.Millisecond)

	// Two failures
	_ = cb.Call(func() error { return errors.New("fail") })
	_ = cb.Call(func() error { return errors.New("fail") })

	// One success - should reset counter
	_ = cb.Call(func() error { return nil })

	// Two more failures should not open circuit
	_ = cb.Call(func() error { return errors.New("fail") })
	_ = cb.Call(func() error { return errors.New("fail") })

	if cb.GetState() != StateClosed {
		t.Error("Success should reset failure counter, circuit should still be CLOSED")
	}

	// One more failure should open it (threshold reached again)
	_ = cb.Call(func() error { return errors.New("fail") })
	if cb.GetState() != StateOpen {
		t.Error("Circuit should be OPEN after reaching threshold again")
	}
}

// TestCircuitBreaker_String verifies string representation of states
func TestCircuitBreaker_String(t *testing.T) {
	tests := []struct {
		state    CircuitBreakerState
		expected string
	}{
		{StateClosed, "closed"},
		{StateOpen, "open"},
		{StateHalfOpen, "half-open"},
		{CircuitBreakerState(99), "unknown(99)"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			result := tt.state.String()
			if result != tt.expected {
				t.Errorf("State.String() = %q, expected %q", result, tt.expected)
			}
		})
	}
}

// TestCircuitBreaker_ConcurrentCalls verifies thread safety
func TestCircuitBreaker_ConcurrentCalls(t *testing.T) {
	cb := NewCircuitBreaker(10, 100*time.Millisecond)

	done := make(chan bool)
	for i := 0; i < 20; i++ {
		go func(id int) {
			defer func() { done <- true }()
			for j := 0; j < 10; j++ {
				_ = cb.Call(func() error {
					time.Sleep(1 * time.Millisecond)
					if id%2 == 0 {
						return nil
					}
					return errors.New("failure")
				})
			}
		}(i)
	}

	// Wait for all goroutines
	for i := 0; i < 20; i++ {
		<-done
	}

	// Should not panic or deadlock
	state := cb.GetState()
	t.Logf("Final state after concurrent calls: %s", state)
}

// TestRetryWithBackoff_AllFailures verifies exhausting all retry attempts
func TestRetryWithBackoff_AllFailures(t *testing.T) {
	config := RetryConfig{
		MaxAttempts:  3,
		InitialDelay: 10 * time.Millisecond,
		MaxDelay:     100 * time.Millisecond,
		Multiplier:   2.0,
	}

	attemptCount := 0
	err := RetryWithBackoff(config, func() error {
		attemptCount++
		return errors.New("persistent failure")
	})

	if err == nil {
		t.Error("Expected error after exhausting retries")
	}

	expectedAttempts := config.MaxAttempts
	if attemptCount != expectedAttempts {
		t.Errorf("Expected %d attempts, got %d", expectedAttempts, attemptCount)
	}
}

// TestRetryWithBackoff_SuccessAfterRetries verifies success before exhausting retries
func TestRetryWithBackoff_SuccessAfterRetries(t *testing.T) {
	config := RetryConfig{
		MaxAttempts:  5,
		InitialDelay: 10 * time.Millisecond,
		MaxDelay:     100 * time.Millisecond,
		Multiplier:   2.0,
	}

	attemptCount := 0
	err := RetryWithBackoff(config, func() error {
		attemptCount++
		if attemptCount < 3 {
			return errors.New("temporary failure")
		}
		return nil // Success on 3rd attempt
	})

	if err != nil {
		t.Errorf("Expected success after retries, got error: %v", err)
	}

	if attemptCount != 3 {
		t.Errorf("Expected 3 attempts, got %d", attemptCount)
	}
}

// TestRetryWithBackoff_ImmediateSuccess verifies no retry on immediate success
func TestRetryWithBackoff_ImmediateSuccess(t *testing.T) {
	config := RetryConfig{
		MaxAttempts:  5,
		InitialDelay: 10 * time.Millisecond,
		MaxDelay:     100 * time.Millisecond,
		Multiplier:   2.0,
	}

	attemptCount := 0
	startTime := time.Now()
	err := RetryWithBackoff(config, func() error {
		attemptCount++
		return nil // Immediate success
	})
	duration := time.Since(startTime)

	if err != nil {
		t.Errorf("Expected success, got error: %v", err)
	}

	if attemptCount != 1 {
		t.Errorf("Expected 1 attempt, got %d", attemptCount)
	}

	// Should complete quickly (no backoff wait)
	if duration > 50*time.Millisecond {
		t.Errorf("Immediate success took too long: %v", duration)
	}
}

// TestRetryWithBackoff_MaxWaitCap verifies wait time doesn't exceed max
func TestRetryWithBackoff_MaxWaitCap(t *testing.T) {
	config := RetryConfig{
		MaxAttempts:  10,
		InitialDelay: 5 * time.Millisecond,
		MaxDelay:     20 * time.Millisecond,
		Multiplier:   3.0, // Aggressive multiplier
	}

	startTime := time.Now()
	_ = RetryWithBackoff(config, func() error {
		return errors.New("always fail")
	})
	duration := time.Since(startTime)

	// Total wait should be capped by MaxWait
	// With 10 retries, wait times: 5, 15(capped to 20), 20, 20, ...
	// Total: ~200ms + execution time
	maxExpectedDuration := 300 * time.Millisecond
	if duration > maxExpectedDuration {
		t.Errorf("Duration %v exceeds max expected %v (MaxWait not working)", duration, maxExpectedDuration)
	}
}
