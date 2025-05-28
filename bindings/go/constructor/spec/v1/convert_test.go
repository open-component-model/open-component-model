package v1

import (
	"testing"

	"github.com/stretchr/testify/assert"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
)

func TestConvertToRuntimeResource(t *testing.T) {
	tests := []struct {
		name     string
		input    Resource
		expected descriptor.Resource
	}{
		{
			name: "basic resource conversion",
			input: Resource{
				ElementMeta: ElementMeta{
					ObjectMeta: ObjectMeta{
						Name:    "test-resource",
						Version: "1.0.0",
					},
				},
				Type:     "ociImage",
				Relation: "local",
			},
			expected: descriptor.Resource{
				ElementMeta: descriptor.ElementMeta{
					ObjectMeta: descriptor.ObjectMeta{
						Name:    "test-resource",
						Version: "1.0.0",
					},
				},
				Type:     "ociImage",
				Relation: descriptor.LocalRelation,
			},
		},
		{
			name: "resource with labels",
			input: Resource{
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
				Type: "ociImage",
			},
			expected: descriptor.Resource{
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
				Type: "ociImage",
			},
		},
		{
			name: "resource with source refs",
			input: Resource{
				ElementMeta: ElementMeta{
					ObjectMeta: ObjectMeta{
						Name:    "test-resource",
						Version: "1.0.0",
					},
				},
				Type: "ociImage",
				SourceRefs: []SourceRef{
					{
						IdentitySelector: map[string]string{
							"name": "test-source",
						},
						Labels: []Label{
							{
								Name:    "source-label",
								Value:   "source-value",
								Signing: true,
							},
						},
					},
				},
			},
			expected: descriptor.Resource{
				ElementMeta: descriptor.ElementMeta{
					ObjectMeta: descriptor.ObjectMeta{
						Name:    "test-resource",
						Version: "1.0.0",
					},
				},
				Type: "ociImage",
				SourceRefs: []descriptor.SourceRef{
					{
						IdentitySelector: map[string]string{
							"name": "test-source",
						},
						Labels: []descriptor.Label{
							{
								Name:    "source-label",
								Value:   "source-value",
								Signing: true,
							},
						},
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ConvertToRuntimeResource(tt.input)

			// Check basic fields
			assert.Equal(t, tt.expected.ElementMeta.ObjectMeta.Name, result.ElementMeta.ObjectMeta.Name)
			assert.Equal(t, tt.expected.ElementMeta.ObjectMeta.Version, result.ElementMeta.ObjectMeta.Version)
			assert.Equal(t, tt.expected.Type, result.Type)
			assert.Equal(t, tt.expected.Relation, result.Relation)

			// Check creation time is set
			assert.NotZero(t, result.CreationTime)

			// Check labels if present
			if tt.input.Labels != nil {
				assert.Equal(t, tt.expected.ElementMeta.ObjectMeta.Labels, result.ElementMeta.ObjectMeta.Labels)
			}

			// Check source refs if present
			if tt.input.SourceRefs != nil {
				assert.Equal(t, tt.expected.SourceRefs, result.SourceRefs)
			}
		})
	}
}

func TestConvertToRuntimeSource(t *testing.T) {
	tests := []struct {
		name     string
		input    Source
		expected descriptor.Source
	}{
		{
			name: "basic source conversion",
			input: Source{
				ElementMeta: ElementMeta{
					ObjectMeta: ObjectMeta{
						Name:    "test-source",
						Version: "1.0.0",
					},
				},
				Type: "ociImage",
			},
			expected: descriptor.Source{
				ElementMeta: descriptor.ElementMeta{
					ObjectMeta: descriptor.ObjectMeta{
						Name:    "test-source",
						Version: "1.0.0",
					},
				},
				Type: "ociImage",
			},
		},
		{
			name: "source with labels",
			input: Source{
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
				Type: "ociImage",
			},
			expected: descriptor.Source{
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
				Type: "ociImage",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ConvertToRuntimeSource(tt.input)

			// Check basic fields
			assert.Equal(t, tt.expected.ElementMeta.ObjectMeta.Name, result.ElementMeta.ObjectMeta.Name)
			assert.Equal(t, tt.expected.ElementMeta.ObjectMeta.Version, result.ElementMeta.ObjectMeta.Version)
			assert.Equal(t, tt.expected.Type, result.Type)

			// Check labels if present
			if tt.input.Labels != nil {
				assert.Equal(t, tt.expected.ElementMeta.ObjectMeta.Labels, result.ElementMeta.ObjectMeta.Labels)
			}
		})
	}
}

func TestConvertToRuntimeReference(t *testing.T) {
	tests := []struct {
		name     string
		input    Reference
		expected descriptor.Reference
	}{
		{
			name: "basic reference conversion",
			input: Reference{
				ElementMeta: ElementMeta{
					ObjectMeta: ObjectMeta{
						Name:    "test-reference",
						Version: "1.0.0",
					},
				},
				Component: "test-component",
			},
			expected: descriptor.Reference{
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
			input: Reference{
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
			expected: descriptor.Reference{
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
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ConvertToRuntimeReference(tt.input)

			// Check basic fields
			assert.Equal(t, tt.expected.ElementMeta.ObjectMeta.Name, result.ElementMeta.ObjectMeta.Name)
			assert.Equal(t, tt.expected.ElementMeta.ObjectMeta.Version, result.ElementMeta.ObjectMeta.Version)
			assert.Equal(t, tt.expected.Component, result.Component)

			// Check labels if present
			if tt.input.Labels != nil {
				assert.Equal(t, tt.expected.ElementMeta.ObjectMeta.Labels, result.ElementMeta.ObjectMeta.Labels)
			}
		})
	}
}

func TestConvertToRuntimeComponent(t *testing.T) {
	tests := []struct {
		name     string
		input    Component
		expected descriptor.Component
	}{
		{
			name: "basic component conversion",
			input: Component{
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
			expected: descriptor.Component{
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
			name: "component with provider labels",
			input: Component{
				ComponentMeta: ComponentMeta{
					ObjectMeta: ObjectMeta{
						Name:    "test-component",
						Version: "1.0.0",
					},
				},
				Provider: Provider{
					Name: "test-provider",
					Labels: []Label{
						{
							Name:    "provider-label",
							Value:   "provider-value",
							Signing: true,
						},
					},
				},
			},
			expected: descriptor.Component{
				ComponentMeta: descriptor.ComponentMeta{
					ObjectMeta: descriptor.ObjectMeta{
						Name:    "test-component",
						Version: "1.0.0",
					},
				},
				Provider: descriptor.Provider{
					Name: "test-provider",
					Labels: []descriptor.Label{
						{
							Name:    "provider-label",
							Value:   "provider-value",
							Signing: true,
						},
					},
				},
			},
		},
		{
			name: "component with resources and sources",
			input: Component{
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
						Type:     "ociImage",
						Relation: "local",
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
						Type: "ociImage",
					},
				},
			},
			expected: descriptor.Component{
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
						Type:     "ociImage",
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
						Type: "ociImage",
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ConvertToRuntimeComponent(tt.input)

			// Check basic fields
			assert.Equal(t, tt.expected.ComponentMeta.ObjectMeta.Name, result.ComponentMeta.ObjectMeta.Name)
			assert.Equal(t, tt.expected.ComponentMeta.ObjectMeta.Version, result.ComponentMeta.ObjectMeta.Version)

			// Check provider
			assert.Equal(t, tt.expected.Provider, result.Provider)

			// Check resources if present
			if tt.input.Resources != nil {
				assert.Equal(t, len(tt.expected.Resources), len(result.Resources))
				for i := range tt.expected.Resources {
					assert.Equal(t, tt.expected.Resources[i].ElementMeta.ObjectMeta.Name, result.Resources[i].ElementMeta.ObjectMeta.Name)
					assert.Equal(t, tt.expected.Resources[i].ElementMeta.ObjectMeta.Version, result.Resources[i].ElementMeta.ObjectMeta.Version)
					assert.Equal(t, tt.expected.Resources[i].Type, result.Resources[i].Type)
					assert.Equal(t, tt.expected.Resources[i].Relation, result.Resources[i].Relation)
				}
			}

			// Check sources if present
			if tt.input.Sources != nil {
				assert.Equal(t, len(tt.expected.Sources), len(result.Sources))
				for i := range tt.expected.Sources {
					assert.Equal(t, tt.expected.Sources[i].ElementMeta.ObjectMeta.Name, result.Sources[i].ElementMeta.ObjectMeta.Name)
					assert.Equal(t, tt.expected.Sources[i].ElementMeta.ObjectMeta.Version, result.Sources[i].ElementMeta.ObjectMeta.Version)
					assert.Equal(t, tt.expected.Sources[i].Type, result.Sources[i].Type)
				}
			}
		})
	}
}

func TestConvertToRuntimeDescriptor(t *testing.T) {
	tests := []struct {
		name     string
		input    ComponentConstructor
		expected *descriptor.Descriptor
	}{
		{
			name: "basic descriptor conversion",
			input: ComponentConstructor{
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
					Version: "v1",
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
		{
			name: "empty components",
			input: ComponentConstructor{
				Components: []Component{},
			},
			expected: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ConvertToRuntimeDescriptor(tt.input)
			if tt.expected == nil {
				assert.Nil(t, result)
				return
			}

			// Check meta
			assert.Equal(t, tt.expected.Meta.Version, result.Meta.Version)

			// Check component
			assert.Equal(t, tt.expected.Component.ComponentMeta.ObjectMeta.Name, result.Component.ComponentMeta.ObjectMeta.Name)
			assert.Equal(t, tt.expected.Component.ComponentMeta.ObjectMeta.Version, result.Component.ComponentMeta.ObjectMeta.Version)
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
			name:     "nil source refs",
			input:    nil,
			expected: nil,
		},
		{
			name:     "empty source refs",
			input:    []SourceRef{},
			expected: []descriptor.SourceRef{},
		},
		{
			name: "single source ref",
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
		{
			name: "multiple source refs",
			input: []SourceRef{
				{
					IdentitySelector: map[string]string{
						"name": "source1",
					},
					Labels: []Label{
						{
							Name:    "label1",
							Value:   "value1",
							Signing: true,
						},
					},
				},
				{
					IdentitySelector: map[string]string{
						"name": "source2",
					},
					Labels: []Label{
						{
							Name:    "label2",
							Value:   "value2",
							Signing: false,
						},
					},
				},
			},
			expected: []descriptor.SourceRef{
				{
					IdentitySelector: map[string]string{
						"name": "source1",
					},
					Labels: []descriptor.Label{
						{
							Name:    "label1",
							Value:   "value1",
							Signing: true,
						},
					},
				},
				{
					IdentitySelector: map[string]string{
						"name": "source2",
					},
					Labels: []descriptor.Label{
						{
							Name:    "label2",
							Value:   "value2",
							Signing: false,
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
