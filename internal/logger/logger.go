package logger

import (
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/lmittmann/tint"
)

// Global logger instance
var Log *slog.Logger

// Init initializes the global logger with the specified level and format
func Init(levelStr, format string) error {
	// Parse log level
	_, err := parseLogLevel(levelStr)
	if err != nil {
		return err
	}
	w := os.Stderr

	// Create handler based on format
	// var handler slog.Handler
	// switch strings.ToLower(format) {
	// case "json":
	// 	handler = slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
	// 		Level: level,
	// 	})
	// case "text", "":
	// 	handler = slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
	// 		Level: level,
	// 	})
	// default:
	// 	return fmt.Errorf("unknown log format: %s", format)
	// }

	// Initialize global logger
	Log = slog.New(tint.NewHandler(w, nil))
	slog.SetDefault(slog.New(
		tint.NewHandler(w, &tint.Options{
			Level:      slog.LevelDebug,
			TimeFormat: time.DateTime,
		}),
	))

	return nil
}

// parseLogLevel converts a string level to a slog.Level
func parseLogLevel(levelStr string) (slog.Level, error) {
	switch strings.ToLower(levelStr) {
	case "debug":
		return slog.LevelDebug, nil
	case "info", "":
		return slog.LevelInfo, nil
	case "warn", "warning":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	default:
		return slog.LevelInfo, fmt.Errorf("unknown log level: %s", levelStr)
	}
}
