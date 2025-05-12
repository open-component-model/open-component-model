package plugins

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"ocm.software/open-component-model/bindings/go/plugin/manager/types"
)

// TestParseLine tests the parseLine function
func TestParseLine(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    record
		wantErr bool
	}{
		{
			name:  "valid log line",
			input: `{"level":"info","msg":"test message","key1":"value1","key2":42}`,
			want: record{
				level: "info",
				msg:   "test message",
				args:  []any{"key1", "value1", "key2", float64(42)},
			},
			wantErr: false,
		},
		{
			name:    "invalid JSON",
			input:   `{"level":"info","msg":"test message"`,
			want:    record{},
			wantErr: true,
		},
		{
			name:    "missing msg field",
			input:   `{"level":"info","key1":"value1"}`,
			want:    record{},
			wantErr: true,
		},
		{
			name:    "missing level field",
			input:   `{"msg":"test message","key1":"value1"}`,
			want:    record{},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseLine(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseLine() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if got.level != tt.want.level {
				t.Errorf("parseLine() level = %v, want %v", got.level, tt.want.level)
			}
			if got.msg != tt.want.msg {
				t.Errorf("parseLine() msg = %v, want %v", got.msg, tt.want.msg)
			}

			assert.Equal(t, tt.want.level, got.level)
			assert.Equal(t, tt.want.msg, got.msg)
			assert.Equal(t, tt.want.args, got.args)
		})
	}
}

// TestStartLogStreamerNilStderr tests the StartLogStreamer function with nil stderr
func TestStartLogStreamerNilStderr(t *testing.T) {
	// Create a plugin with nil stderr
	plugin := &types.Plugin{
		ID:     "test-plugin",
		Stderr: nil,
	}

	// Create a context with timeout to ensure test doesn't hang
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	// This should return immediately without error
	StartLogStreamer(ctx, plugin)
}
