package main

import (
	"sync"
	"testing"
)

// TestLogHelpers_WithoutInitializedLogger exercises the fallback path every
// helper takes before InitLogger has run.
func TestLogHelpers_WithoutInitializedLogger(t *testing.T) {
	original := defaultLogger
	defaultLogger = nil
	t.Cleanup(func() {
		defaultLogger = original
	})

	data := map[string]interface{}{"key": "value"}

	LogDebug("debug message")
	LogDebugWithData("debug message", data)
	LogInfo("info message")
	LogInfoWithData("info message", data)
	LogWarn("warn message")
	LogWarnWithData("warn message", data)
	LogError("error message")
	LogErrorWithData("error message", data)
}

// TestLogHelpers_WithInitializedLogger drives every helper through the logger.
func TestLogHelpers_WithInitializedLogger(t *testing.T) {
	original := defaultLogger
	defaultLogger = nil
	once = sync.Once{}
	InitLogger("debug", false, "helpers")
	t.Cleanup(func() {
		defaultLogger = original
	})

	data := map[string]interface{}{"key": "value"}

	LogDebug("debug message")
	LogDebugWithData("debug message", data)
	LogInfo("info message")
	LogInfoWithData("info message", data)
	LogWarn("warn message")
	LogWarnWithData("warn message", data)
	LogError("error message")
	LogErrorWithData("error message", data)
}

// TestLogger_NilReceiverIsSafe verifies the guard inside (*Logger).log.
func TestLogger_NilReceiverIsSafe(t *testing.T) {
	var logger *Logger
	logger.log(LogLevelError, "must not panic", nil)
}
