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
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.meta.ToIdentity()
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestComponentConstructor_Validate(t *testing.T) {
	tests := []struct {
		name    string
		cc      *ComponentConstructor
		wantErr bool
	}{
		{
			name:    "nil constructor",
			cc:      nil,
			wantErr: false,
		},
		{
			name: "valid constructor",
			cc: &ComponentConstructor{
				Components: []Component{
					{
						ComponentMeta: ComponentMeta{
							ObjectMeta: ObjectMeta{
								Name:    "test",
								Version: "1.0.0",
							},
						},
						Provider: Provider{
							Name: "test-provider",
						},
					},
				},
			},
			wantErr: false,
		},
		{
			name: "invalid component",
			cc: &ComponentConstructor{
				Components: []Component{
					{
						ComponentMeta: ComponentMeta{
							ObjectMeta: ObjectMeta{
								Name:    "test",
								Version: "1.0.0",
							},
						},
						Resources: []Resource{
							{
								AccessOrInput: AccessOrInput{
									Access: &runtime.Unstructured{},
									Input:  &runtime.Unstructured{},
								},
							},
						},
					},
				},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cc.Validate()
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
