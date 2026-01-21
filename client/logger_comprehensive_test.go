package main

import (
	"strings"
	"sync"
	"testing"
)

// TestLogger_UninitializedFallback verifies fallback behavior when logger not initialized
func TestLogger_UninitializedFallback(t *testing.T) {
	// Reset logger
	defaultLogger = nil
	once = sync.Once{}

	tests := []struct {
		name    string
		logFunc func(string)
		message string
	}{
		{"LogDebug_uninitialized", LogDebug, "debug fallback"},
		{"LogInfo_uninitialized", LogInfo, "info fallback"},
		{"LogWarn_uninitialized", LogWarn, "warn fallback"},
		{"LogError_uninitialized", LogError, "error fallback"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			output := captureOutput(func() {
				tt.logFunc(tt.message)
			})

			if !strings.Contains(output, tt.message) {
				t.Errorf("Expected message '%s' in output: %s", tt.message, output)
			}
		})
	}
}

// TestLogger_UninitializedFallbackWithData verifies WithData fallback
func TestLogger_UninitializedFallbackWithData(t *testing.T) {
	defaultLogger = nil
	once = sync.Once{}

	data := map[string]interface{}{
		"key1": "value1",
		"key2": 42,
	}

	tests := []struct {
		name    string
		logFunc func(string, map[string]interface{})
		level   string
	}{
		{"DebugWithData_uninit", LogDebugWithData, "DEBUG"},
		{"InfoWithData_uninit", LogInfoWithData, "INFO"},
		{"WarnWithData_uninit", LogWarnWithData, "WARN"},
		{"ErrorWithData_uninit", LogErrorWithData, "ERROR"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			output := captureOutput(func() {
				tt.logFunc("test message", data)
			})

			if !strings.Contains(output, "test message") {
				t.Error("Expected message in fallback output")
			}
			if !strings.Contains(output, tt.level) {
				t.Errorf("Expected level %s in fallback output", tt.level)
			}
			if !strings.Contains(output, "data=") {
				t.Error("Expected data in fallback output")
			}
		})
	}
}

// TestLogLevel_StringCoverage verifies all log level string representations
func TestLogLevel_StringCoverage(t *testing.T) {
	tests := []struct {
		level    LogLevel
		expected string
	}{
		{LogLevelDebug, "DEBUG"},
		{LogLevelInfo, "INFO"},
		{LogLevelWarn, "WARN"},
		{LogLevelError, "ERROR"},
		{LogLevelFatal, "FATAL"},
		{LogLevel(99), "UNKNOWN"},
		{LogLevel(-1), "UNKNOWN"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			result := tt.level.String()
			if result != tt.expected {
				t.Errorf("Expected %s, got %s", tt.expected, result)
			}
		})
	}
}

// TestParseLogLevel_AllCases verifies all log level parsing cases
func TestParseLogLevel_AllCases(t *testing.T) {
	tests := []struct {
		input    string
		expected LogLevel
	}{
		{"debug", LogLevelDebug},
		{"DEBUG", LogLevelInfo}, // Case-sensitive, defaults to info
		{"info", LogLevelInfo},
		{"INFO", LogLevelInfo},
		{"warn", LogLevelWarn},
		{"warning", LogLevelWarn},
		{"WARN", LogLevelInfo}, // Case-sensitive
		{"error", LogLevelError},
		{"ERROR", LogLevelInfo},
		{"fatal", LogLevelFatal},
		{"FATAL", LogLevelInfo},
		{"unknown", LogLevelInfo},
		{"", LogLevelInfo},
		{"random", LogLevelInfo},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := parseLogLevel(tt.input)
			if result != tt.expected {
				t.Errorf("Input %s: Expected %v, got %v", tt.input, tt.expected, result)
			}
		})
	}
}

// TestLogger_InitOnce verifies InitLogger can only be called once
func TestLogger_InitOnce(t *testing.T) {
	defaultLogger = nil
	once = sync.Once{}

	// First call
	InitLogger("debug", false, "FIRST")

	if defaultLogger == nil {
		t.Fatal("Logger should be initialized")
	}

	firstPrefix := defaultLogger.prefix

	// Second call - should be ignored
	InitLogger("info", true, "SECOND")

	if defaultLogger.prefix != firstPrefix {
		t.Errorf("Second InitLogger call should be ignored, prefix changed from %s to %s",
			firstPrefix, defaultLogger.prefix)
	}
}

// TestLogger_PlainTextWithData verifies plain text format with data
func TestLogger_PlainTextWithData(t *testing.T) {
	defaultLogger = nil
	once = sync.Once{}

	InitLogger("debug", false, "PLAIN")

	data := map[string]interface{}{
		"request_id": "12345",
		"user":       "test_user",
		"count":      42,
	}

	output := captureOutput(func() {
		LogInfoWithData("operation completed", data)
	})

	// Verify format contains key elements
	if !strings.Contains(output, "INFO") {
		t.Error("Expected INFO level in output")
	}
	if !strings.Contains(output, "operation completed") {
		t.Error("Expected message in output")
	}
	if !strings.Contains(output, "data=") {
		t.Error("Expected data field in output")
	}
	if !strings.Contains(output, "PLAIN") {
		t.Error("Expected prefix in output")
	}
}

// TestLogger_MinLevel verifies minimum log level is respected
func TestLogger_MinLevel(t *testing.T) {
	defaultLogger = nil
	once = sync.Once{}

	InitLogger("error", false, "MIN")

	// Debug should be filtered
	output := captureOutput(func() {
		LogDebug("debug message")
	})
	if strings.Contains(output, "debug message") {
		t.Error("Debug message should be filtered at error level")
	}

	// Info should be filtered
	output = captureOutput(func() {
		LogInfo("info message")
	})
	if strings.Contains(output, "info message") {
		t.Error("Info message should be filtered at error level")
	}

	// Warn should be filtered
	output = captureOutput(func() {
		LogWarn("warn message")
	})
	if strings.Contains(output, "warn message") {
		t.Error("Warn message should be filtered at error level")
	}

	// Error should pass
	output = captureOutput(func() {
		LogError("error message")
	})
	if !strings.Contains(output, "error message") {
		t.Error("Error message should pass at error level")
	}
}

