package runtime

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"ocm.software/open-component-model/bindings/go/runtime"
)

func TestAccessOrInput_Validate(t *testing.T) {
	tests := []struct {
		name    string
		input   AccessOrInput
		wantErr bool
	}{
		{
			name: "valid with access",
			input: AccessOrInput{
				Access: &runtime.Unstructured{},
			},
			wantErr: false,
		},
		{
			name: "valid with input",
			input: AccessOrInput{
				Input: &runtime.Unstructured{},
			},
			wantErr: false,
		},
		{
			name:    "invalid - neither access nor input",
			input:   AccessOrInput{},
			wantErr: true,
		},
		{
			name: "invalid - both access and input",
			input: AccessOrInput{
				Access: &runtime.Unstructured{},
				Input:  &runtime.Unstructured{},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.input.Validate()
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestElementMeta_ToIdentity(t *testing.T) {
	tests := []struct {
		name     string
		meta     *ElementMeta
		expected runtime.Identity
	}{
		{
			name:     "nil meta",
			meta:     nil,
			expected: nil,
		},
		{
			name: "basic identity",
			meta: &ElementMeta{
				ObjectMeta: ObjectMeta{
					Name:    "test",
					Version: "1.0.0",
				},
			},
			expected: runtime.Identity{
				IdentityAttributeName:    "test",
				IdentityAttributeVersion: "1.0.0",
			},
		},
		{
			name: "with extra identity",
			meta: &ElementMeta{
				ObjectMeta: ObjectMeta{
					Name:    "test",
					Version: "1.0.0",
				},
				ExtraIdentity: runtime.Identity{
					"extra": "value",
				},
			},
			expected: runtime.Identity{
				IdentityAttributeName:    "test",
				IdentityAttributeVersion: "1.0.0",
				"extra":                  "value",
			},
		},
		{
			name: "identity without version",
			meta: &ElementMeta{
				ObjectMeta: ObjectMeta{
					Name: "test",
				},
			},
			expected: runtime.Identity{
				IdentityAttributeName: "test",
			},
		},
		{
			name: "identity without name",
			meta: &ElementMeta{
				ObjectMeta: ObjectMeta{
					Version: "test",
				},
			},
			expected: runtime.Identity{
				IdentityAttributeVersion: "test",
			},
		},
		{
			name: "identity without anything",
			meta: &ElementMeta{
				ObjectMeta: ObjectMeta{},
			},
			expected: runtime.Identity{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.meta.ToIdentity()
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestComponentMeta_ToIdentity(t *testing.T) {
	tests := []struct {
		name     string
		meta     *ComponentMeta
		expected runtime.Identity
	}{
		{
			name:     "nil meta",
			meta:     nil,
			expected: nil,
		},
		{
			name: "basic component identity",
			meta: &ComponentMeta{
				ObjectMeta: ObjectMeta{
					Name:    "test-component",
					Version: "1.0.0",
				},
			},
			expected: runtime.Identity{
				IdentityAttributeName:    "test-component",
				IdentityAttributeVersion: "1.0.0",
			},
		},
		{
			name: "name without version",
			meta: &ComponentMeta{
				ObjectMeta: ObjectMeta{
					Name: "test-component",
				},
			},
			expected: runtime.Identity{
				IdentityAttributeName: "test-component",
			},
		},
		{
			name: "version without name",
			meta: &ComponentMeta{
				ObjectMeta: ObjectMeta{
					Version: "1.0.0",
				},
			},
			expected: runtime.Identity{
				IdentityAttributeVersion: "1.0.0",
			},
		},
		{
			name: "empty component meta",
			meta: &ComponentMeta{
				ObjectMeta: ObjectMeta{},
			},
			expected: runtime.Identity{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.meta.ToIdentity()
			assert.Equal(t, tt.expected, result)
		})
	}
}
