package logging

import (
	"bytes"
	"errors"
	"log/slog"
	"strings"
	"testing"
)

func TestNew_TextWriter_EmitsRecord(t *testing.T) {
	var buf bytes.Buffer
	logger := New(Options{
		Level:  slog.LevelInfo,
		Format: FormatText,
		Writer: &buf,
	})
	logger.Info("hello", "key", "value")
	out := buf.String()
	if !strings.Contains(out, "hello") {
		t.Errorf("output %q does not contain message", out)
	}
	if !strings.Contains(out, "key=value") {
		t.Errorf("output %q does not contain key=value", out)
	}
}

func TestNew_JSONWriter_EmitsJSON(t *testing.T) {
	var buf bytes.Buffer
	logger := New(Options{
		Level:  slog.LevelInfo,
		Format: FormatJSON,
		Writer: &buf,
	})
	logger.Info("ping")
	out := buf.String()
	if !strings.HasPrefix(strings.TrimSpace(out), "{") {
		t.Errorf("output %q does not look like JSON", out)
	}
}

func TestNew_NilWriter_NoPanic(t *testing.T) {
	logger := New(Options{Level: slog.LevelDebug})
	logger.Debug("nothing receives this") // Writer defaults to io.Discard.
}

func TestNew_UnknownFormat_FallsBackToText(t *testing.T) {
	var buf bytes.Buffer
	logger := New(Options{
		Level:  slog.LevelInfo,
		Format: Format("garbage"),
		Writer: &buf,
	})
	logger.Info("ok")
	if buf.Len() == 0 {
		t.Fatal("logger produced no output for unknown format")
	}
	if strings.HasPrefix(strings.TrimSpace(buf.String()), "{") {
		t.Errorf("unknown format unexpectedly produced JSON: %q", buf.String())
	}
}

func TestParseLevel(t *testing.T) {
	cases := []struct {
		in   string
		want slog.Level
	}{
		{"debug", slog.LevelDebug},
		{"DEBUG", slog.LevelDebug},
		{"info", slog.LevelInfo},
		{"", slog.LevelInfo},
		{"warn", slog.LevelWarn},
		{"warning", slog.LevelWarn},
		{"error", slog.LevelError},
		{"err", slog.LevelError},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			got, err := ParseLevel(tc.in)
			if err != nil {
				t.Fatalf("ParseLevel(%q) error: %v", tc.in, err)
			}
			if got != tc.want {
				t.Errorf("ParseLevel(%q) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

func TestParseLevel_Unknown_Error(t *testing.T) {
	if _, err := ParseLevel("verbose"); err == nil {
		t.Error("ParseLevel(\"verbose\") want error, got nil")
	}
}

func TestParseFormat(t *testing.T) {
	cases := []struct {
		in   string
		want Format
	}{
		{"json", FormatJSON},
		{"JSON", FormatJSON},
		{"text", FormatText},
		{"", FormatText},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			got, err := ParseFormat(tc.in)
			if err != nil {
				t.Fatalf("ParseFormat(%q) error: %v", tc.in, err)
			}
			if got != tc.want {
				t.Errorf("ParseFormat(%q) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

func TestParseFormat_Unknown_Error(t *testing.T) {
	_, err := ParseFormat("yaml")
	if err == nil {
		t.Fatal("ParseFormat(\"yaml\") want error, got nil")
	}
	// Sanity: errors are not sentinel today but should always be non-nil.
	if errors.Is(err, nil) {
		t.Fatal("error should not match nil")
	}
}

func TestDiscard_SwallowsRecords(t *testing.T) {
	logger := Discard()
	logger.Info("noise") // must not panic, must produce no output anywhere observable.
}
