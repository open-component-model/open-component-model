package spec

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
