package logger

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
)

// Setup initializes the global slog instance based on configuration.
func Setup(levelStr, logDir string) {
	var lvl slog.Level
	switch strings.ToLower(levelStr) {
	case "debug":
		lvl = slog.LevelDebug
	case "warn":
		lvl = slog.LevelWarn
	case "error":
		lvl = slog.LevelError
	default:
		lvl = slog.LevelInfo
	}

	var w io.Writer = os.Stderr

	if logDir != "" {
		if err := os.MkdirAll(logDir, 0755); err == nil {
			logFile, err := os.OpenFile(filepath.Join(logDir, "memcp.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
			if err == nil {
				w = io.MultiWriter(os.Stderr, logFile)
			}
		}
	}

	opts := &slog.HandlerOptions{
		Level: lvl,
	}

	// We use TextHandler for human readability
	// CRITICAL: Writing to os.Stdout breaks the MCP JSON-RPC protocol
	handler := slog.NewTextHandler(w, opts)
	
	logger := slog.New(handler)
	slog.SetDefault(logger)
}
