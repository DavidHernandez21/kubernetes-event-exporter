package metrics

import (
	"log/slog"
	"testing"
)

func TestParseLogLevel(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  slog.Level
	}{
		{"empty -> info", "", slog.LevelInfo},
		{"debug", "debug", slog.LevelDebug},
		{"info uppercase", "INFO", slog.LevelInfo},
		{"warn", "warn", slog.LevelWarn},
		{"invalid -> fallback", "not-a-level", slog.LevelInfo},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			got := parseLogLevel(tc.input)
			if got != tc.want {
				t.Fatalf("parseLogLevel(%q) = %v, want %v", tc.input, got, tc.want)
			}
		})
	}
}