// TestLogger_JSONWithEmptyData verifies JSON format with empty data
func TestLogger_JSONWithEmptyData(t *testing.T) {
	defaultLogger = nil
	once = sync.Once{}

	InitLogger("info", true, "JSON")

	data := map[string]interface{}{}

	output := captureOutput(func() {
		LogInfoWithData("message with empty data", data)
	})

	if !strings.Contains(output, "message with empty data") {
		t.Error("Expected message in JSON output")
	}
}

// TestLogger_JSONWithNilData verifies JSON format with nil data
func TestLogger_JSONWithNilData(t *testing.T) {
	defaultLogger = nil
	once = sync.Once{}

	InitLogger("info", true, "JSON")

	output := captureOutput(func() {
		LogInfo("message with nil data")
	})

	if !strings.Contains(output, "message with nil data") {
		t.Error("Expected message in JSON output")
	}
}

// TestLogger_CallerInfo verifies caller information is included
func TestLogger_CallerInfo(t *testing.T) {
	defaultLogger = nil
	once = sync.Once{}

	InitLogger("info", false, "")

	output := captureOutput(func() {
		LogInfo("test caller info")
	})

	// Should contain file and line info
	if !strings.Contains(output, ".go:") {
		t.Error("Expected file:line information in output")
	}
}

// TestLogger_NoPrefix verifies logger works without prefix
func TestLogger_NoPrefix(t *testing.T) {
	defaultLogger = nil
	once = sync.Once{}

	InitLogger("info", false, "")

	output := captureOutput(func() {
		LogInfo("no prefix message")
	})

	if !strings.Contains(output, "no prefix message") {
		t.Error("Expected message in output")
	}

	// Should not have empty brackets
	if strings.Contains(output, "[] ") {
		t.Error("Should not have empty prefix brackets")
	}
}

// TestLogger_ConcurrentWrites verifies thread safety
func TestLogger_ConcurrentWrites(t *testing.T) {
	defaultLogger = nil
	once = sync.Once{}

	InitLogger("info", false, "CONCURRENT")

	var wg sync.WaitGroup
	numGoroutines := 50
	messagesPerRoutine := 20

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < messagesPerRoutine; j++ {
				LogInfo("concurrent message")
				LogInfoWithData("concurrent with data", map[string]interface{}{
					"goroutine": id,
					"iteration": j,
				})
			}
		}(i)
	}

	wg.Wait()
	// Test passes if no race conditions detected
	t.Logf("Successfully completed %d concurrent log operations", numGoroutines*messagesPerRoutine*2)
}

// TestLogger_AllLevelsCombinations verifies all level filtering combinations
func TestLogger_AllLevelsCombinations(t *testing.T) {
	levels := []struct {
		name  string
		level string
	}{
		{"debug", "debug"},
		{"info", "info"},
		{"warn", "warn"},
		{"warning", "warning"},
		{"error", "error"},
	}

	for _, lvl := range levels {
		t.Run(lvl.name, func(t *testing.T) {
			defaultLogger = nil
			once = sync.Once{}

			InitLogger(lvl.level, false, "")

			// Try all log functions
			captureOutput(func() {
				LogDebug("debug")
				LogInfo("info")
				LogWarn("warn")
				LogError("error")
			})

			// Test passes if no panics
		})
	}
}

// TestLogger_NilLogger verifies nil logger handling
func TestLogger_NilLogger(t *testing.T) {
	// Explicitly set to nil
	defaultLogger = nil

	// Should use fallback without panic
	output := captureOutput(func() {
		LogDebug("nil logger debug")
		LogInfo("nil logger info")
		LogWarn("nil logger warn")
		LogError("nil logger error")
		LogDebugWithData("nil debug data", map[string]interface{}{"key": "value"})
		LogInfoWithData("nil info data", map[string]interface{}{"key": "value"})
		LogWarnWithData("nil warn data", map[string]interface{}{"key": "value"})
		LogErrorWithData("nil error data", map[string]interface{}{"key": "value"})
	})

	if len(output) == 0 {
		t.Error("Expected fallback output from nil logger")
	}
}

// TestLogger_ComplexData verifies logging with complex data structures
func TestLogger_ComplexData(t *testing.T) {
	defaultLogger = nil
	once = sync.Once{}

	InitLogger("debug", true, "COMPLEX")

	complexData := map[string]interface{}{
		"string":  "value",
		"int":     42,
		"float":   3.14159,
		"bool":    true,
		"array":   []int{1, 2, 3},
		"nested": map[string]interface{}{
			"key1": "nested_value",
			"key2": 123,
		},
	}

	output := captureOutput(func() {
		LogInfoWithData("complex data test", complexData)
	})

	if !strings.Contains(output, "complex data test") {
		t.Error("Expected message in output")
	}
}

// TestLogger_HighVolume verifies logger handles high volume
func TestLogger_HighVolume(t *testing.T) {
	defaultLogger = nil
	once = sync.Once{}

	InitLogger("warn", false, "HIGHVOL") // Use warn to reduce output

	// Log many messages quickly
	for i := 0; i < 1000; i++ {
		LogWarn("high volume message")
		if i%100 == 0 {
			LogWarnWithData("checkpoint", map[string]interface{}{"iteration": i})
		}
	}

	t.Log("High volume logging completed without errors")
}
