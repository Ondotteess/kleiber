package logging

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
)

// Format identifies the shape of a slog handler.
type Format string

const (
	// FormatText emits human-readable key=value records, suitable for
	// development and tail-friendly log files.
	FormatText Format = "text"

	// FormatJSON emits one JSON object per record, suitable for ingestion
	// by structured-log collectors.
	FormatJSON Format = "json"
)

// Options configures a logger built by New.
type Options struct {
	// Level is the minimum slog.Level to emit.
	Level slog.Level

	// Format selects between text and JSON handlers. The empty string is
	// treated as FormatText.
	Format Format

	// Writer is the destination for log records. A nil Writer means
	// io.Discard — useful in tests and as a safe default.
	Writer io.Writer

	// AddSource includes the call site (file and line) in each record.
	AddSource bool
}

// New returns a *slog.Logger configured per opts.
//
// New never returns nil: an unknown Format silently falls back to
// FormatText so a typo in user configuration cannot crash startup.
func New(opts Options) *slog.Logger {
	w := opts.Writer
	if w == nil {
		w = io.Discard
	}
	handlerOpts := &slog.HandlerOptions{
		Level:     opts.Level,
		AddSource: opts.AddSource,
	}
	switch opts.Format {
	case FormatJSON:
		return slog.New(slog.NewJSONHandler(w, handlerOpts))
	case FormatText, "":
		return slog.New(slog.NewTextHandler(w, handlerOpts))
	default:
		return slog.New(slog.NewTextHandler(w, handlerOpts))
	}
}

// Discard returns a logger that swallows every record. Convenient as a
// nil-safe default in constructors that accept a *slog.Logger.
func Discard() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// OpenFile opens the log file at path for appending, creating it and any
// missing parent directories. The returned file is the caller's to
// close; pass it as Options.Writer to route logs to disk.
//
// It opens in append mode so restarts accumulate rather than truncate a
// prior session's log.
func OpenFile(path string) (*os.File, error) {
	if dir := filepath.Dir(path); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("logging: creating log directory %s: %w", dir, err)
		}
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, fmt.Errorf("logging: opening log file %s: %w", path, err)
	}
	return f, nil
}

// ParseLevel maps a case-insensitive level name ("debug", "info", "warn",
// "error") to a slog.Level. The empty string maps to slog.LevelInfo so
// that a missing config value is treated as "default".
func ParseLevel(s string) (slog.Level, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "debug":
		return slog.LevelDebug, nil
	case "info", "":
		return slog.LevelInfo, nil
	case "warn", "warning":
		return slog.LevelWarn, nil
	case "error", "err":
		return slog.LevelError, nil
	default:
		return 0, fmt.Errorf("logging: unknown level %q", s)
	}
}

// ParseFormat maps a case-insensitive format name to a Format. The empty
// string maps to FormatText.
func ParseFormat(s string) (Format, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case string(FormatJSON):
		return FormatJSON, nil
	case string(FormatText), "":
		return FormatText, nil
	default:
		return "", fmt.Errorf("logging: unknown format %q", s)
	}
}
