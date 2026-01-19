package main

import (
	"bytes"
	"encoding/json"
	"io"
	"log"
	"os"
	"strings"
	"sync"
	"testing"
)

// TestLogLevel_String verifies LogLevel string representation
func TestLogLevel_String(t *testing.T) {
	tests := []struct {
		level    LogLevel
		expected string
	}{
		{LogLevelDebug, "DEBUG"},
		{LogLevelInfo, "INFO"},
		{LogLevelWarn, "WARN"},
		{LogLevelError, "ERROR"},
		{LogLevelFatal, "FATAL"},
		{LogLevel(999), "UNKNOWN"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			if got := tt.level.String(); got != tt.expected {
				t.Errorf("Expected %s, got %s", tt.expected, got)
			}
		})
	}
}

// TestInitLogger verifies logger initialization
func TestInitLogger(t *testing.T) {
	// Reset defaultLogger for testing
	defaultLogger = nil
	once = sync.Once{}

	tests := []struct {
		name       string
		level      string
		jsonFormat bool
		prefix     string
	}{
		{"debug_json", "debug", true, "TEST"},
		{"info_plain", "info", false, "APP"},
		{"warn_plain", "warn", false, ""},
		{"error_json", "error", true, "ERR"},
		{"invalid_defaults", "invalid", false, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Reset for each test
			defaultLogger = nil
			once = sync.Once{}

			InitLogger(tt.level, tt.jsonFormat, tt.prefix)

			if defaultLogger == nil {
				t.Fatal("defaultLogger should not be nil after InitLogger")
			}

			if defaultLogger.jsonFormat != tt.jsonFormat {
				t.Errorf("Expected jsonFormat %v, got %v", tt.jsonFormat, defaultLogger.jsonFormat)
			}

			if defaultLogger.prefix != tt.prefix {
				t.Errorf("Expected prefix %s, got %s", tt.prefix, defaultLogger.prefix)
			}
		})
	}
}

// TestParseLogLevel verifies log level parsing
func TestParseLogLevel(t *testing.T) {
	tests := []struct {
		input    string
		expected LogLevel
	}{
		{"debug", LogLevelDebug},
		{"info", LogLevelInfo},
		{"warn", LogLevelWarn},
		{"warning", LogLevelWarn},
		{"error", LogLevelError},
		{"fatal", LogLevelFatal},
		{"invalid", LogLevelInfo},
		{"", LogLevelInfo},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			if got := parseLogLevel(tt.input); got != tt.expected {
				t.Errorf("parseLogLevel(%s) = %v, want %v", tt.input, got, tt.expected)
			}
		})
	}
}

// captureOutput captures stdout/stderr for testing log output
func captureOutput(f func()) string {
	// Redirect log output
	var buf bytes.Buffer
	log.SetOutput(&buf)
	defer log.SetOutput(os.Stderr)

	// Also capture stdout for JSON format
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	f()

	w.Close()
	os.Stdout = oldStdout

	stdoutBytes, _ := io.ReadAll(r)

	// Combine log output and stdout
	return buf.String() + string(stdoutBytes)
}

// TestLogger_PlainTextFormat verifies plain text logging
func TestLogger_PlainTextFormat(t *testing.T) {
	defaultLogger = nil
	once = sync.Once{}

	InitLogger("debug", false, "TEST")

	output := captureOutput(func() {
		LogDebug("debug message")
	})

	if !strings.Contains(output, "DEBUG") {
		t.Errorf("Expected DEBUG in output, got: %s", output)
	}
	if !strings.Contains(output, "debug message") {
		t.Errorf("Expected 'debug message' in output, got: %s", output)
	}
	if !strings.Contains(output, "TEST") {
		t.Errorf("Expected prefix 'TEST' in output, got: %s", output)
	}
}

