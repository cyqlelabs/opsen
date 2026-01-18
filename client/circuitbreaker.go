package main

import (
	"errors"
	"fmt"
	"sync"
	"time"
)

var (
	ErrCircuitOpen = errors.New("circuit breaker is open")
	ErrTooManyRequests = errors.New("too many requests")
)

// CircuitBreakerState represents the state of the circuit breaker
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

// CircuitBreaker implements the circuit breaker pattern
type CircuitBreaker struct {
	mu sync.RWMutex

	maxFailures     uint32        // Maximum failures before opening
	resetTimeout    time.Duration // Time to wait before transitioning to half-open
	halfOpenTimeout time.Duration // Time to wait in half-open state

	state           CircuitBreakerState
	failures        uint32
	lastFailureTime time.Time
	lastStateChange time.Time
}

// NewCircuitBreaker creates a new circuit breaker
func NewCircuitBreaker(maxFailures uint32, resetTimeout time.Duration) *CircuitBreaker {
	return &CircuitBreaker{
		maxFailures:     maxFailures,
		resetTimeout:    resetTimeout,
		halfOpenTimeout: resetTimeout / 2,
		state:           StateClosed,
		lastStateChange: time.Now(),
	}
}

// Call executes the given function if the circuit breaker is not open
func (cb *CircuitBreaker) Call(fn func() error) error {
	if err := cb.beforeCall(); err != nil {
		return err
	}

	err := fn()
	cb.afterCall(err)
	return err
}

func (cb *CircuitBreaker) beforeCall() error {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	now := time.Now()

	switch cb.state {
	case StateClosed:
		return nil

	case StateOpen:
		// Check if we should transition to half-open
		if now.Sub(cb.lastStateChange) > cb.resetTimeout {
			cb.setState(StateHalfOpen, now)
			return nil
		}
		return ErrCircuitOpen

	case StateHalfOpen:
		// Allow one request through
		return nil

	default:
		return ErrCircuitOpen
	}
}

func (cb *CircuitBreaker) afterCall(err error) {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	now := time.Now()

	if err != nil {
		cb.failures++
		cb.lastFailureTime = now

		switch cb.state {
		case StateClosed:
			if cb.failures >= cb.maxFailures {
				cb.setState(StateOpen, now)
			}

		case StateHalfOpen:
			// Failed while half-open, go back to open
			cb.setState(StateOpen, now)
		}
	} else {
		// Success
		switch cb.state {
		case StateClosed:
			// Reset failure count on success
			cb.failures = 0

		case StateHalfOpen:
			// Success in half-open state, transition to closed
			cb.setState(StateClosed, now)
			cb.failures = 0
		}
	}
}

func (cb *CircuitBreaker) setState(state CircuitBreakerState, now time.Time) {
	if cb.state != state {
		prevState := cb.state
		cb.state = state
		cb.lastStateChange = now
		LogInfo(fmt.Sprintf("Circuit breaker state changed: %s -> %s (failures: %d)",
			prevState, state, cb.failures))
	}
}

// GetState returns the current state of the circuit breaker
func (cb *CircuitBreaker) GetState() CircuitBreakerState {
	cb.mu.RLock()
	defer cb.mu.RUnlock()
	return cb.state
}

// GetFailures returns the current failure count
func (cb *CircuitBreaker) GetFailures() uint32 {
	cb.mu.RLock()
	defer cb.mu.RUnlock()
	return cb.failures
}

// Reset manually resets the circuit breaker to closed state
func (cb *CircuitBreaker) Reset() {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.setState(StateClosed, time.Now())
	cb.failures = 0
}

// RetryConfig defines retry behavior
type RetryConfig struct {
	MaxAttempts  int
	InitialDelay time.Duration
	MaxDelay     time.Duration
	Multiplier   float64
}

// DefaultRetryConfig returns sensible defaults for retry
func DefaultRetryConfig() RetryConfig {
	return RetryConfig{
		MaxAttempts:  5,
		InitialDelay: 1 * time.Second,
		MaxDelay:     30 * time.Second,
		Multiplier:   2.0,
	}
}

// RetryWithBackoff retries a function with exponential backoff
func RetryWithBackoff(config RetryConfig, fn func() error) error {
	var err error
	delay := config.InitialDelay

	for attempt := 0; attempt < config.MaxAttempts; attempt++ {
		err = fn()
		if err == nil {
			return nil
		}

		// Don't retry if circuit is open
		if errors.Is(err, ErrCircuitOpen) {
			return err
		}

		// Last attempt, don't sleep
		if attempt == config.MaxAttempts-1 {
			break
		}

		LogWarn(fmt.Sprintf("Attempt %d/%d failed: %v. Retrying in %s...",
			attempt+1, config.MaxAttempts, err, delay))

		time.Sleep(delay)

		// Calculate next delay with exponential backoff
		delay = time.Duration(float64(delay) * config.Multiplier)
		if delay > config.MaxDelay {
			delay = config.MaxDelay
		}
	}

	return fmt.Errorf("failed after %d attempts: %w", config.MaxAttempts, err)
}
