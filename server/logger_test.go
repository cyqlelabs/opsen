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

// captureOutput captures stdout/stderr for testing log output
func captureOutput(f func()) string {
	var buf bytes.Buffer
	log.SetOutput(&buf)
	defer log.SetOutput(os.Stderr)

	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	f()

	w.Close()
	os.Stdout = oldStdout

	stdoutBytes, _ := io.ReadAll(r)
	return buf.String() + string(stdoutBytes)
}

// TestServerLogLevel_String verifies LogLevel string representation
func TestServerLogLevel_String(t *testing.T) {
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

// TestServerInitLogger verifies logger initialization
func TestServerInitLogger(t *testing.T) {
	defaultLogger = nil
	once = sync.Once{}

	InitLogger("info", false, "SERVER")

	if defaultLogger == nil {
		t.Fatal("defaultLogger should not be nil after InitLogger")
	}

	if defaultLogger.jsonFormat != false {
		t.Errorf("Expected jsonFormat false, got %v", defaultLogger.jsonFormat)
	}

	if defaultLogger.prefix != "SERVER" {
		t.Errorf("Expected prefix SERVER, got %s", defaultLogger.prefix)
	}
}

// TestServerLogger_PlainTextFormat verifies plain text logging
func TestServerLogger_PlainTextFormat(t *testing.T) {
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
}

// TestServerLogger_JSONFormat verifies JSON logging
func TestServerLogger_JSONFormat(t *testing.T) {
	defaultLogger = nil
	once = sync.Once{}

	InitLogger("info", true, "APP")

	output := captureOutput(func() {
		LogInfo("json test message")
	})

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

	var entry LogEntry
	if err := json.Unmarshal([]byte(jsonLine), &entry); err != nil {
		t.Fatalf("Failed to parse JSON output: %v\nOutput: %s", err, jsonLine)
	}

	if entry.Level != "INFO" {
		t.Errorf("Expected level INFO, got %s", entry.Level)
	}
	if entry.Message != "json test message" {
		t.Errorf("Expected message 'json test message', got %s", entry.Message)
	}
}

// TestServerLogger_WithData verifies logging with additional data
func TestServerLogger_WithData(t *testing.T) {
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
}

// TestServerLogger_AllLogLevels verifies all logging functions
func TestServerLogger_AllLogLevels(t *testing.T) {
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

// TestServerLogger_WithDataFunctions verifies all WithData logging functions
func TestServerLogger_WithDataFunctions(t *testing.T) {
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

// TestServerLogger_LevelFiltering verifies log level filtering works
func TestServerLogger_LevelFiltering(t *testing.T) {
	tests := []struct {
		name         string
		logLevel     string
		shouldLog    map[string]bool
	}{
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

// TestServerLogger_LevelFilteringWithData verifies log level filtering for WithData functions
func TestServerLogger_LevelFilteringWithData(t *testing.T) {
	data := map[string]interface{}{"test": "data"}

	tests := []struct {
		logLevel     string
		shouldLogDebug bool
		shouldLogWarn  bool
	}{
		{"info", false, true},
		{"warn", false, true},
		{"error", false, false},
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

			// Test WarnWithData
			output = captureOutput(func() { LogWarnWithData("warn with data", data) })
			if tt.shouldLogWarn && !strings.Contains(output, "warn with data") {
				t.Errorf("Expected warn message to be logged at level %s", tt.logLevel)
			}
			if !tt.shouldLogWarn && strings.Contains(output, "warn with data") {
				t.Errorf("Expected warn message to be filtered at level %s", tt.logLevel)
			}

			// ErrorWithData should always log
			output = captureOutput(func() { LogErrorWithData("error with data", data) })
			if !strings.Contains(output, "error with data") {
				t.Errorf("Expected error message to be logged at level %s", tt.logLevel)
			}
		})
	}
}

// TestServerLogger_NoDefaultLogger verifies fallback when logger not initialized
func TestServerLogger_NoDefaultLogger(t *testing.T) {
	defaultLogger = nil

	output := captureOutput(func() {
		LogInfo("fallback message")
	})

	if !strings.Contains(output, "INFO: fallback message") {
		t.Errorf("Expected fallback log format, got: %s", output)
	}
}
