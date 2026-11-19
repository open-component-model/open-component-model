package common_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"ocm.software/open-component-model/bindings/go/descriptor/normalisation/json/internal/common"
)

func TestMapResourcesWithAccessType(t *testing.T) {
	tests := []struct {
		name     string
		input    interface{}
		expected interface{}
	}{
		{
			name: "valid access type",
			input: map[string]interface{}{
				"access": map[string]interface{}{
					"type": "none",
				},
				"digest": "test",
			},
			expected: map[string]interface{}{
				"access": map[string]interface{}{
					"type": "none",
				},
			},
		},
		{
			name: "invalid access type",
			input: map[string]interface{}{
				"access": map[string]interface{}{
					"type": 123, // invalid type
				},
				"digest": "test",
			},
			expected: map[string]interface{}{
				"access": map[string]interface{}{
					"type": 123,
				},
				"digest": "test",
			},
		},
		{
			name: "missing access type",
			input: map[string]interface{}{
				"access": map[string]interface{}{},
				"digest": "test",
			},
			expected: map[string]interface{}{
				"access": map[string]interface{}{},
				"digest": "test",
			},
		},
		{
			name: "missing access",
			input: map[string]interface{}{
				"digest": "test",
			},
			expected: map[string]interface{}{
				"digest": "test",
			},
		},
		{
			name: "nil access",
			input: map[string]interface{}{
				"access": nil,
				"digest": "test",
			},
			expected: map[string]interface{}{
				"access": nil,
				"digest": "test",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := common.MapResourcesWithAccessType(common.IsNoneAccessKind, func(v interface{}) interface{} {
				m := v.(map[string]interface{})
				delete(m, "digest")
				return m
			}, tt.input)
			assert.Equal(t, tt.expected, got)
		})
	}
}

func TestMapResourcesWithNoneAccess(t *testing.T) {
	tests := []struct {
		name     string
		input    interface{}
		expected interface{}
	}{
		{
			name: "valid none access type",
			input: map[string]interface{}{
				"access": map[string]interface{}{
					"type": "none",
				},
				"digest": "test",
			},
			expected: map[string]interface{}{
				"access": map[string]interface{}{
					"type": "none",
				},
			},
		},
		{
			name: "valid legacy none access type",
			input: map[string]interface{}{
				"access": map[string]interface{}{
					"type": "None",
				},
				"digest": "test",
			},
			expected: map[string]interface{}{
				"access": map[string]interface{}{
					"type": "None",
				},
			},
		},
		{
			name: "invalid access type",
			input: map[string]interface{}{
				"access": map[string]interface{}{
					"type": 123, // invalid type
				},
				"digest": "test",
			},
			expected: map[string]interface{}{
				"access": map[string]interface{}{
					"type": 123,
				},
				"digest": "test",
			},
		},
		{
			name: "missing access type",
			input: map[string]interface{}{
				"access": map[string]interface{}{},
				"digest": "test",
			},
			expected: map[string]interface{}{
				"access": map[string]interface{}{},
				"digest": "test",
			},
		},
		{
			name: "missing access",
			input: map[string]interface{}{
				"digest": "test",
			},
			expected: map[string]interface{}{
				"digest": "test",
			},
		},
		{
			name: "nil access",
			input: map[string]interface{}{
				"access": nil,
				"digest": "test",
			},
			expected: map[string]interface{}{
				"access": nil,
				"digest": "test",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := common.MapResourcesWithNoneAccess(tt.input)
			assert.Equal(t, tt.expected, got)
		})
	}
}

func TestIgnoreLabelsWithoutSignature(t *testing.T) {
	tests := []struct {
		name     string
		input    interface{}
		expected bool
	}{
		{
			name: "with signature true",
			input: map[string]interface{}{
				"signing": true,
			},
			expected: false,
		},
		{
			name: "with signature string true",
			input: map[string]interface{}{
				"signing": "true",
			},
			expected: false,
		},
		{
			name: "with signature false",
			input: map[string]interface{}{
				"signing": false,
			},
			expected: true,
		},
		{
			name: "with signature string false",
			input: map[string]interface{}{
				"signing": "false",
			},
			expected: true,
		},
		{
			name: "without signature",
			input: map[string]interface{}{
				"other": "value",
			},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := common.IgnoreLabelsWithoutSignature(tt.input)
			assert.Equal(t, tt.expected, got)
		})
	}
}
