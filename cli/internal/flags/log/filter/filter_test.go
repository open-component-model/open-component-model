package filter

import (
	"bytes"
	"context"
	"log/slog"
	"testing"
)

func TestFilteredHandler(t *testing.T) {
	type testRecord struct {
		level slog.Level
		realm string
		msg   string
	}
	tests := []struct {
		name     string
		filters  []string
		records  []testRecord
		expected []string
	}{
		{
			name:    "oci=WARN should filter out INFO messages with realm=oci",
			filters: []string{"oci=WARN"},
			records: []testRecord{
				{level: slog.LevelInfo, realm: "oci", msg: "oci info message"},
				{level: slog.LevelWarn, realm: "oci", msg: "oci warn message"},
				{level: slog.LevelError, realm: "oci", msg: "oci error message"},
				{level: slog.LevelInfo, realm: "other", msg: "other info message"},
			},
			expected: []string{
				"oci warn message",
				"oci error message",
				"other info message",
			},
		},
		{
			name:    "multiple filters",
			filters: []string{"oci=WARN", "auth=ERROR"},
			records: []testRecord{
				{level: slog.LevelInfo, realm: "oci", msg: "oci info message"},
				{level: slog.LevelWarn, realm: "oci", msg: "oci warn message"},
				{level: slog.LevelInfo, realm: "auth", msg: "auth info message"},
				{level: slog.LevelWarn, realm: "auth", msg: "auth warn message"},
				{level: slog.LevelError, realm: "auth", msg: "auth error message"},
				{level: slog.LevelInfo, realm: "other", msg: "other info message"},
			},
			expected: []string{
				"oci warn message",
				"auth error message",
				"other info message",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a buffer to capture log output
			var buf bytes.Buffer
			handler := slog.NewTextHandler(&buf, &slog.HandlerOptions{
				Level: slog.LevelDebug,
			})

			// Create filters
			filters, err := KeyFiltersFromStrings(tt.filters...)
			if err != nil {
				t.Fatalf("Failed to create filters: %v", err)
			}

			// Create filtered handler
			filteredHandler := New(handler, LoggingKeyRealm, filters)

			// Log all test records
			logger := slog.New(filteredHandler)
			for _, record := range tt.records {
				logger.Log(context.Background(), record.level, record.msg, LoggingKeyRealm, record.realm)
			}

			// Check output
			for _, expected := range tt.expected {
				if !bytes.Contains(buf.Bytes(), []byte(expected)) {
					t.Errorf("Expected to find message '%s' in output, but it was missing", expected)
				}
			}

			// Check that filtered messages are not present
			for _, record := range tt.records {
				shouldBeFiltered := false
				// Simple check: if this record should be filtered based on our test logic
				if record.realm == "oci" && record.level < slog.LevelWarn {
					shouldBeFiltered = true
				}
				if record.realm == "auth" && record.level < slog.LevelError {
					shouldBeFiltered = true
				}
				if shouldBeFiltered && bytes.Contains(buf.Bytes(), []byte(record.msg)) {
					t.Errorf("Expected message '%s' to be filtered out, but it was present in output", record.msg)
				}
			}
		})
	}
}

func TestFilteredHandlerWithAttrs(t *testing.T) {
	type testRecord struct {
		level slog.Level
		msg   string
	}
	tests := []struct {
		name      string
		filters   []string
		withAttrs []slog.Attr
		records   []testRecord
		expected  []string
	}{
		{
			name:    "WithAttrs sets realm for all subsequent logs",
			filters: []string{"oci=WARN"},
			withAttrs: []slog.Attr{
				slog.String(LoggingKeyRealm, "oci"),
			},
			records: []testRecord{
				{level: slog.LevelInfo, msg: "oci info message"},
				{level: slog.LevelWarn, msg: "oci warn message"},
				{level: slog.LevelError, msg: "oci error message"},
			},
			expected: []string{
				"oci warn message",
				"oci error message",
			},
		},
		{
			name:    "WithAttrs with multiple attributes",
			filters: []string{"auth=ERROR"},
			withAttrs: []slog.Attr{
				slog.String(LoggingKeyRealm, "auth"),
				slog.String("user", "testuser"),
			},
			records: []testRecord{
				{level: slog.LevelInfo, msg: "auth info message"},
				{level: slog.LevelWarn, msg: "auth warn message"},
				{level: slog.LevelError, msg: "auth error message"},
			},
			expected: []string{
				"auth error message",
			},
		},
		{
			name:    "WithAttrs overrides individual record attributes",
			filters: []string{"oci=WARN", "auth=ERROR"},
			withAttrs: []slog.Attr{
				slog.String(LoggingKeyRealm, "oci"),
			},
			records: []testRecord{
				{level: slog.LevelInfo, msg: "should be filtered (oci info)"},
				{level: slog.LevelWarn, msg: "should pass (oci warn)"},
				{level: slog.LevelError, msg: "should pass (oci error)"},
			},
			expected: []string{
				"should pass (oci warn)",
				"should pass (oci error)",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a buffer to capture log output
			var buf bytes.Buffer
			handler := slog.NewTextHandler(&buf, &slog.HandlerOptions{
				Level: slog.LevelDebug,
			})

			// Create filters
			filters, err := KeyFiltersFromStrings(tt.filters...)
			if err != nil {
				t.Fatalf("Failed to create filters: %v", err)
			}

			// Create filtered handler
			filteredHandler := New(handler, LoggingKeyRealm, filters)

			// Create logger with WithAttrs
			logger := slog.New(filteredHandler)
			if len(tt.withAttrs) > 0 {
				// Convert slog.Attr slice to key-value pairs for logger.With
				args := make([]any, 0, len(tt.withAttrs)*2)
				for _, attr := range tt.withAttrs {
					args = append(args, attr.Key, attr.Value)
				}
				logger = logger.With(args...)
			}

			// Log all test records
			for _, record := range tt.records {
				logger.Log(context.Background(), record.level, record.msg)
			}

			// Check output
			for _, expected := range tt.expected {
				if !bytes.Contains(buf.Bytes(), []byte(expected)) {
					t.Errorf("Expected to find message '%s' in output, but it was missing", expected)
				}
			}

			// Check that filtered messages are not present
			for _, record := range tt.records {
				shouldBeFiltered := false
				// Determine if this record should be filtered based on the test case
				if tt.name == "WithAttrs sets realm for all subsequent logs" {
					if record.level < slog.LevelWarn {
						shouldBeFiltered = true
					}
				} else if tt.name == "WithAttrs with multiple attributes" {
					if record.level < slog.LevelError {
						shouldBeFiltered = true
					}
				} else if tt.name == "WithAttrs overrides individual record attributes" {
					if record.level < slog.LevelWarn {
						shouldBeFiltered = true
					}
				}

				if shouldBeFiltered && bytes.Contains(buf.Bytes(), []byte(record.msg)) {
					t.Errorf("Expected message '%s' to be filtered out, but it was present in output", record.msg)
				}
			}
		})
	}
}
