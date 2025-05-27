package runtime

import (
	"testing"

	"github.com/stretchr/testify/assert"

	v1 "ocm.software/open-component-model/bindings/go/constructor/spec/v1"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	rt "ocm.software/open-component-model/bindings/go/runtime"
)

func TestConvertToRuntimeResource(t *testing.T) {
	tests := []struct {
		name     string
		resource *v1.Resource
		want     descriptor.Resource
	}{
		{
			name:     "nil resource",
			resource: nil,
			want:     descriptor.Resource{},
		},
		{
			name: "basic resource",
			resource: &v1.Resource{
				ElementMeta: v1.ElementMeta{
					ObjectMeta: v1.ObjectMeta{
						Name:    "test",
						Version: "1.0.0",
						Labels: []v1.Label{
							{Name: "test", Value: "value", Signing: true},
						},
					},
				},
				Type:     "test-type",
				Relation: v1.LocalRelation,
			},
			want: descriptor.Resource{
				ElementMeta: descriptor.ElementMeta{
					ObjectMeta: descriptor.ObjectMeta{
						Name:    "test",
						Version: "1.0.0",
						Labels: []descriptor.Label{
							{Name: "test", Value: "value", Signing: true},
						},
					},
				},
				Type:     "test-type",
				Relation: descriptor.LocalRelation,
			},
		},
		{
			name: "resource with source refs",
			resource: &v1.Resource{
				ElementMeta: v1.ElementMeta{
					ObjectMeta: v1.ObjectMeta{
						Name:    "test",
						Version: "1.0.0",
					},
				},
				Type:     "test-type",
				Relation: v1.LocalRelation,
				SourceRefs: []v1.SourceRef{
					{
						IdentitySelector: map[string]string{"name": "test"},
						Labels: []v1.Label{
							{Name: "test", Value: "value"},
						},
					},
				},
			},
			want: descriptor.Resource{
				ElementMeta: descriptor.ElementMeta{
					ObjectMeta: descriptor.ObjectMeta{
						Name:    "test",
						Version: "1.0.0",
					},
				},
				Type:     "test-type",
				Relation: descriptor.LocalRelation,
				SourceRefs: []descriptor.SourceRef{
					{
						IdentitySelector: map[string]string{"name": "test"},
						Labels: []descriptor.Label{
							{Name: "test", Value: "value"},
						},
					},
				},
			},
		},
		{
			name: "resource with access",
			resource: &v1.Resource{
				ElementMeta: v1.ElementMeta{
					ObjectMeta: v1.ObjectMeta{
						Name:    "test",
						Version: "1.0.0",
					},
				},
				Type:     "test-type",
				Relation: v1.LocalRelation,
				AccessOrInput: v1.AccessOrInput{
					Access: &rt.Raw{
						Type: rt.NewUnversionedType("test-access"),
						Data: []byte(`{"test": "value"}`),
					},
				},
			},
			want: descriptor.Resource{
				ElementMeta: descriptor.ElementMeta{
					ObjectMeta: descriptor.ObjectMeta{
						Name:    "test",
						Version: "1.0.0",
					},
				},
				Type:     "test-type",
				Relation: descriptor.LocalRelation,
				Access: &rt.Raw{
					Type: rt.NewUnversionedType("test-access"),
					Data: []byte(`{"test": "value"}`),
				},
			},
		},
		{
			name: "resource with input",
			resource: &v1.Resource{
				ElementMeta: v1.ElementMeta{
					ObjectMeta: v1.ObjectMeta{
						Name:    "test",
						Version: "1.0.0",
					},
				},
				Type:     "test-type",
				Relation: v1.LocalRelation,
				AccessOrInput: v1.AccessOrInput{
					Input: &rt.Raw{
						Type: rt.NewUnversionedType("test-input"),
						Data: []byte(`{"test": "value"}`),
					},
				},
			},
			want: descriptor.Resource{
				ElementMeta: descriptor.ElementMeta{
					ObjectMeta: descriptor.ObjectMeta{
						Name:    "test",
						Version: "1.0.0",
					},
				},
				Type:     "test-type",
				Relation: descriptor.LocalRelation,
				Access: &rt.Raw{
					Type: rt.NewUnversionedType("test-input"),
					Data: []byte(`{"test": "value"}`),
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ConvertToRuntimeResource(tt.resource)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestConvertToRuntimeSource(t *testing.T) {
	tests := []struct {
		name   string
		source *v1.Source
		want   descriptor.Source
	}{
		{
			name:   "nil source",
			source: nil,
			want:   descriptor.Source{},
		},
		{
			name: "basic source",
			source: &v1.Source{
				ElementMeta: v1.ElementMeta{
					ObjectMeta: v1.ObjectMeta{
						Name:    "test",
						Version: "1.0.0",
						Labels: []v1.Label{
							{Name: "test", Value: "value", Signing: true},
						},
					},
				},
				Type: "test-type",
			},
			want: descriptor.Source{
				ElementMeta: descriptor.ElementMeta{
					ObjectMeta: descriptor.ObjectMeta{
						Name:    "test",
						Version: "1.0.0",
						Labels: []descriptor.Label{
							{Name: "test", Value: "value", Signing: true},
						},
					},
				},
				Type: "test-type",
			},
		},
		{
			name: "source with access",
			source: &v1.Source{
				ElementMeta: v1.ElementMeta{
					ObjectMeta: v1.ObjectMeta{
						Name:    "test",
						Version: "1.0.0",
					},
				},
				Type: "test-type",
				AccessOrInput: v1.AccessOrInput{
					Access: &rt.Raw{
						Type: rt.NewUnversionedType("test-access"),
						Data: []byte(`{"test": "value"}`),
					},
				},
			},
			want: descriptor.Source{
				ElementMeta: descriptor.ElementMeta{
					ObjectMeta: descriptor.ObjectMeta{
						Name:    "test",
						Version: "1.0.0",
					},
				},
				Type: "test-type",
				Access: &rt.Raw{
					Type: rt.NewUnversionedType("test-access"),
					Data: []byte(`{"test": "value"}`),
				},
			},
		},
		{
			name: "source with input",
			source: &v1.Source{
				ElementMeta: v1.ElementMeta{
					ObjectMeta: v1.ObjectMeta{
						Name:    "test",
						Version: "1.0.0",
					},
				},
				Type: "test-type",
				AccessOrInput: v1.AccessOrInput{
					Input: &rt.Raw{
						Type: rt.NewUnversionedType("test-input"),
						Data: []byte(`{"test": "value"}`),
					},
				},
			},
			want: descriptor.Source{
				ElementMeta: descriptor.ElementMeta{
					ObjectMeta: descriptor.ObjectMeta{
						Name:    "test",
						Version: "1.0.0",
					},
				},
				Type: "test-type",
				Access: &rt.Raw{
					Type: rt.NewUnversionedType("test-input"),
					Data: []byte(`{"test": "value"}`),
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ConvertToRuntimeSource(tt.source)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestConvertToRuntimeReference(t *testing.T) {
	tests := []struct {
		name      string
		reference *v1.Reference
		want      descriptor.Reference
	}{
		{
			name:      "nil reference",
			reference: nil,
			want:      descriptor.Reference{},
		},
		{
			name: "basic reference",
			reference: &v1.Reference{
				ElementMeta: v1.ElementMeta{
					ObjectMeta: v1.ObjectMeta{
						Name:    "test",
						Version: "1.0.0",
						Labels: []v1.Label{
							{Name: "test", Value: "value", Signing: true},
						},
					},
				},
				Component: "test-component",
			},
			want: descriptor.Reference{
				ElementMeta: descriptor.ElementMeta{
					ObjectMeta: descriptor.ObjectMeta{
						Name:    "test",
						Version: "1.0.0",
						Labels: []descriptor.Label{
							{Name: "test", Value: "value", Signing: true},
						},
					},
				},
				Component: "test-component",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ConvertToRuntimeReference(tt.reference)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestConvertToRuntimeComponent(t *testing.T) {
	tests := []struct {
		name      string
		component *v1.Component
		want      descriptor.Component
	}{
		{
			name:      "nil component",
			component: nil,
			want:      descriptor.Component{},
		},
		{
			name: "basic component",
			component: &v1.Component{
				ComponentMeta: v1.ComponentMeta{
					ObjectMeta: v1.ObjectMeta{
						Name:    "test",
						Version: "1.0.0",
						Labels: []v1.Label{
							{Name: "test", Value: "value", Signing: true},
						},
					},
					CreationTime: "2024-01-01T00:00:00Z",
				},
				Provider: v1.Provider{
					Name: "test-provider",
					Labels: []v1.Label{
						{Name: "test", Value: "value"},
					},
				},
			},
			want: descriptor.Component{
				ComponentMeta: descriptor.ComponentMeta{
					ObjectMeta: descriptor.ObjectMeta{
						Name:    "test",
						Version: "1.0.0",
						Labels: []descriptor.Label{
							{Name: "test", Value: "value", Signing: true},
						},
					},
					CreationTime: "2024-01-01T00:00:00Z",
				},
				Provider: rt.Identity{
					"name": "test-provider",
					"test": "value",
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ConvertToRuntimeComponent(tt.component)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestConvertToRuntimeDescriptor(t *testing.T) {
	tests := []struct {
		name        string
		constructor *v1.ComponentConstructor
		want        *descriptor.Descriptor
	}{
		{
			name:        "nil constructor",
			constructor: nil,
			want:        nil,
		},
		{
			name:        "empty constructor",
			constructor: &v1.ComponentConstructor{},
			want:        nil,
		},
		{
			name: "basic constructor",
			constructor: &v1.ComponentConstructor{
				Components: []v1.Component{
					{
						ComponentMeta: v1.ComponentMeta{
							ObjectMeta: v1.ObjectMeta{
								Name:    "test",
								Version: "1.0.0",
							},
						},
						Provider: v1.Provider{
							Name: "test-provider",
						},
					},
				},
			},
			want: &descriptor.Descriptor{
				Meta: descriptor.Meta{
					Version: "v1",
				},
				Component: descriptor.Component{
					ComponentMeta: descriptor.ComponentMeta{
						ObjectMeta: descriptor.ObjectMeta{
							Name:    "test",
							Version: "1.0.0",
						},
					},
					Provider: rt.Identity{
						"name": "test-provider",
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ConvertToRuntimeDescriptor(tt.constructor)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestConvertToRuntimeConstructor(t *testing.T) {
	tests := []struct {
		name        string
		constructor *v1.ComponentConstructor
		want        *ComponentConstructor
	}{
		{
			name:        "nil constructor",
			constructor: nil,
			want:        nil,
		},
		{
			name: "basic constructor",
			constructor: &v1.ComponentConstructor{
				Components: []v1.Component{
					{
						ComponentMeta: v1.ComponentMeta{
							ObjectMeta: v1.ObjectMeta{
								Name:    "test-component",
								Version: "1.0.0",
							},
						},
						Provider: v1.Provider{
							Name: "test-provider",
						},
					},
				},
			},
			want: &ComponentConstructor{
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
		},
		{
			name: "constructor with resources and sources",
			constructor: &v1.ComponentConstructor{
				Components: []v1.Component{
					{
						ComponentMeta: v1.ComponentMeta{
							ObjectMeta: v1.ObjectMeta{
								Name:    "test-component",
								Version: "1.0.0",
							},
						},
						Provider: v1.Provider{
							Name: "test-provider",
						},
						Resources: []v1.Resource{
							{
								ElementMeta: v1.ElementMeta{
									ObjectMeta: v1.ObjectMeta{
										Name:    "test-resource",
										Version: "1.0.0",
									},
								},
								Type:     "blob",
								Relation: v1.LocalRelation,
							},
						},
						Sources: []v1.Source{
							{
								ElementMeta: v1.ElementMeta{
									ObjectMeta: v1.ObjectMeta{
										Name:    "test-source",
										Version: "1.0.0",
									},
								},
								Type: "git",
							},
						},
					},
				},
			},
			want: &ComponentConstructor{
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
				},
			},
		},
		{
			name: "constructor with provider labels",
			constructor: &v1.ComponentConstructor{
				Components: []v1.Component{
					{
						ComponentMeta: v1.ComponentMeta{
							ObjectMeta: v1.ObjectMeta{
								Name:    "test-component",
								Version: "1.0.0",
							},
						},
						Provider: v1.Provider{
							Name: "test-provider",
							Labels: []v1.Label{
								{
									Name:    "provider-label",
									Value:   "provider-value",
									Signing: true,
								},
							},
						},
					},
				},
			},
			want: &ComponentConstructor{
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
							Labels: []Label{
								{
									Name:    "provider-label",
									Value:   "provider-value",
									Signing: true,
								},
							},
						},
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ConvertToRuntimeConstructor(tt.constructor)
			if tt.want == nil {
				assert.Nil(t, got)
				return
			}

			// Check basic component fields
			assert.Equal(t, len(tt.want.Components), len(got.Components))
			if len(got.Components) > 0 {
				assert.Equal(t, tt.want.Components[0].Name, got.Components[0].Name)
				assert.Equal(t, tt.want.Components[0].Version, got.Components[0].Version)
				assert.Equal(t, tt.want.Components[0].Provider.Name, got.Components[0].Provider.Name)

				// Check provider labels
				assert.Equal(t, len(tt.want.Components[0].Provider.Labels), len(got.Components[0].Provider.Labels))
				if len(got.Components[0].Provider.Labels) > 0 {
					assert.Equal(t, tt.want.Components[0].Provider.Labels[0].Name, got.Components[0].Provider.Labels[0].Name)
					assert.Equal(t, tt.want.Components[0].Provider.Labels[0].Value, got.Components[0].Provider.Labels[0].Value)
					assert.Equal(t, tt.want.Components[0].Provider.Labels[0].Signing, got.Components[0].Provider.Labels[0].Signing)
				}

				// Check resources
				assert.Equal(t, len(tt.want.Components[0].Resources), len(got.Components[0].Resources))
				if len(got.Components[0].Resources) > 0 {
					assert.Equal(t, tt.want.Components[0].Resources[0].Name, got.Components[0].Resources[0].Name)
					assert.Equal(t, tt.want.Components[0].Resources[0].Version, got.Components[0].Resources[0].Version)
					assert.Equal(t, tt.want.Components[0].Resources[0].Type, got.Components[0].Resources[0].Type)
					assert.Equal(t, tt.want.Components[0].Resources[0].Relation, got.Components[0].Resources[0].Relation)
				}

				// Check sources
				assert.Equal(t, len(tt.want.Components[0].Sources), len(got.Components[0].Sources))
				if len(got.Components[0].Sources) > 0 {
					assert.Equal(t, tt.want.Components[0].Sources[0].Name, got.Components[0].Sources[0].Name)
					assert.Equal(t, tt.want.Components[0].Sources[0].Version, got.Components[0].Sources[0].Version)
					assert.Equal(t, tt.want.Components[0].Sources[0].Type, got.Components[0].Sources[0].Type)
				}
			}
		})
	}
}
