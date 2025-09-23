package repository

import (
	"log/slog"
	"os"
	"testing"
)

func TestWithPageSize(t *testing.T) {
	tests := []struct {
		name     string
		input    int
		expected int
	}{
		{"page size equals one", 1, 1},
		{"page size equals ten", 10, 10},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a ComponentListerOptions instance
			opts := &ComponentListerOptions{}

			// Apply the option function
			optionFunc := WithPageSize(tt.input)
			optionFunc(opts)

			// Verify the field was set correctly
			if opts.NameListPageSize != tt.expected {
				t.Errorf("Expected NameListPageSize to be %v, got %v", tt.expected, opts.SortAlphabetically)
			}
		})
	}
}

func TestWithSortAlphabetically(t *testing.T) {
	tests := []struct {
		name     string
		input    bool
		expected bool
	}{
		{"sort true", true, true},
		{"sort false", false, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a ComponentListerOptions instance
			opts := &ComponentListerOptions{}

			// Apply the option function
			optionFunc := WithSortAlphabetically(tt.input)
			optionFunc(opts)

			// Verify the field was set correctly
			if opts.SortAlphabetically != tt.expected {
				t.Errorf("Expected SortAlphabetically to be %v, got %v", tt.expected, opts.SortAlphabetically)
			}
		})
	}
}

func TestWithLogger(t *testing.T) {
	tests := []struct {
		name   string
		logger *slog.Logger
	}{
		{
			name:   "with text handler logger",
			logger: slog.New(slog.NewTextHandler(os.Stdout, nil)),
		},
		{
			name:   "with json handler logger",
			logger: slog.New(slog.NewJSONHandler(os.Stdout, nil)),
		},
		{
			name:   "with nil logger",
			logger: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a ComponentListerOptions instance
			opts := &ComponentListerOptions{}

			// Apply the option function
			optionFunc := WithLogger(tt.logger)
			optionFunc(opts)

			// Verify the logger was set correctly
			if opts.Logger != tt.logger {
				t.Errorf("Expected Logger to be %v, got %v", tt.logger, opts.Logger)
			}
		})
	}
}
