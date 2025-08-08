package spec

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLayerInfo_GetProperties(t *testing.T) {
	tests := []struct {
		name     string
		layer    LayerInfo
		expected map[string]string
	}{
		{
			name: "basic layer properties",
			layer: LayerInfo{
				Index:     0,
				MediaType: "application/vnd.docker.image.rootfs.diff.tar.gzip",
			},
			expected: map[string]string{
				LayerIndexKey:     "0",
				LayerMediaTypeKey: "application/vnd.docker.image.rootfs.diff.tar.gzip",
			},
		},
		{
			name: "layer with different index",
			layer: LayerInfo{
				Index:     5,
				MediaType: "application/vnd.oci.image.layer.v1.tar+gzip",
			},
			expected: map[string]string{
				LayerIndexKey:     "5",
				LayerMediaTypeKey: "application/vnd.oci.image.layer.v1.tar+gzip",
			},
		},
		{
			name: "layer with annotations",
			layer: LayerInfo{
				Index:     1,
				MediaType: "application/vnd.oci.image.layer.v1.tar+gzip",
				Annotations: map[string]string{
					"custom.annotation": "custom-value",
					"another.key":       "another-value",
				},
			},
			expected: map[string]string{
				LayerIndexKey:       "1",
				LayerMediaTypeKey:   "application/vnd.oci.image.layer.v1.tar+gzip",
				"custom.annotation": "custom-value",
				"another.key":       "another-value",
			},
		},
		{
			name: "layer with nil annotations",
			layer: LayerInfo{
				Index:       2,
				MediaType:   "application/vnd.docker.image.rootfs.diff.tar.gzip",
				Annotations: nil,
			},
			expected: map[string]string{
				LayerIndexKey:     "2",
				LayerMediaTypeKey: "application/vnd.docker.image.rootfs.diff.tar.gzip",
			},
		},
		{
			name: "layer with empty annotations",
			layer: LayerInfo{
				Index:       3,
				MediaType:   "application/vnd.oci.image.layer.v1.tar+gzip",
				Annotations: map[string]string{},
			},
			expected: map[string]string{
				LayerIndexKey:     "3",
				LayerMediaTypeKey: "application/vnd.oci.image.layer.v1.tar+gzip",
			},
		},
		{
			name: "annotation overrides predefined key",
			layer: LayerInfo{
				Index:     0,
				MediaType: "application/vnd.docker.image.rootfs.diff.tar.gzip",
				Annotations: map[string]string{
					LayerIndexKey: "overridden-index",
				},
			},
			expected: map[string]string{
				LayerIndexKey:     "overridden-index", // annotation takes precedence
				LayerMediaTypeKey: "application/vnd.docker.image.rootfs.diff.tar.gzip",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.layer.GetProperties()
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestLayerSelector_Matches_NilSelector(t *testing.T) {
	var selector *LayerSelector
	layer := LayerInfo{
		Index:     0,
		MediaType: "application/vnd.docker.image.rootfs.diff.tar.gzip",
	}

	result := selector.Matches(layer)
	assert.True(t, result, "nil selector should match everything")
}

func TestLayerSelector_Matches_MatchLabels(t *testing.T) {
	tests := []struct {
		name     string
		selector *LayerSelector
		layer    LayerInfo
		expected bool
	}{
		{
			name: "match by index",
			selector: &LayerSelector{
				MatchProperties: map[string]string{
					LayerIndexKey: "0",
				},
			},
			layer: LayerInfo{
				Index:     0,
				MediaType: "application/vnd.docker.image.rootfs.diff.tar.gzip",
			},
			expected: true,
		},
		{
			name: "match by media type",
			selector: &LayerSelector{
				MatchProperties: map[string]string{
					LayerMediaTypeKey: "application/vnd.docker.image.rootfs.diff.tar.gzip",
				},
			},
			layer: LayerInfo{
				Index:     0,
				MediaType: "application/vnd.docker.image.rootfs.diff.tar.gzip",
			},
			expected: true,
		},
		{
			name: "match multiple labels",
			selector: &LayerSelector{
				MatchProperties: map[string]string{
					LayerIndexKey:     "1",
					LayerMediaTypeKey: "application/vnd.oci.image.layer.v1.tar+gzip",
				},
			},
			layer: LayerInfo{
				Index:     1,
				MediaType: "application/vnd.oci.image.layer.v1.tar+gzip",
			},
			expected: true,
		},
		{
			name: "no match - wrong index",
			selector: &LayerSelector{
				MatchProperties: map[string]string{
					LayerIndexKey: "5",
				},
			},
			layer: LayerInfo{
				Index:     0,
				MediaType: "application/vnd.docker.image.rootfs.diff.tar.gzip",
			},
			expected: false,
		},
		{
			name: "no match - wrong media type",
			selector: &LayerSelector{
				MatchProperties: map[string]string{
					LayerMediaTypeKey: "application/vnd.oci.image.layer.v1.tar+gzip",
				},
			},
			layer: LayerInfo{
				Index:     0,
				MediaType: "application/vnd.docker.image.rootfs.diff.tar.gzip",
			},
			expected: false,
		},
		{
			name: "no match - partial match not sufficient",
			selector: &LayerSelector{
				MatchProperties: map[string]string{
					LayerIndexKey:     "0",
					LayerMediaTypeKey: "wrong-media-type",
				},
			},
			layer: LayerInfo{
				Index:     0,
				MediaType: "application/vnd.docker.image.rootfs.diff.tar.gzip",
			},
			expected: false,
		},
		{
			name: "empty match labels matches everything",
			selector: &LayerSelector{
				MatchProperties: map[string]string{},
			},
			layer: LayerInfo{
				Index:     0,
				MediaType: "application/vnd.docker.image.rootfs.diff.tar.gzip",
			},
			expected: true,
		},
		{
			name: "match by annotation",
			selector: &LayerSelector{
				MatchProperties: map[string]string{
					"custom.annotation": "custom-value",
				},
			},
			layer: LayerInfo{
				Index:     0,
				MediaType: "application/vnd.docker.image.rootfs.diff.tar.gzip",
				Annotations: map[string]string{
					"custom.annotation": "custom-value",
				},
			},
			expected: true,
		},
		{
			name: "no match - annotation value differs",
			selector: &LayerSelector{
				MatchProperties: map[string]string{
					"custom.annotation": "expected-value",
				},
			},
			layer: LayerInfo{
				Index:     0,
				MediaType: "application/vnd.docker.image.rootfs.diff.tar.gzip",
				Annotations: map[string]string{
					"custom.annotation": "actual-value",
				},
			},
			expected: false,
		},
		{
			name: "no match - annotation key missing",
			selector: &LayerSelector{
				MatchProperties: map[string]string{
					"missing.annotation": "some-value",
				},
			},
			layer: LayerInfo{
				Index:     0,
				MediaType: "application/vnd.docker.image.rootfs.diff.tar.gzip",
				Annotations: map[string]string{
					"different.annotation": "some-value",
				},
			},
			expected: false,
		},
		{
			name: "match mixed predefined and annotation properties",
			selector: &LayerSelector{
				MatchProperties: map[string]string{
					LayerIndexKey:       "1",
					"custom.annotation": "custom-value",
				},
			},
			layer: LayerInfo{
				Index:     1,
				MediaType: "application/vnd.docker.image.rootfs.diff.tar.gzip",
				Annotations: map[string]string{
					"custom.annotation": "custom-value",
				},
			},
			expected: true,
		},
		{
			name: "no match - one annotation matches but predefined property fails",
			selector: &LayerSelector{
				MatchProperties: map[string]string{
					LayerIndexKey:       "2",
					"custom.annotation": "custom-value",
				},
			},
			layer: LayerInfo{
				Index:     1, // wrong index
				MediaType: "application/vnd.docker.image.rootfs.diff.tar.gzip",
				Annotations: map[string]string{
					"custom.annotation": "custom-value",
				},
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.selector.Matches(tt.layer)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestLayerSelector_Matches_MatchExpressions(t *testing.T) {
	tests := []struct {
		name     string
		selector *LayerSelector
		layer    LayerInfo
		expected bool
	}{
		{
			name: "In operator - single value match",
			selector: &LayerSelector{
				MatchExpressions: []LayerSelectorRequirement{
					{
						Key:      LayerIndexKey,
						Operator: LayerSelectorOpIn,
						Values:   []string{"0"},
					},
				},
			},
			layer: LayerInfo{
				Index:     0,
				MediaType: "application/vnd.docker.image.rootfs.diff.tar.gzip",
			},
			expected: true,
		},
		{
			name: "In operator - multiple values match",
			selector: &LayerSelector{
				MatchExpressions: []LayerSelectorRequirement{
					{
						Key:      LayerIndexKey,
						Operator: LayerSelectorOpIn,
						Values:   []string{"0", "1", "2"},
					},
				},
			},
			layer: LayerInfo{
				Index:     1,
				MediaType: "application/vnd.docker.image.rootfs.diff.tar.gzip",
			},
			expected: true,
		},
		{
			name: "In operator - no match",
			selector: &LayerSelector{
				MatchExpressions: []LayerSelectorRequirement{
					{
						Key:      LayerIndexKey,
						Operator: LayerSelectorOpIn,
						Values:   []string{"5", "6"},
					},
				},
			},
			layer: LayerInfo{
				Index:     0,
				MediaType: "application/vnd.docker.image.rootfs.diff.tar.gzip",
			},
			expected: false,
		},
		{
			name: "NotIn operator - match",
			selector: &LayerSelector{
				MatchExpressions: []LayerSelectorRequirement{
					{
						Key:      LayerIndexKey,
						Operator: LayerSelectorOpNotIn,
						Values:   []string{"5", "6"},
					},
				},
			},
			layer: LayerInfo{
				Index:     0,
				MediaType: "application/vnd.docker.image.rootfs.diff.tar.gzip",
			},
			expected: true,
		},
		{
			name: "NotIn operator - no match",
			selector: &LayerSelector{
				MatchExpressions: []LayerSelectorRequirement{
					{
						Key:      LayerIndexKey,
						Operator: LayerSelectorOpNotIn,
						Values:   []string{"0", "1"},
					},
				},
			},
			layer: LayerInfo{
				Index:     0,
				MediaType: "application/vnd.docker.image.rootfs.diff.tar.gzip",
			},
			expected: false,
		},
		{
			name: "NotIn operator - key does not exist",
			selector: &LayerSelector{
				MatchExpressions: []LayerSelectorRequirement{
					{
						Key:      "nonexistent.key",
						Operator: LayerSelectorOpNotIn,
						Values:   []string{"value"},
					},
				},
			},
			layer: LayerInfo{
				Index:     0,
				MediaType: "application/vnd.docker.image.rootfs.diff.tar.gzip",
			},
			expected: true,
		},
		{
			name: "Exists operator - key exists",
			selector: &LayerSelector{
				MatchExpressions: []LayerSelectorRequirement{
					{
						Key:      LayerIndexKey,
						Operator: LayerSelectorOpExists,
					},
				},
			},
			layer: LayerInfo{
				Index:     0,
				MediaType: "application/vnd.docker.image.rootfs.diff.tar.gzip",
			},
			expected: true,
		},
		{
			name: "Exists operator - key does not exist",
			selector: &LayerSelector{
				MatchExpressions: []LayerSelectorRequirement{
					{
						Key:      "nonexistent.key",
						Operator: LayerSelectorOpExists,
					},
				},
			},
			layer: LayerInfo{
				Index:     0,
				MediaType: "application/vnd.docker.image.rootfs.diff.tar.gzip",
			},
			expected: false,
		},
		{
			name: "DoesNotExist operator - key does not exist",
			selector: &LayerSelector{
				MatchExpressions: []LayerSelectorRequirement{
					{
						Key:      "nonexistent.key",
						Operator: LayerSelectorOpDoesNotExist,
					},
				},
			},
			layer: LayerInfo{
				Index:     0,
				MediaType: "application/vnd.docker.image.rootfs.diff.tar.gzip",
			},
			expected: true,
		},
		{
			name: "DoesNotExist operator - key exists",
			selector: &LayerSelector{
				MatchExpressions: []LayerSelectorRequirement{
					{
						Key:      LayerIndexKey,
						Operator: LayerSelectorOpDoesNotExist,
					},
				},
			},
			layer: LayerInfo{
				Index:     0,
				MediaType: "application/vnd.docker.image.rootfs.diff.tar.gzip",
			},
			expected: false,
		},
		{
			name: "multiple expressions - all match",
			selector: &LayerSelector{
				MatchExpressions: []LayerSelectorRequirement{
					{
						Key:      LayerIndexKey,
						Operator: LayerSelectorOpIn,
						Values:   []string{"0", "1"},
					},
					{
						Key:      LayerMediaTypeKey,
						Operator: LayerSelectorOpExists,
					},
				},
			},
			layer: LayerInfo{
				Index:     0,
				MediaType: "application/vnd.docker.image.rootfs.diff.tar.gzip",
			},
			expected: true,
		},
		{
			name: "multiple expressions - one fails",
			selector: &LayerSelector{
				MatchExpressions: []LayerSelectorRequirement{
					{
						Key:      LayerIndexKey,
						Operator: LayerSelectorOpIn,
						Values:   []string{"0", "1"},
					},
					{
						Key:      "nonexistent.key",
						Operator: LayerSelectorOpExists,
					},
				},
			},
			layer: LayerInfo{
				Index:     0,
				MediaType: "application/vnd.docker.image.rootfs.diff.tar.gzip",
			},
			expected: false,
		},
		{
			name: "unknown operator",
			selector: &LayerSelector{
				MatchExpressions: []LayerSelectorRequirement{
					{
						Key:      LayerIndexKey,
						Operator: "UnknownOp",
						Values:   []string{"0"},
					},
				},
			},
			layer: LayerInfo{
				Index:     0,
				MediaType: "application/vnd.docker.image.rootfs.diff.tar.gzip",
			},
			expected: false,
		},
		{
			name: "In operator - annotation match",
			selector: &LayerSelector{
				MatchExpressions: []LayerSelectorRequirement{
					{
						Key:      "custom.annotation",
						Operator: LayerSelectorOpIn,
						Values:   []string{"value1", "value2", "value3"},
					},
				},
			},
			layer: LayerInfo{
				Index:     0,
				MediaType: "application/vnd.docker.image.rootfs.diff.tar.gzip",
				Annotations: map[string]string{
					"custom.annotation": "value2",
				},
			},
			expected: true,
		},
		{
			name: "In operator - annotation no match",
			selector: &LayerSelector{
				MatchExpressions: []LayerSelectorRequirement{
					{
						Key:      "custom.annotation",
						Operator: LayerSelectorOpIn,
						Values:   []string{"value1", "value2"},
					},
				},
			},
			layer: LayerInfo{
				Index:     0,
				MediaType: "application/vnd.docker.image.rootfs.diff.tar.gzip",
				Annotations: map[string]string{
					"custom.annotation": "value3",
				},
			},
			expected: false,
		},
		{
			name: "NotIn operator - annotation match",
			selector: &LayerSelector{
				MatchExpressions: []LayerSelectorRequirement{
					{
						Key:      "custom.annotation",
						Operator: LayerSelectorOpNotIn,
						Values:   []string{"unwanted1", "unwanted2"},
					},
				},
			},
			layer: LayerInfo{
				Index:     0,
				MediaType: "application/vnd.docker.image.rootfs.diff.tar.gzip",
				Annotations: map[string]string{
					"custom.annotation": "allowed-value",
				},
			},
			expected: true,
		},
		{
			name: "NotIn operator - annotation no match",
			selector: &LayerSelector{
				MatchExpressions: []LayerSelectorRequirement{
					{
						Key:      "custom.annotation",
						Operator: LayerSelectorOpNotIn,
						Values:   []string{"unwanted1", "unwanted2"},
					},
				},
			},
			layer: LayerInfo{
				Index:     0,
				MediaType: "application/vnd.docker.image.rootfs.diff.tar.gzip",
				Annotations: map[string]string{
					"custom.annotation": "unwanted1",
				},
			},
			expected: false,
		},
		{
			name: "Exists operator - annotation exists",
			selector: &LayerSelector{
				MatchExpressions: []LayerSelectorRequirement{
					{
						Key:      "custom.annotation",
						Operator: LayerSelectorOpExists,
					},
				},
			},
			layer: LayerInfo{
				Index:     0,
				MediaType: "application/vnd.docker.image.rootfs.diff.tar.gzip",
				Annotations: map[string]string{
					"custom.annotation": "any-value",
				},
			},
			expected: true,
		},
		{
			name: "Exists operator - annotation does not exist",
			selector: &LayerSelector{
				MatchExpressions: []LayerSelectorRequirement{
					{
						Key:      "missing.annotation",
						Operator: LayerSelectorOpExists,
					},
				},
			},
			layer: LayerInfo{
				Index:     0,
				MediaType: "application/vnd.docker.image.rootfs.diff.tar.gzip",
				Annotations: map[string]string{
					"different.annotation": "some-value",
				},
			},
			expected: false,
		},
		{
			name: "DoesNotExist operator - annotation does not exist",
			selector: &LayerSelector{
				MatchExpressions: []LayerSelectorRequirement{
					{
						Key:      "unwanted.annotation",
						Operator: LayerSelectorOpDoesNotExist,
					},
				},
			},
			layer: LayerInfo{
				Index:     0,
				MediaType: "application/vnd.docker.image.rootfs.diff.tar.gzip",
				Annotations: map[string]string{
					"allowed.annotation": "some-value",
				},
			},
			expected: true,
		},
		{
			name: "DoesNotExist operator - annotation exists",
			selector: &LayerSelector{
				MatchExpressions: []LayerSelectorRequirement{
					{
						Key:      "unwanted.annotation",
						Operator: LayerSelectorOpDoesNotExist,
					},
				},
			},
			layer: LayerInfo{
				Index:     0,
				MediaType: "application/vnd.docker.image.rootfs.diff.tar.gzip",
				Annotations: map[string]string{
					"unwanted.annotation": "some-value",
				},
			},
			expected: false,
		},
		{
			name: "mixed expressions - predefined and annotation",
			selector: &LayerSelector{
				MatchExpressions: []LayerSelectorRequirement{
					{
						Key:      LayerIndexKey,
						Operator: LayerSelectorOpIn,
						Values:   []string{"0", "1"},
					},
					{
						Key:      "custom.annotation",
						Operator: LayerSelectorOpExists,
					},
				},
			},
			layer: LayerInfo{
				Index:     0,
				MediaType: "application/vnd.docker.image.rootfs.diff.tar.gzip",
				Annotations: map[string]string{
					"custom.annotation": "some-value",
				},
			},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.selector.Matches(tt.layer)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestLayerSelector_Matches_CombinedLabelsAndExpressions(t *testing.T) {
	tests := []struct {
		name     string
		selector *LayerSelector
		layer    LayerInfo
		expected bool
	}{
		{
			name: "both match labels and expressions match",
			selector: &LayerSelector{
				MatchProperties: map[string]string{
					LayerIndexKey: "0",
				},
				MatchExpressions: []LayerSelectorRequirement{
					{
						Key:      LayerMediaTypeKey,
						Operator: LayerSelectorOpExists,
					},
				},
			},
			layer: LayerInfo{
				Index:     0,
				MediaType: "application/vnd.docker.image.rootfs.diff.tar.gzip",
			},
			expected: true,
		},
		{
			name: "match labels succeed but expressions fail",
			selector: &LayerSelector{
				MatchProperties: map[string]string{
					LayerIndexKey: "0",
				},
				MatchExpressions: []LayerSelectorRequirement{
					{
						Key:      "nonexistent.key",
						Operator: LayerSelectorOpExists,
					},
				},
			},
			layer: LayerInfo{
				Index:     0,
				MediaType: "application/vnd.docker.image.rootfs.diff.tar.gzip",
			},
			expected: false,
		},
		{
			name: "match labels fail but expressions succeed",
			selector: &LayerSelector{
				MatchProperties: map[string]string{
					LayerIndexKey: "5",
				},
				MatchExpressions: []LayerSelectorRequirement{
					{
						Key:      LayerMediaTypeKey,
						Operator: LayerSelectorOpExists,
					},
				},
			},
			layer: LayerInfo{
				Index:     0,
				MediaType: "application/vnd.docker.image.rootfs.diff.tar.gzip",
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.selector.Matches(tt.layer)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestLayerSelectorOperators(t *testing.T) {
	require.Equal(t, LayerSelectorOperator("In"), LayerSelectorOpIn)
	require.Equal(t, LayerSelectorOperator("NotIn"), LayerSelectorOpNotIn)
	require.Equal(t, LayerSelectorOperator("Exists"), LayerSelectorOpExists)
	require.Equal(t, LayerSelectorOperator("DoesNotExist"), LayerSelectorOpDoesNotExist)
}

func TestPredefinedKeys(t *testing.T) {
	require.Equal(t, "layer.index", LayerIndexKey)
	require.Equal(t, "layer.mediaType", LayerMediaTypeKey)
}
