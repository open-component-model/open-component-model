package enum

import (
	"testing"

	"github.com/spf13/pflag"
	"github.com/stretchr/testify/assert"
)

func TestNew(t *testing.T) {
	t.Run("should panic with empty options", func(t *testing.T) {
		assert.Panics(t, func() {
			New()
		})
	})

	t.Run("should create flag with default value", func(t *testing.T) {
		flag := New("option1", "option2", "option3")
		assert.Equal(t, "option1", flag.String())
		assert.Equal(t, []string{"option1", "option2", "option3"}, flag.options)
	})
}

func TestFlag_Set(t *testing.T) {
	tests := []struct {
		name        string
		options     []string
		value       string
		expectError bool
		expected    string
	}{
		{
			name:        "valid value",
			options:     []string{"option1", "option2", "option3"},
			value:       "option2",
			expectError: false,
			expected:    "option2",
		},
		{
			name:        "invalid value",
			options:     []string{"option1", "option2", "option3"},
			value:       "invalid",
			expectError: true,
			expected:    "option1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			flag := New(tt.options...)
			err := flag.Set(tt.value)
			if tt.expectError {
				assert.Error(t, err)
				assert.Equal(t, tt.options[0], flag.String())
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expected, flag.String())
			}
		})
	}
}

func TestGet(t *testing.T) {
	fs := pflag.NewFlagSet("test", pflag.ContinueOnError)
	Var(fs, "test-flag", []string{"option1", "option2", "option3"}, "test flag")

	t.Run("should get flag value", func(t *testing.T) {
		err := fs.Set("test-flag", "option2")
		assert.NoError(t, err)

		value, err := Get(fs, "test-flag")
		assert.NoError(t, err)
		assert.Equal(t, "option2", value)
	})

	t.Run("should error on non-existent flag", func(t *testing.T) {
		_, err := Get(fs, "non-existent")
		assert.Error(t, err)
	})
}

func TestVar(t *testing.T) {
	fs := pflag.NewFlagSet("test", pflag.ContinueOnError)
	options := []string{"option1", "option2", "option3"}
	usage := "test flag"

	t.Run("should add flag without shorthand", func(t *testing.T) {
		Var(fs, "test-flag", options, usage)
		assert.True(t, fs.HasFlags())
		assert.NotNil(t, fs.Lookup("test-flag"))
	})

	t.Run("should add flag with shorthand", func(t *testing.T) {
		VarP(fs, "test-flag-p", "t", options, usage)
		flag := fs.Lookup("test-flag-p")
		assert.NotNil(t, flag)
		assert.Equal(t, "t", flag.Shorthand)
	})
}