// TestLogger_JSONFormat verifies JSON logging
func TestLogger_JSONFormat(t *testing.T) {
	defaultLogger = nil
	once = sync.Once{}

	InitLogger("info", true, "APP")

	output := captureOutput(func() {
		LogInfo("json test message")
	})

	// Parse JSON
	var entry LogEntry
	// Find the JSON line in the output
	lines := strings.Split(output, "\n")
	var jsonLine string
	for _, line := range lines {
		if strings.HasPrefix(line, "{") {
			jsonLine = line
			break
		}
	}

	if jsonLine == "" {
		t.Fatalf("No JSON output found in: %s", output)
	}

	if err := json.Unmarshal([]byte(jsonLine), &entry); err != nil {
		t.Fatalf("Failed to parse JSON output: %v\nOutput: %s", err, jsonLine)
	}

	if entry.Level != "INFO" {
		t.Errorf("Expected level INFO, got %s", entry.Level)
	}
	if entry.Message != "json test message" {
		t.Errorf("Expected message 'json test message', got %s", entry.Message)
	}
	if entry.Prefix != "APP" {
		t.Errorf("Expected prefix APP, got %s", entry.Prefix)
	}
}

// TestLogger_WithData verifies logging with additional data
func TestLogger_WithData(t *testing.T) {
	defaultLogger = nil
	once = sync.Once{}

	InitLogger("debug", true, "")

	data := map[string]interface{}{
		"user_id": 123,
		"action":  "login",
	}

	output := captureOutput(func() {
		LogInfoWithData("user action", data)
	})

	// Find JSON line
	lines := strings.Split(output, "\n")
	var jsonLine string
	for _, line := range lines {
		if strings.HasPrefix(line, "{") {
			jsonLine = line
			break
		}
	}

	var entry LogEntry
	if err := json.Unmarshal([]byte(jsonLine), &entry); err != nil {
		t.Fatalf("Failed to parse JSON: %v", err)
	}

	if entry.Data == nil {
		t.Fatal("Expected data field in log entry")
	}
	if entry.Data["user_id"] != float64(123) {
		t.Errorf("Expected user_id 123, got %v", entry.Data["user_id"])
	}
	if entry.Data["action"] != "login" {
		t.Errorf("Expected action 'login', got %v", entry.Data["action"])
	}
}

// TestLogger_LevelFiltering verifies log level filtering
func TestLogger_LevelFiltering(t *testing.T) {
	defaultLogger = nil
	once = sync.Once{}

	// Set to WARN level
	InitLogger("warn", false, "")

	// Debug should not appear
	debugOutput := captureOutput(func() {
		LogDebug("should not appear")
	})
	if strings.Contains(debugOutput, "should not appear") {
		t.Error("DEBUG message should be filtered")
	}

	// Info should not appear
	infoOutput := captureOutput(func() {
		LogInfo("should not appear")
	})
	if strings.Contains(infoOutput, "should not appear") {
		t.Error("INFO message should be filtered")
	}

	// Warn should appear
	warnOutput := captureOutput(func() {
		LogWarn("should appear")
	})
	if !strings.Contains(warnOutput, "should appear") {
		t.Error("WARN message should appear")
	}

	// Error should appear
	errorOutput := captureOutput(func() {
		LogError("should appear")
	})
	if !strings.Contains(errorOutput, "should appear") {
		t.Error("ERROR message should appear")
	}
}

// TestLogger_AllLogLevels verifies all logging functions
func TestLogger_AllLogLevels(t *testing.T) {
	defaultLogger = nil
	once = sync.Once{}

	InitLogger("debug", false, "")

	tests := []struct {
		name     string
		logFunc  func(string)
		expected string
	}{
		{"Debug", LogDebug, "DEBUG"},
		{"Info", LogInfo, "INFO"},
		{"Warn", LogWarn, "WARN"},
		{"Error", LogError, "ERROR"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			output := captureOutput(func() {
				tt.logFunc("test message")
			})
			if !strings.Contains(output, tt.expected) {
				t.Errorf("Expected %s in output, got: %s", tt.expected, output)
			}
		})
	}
}

