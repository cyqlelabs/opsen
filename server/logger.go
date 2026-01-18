package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"runtime"
	"sync"
	"time"
)

// LogLevel represents logging severity
type LogLevel int

const (
	LogLevelDebug LogLevel = iota
	LogLevelInfo
	LogLevelWarn
	LogLevelError
	LogLevelFatal
)

func (l LogLevel) String() string {
	switch l {
	case LogLevelDebug:
		return "DEBUG"
	case LogLevelInfo:
		return "INFO"
	case LogLevelWarn:
		return "WARN"
	case LogLevelError:
		return "ERROR"
	case LogLevelFatal:
		return "FATAL"
	default:
		return "UNKNOWN"
	}
}

// Logger provides structured logging capabilities
type Logger struct {
	mu         sync.Mutex
	minLevel   LogLevel
	jsonFormat bool
	prefix     string
}

var (
	defaultLogger *Logger
	once          sync.Once
)

// InitLogger initializes the default logger
func InitLogger(level string, jsonFormat bool, prefix string) {
	once.Do(func() {
		defaultLogger = &Logger{
			minLevel:   parseLogLevel(level),
			jsonFormat: jsonFormat,
			prefix:     prefix,
		}
	})
}

func parseLogLevel(level string) LogLevel {
	switch level {
	case "debug":
		return LogLevelDebug
	case "info":
		return LogLevelInfo
	case "warn", "warning":
		return LogLevelWarn
	case "error":
		return LogLevelError
	case "fatal":
		return LogLevelFatal
	default:
		return LogLevelInfo
	}
}

// LogEntry represents a structured log entry
type LogEntry struct {
	Timestamp string                 `json:"timestamp"`
	Level     string                 `json:"level"`
	Message   string                 `json:"message"`
	Prefix    string                 `json:"prefix,omitempty"`
	File      string                 `json:"file,omitempty"`
	Line      int                    `json:"line,omitempty"`
	Data      map[string]interface{} `json:"data,omitempty"`
}

func (l *Logger) log(level LogLevel, message string, data map[string]interface{}) {
	if l == nil || level < l.minLevel {
		return
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	// Get caller information
	_, file, line, ok := runtime.Caller(2)
	if !ok {
		file = "???"
		line = 0
	}

	// Shorten file path to just filename
	for i := len(file) - 1; i > 0; i-- {
		if file[i] == '/' {
			file = file[i+1:]
			break
		}
	}

	if l.jsonFormat {
		entry := LogEntry{
			Timestamp: time.Now().Format(time.RFC3339),
			Level:     level.String(),
			Message:   message,
			Prefix:    l.prefix,
			File:      file,
			Line:      line,
			Data:      data,
		}

		jsonBytes, err := json.Marshal(entry)
		if err != nil {
			log.Printf("ERROR: Failed to marshal log entry: %v", err)
			return
		}

		fmt.Println(string(jsonBytes))
	} else {
		// Plain text format
		prefix := ""
		if l.prefix != "" {
			prefix = fmt.Sprintf("[%s] ", l.prefix)
		}

		dataStr := ""
		if len(data) > 0 {
			dataBytes, _ := json.Marshal(data)
			dataStr = fmt.Sprintf(" | data=%s", string(dataBytes))
		}

		log.Printf("%s%s %s:%d - %s%s",
			prefix,
			level.String(),
			file,
			line,
			message,
			dataStr)
	}

	// Exit on fatal
	if level == LogLevelFatal {
		os.Exit(1)
	}
}

// LogDebug logs a debug message
func LogDebug(message string) {
	if defaultLogger == nil {
		log.Printf("DEBUG: %s", message)
		return
	}
	defaultLogger.log(LogLevelDebug, message, nil)
}

// LogDebugWithData logs a debug message with additional data
func LogDebugWithData(message string, data map[string]interface{}) {
	if defaultLogger == nil {
		log.Printf("DEBUG: %s | data=%+v", message, data)
		return
	}
	defaultLogger.log(LogLevelDebug, message, data)
}

// LogInfo logs an info message
func LogInfo(message string) {
	if defaultLogger == nil {
		log.Printf("INFO: %s", message)
		return
	}
	defaultLogger.log(LogLevelInfo, message, nil)
}

// LogInfoWithData logs an info message with additional data
func LogInfoWithData(message string, data map[string]interface{}) {
	if defaultLogger == nil {
		log.Printf("INFO: %s | data=%+v", message, data)
		return
	}
	defaultLogger.log(LogLevelInfo, message, data)
}

// LogWarn logs a warning message
func LogWarn(message string) {
	if defaultLogger == nil {
		log.Printf("WARN: %s", message)
		return
	}
	defaultLogger.log(LogLevelWarn, message, nil)
}

// LogWarnWithData logs a warning message with additional data
func LogWarnWithData(message string, data map[string]interface{}) {
	if defaultLogger == nil {
		log.Printf("WARN: %s | data=%+v", message, data)
		return
	}
	defaultLogger.log(LogLevelWarn, message, data)
}

// LogError logs an error message
func LogError(message string) {
	if defaultLogger == nil {
		log.Printf("ERROR: %s", message)
		return
	}
	defaultLogger.log(LogLevelError, message, nil)
}

// LogErrorWithData logs an error message with additional data
func LogErrorWithData(message string, data map[string]interface{}) {
	if defaultLogger == nil {
		log.Printf("ERROR: %s | data=%+v", message, data)
		return
	}
	defaultLogger.log(LogLevelError, message, data)
}

// LogFatal logs a fatal message and exits
func LogFatal(message string) {
	if defaultLogger == nil {
		log.Fatalf("FATAL: %s", message)
		return
	}
	defaultLogger.log(LogLevelFatal, message, nil)
}

// LogFatalWithData logs a fatal message with additional data and exits
func LogFatalWithData(message string, data map[string]interface{}) {
	if defaultLogger == nil {
		log.Fatalf("FATAL: %s | data=%+v", message, data)
		return
	}
	defaultLogger.log(LogLevelFatal, message, data)
}
