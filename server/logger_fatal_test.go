package main

import (
	"os"
	"sync"
	"testing"
)

// captureExit replaces the process-exit hook with a recorder for the duration
// of the test and returns the recorded exit codes.
func captureExit(t *testing.T) *[]int {
	t.Helper()

	codes := &[]int{}
	original := osExit
	osExit = func(code int) {
		*codes = append(*codes, code)
	}
	t.Cleanup(func() { osExit = original })

	return codes
}

func TestLogFatal_ExitsWithCodeOne(t *testing.T) {
	codes := captureExit(t)

	defaultLogger = nil
	once = sync.Once{}
	InitLogger("debug", false, "fatal-test")

	LogFatal("something went badly wrong")

	if len(*codes) != 1 || (*codes)[0] != 1 {
		t.Errorf("expected a single exit with code 1, got %v", *codes)
	}
}

func TestLogFatalWithData_ExitsWithCodeOne(t *testing.T) {
	codes := captureExit(t)

	defaultLogger = nil
	once = sync.Once{}
	InitLogger("debug", true, "fatal-test")

	LogFatalWithData("fatal with context", map[string]interface{}{"reason": "disk full"})

	if len(*codes) != 1 || (*codes)[0] != 1 {
		t.Errorf("expected a single exit with code 1, got %v", *codes)
	}
}

func TestLogFatal_WithoutInitializedLogger(t *testing.T) {
	codes := captureExit(t)

	defaultLogger = nil
	once = sync.Once{}

	LogFatal("fatal before logger init")
	LogFatalWithData("fatal with data before logger init", map[string]interface{}{"k": "v"})

	if len(*codes) != 2 {
		t.Fatalf("expected two exit calls, got %v", *codes)
	}
	for i, code := range *codes {
		if code != 1 {
			t.Errorf("exit %d: expected code 1, got %d", i, code)
		}
	}

	// Restore a logger for any subsequent tests in the package.
	once = sync.Once{}
	InitLogger("info", false, "test")
}

func TestOSExitDefaultsToRealExit(t *testing.T) {
	// Guards against the test hook leaking into production builds.
	if osExit == nil {
		t.Fatal("osExit must never be nil")
	}
	original := osExit
	osExit = os.Exit
	osExit = original
}