// TestLogger_WithDataFunctions verifies all WithData logging functions
func TestLogger_WithDataFunctions(t *testing.T) {
	defaultLogger = nil
	once = sync.Once{}

	InitLogger("debug", true, "")

	data := map[string]interface{}{"key": "value"}

	tests := []struct {
		name     string
		logFunc  func(string, map[string]interface{})
		expected string
	}{
		{"DebugWithData", LogDebugWithData, "DEBUG"},
		{"InfoWithData", LogInfoWithData, "INFO"},
		{"WarnWithData", LogWarnWithData, "WARN"},
		{"ErrorWithData", LogErrorWithData, "ERROR"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			output := captureOutput(func() {
				tt.logFunc("test message", data)
			})

			// Find JSON line
			lines := strings.Split(output, "\n")
			var jsonLine string
			for _, line := range lines {
				if strings.HasPrefix(line, "{") {
					jsonLine = line
					break
				}
			}

			var entry LogEntry
			if err := json.Unmarshal([]byte(jsonLine), &entry); err != nil {
				t.Fatalf("Failed to parse JSON: %v", err)
			}

			if entry.Level != tt.expected {
				t.Errorf("Expected level %s, got %s", tt.expected, entry.Level)
			}
			if entry.Data["key"] != "value" {
				t.Errorf("Expected data key=value, got %v", entry.Data)
			}
		})
	}
}

// TestLogger_LevelFilteringWithData verifies log level filtering for WithData functions
func TestLogger_LevelFilteringWithData(t *testing.T) {
	data := map[string]interface{}{"test": "data"}

	tests := []struct {
		logLevel       string
		shouldLogDebug bool
		shouldLogInfo  bool
		shouldLogWarn  bool
		shouldLogError bool
	}{
		{"debug", true, true, true, true},
		{"info", false, true, true, true},
		{"warn", false, false, true, true},
		{"error", false, false, false, true},
	}

	for _, tt := range tests {
		t.Run("Level_"+tt.logLevel, func(t *testing.T) {
			defaultLogger = nil
			once = sync.Once{}
			InitLogger(tt.logLevel, true, "")

			// Test DebugWithData
			output := captureOutput(func() { LogDebugWithData("debug with data", data) })
			if tt.shouldLogDebug && !strings.Contains(output, "debug with data") {
				t.Errorf("Expected debug message to be logged at level %s", tt.logLevel)
			}
			if !tt.shouldLogDebug && strings.Contains(output, "debug with data") {
				t.Errorf("Expected debug message to be filtered at level %s", tt.logLevel)
			}

			// Test InfoWithData
			output = captureOutput(func() { LogInfoWithData("info with data", data) })
			if tt.shouldLogInfo && !strings.Contains(output, "info with data") {
				t.Errorf("Expected info message to be logged at level %s", tt.logLevel)
			}
			if !tt.shouldLogInfo && strings.Contains(output, "info with data") {
				t.Errorf("Expected info message to be filtered at level %s", tt.logLevel)
			}

			// Test WarnWithData
			output = captureOutput(func() { LogWarnWithData("warn with data", data) })
			if tt.shouldLogWarn && !strings.Contains(output, "warn with data") {
				t.Errorf("Expected warn message to be logged at level %s", tt.logLevel)
			}
			if !tt.shouldLogWarn && strings.Contains(output, "warn with data") {
				t.Errorf("Expected warn message to be filtered at level %s", tt.logLevel)
			}

			// Test ErrorWithData
			output = captureOutput(func() { LogErrorWithData("error with data", data) })
			if tt.shouldLogError && !strings.Contains(output, "error with data") {
				t.Errorf("Expected error message to be logged at level %s", tt.logLevel)
			}
			if !tt.shouldLogError && strings.Contains(output, "error with data") {
				t.Errorf("Expected error message to be filtered at level %s", tt.logLevel)
			}
		})
	}
}

