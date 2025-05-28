package runtime

import (
	"testing"

	"github.com/stretchr/testify/assert"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/runtime"
)

func TestConvertToDescriptorResource(t *testing.T) {
	tests := []struct {
		name     string
		input    *Resource
		expected *descriptor.Resource
	}{
		{
			name:     "nil resource",
			input:    nil,
			expected: nil,
		},
		{
			name: "basic resource",
			input: &Resource{
				ElementMeta: ElementMeta{
					ObjectMeta: ObjectMeta{
						Name:    "test-resource",
						Version: "1.0.0",
					},
				},
				Type:     "blob",
				Relation: LocalRelation,
				AccessOrInput: AccessOrInput{
					Access: &runtime.Raw{
						Type: runtime.Type{
							Version: "v1alpha1",
							Name:    "Typ",
						},
						Data: []byte(`{"type": "Typ/v1alpha1"}`),
					},
				},
			},
			expected: &descriptor.Resource{
				ElementMeta: descriptor.ElementMeta{
					ObjectMeta: descriptor.ObjectMeta{
						Name:    "test-resource",
						Version: "1.0.0",
					},
				},
				Type:     "blob",
				Relation: descriptor.LocalRelation,
				Access: &runtime.Raw{
					Type: runtime.Type{
						Version: "v1alpha1",
						Name:    "Typ",
					},
					Data: []byte(`{"type": "Typ/v1alpha1"}`),
				},
			},
		},
		{
			name: "resource with labels",
			input: &Resource{
				ElementMeta: ElementMeta{
					ObjectMeta: ObjectMeta{
						Name:    "test-resource",
						Version: "1.0.0",
						Labels: []Label{
							{
								Name:    "test-label",
								Value:   "test-value",
								Signing: true,
							},
						},
					},
				},
				Type:     "blob",
				Relation: LocalRelation,
			},
			expected: &descriptor.Resource{
				ElementMeta: descriptor.ElementMeta{
					ObjectMeta: descriptor.ObjectMeta{
						Name:    "test-resource",
						Version: "1.0.0",
						Labels: []descriptor.Label{
							{
								Name:    "test-label",
								Value:   "test-value",
								Signing: true,
							},
						},
					},
				},
				Type:     "blob",
				Relation: descriptor.LocalRelation,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ConvertToDescriptorResource(tt.input)
			if tt.expected == nil {
				assert.Nil(t, result)
				return
			}

			assert.Equal(t, tt.expected.Name, result.Name)
			assert.Equal(t, tt.expected.Version, result.Version)
			assert.Equal(t, tt.expected.Type, result.Type)
			assert.Equal(t, tt.expected.Relation, result.Relation)
			assert.Equal(t, tt.expected.Labels, result.Labels)
			if tt.expected.Access != nil {
				assert.NotNil(t, result.Access)
				assert.Equal(t, tt.expected.Access.GetType(), result.Access.GetType())
			}
		})
	}
}

func TestConvertToDescriptorSource(t *testing.T) {
	tests := []struct {
		name     string
		input    *Source
		expected *descriptor.Source
	}{
		{
			name:     "nil source",
			input:    nil,
			expected: nil,
		},
		{
			name: "basic source",
			input: &Source{
				ElementMeta: ElementMeta{
					ObjectMeta: ObjectMeta{
						Name:    "test-source",
						Version: "1.0.0",
					},
				},
				Type: "git",
				AccessOrInput: AccessOrInput{
					Access: &runtime.Raw{
						Type: runtime.Type{
							Version: "v1alpha1",
							Name:    "Typ",
						},
						Data: []byte(`{"type": "Typ/v1alpha1"}`),
					},
				},
			},
			expected: &descriptor.Source{
				ElementMeta: descriptor.ElementMeta{
					ObjectMeta: descriptor.ObjectMeta{
						Name:    "test-source",
						Version: "1.0.0",
					},
				},
				Type: "git",
				Access: &runtime.Raw{
					Type: runtime.Type{
						Version: "v1alpha1",
						Name:    "Typ",
					},
					Data: []byte(`{"type": "Typ/v1alpha1"}`),
				},
			},
		},
		{
			name: "source with labels",
			input: &Source{
				ElementMeta: ElementMeta{
					ObjectMeta: ObjectMeta{
						Name:    "test-source",
						Version: "1.0.0",
						Labels: []Label{
							{
								Name:    "test-label",
								Value:   "test-value",
								Signing: true,
							},
						},
					},
				},
				Type: "git",
			},
			expected: &descriptor.Source{
				ElementMeta: descriptor.ElementMeta{
					ObjectMeta: descriptor.ObjectMeta{
						Name:    "test-source",
						Version: "1.0.0",
						Labels: []descriptor.Label{
							{
								Name:    "test-label",
								Value:   "test-value",
								Signing: true,
							},
						},
					},
				},
				Type: "git",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ConvertToDescriptorSource(tt.input)
			if tt.expected == nil {
				assert.Nil(t, result)
				return
			}

			assert.Equal(t, tt.expected.Name, result.Name)
			assert.Equal(t, tt.expected.Version, result.Version)
			assert.Equal(t, tt.expected.Type, result.Type)
			assert.Equal(t, tt.expected.Labels, result.Labels)
			if tt.expected.Access != nil {
				assert.NotNil(t, result.Access)
				assert.Equal(t, tt.expected.Access.GetType(), result.Access.GetType())
			}
		})
	}
}

