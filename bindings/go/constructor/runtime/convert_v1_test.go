package runtime

import (
	"testing"

	"github.com/stretchr/testify/assert"

	v1 "ocm.software/open-component-model/bindings/go/constructor/spec/v1"
	rt "ocm.software/open-component-model/bindings/go/runtime"
)

func TestConvertToRuntimeResource(t *testing.T) {
	tests := []struct {
		name     string
		resource *v1.Resource
		want     Resource
	}{
		{
			name:     "nil resource",
			resource: nil,
			want:     Resource{},
		},
		{
			name: "basic resource",
			resource: &v1.Resource{
				ElementMeta: v1.ElementMeta{
					ObjectMeta: v1.ObjectMeta{
						Name:    "test",
						Version: "1.0.0",
						Labels: []v1.Label{
							{Name: "test", Value: []byte("value"), Signing: true},
						},
					},
				},
				Type:     "test-type",
				Relation: v1.LocalRelation,
			},
			want: Resource{
				ElementMeta: ElementMeta{
					ObjectMeta: ObjectMeta{
						Name:    "test",
						Version: "1.0.0",
						Labels: []Label{
							{Name: "test", Value: []byte("value"), Signing: true},
						},
					},
				},
				Type:     "test-type",
				Relation: LocalRelation,
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
						IdentitySelector: map[string]string{
							"name": "test-source",
						},
						Labels: []v1.Label{
							{
								Name:    "source-label",
								Value:   []byte("source-value"),
								Signing: true,
							},
						},
					},
				},
			},
			want: Resource{
				ElementMeta: ElementMeta{
					ObjectMeta: ObjectMeta{
						Name:    "test",
						Version: "1.0.0",
					},
				},
				Type:     "test-type",
				Relation: LocalRelation,
				SourceRefs: []SourceRef{
					{
						IdentitySelector: map[string]string{
							"name": "test-source",
						},
						Labels: []Label{
							{
								Name:    "source-label",
								Value:   []byte("source-value"),
								Signing: true,
							},
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
			want: Resource{
				ElementMeta: ElementMeta{
					ObjectMeta: ObjectMeta{
						Name:    "test",
						Version: "1.0.0",
					},
				},
				Type:     "test-type",
				Relation: LocalRelation,
				AccessOrInput: AccessOrInput{
					Access: &rt.Raw{
						Type: rt.NewUnversionedType("test-access"),
						Data: []byte(`{"test": "value"}`),
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ConvertFromV1Resource(tt.resource)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestConvertToRuntimeSource(t *testing.T) {
	tests := []struct {
		name   string
		source *v1.Source
		want   Source
	}{
		{
			name:   "nil source",
			source: nil,
			want:   Source{},
		},
		{
			name: "basic source",
			source: &v1.Source{
				ElementMeta: v1.ElementMeta{
					ObjectMeta: v1.ObjectMeta{
						Name:    "test",
						Version: "1.0.0",
						Labels: []v1.Label{
							{Name: "test", Value: []byte("value"), Signing: true},
						},
					},
				},
				Type: "test-type",
			},
			want: Source{
				ElementMeta: ElementMeta{
					ObjectMeta: ObjectMeta{
						Name:    "test",
						Version: "1.0.0",
						Labels: []Label{
							{Name: "test", Value: []byte("value"), Signing: true},
						},
					},
				},
				Type:          "test-type",
				AccessOrInput: AccessOrInput{},
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
			want: Source{
				ElementMeta: ElementMeta{
					ObjectMeta: ObjectMeta{
						Name:    "test",
						Version: "1.0.0",
					},
				},
				Type: "test-type",
				AccessOrInput: AccessOrInput{
					Access: &rt.Raw{
						Type: rt.NewUnversionedType("test-access"),
						Data: []byte(`{"test": "value"}`),
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ConvertFromV1Source(tt.source)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestConvertToRuntimeReference(t *testing.T) {
	tests := []struct {
		name      string
		reference *v1.Reference
		want      Reference
	}{
		{
			name:      "nil reference",
			reference: nil,
			want:      Reference{},
		},
		{
			name: "basic reference",
			reference: &v1.Reference{
				ElementMeta: v1.ElementMeta{
					ObjectMeta: v1.ObjectMeta{
						Name:    "test",
						Version: "1.0.0",
						Labels: []v1.Label{
							{Name: "test", Value: []byte("value"), Signing: true},
						},
					},
				},
				Component: "test-component",
			},
			want: Reference{
				ElementMeta: ElementMeta{
					ObjectMeta: ObjectMeta{
						Name:    "test",
						Version: "1.0.0",
						Labels: []Label{
							{Name: "test", Value: []byte("value"), Signing: true},
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
		want      Component
	}{
		{
			name:      "nil component",
			component: nil,
			want:      Component{},
		},
		{
			name: "basic component",
			component: &v1.Component{
				ComponentMeta: v1.ComponentMeta{
					ObjectMeta: v1.ObjectMeta{
						Name:    "test",
						Version: "1.0.0",
						Labels: []v1.Label{
							{Name: "test", Value: []byte("value"), Signing: true},
						},
					},
					CreationTime: "2024-01-01T00:00:00Z",
				},
				Provider: v1.Provider{
					Name: "test-provider",
					Labels: []v1.Label{
						{Name: "test", Value: []byte("value")},
					},
				},
			},
			want: Component{
				ComponentMeta: ComponentMeta{
					ObjectMeta: ObjectMeta{
						Name:    "test",
						Version: "1.0.0",
						Labels: []Label{
							{Name: "test", Value: []byte("value"), Signing: true},
						},
					},
					CreationTime: "2024-01-01T00:00:00Z",
				},
				Provider: Provider{
					Name: "test-provider",
					Labels: []Label{
						{Name: "test", Value: []byte("value")},
					},
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
									Value:   []byte("provider-value"),
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
									Value:   []byte("provider-value"),
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

func TestConvertToV1Component(t *testing.T) {
	tests := []struct {
		name      string
		component *Component
		want      *v1.Component
		wantErr   bool
	}{
		{
			name:      "nil component",
			component: nil,
			want:      nil,
			wantErr:   false,
		},
		{
			name: "basic component",
			component: &Component{
				ComponentMeta: ComponentMeta{
					ObjectMeta: ObjectMeta{
						Name:    "test",
						Version: "1.0.0",
						Labels: []Label{
							{Name: "test", Value: []byte("value"), Signing: true},
						},
					},
					CreationTime: "2024-01-01T00:00:00Z",
				},
				Provider: Provider{
					Name: "test-provider",
					Labels: []Label{
						{Name: "test", Value: []byte("value")},
					},
				},
			},
			want: &v1.Component{
				ComponentMeta: v1.ComponentMeta{
					ObjectMeta: v1.ObjectMeta{
						Name:    "test",
						Version: "1.0.0",
						Labels: []v1.Label{
							{Name: "test", Value: []byte("value"), Signing: true},
						},
					},
					CreationTime: "2024-01-01T00:00:00Z",
				},
				Provider: v1.Provider{
					Name: "test-provider",
					Labels: []v1.Label{
						{Name: "test", Value: []byte("value")},
					},
				},
			},
			wantErr: false,
		},
		{
			name: "component with resources and sources",
			component: &Component{
				ComponentMeta: ComponentMeta{
					ObjectMeta: ObjectMeta{
						Name:    "test",
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
						AccessOrInput: AccessOrInput{
							Access: &rt.Raw{
								Type: rt.NewUnversionedType("test-access"),
								Data: []byte(`{"test": "value"}`),
							},
						},
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
						AccessOrInput: AccessOrInput{
							Access: &rt.Raw{
								Type: rt.NewUnversionedType("test-access"),
								Data: []byte(`{"test": "value"}`),
							},
						},
					},
				},
			},
			want: &v1.Component{
				ComponentMeta: v1.ComponentMeta{
					ObjectMeta: v1.ObjectMeta{
						Name:    "test",
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
						AccessOrInput: v1.AccessOrInput{
							Access: &rt.Raw{
								Type: rt.NewUnversionedType("test-access"),
								Data: []byte(`{"test": "value"}`),
							},
						},
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
						AccessOrInput: v1.AccessOrInput{
							Access: &rt.Raw{
								Type: rt.NewUnversionedType("test-access"),
								Data: []byte(`{"test": "value"}`),
							},
						},
					},
				},
			},
			wantErr: false,
		},
		{
			name: "component with references",
			component: &Component{
				ComponentMeta: ComponentMeta{
					ObjectMeta: ObjectMeta{
						Name:    "test",
						Version: "1.0.0",
					},
				},
				Provider: Provider{
					Name: "test-provider",
				},
				References: []Reference{
					{
						ElementMeta: ElementMeta{
							ObjectMeta: ObjectMeta{
								Name:    "test-ref",
								Version: "1.0.0",
								Labels: []Label{
									{Name: "ref-label", Value: []byte("ref-value"), Signing: true},
								},
							},
							ExtraIdentity: rt.Identity{
								"namespace": "test-namespace",
							},
						},
						Component: "referenced-component",
					},
				},
			},
			want: &v1.Component{
				ComponentMeta: v1.ComponentMeta{
					ObjectMeta: v1.ObjectMeta{
						Name:    "test",
						Version: "1.0.0",
					},
				},
				Provider: v1.Provider{
					Name: "test-provider",
				},
				References: []v1.Reference{
					{
						ElementMeta: v1.ElementMeta{
							ObjectMeta: v1.ObjectMeta{
								Name:    "test-ref",
								Version: "1.0.0",
								Labels: []v1.Label{
									{Name: "ref-label", Value: []byte("ref-value"), Signing: true},
								},
							},
							ExtraIdentity: rt.Identity{
								"namespace": "test-namespace",
							},
						},
						Component: "referenced-component",
					},
				},
			},
			wantErr: false,
		},
		{
			name: "component with input resources",
			component: &Component{
				ComponentMeta: ComponentMeta{
					ObjectMeta: ObjectMeta{
						Name:    "test",
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
						AccessOrInput: AccessOrInput{
							Input: &rt.Raw{
								Type: rt.NewUnversionedType("test-input"),
								Data: []byte(`{"input": "value"}`),
							},
						},
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
						AccessOrInput: AccessOrInput{
							Input: &rt.Raw{
								Type: rt.NewUnversionedType("test-input"),
								Data: []byte(`{"input": "value"}`),
							},
						},
					},
				},
			},
			want: &v1.Component{
				ComponentMeta: v1.ComponentMeta{
					ObjectMeta: v1.ObjectMeta{
						Name:    "test",
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
						AccessOrInput: v1.AccessOrInput{
							Input: &rt.Raw{
								Type: rt.NewUnversionedType("test-input"),
								Data: []byte(`{"input": "value"}`),
							},
						},
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
						AccessOrInput: v1.AccessOrInput{
							Input: &rt.Raw{
								Type: rt.NewUnversionedType("test-input"),
								Data: []byte(`{"input": "value"}`),
							},
						},
					},
				},
			},
			wantErr: false,
		},
		{
			name: "component with mixed access and input",
			component: &Component{
				ComponentMeta: ComponentMeta{
					ObjectMeta: ObjectMeta{
						Name:    "test",
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
								Name:    "test-resource-access",
								Version: "1.0.0",
							},
						},
						Type:     "blob",
						Relation: LocalRelation,
						AccessOrInput: AccessOrInput{
							Access: &rt.Raw{
								Type: rt.NewUnversionedType("test-access"),
								Data: []byte(`{"access": "value"}`),
							},
						},
					},
					{
						ElementMeta: ElementMeta{
							ObjectMeta: ObjectMeta{
								Name:    "test-resource-input",
								Version: "1.0.0",
							},
						},
						Type:     "blob",
						Relation: LocalRelation,
						AccessOrInput: AccessOrInput{
							Input: &rt.Raw{
								Type: rt.NewUnversionedType("test-input"),
								Data: []byte(`{"input": "value"}`),
							},
						},
					},
				},
			},
			want: &v1.Component{
				ComponentMeta: v1.ComponentMeta{
					ObjectMeta: v1.ObjectMeta{
						Name:    "test",
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
								Name:    "test-resource-access",
								Version: "1.0.0",
							},
						},
						Type:     "blob",
						Relation: v1.LocalRelation,
						AccessOrInput: v1.AccessOrInput{
							Access: &rt.Raw{
								Type: rt.NewUnversionedType("test-access"),
								Data: []byte(`{"access": "value"}`),
							},
						},
					},
					{
						ElementMeta: v1.ElementMeta{
							ObjectMeta: v1.ObjectMeta{
								Name:    "test-resource-input",
								Version: "1.0.0",
							},
						},
						Type:     "blob",
						Relation: v1.LocalRelation,
						AccessOrInput: v1.AccessOrInput{
							Input: &rt.Raw{
								Type: rt.NewUnversionedType("test-input"),
								Data: []byte(`{"input": "value"}`),
							},
						},
					},
				},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ConvertToV1Component(tt.component)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestConvertToV1Reference(t *testing.T) {
	tests := []struct {
		name      string
		reference *Reference
		want      *v1.Reference
		wantErr   bool
	}{
		{
			name:      "nil reference",
			reference: nil,
			want:      nil,
			wantErr:   false,
		},
		{
			name: "basic reference",
			reference: &Reference{
				ElementMeta: ElementMeta{
					ObjectMeta: ObjectMeta{
						Name:    "test",
						Version: "1.0.0",
						Labels: []Label{
							{Name: "test", Value: []byte("value"), Signing: true},
						},
					},
				},
				Component: "test-component",
			},
			want: &v1.Reference{
				ElementMeta: v1.ElementMeta{
					ObjectMeta: v1.ObjectMeta{
						Name:    "test",
						Version: "1.0.0",
						Labels: []v1.Label{
							{Name: "test", Value: []byte("value"), Signing: true},
						},
					},
				},
				Component: "test-component",
			},
			wantErr: false,
		},
		{
			name: "reference with extra identity",
			reference: &Reference{
				ElementMeta: ElementMeta{
					ObjectMeta: ObjectMeta{
						Name:    "test",
						Version: "1.0.0",
					},
					ExtraIdentity: rt.Identity{
						"namespace": "test-namespace",
					},
				},
				Component: "test-component",
			},
			want: &v1.Reference{
				ElementMeta: v1.ElementMeta{
					ObjectMeta: v1.ObjectMeta{
						Name:    "test",
						Version: "1.0.0",
					},
					ExtraIdentity: rt.Identity{
						"namespace": "test-namespace",
					},
				},
				Component: "test-component",
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ConvertToV1Reference(tt.reference)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestConvertToV1Resource(t *testing.T) {
	tests := []struct {
		name     string
		resource *Resource
		want     *v1.Resource
		wantErr  bool
	}{
		{
			name:     "nil resource",
			resource: nil,
			want:     nil,
			wantErr:  false,
		},
		{
			name: "basic resource",
			resource: &Resource{
				ElementMeta: ElementMeta{
					ObjectMeta: ObjectMeta{
						Name:    "test",
						Version: "1.0.0",
						Labels: []Label{
							{Name: "test", Value: []byte("value"), Signing: true},
						},
					},
				},
				Type:     "test-type",
				Relation: LocalRelation,
			},
			want: &v1.Resource{
				ElementMeta: v1.ElementMeta{
					ObjectMeta: v1.ObjectMeta{
						Name:    "test",
						Version: "1.0.0",
						Labels: []v1.Label{
							{Name: "test", Value: []byte("value"), Signing: true},
						},
					},
				},
				Type:     "test-type",
				Relation: v1.LocalRelation,
			},
			wantErr: false,
		},
		{
			name: "resource with source refs",
			resource: &Resource{
				ElementMeta: ElementMeta{
					ObjectMeta: ObjectMeta{
						Name:    "test",
						Version: "1.0.0",
					},
				},
				Type:     "test-type",
				Relation: LocalRelation,
				SourceRefs: []SourceRef{
					{
						IdentitySelector: map[string]string{
							"name": "test-source",
						},
						Labels: []Label{
							{
								Name:    "source-label",
								Value:   []byte("source-value"),
								Signing: true,
							},
						},
					},
				},
			},
			want: &v1.Resource{
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
						IdentitySelector: map[string]string{
							"name": "test-source",
						},
						Labels: []v1.Label{
							{
								Name:    "source-label",
								Value:   []byte("source-value"),
								Signing: true,
							},
						},
					},
				},
			},
			wantErr: false,
		},
		{
			name: "resource with multiple source refs",
			resource: &Resource{
				ElementMeta: ElementMeta{
					ObjectMeta: ObjectMeta{
						Name:    "test",
						Version: "1.0.0",
					},
				},
				Type:     "test-type",
				Relation: LocalRelation,
				SourceRefs: []SourceRef{
					{
						IdentitySelector: map[string]string{
							"name": "source1",
						},
						Labels: []Label{
							{
								Name:    "label1",
								Value:   []byte("value1"),
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
								Value:   []byte("value2"),
								Signing: false,
							},
						},
					},
				},
			},
			want: &v1.Resource{
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
						IdentitySelector: map[string]string{
							"name": "source1",
						},
						Labels: []v1.Label{
							{
								Name:    "label1",
								Value:   []byte("value1"),
								Signing: true,
							},
						},
					},
					{
						IdentitySelector: map[string]string{
							"name": "source2",
						},
						Labels: []v1.Label{
							{
								Name:    "label2",
								Value:   []byte("value2"),
								Signing: false,
							},
						},
					},
				},
			},
			wantErr: false,
		},
		{
			name: "resource with source refs and access",
			resource: &Resource{
				ElementMeta: ElementMeta{
					ObjectMeta: ObjectMeta{
						Name:    "test",
						Version: "1.0.0",
					},
				},
				Type:     "test-type",
				Relation: LocalRelation,
				SourceRefs: []SourceRef{
					{
						IdentitySelector: map[string]string{
							"name": "test-source",
						},
					},
				},
				AccessOrInput: AccessOrInput{
					Access: &rt.Raw{
						Type: rt.NewUnversionedType("test-access"),
						Data: []byte(`{"test": "value"}`),
					},
				},
			},
			want: &v1.Resource{
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
						IdentitySelector: map[string]string{
							"name": "test-source",
						},
					},
				},
				AccessOrInput: v1.AccessOrInput{
					Access: &rt.Raw{
						Type: rt.NewUnversionedType("test-access"),
						Data: []byte(`{"test": "value"}`),
					},
				},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ConvertToV1Resource(tt.resource)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestConvertFromV1Resource(t *testing.T) {
	tests := []struct {
		name     string
		resource *v1.Resource
		want     Resource
	}{
		{
			name:     "nil resource",
			resource: nil,
			want:     Resource{},
		},
		{
			name: "basic resource",
			resource: &v1.Resource{
				ElementMeta: v1.ElementMeta{
					ObjectMeta: v1.ObjectMeta{
						Name:    "test",
						Version: "1.0.0",
						Labels: []v1.Label{
							{Name: "test", Value: []byte("value"), Signing: true},
						},
					},
				},
				Type:     "test-type",
				Relation: v1.LocalRelation,
			},
			want: Resource{
				ElementMeta: ElementMeta{
					ObjectMeta: ObjectMeta{
						Name:    "test",
						Version: "1.0.0",
						Labels: []Label{
							{Name: "test", Value: []byte("value"), Signing: true},
						},
					},
				},
				Type:     "test-type",
				Relation: LocalRelation,
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
						IdentitySelector: map[string]string{
							"name": "test-source",
						},
						Labels: []v1.Label{
							{
								Name:    "source-label",
								Value:   []byte("source-value"),
								Signing: true,
							},
						},
					},
				},
			},
			want: Resource{
				ElementMeta: ElementMeta{
					ObjectMeta: ObjectMeta{
						Name:    "test",
						Version: "1.0.0",
					},
				},
				Type:     "test-type",
				Relation: LocalRelation,
				SourceRefs: []SourceRef{
					{
						IdentitySelector: map[string]string{
							"name": "test-source",
						},
						Labels: []Label{
							{
								Name:    "source-label",
								Value:   []byte("source-value"),
								Signing: true,
							},
						},
					},
				},
			},
		},
		{
			name: "resource with multiple source refs",
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
						IdentitySelector: map[string]string{
							"name": "source1",
						},
						Labels: []v1.Label{
							{
								Name:    "label1",
								Value:   []byte("value1"),
								Signing: true,
							},
						},
					},
					{
						IdentitySelector: map[string]string{
							"name": "source2",
						},
						Labels: []v1.Label{
							{
								Name:    "label2",
								Value:   []byte("value2"),
								Signing: false,
							},
						},
					},
				},
			},
			want: Resource{
				ElementMeta: ElementMeta{
					ObjectMeta: ObjectMeta{
						Name:    "test",
						Version: "1.0.0",
					},
				},
				Type:     "test-type",
				Relation: LocalRelation,
				SourceRefs: []SourceRef{
					{
						IdentitySelector: map[string]string{
							"name": "source1",
						},
						Labels: []Label{
							{
								Name:    "label1",
								Value:   []byte("value1"),
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
								Value:   []byte("value2"),
								Signing: false,
							},
						},
					},
				},
			},
		},
		{
			name: "resource with source refs and access",
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
						IdentitySelector: map[string]string{
							"name": "test-source",
						},
					},
				},
				AccessOrInput: v1.AccessOrInput{
					Access: &rt.Raw{
						Type: rt.NewUnversionedType("test-access"),
						Data: []byte(`{"test": "value"}`),
					},
				},
			},
			want: Resource{
				ElementMeta: ElementMeta{
					ObjectMeta: ObjectMeta{
						Name:    "test",
						Version: "1.0.0",
					},
				},
				Type:     "test-type",
				Relation: LocalRelation,
				SourceRefs: []SourceRef{
					{
						IdentitySelector: map[string]string{
							"name": "test-source",
						},
					},
				},
				AccessOrInput: AccessOrInput{
					Access: &rt.Raw{
						Type: rt.NewUnversionedType("test-access"),
						Data: []byte(`{"test": "value"}`),
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ConvertFromV1Resource(tt.resource)
			assert.Equal(t, tt.want, got)
		})
	}
}