// TestLogger_ComprehensiveLevelFiltering tests all levels and functions
func TestLogger_ComprehensiveLevelFiltering(t *testing.T) {
	tests := []struct {
		name         string
		logLevel     string
		shouldLog    map[string]bool
	}{
		{
			name:     "DebugLevel",
			logLevel: "debug",
			shouldLog: map[string]bool{
				"debug": true,
				"info":  true,
				"warn":  true,
				"error": true,
			},
		},
		{
			name:     "InfoLevel",
			logLevel: "info",
			shouldLog: map[string]bool{
				"debug": false,
				"info":  true,
				"warn":  true,
				"error": true,
			},
		},
		{
			name:     "WarnLevel",
			logLevel: "warn",
			shouldLog: map[string]bool{
				"debug": false,
				"info":  false,
				"warn":  true,
				"error": true,
			},
		},
		{
			name:     "ErrorLevel",
			logLevel: "error",
			shouldLog: map[string]bool{
				"debug": false,
				"info":  false,
				"warn":  false,
				"error": true,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			defaultLogger = nil
			once = sync.Once{}
			InitLogger(tt.logLevel, false, "")

			// Test debug logging
			output := captureOutput(func() { LogDebug("debug message") })
			if tt.shouldLog["debug"] && !strings.Contains(output, "debug message") {
				t.Errorf("Expected debug message to be logged at level %s", tt.logLevel)
			}
			if !tt.shouldLog["debug"] && strings.Contains(output, "debug message") {
				t.Errorf("Expected debug message to be filtered at level %s", tt.logLevel)
			}

			// Test info logging
			output = captureOutput(func() { LogInfo("info message") })
			if tt.shouldLog["info"] && !strings.Contains(output, "info message") {
				t.Errorf("Expected info message to be logged at level %s", tt.logLevel)
			}
			if !tt.shouldLog["info"] && strings.Contains(output, "info message") {
				t.Errorf("Expected info message to be filtered at level %s", tt.logLevel)
			}

			// Test warn logging
			output = captureOutput(func() { LogWarn("warn message") })
			if tt.shouldLog["warn"] && !strings.Contains(output, "warn message") {
				t.Errorf("Expected warn message to be logged at level %s", tt.logLevel)
			}
			if !tt.shouldLog["warn"] && strings.Contains(output, "warn message") {
				t.Errorf("Expected warn message to be filtered at level %s", tt.logLevel)
			}

			// Test error logging
			output = captureOutput(func() { LogError("error message") })
			if tt.shouldLog["error"] && !strings.Contains(output, "error message") {
				t.Errorf("Expected error message to be logged at level %s", tt.logLevel)
			}
			if !tt.shouldLog["error"] && strings.Contains(output, "error message") {
				t.Errorf("Expected error message to be filtered at level %s", tt.logLevel)
			}
		})
	}
}

// TestLogger_NoDefaultLogger verifies fallback when logger not initialized
func TestLogger_NoDefaultLogger(t *testing.T) {
	defaultLogger = nil
	// Don't initialize - test fallback behavior

	output := captureOutput(func() {
		LogInfo("fallback message")
	})

	if !strings.Contains(output, "INFO: fallback message") {
		t.Errorf("Expected fallback log format, got: %s", output)
	}
}

// TestLogger_NoDefaultLoggerWithData verifies fallback with data
func TestLogger_NoDefaultLoggerWithData(t *testing.T) {
	defaultLogger = nil

	data := map[string]interface{}{"test": 123}

	output := captureOutput(func() {
		LogInfoWithData("fallback with data", data)
	})

	if !strings.Contains(output, "INFO: fallback with data") {
		t.Errorf("Expected fallback message, got: %s", output)
	}
	if !strings.Contains(output, "data=") {
		t.Errorf("Expected data in fallback, got: %s", output)
	}
}

// TestLogger_ConcurrentAccess verifies thread-safety
func TestLogger_ConcurrentAccess(t *testing.T) {
	defaultLogger = nil
	once = sync.Once{}

	InitLogger("info", false, "CONCURRENT")

	var wg sync.WaitGroup
	iterations := 100

	// Concurrent writes
	for i := 0; i < iterations; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			LogInfo("concurrent message")
		}(i)
	}

	wg.Wait()
	// Test passes if no race conditions detected
}