func TestConvertToDescriptorComponent(t *testing.T) {
	tests := []struct {
		name     string
		input    *Component
		expected *descriptor.Component
	}{
		{
			name:     "nil component",
			input:    nil,
			expected: nil,
		},
		{
			name: "basic component",
			input: &Component{
				ComponentMeta: ComponentMeta{
					ObjectMeta: ObjectMeta{
						Name:    "test-component",
						Version: "1.0.0",
					},
				},
				Provider: Provider{
					Name: "test-provider",
				},
			},
			expected: &descriptor.Component{
				ComponentMeta: descriptor.ComponentMeta{
					ObjectMeta: descriptor.ObjectMeta{
						Name:    "test-component",
						Version: "1.0.0",
					},
				},
				Provider: descriptor.Provider{
					Name: "test-provider",
				},
			},
		},
		{
			name: "component with resources and sources",
			input: &Component{
				ComponentMeta: ComponentMeta{
					ObjectMeta: ObjectMeta{
						Name:    "test-component",
						Version: "1.0.0",
					},
				},
				Provider: Provider{
					Name: "test-provider",
				},
				Resources: []Resource{
					{
						ElementMeta: ElementMeta{
							ObjectMeta: ObjectMeta{
								Name:    "test-resource",
								Version: "1.0.0",
							},
						},
						Type:     "blob",
						Relation: LocalRelation,
					},
				},
				Sources: []Source{
					{
						ElementMeta: ElementMeta{
							ObjectMeta: ObjectMeta{
								Name:    "test-source",
								Version: "1.0.0",
							},
						},
						Type: "git",
					},
				},
			},
			expected: &descriptor.Component{
				ComponentMeta: descriptor.ComponentMeta{
					ObjectMeta: descriptor.ObjectMeta{
						Name:    "test-component",
						Version: "1.0.0",
					},
				},
				Provider: descriptor.Provider{
					Name: "test-provider",
				},
				Resources: []descriptor.Resource{
					{
						ElementMeta: descriptor.ElementMeta{
							ObjectMeta: descriptor.ObjectMeta{
								Name:    "test-resource",
								Version: "1.0.0",
							},
						},
						Type:     "blob",
						Relation: descriptor.LocalRelation,
					},
				},
				Sources: []descriptor.Source{
					{
						ElementMeta: descriptor.ElementMeta{
							ObjectMeta: descriptor.ObjectMeta{
								Name:    "test-source",
								Version: "1.0.0",
							},
						},
						Type: "git",
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ConvertToDescriptorComponent(tt.input)
			if tt.expected == nil {
				assert.Nil(t, result)
				return
			}

			assert.Equal(t, tt.expected.Name, result.Name)
			assert.Equal(t, tt.expected.Version, result.Version)
			assert.Equal(t, tt.expected.Provider, result.Provider)
			assert.Equal(t, len(tt.expected.Resources), len(result.Resources))
			assert.Equal(t, len(tt.expected.Sources), len(result.Sources))
		})
	}
}

func TestConvertToDescriptor(t *testing.T) {
	tests := []struct {
		name     string
		input    *ComponentConstructor
		expected *descriptor.Descriptor
	}{
		{
			name:     "nil constructor",
			input:    nil,
			expected: nil,
		},
		{
			name:     "empty constructor",
			input:    &ComponentConstructor{},
			expected: nil,
		},
		{
			name: "basic constructor",
			input: &ComponentConstructor{
				Components: []Component{
					{
						ComponentMeta: ComponentMeta{
							ObjectMeta: ObjectMeta{
								Name:    "test-component",
								Version: "1.0.0",
							},
						},
						Provider: Provider{
							Name: "test-provider",
						},
					},
				},
			},
			expected: &descriptor.Descriptor{
				Meta: descriptor.Meta{
					Version: "v2",
				},
				Component: descriptor.Component{
					ComponentMeta: descriptor.ComponentMeta{
						ObjectMeta: descriptor.ObjectMeta{
							Name:    "test-component",
							Version: "1.0.0",
						},
					},
					Provider: descriptor.Provider{
						Name: "test-provider",
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ConvertToDescriptor(tt.input)
			if tt.expected == nil {
				assert.Nil(t, result)
				return
			}

			assert.Equal(t, tt.expected.Meta.Version, result.Meta.Version)
			assert.Equal(t, tt.expected.Component.Name, result.Component.Name)
			assert.Equal(t, tt.expected.Component.Version, result.Component.Version)
			assert.Equal(t, tt.expected.Component.Provider, result.Component.Provider)
		})
	}
}

func TestConvertFromLabels(t *testing.T) {
	tests := []struct {
		name     string
		input    []Label
		expected []descriptor.Label
	}{
		{
			name:     "nil labels",
			input:    nil,
			expected: nil,
		},
		{
			name:     "empty labels",
			input:    []Label{},
			expected: []descriptor.Label{},
		},
		{
			name: "single label",
			input: []Label{
				{
					Name:    "test-label",
					Value:   "test-value",
					Signing: true,
				},
			},
			expected: []descriptor.Label{
				{
					Name:    "test-label",
					Value:   "test-value",
					Signing: true,
				},
			},
		},
		{
			name: "multiple labels",
			input: []Label{
				{
					Name:    "label1",
					Value:   "value1",
					Signing: true,
				},
				{
					Name:    "label2",
					Value:   "value2",
					Signing: false,
				},
			},
			expected: []descriptor.Label{
				{
					Name:    "label1",
					Value:   "value1",
					Signing: true,
				},
				{
					Name:    "label2",
					Value:   "value2",
					Signing: false,
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ConvertFromLabels(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestConvertFromSourceRefs(t *testing.T) {
	tests := []struct {
		name     string
		input    []SourceRef
		expected []descriptor.SourceRef
	}{
		{
			name:     "nil refs",
			input:    nil,
			expected: nil,
		},
		{
			name:     "empty refs",
			input:    []SourceRef{},
			expected: []descriptor.SourceRef{},
		},
		{
			name: "single ref",
			input: []SourceRef{
				{
					IdentitySelector: map[string]string{
						"name": "test-source",
					},
					Labels: []Label{
						{
							Name:    "test-label",
							Value:   "test-value",
							Signing: true,
						},
					},
				},
			},
			expected: []descriptor.SourceRef{
				{
					IdentitySelector: map[string]string{
						"name": "test-source",
					},
					Labels: []descriptor.Label{
						{
							Name:    "test-label",
							Value:   "test-value",
							Signing: true,
						},
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ConvertFromSourceRefs(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestConvertToDescriptorReference(t *testing.T) {
	tests := []struct {
		name     string
		input    *Reference
		expected *descriptor.Reference
	}{
		{
			name:     "nil reference",
			input:    nil,
			expected: nil,
		},
		{
			name: "basic reference",
			input: &Reference{
				ElementMeta: ElementMeta{
					ObjectMeta: ObjectMeta{
						Name:    "test-reference",
						Version: "1.0.0",
					},
				},
				Component: "test-component",
			},
			expected: &descriptor.Reference{
				ElementMeta: descriptor.ElementMeta{
					ObjectMeta: descriptor.ObjectMeta{
						Name:    "test-reference",
						Version: "1.0.0",
					},
				},
				Component: "test-component",
			},
		},
		{
			name: "reference with labels",
			input: &Reference{
				ElementMeta: ElementMeta{
					ObjectMeta: ObjectMeta{
						Name:    "test-reference",
						Version: "1.0.0",
						Labels: []Label{
							{
								Name:    "test-label",
								Value:   "test-value",
								Signing: true,
							},
						},
					},
				},
				Component: "test-component",
			},
			expected: &descriptor.Reference{
				ElementMeta: descriptor.ElementMeta{
					ObjectMeta: descriptor.ObjectMeta{
						Name:    "test-reference",
						Version: "1.0.0",
						Labels: []descriptor.Label{
							{
								Name:    "test-label",
								Value:   "test-value",
								Signing: true,
							},
						},
					},
				},
				Component: "test-component",
			},
		},
		{
			name: "reference with extra identity",
			input: &Reference{
				ElementMeta: ElementMeta{
					ObjectMeta: ObjectMeta{
						Name:    "test-reference",
						Version: "1.0.0",
					},
					ExtraIdentity: runtime.Identity{
						"key1": "value1",
						"key2": "value2",
					},
				},
				Component: "test-component",
			},
			expected: &descriptor.Reference{
				ElementMeta: descriptor.ElementMeta{
					ObjectMeta: descriptor.ObjectMeta{
						Name:    "test-reference",
						Version: "1.0.0",
					},
					ExtraIdentity: runtime.Identity{
						"key1": "value1",
						"key2": "value2",
					},
				},
				Component: "test-component",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ConvertToDescriptorReference(tt.input)
			if tt.expected == nil {
				assert.Nil(t, result)
				return
			}

			assert.Equal(t, tt.expected.Name, result.Name)
			assert.Equal(t, tt.expected.Version, result.Version)
			assert.Equal(t, tt.expected.Component, result.Component)
			assert.Equal(t, tt.expected.Labels, result.Labels)
			assert.Equal(t, tt.expected.ExtraIdentity, result.ExtraIdentity)
		})
	}
}
