package runtime

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"ocm.software/open-component-model/bindings/go/runtime"
)

// mockTyped is a mock implementation of runtime.Typed
type mockTyped struct {
	typ runtime.Type
}

func (m *mockTyped) GetType() runtime.Type {
	return m.typ
}

func (m *mockTyped) SetType(t runtime.Type) {
	m.typ = t
}

func (m *mockTyped) DeepCopyTyped() runtime.Typed {
	return &mockTyped{typ: m.typ}
}

func TestValidationError(t *testing.T) {
	tests := []struct {
		name     string
		err      *ValidationError
		expected string
	}{
		{
			name: "with field",
			err: &ValidationError{
				Field:   "test",
				Message: "error message",
			},
			expected: "test: error message",
		},
		{
			name: "without field",
			err: &ValidationError{
				Message: "error message",
			},
			expected: "error message",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.err.Error())
		})
	}
}

func TestObjectMeta_Validate(t *testing.T) {
	tests := []struct {
		name     string
		meta     ObjectMeta
		hasError bool
	}{
		{
			name: "valid",
			meta: ObjectMeta{
				Name:    "test",
				Version: "1.0.0",
			},
			hasError: false,
		},
		{
			name: "missing name",
			meta: ObjectMeta{
				Version: "1.0.0",
			},
			hasError: true,
		},
		{
			name: "missing version",
			meta: ObjectMeta{
				Name: "test",
			},
			hasError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.meta.ValidateWithVersion()
			if tt.hasError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestElementMeta_Validate(t *testing.T) {
	tests := []struct {
		name     string
		meta     ElementMeta
		hasError bool
	}{
		{
			name: "valid",
			meta: ElementMeta{
				ObjectMeta: ObjectMeta{
					Name:    "test",
					Version: "1.0.0",
				},
			},
			hasError: false,
		},
		{
			name: "invalid object meta",
			meta: ElementMeta{
				ObjectMeta: ObjectMeta{
					Name: "test",
				},
			},
			hasError: true,
		},
		{
			name: "invalid extra identity",
			meta: ElementMeta{
				ObjectMeta: ObjectMeta{
					Name:    "test",
					Version: "1.0.0",
				},
				ExtraIdentity: map[string]string{
					IdentityAttributeName: "test",
				},
			},
			hasError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.meta.ValidateWithVersion()
			if tt.hasError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestProvider_Validate(t *testing.T) {
	tests := []struct {
		name     string
		provider Provider
		hasError bool
	}{
		{
			name: "valid",
			provider: Provider{
				Name: "test",
			},
			hasError: false,
		},
		{
			name:     "missing name",
			provider: Provider{},
			hasError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.provider.Validate()
			if tt.hasError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestResource_Validate(t *testing.T) {
	tests := []struct {
		name     string
		resource Resource
		hasError bool
	}{
		{
			name: "valid",
			resource: Resource{
				ElementMeta: ElementMeta{
					ObjectMeta: ObjectMeta{
						Name:    "test",
						Version: "1.0.0",
					},
				},
				Type:     "test",
				Relation: LocalRelation,
				AccessOrInput: AccessOrInput{
					Access: &mockTyped{typ: runtime.Type{Name: "test"}},
				},
			},
			hasError: false,
		},
		{
			name: "invalid element meta",
			resource: Resource{
				Type:     "test",
				Relation: LocalRelation,
			},
			hasError: true,
		},
		{
			name: "missing type",
			resource: Resource{
				ElementMeta: ElementMeta{
					ObjectMeta: ObjectMeta{
						Name:    "test",
						Version: "1.0.0",
					},
				},
				Relation: LocalRelation,
			},
			hasError: true,
		},
		{
			name: "missing relation",
			resource: Resource{
				ElementMeta: ElementMeta{
					ObjectMeta: ObjectMeta{
						Name:    "test",
						Version: "1.0.0",
					},
				},
				Type: "test",
			},
			hasError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.resource.Validate()
			if tt.hasError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestSource_Validate(t *testing.T) {
	tests := []struct {
		name     string
		source   Source
		hasError bool
	}{
		{
			name: "valid",
			source: Source{
				ElementMeta: ElementMeta{
					ObjectMeta: ObjectMeta{
						Name:    "test",
						Version: "1.0.0",
					},
				},
				Type: "test",
				AccessOrInput: AccessOrInput{
					Access: &mockTyped{typ: runtime.Type{Name: "test"}},
				},
			},
			hasError: false,
		},
		{
			name: "invalid element meta",
			source: Source{
				Type: "test",
			},
			hasError: true,
		},
		{
			name: "missing type",
			source: Source{
				ElementMeta: ElementMeta{
					ObjectMeta: ObjectMeta{
						Name:    "test",
						Version: "1.0.0",
					},
				},
			},
			hasError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.source.Validate()
			if tt.hasError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestReference_Validate(t *testing.T) {
	tests := []struct {
		name      string
		reference Reference
		hasError  bool
	}{
		{
			name: "valid",
			reference: Reference{
				ElementMeta: ElementMeta{
					ObjectMeta: ObjectMeta{
						Name:    "test",
						Version: "1.0.0",
					},
				},
				Component: "test",
			},
			hasError: false,
		},
		{
			name: "invalid element meta",
			reference: Reference{
				Component: "test",
			},
			hasError: true,
		},
		{
			name: "missing component",
			reference: Reference{
				ElementMeta: ElementMeta{
					ObjectMeta: ObjectMeta{
						Name:    "test",
						Version: "1.0.0",
					},
				},
			},
			hasError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.reference.Validate()
			if tt.hasError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestComponent_Validate(t *testing.T) {
	tests := []struct {
		name      string
		component Component
		hasError  bool
	}{
		{
			name: "valid",
			component: Component{
				ComponentMeta: ComponentMeta{
					ObjectMeta: ObjectMeta{
						Name:    "test",
						Version: "1.0.0",
					},
				},
				Provider: Provider{
					Name: "test",
				},
			},
			hasError: false,
		},
		{
			name: "invalid component meta",
			component: Component{
				Provider: Provider{
					Name: "test",
				},
			},
			hasError: true,
		},
		{
			name: "invalid provider",
			component: Component{
				ComponentMeta: ComponentMeta{
					ObjectMeta: ObjectMeta{
						Name:    "test",
						Version: "1.0.0",
					},
				},
			},
			hasError: true,
		},
		{
			name: "invalid resource",
			component: Component{
				ComponentMeta: ComponentMeta{
					ObjectMeta: ObjectMeta{
						Name:    "test",
						Version: "1.0.0",
					},
				},
				Provider: Provider{
					Name: "test",
				},
				Resources: []Resource{
					{
						Type: "test",
					},
				},
			},
			hasError: true,
		},
		{
			name: "invalid source",
			component: Component{
				ComponentMeta: ComponentMeta{
					ObjectMeta: ObjectMeta{
						Name:    "test",
						Version: "1.0.0",
					},
				},
				Provider: Provider{
					Name: "test",
				},
				Sources: []Source{
					{
						Type: "test",
					},
				},
			},
			hasError: true,
		},
		{
			name: "invalid reference",
			component: Component{
				ComponentMeta: ComponentMeta{
					ObjectMeta: ObjectMeta{
						Name:    "test",
						Version: "1.0.0",
					},
				},
				Provider: Provider{
					Name: "test",
				},
				References: []Reference{
					{
						ElementMeta: ElementMeta{
							ObjectMeta: ObjectMeta{
								Name:    "test",
								Version: "1.0.0",
							},
						},
					},
				},
			},
			hasError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.component.Validate()
			if tt.hasError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
