package bar

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- TreeErrorFormatter tests ---

func TestTreeErrorFormatter(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		contains []string
	}{
		{
			name:     "nil error returns empty",
			err:      nil,
			contains: nil,
		},
		{
			name:     "simple error shows one arrow",
			err:      errors.New("connection failed"),
			contains: []string{"↳", "connection failed"},
		},
		{
			name:     "wrapped error creates indented tree",
			err:      fmt.Errorf("push failed: %w", errors.New("connection refused")),
			contains: []string{"↳", "push failed", "connection refused"},
		},
		{
			name:     "deeply nested chain",
			err:      fmt.Errorf("transfer: %w", fmt.Errorf("push blob: %w", fmt.Errorf("connection: %w", errors.New("timeout")))),
			contains: []string{"↳", "transfer", "push blob", "connection", "timeout"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := TreeErrorFormatter(tt.err)
			if tt.contains == nil {
				assert.Empty(t, result)
			} else {
				for _, s := range tt.contains {
					assert.Contains(t, result, s)
				}
			}
		})
	}
}

// --- FramedText tests ---

// loadGolden reads a golden file from testdata/framed_text/<name>.golden.
func loadGolden(t *testing.T, name string) string {
	t.Helper()
	path := filepath.Join("testdata", "framed_text", name+".golden")
	data, err := os.ReadFile(path)
	require.NoError(t, err, "failed to read golden file %s", path)
	return string(data)
}

func TestFramedText(t *testing.T) {
	tests := []struct {
		name           string
		expectedGolden string
		title          string
		content        string
		indent         int
	}{
		{
			name:           "with title",
			expectedGolden: "with_title",
			title:          "Title",
			content:        "hello",
			indent:         0,
		},
		{
			name:           "with indent",
			expectedGolden: "with_indent",
			title:          "Stats",
			content:        "content",
			indent:         4,
		},
		{
			name:           "multiline title",
			expectedGolden: "multiline_title",
			title:          "Line1\nLine2",
			content:        "content",
			indent:         0,
		},
		{
			name:           "multiline content pads to longest",
			expectedGolden: "multiline_content_pads_to_longest",
			title:          "",
			content:        "short\nlonger line",
			indent:         0,
		},
		{
			name:           "no title",
			expectedGolden: "no_title",
			title:          "",
			content:        "hello",
			indent:         0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			expectedGolden := loadGolden(t, tt.expectedGolden)
			result := FramedText(tt.title, tt.content, tt.indent)
			assert.Equal(t, expectedGolden, result)
		})
	}
}
