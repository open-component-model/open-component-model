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
				MatchLabels: map[string]string{
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
				MatchLabels: map[string]string{
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
				MatchLabels: map[string]string{
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
				MatchLabels: map[string]string{
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
				MatchLabels: map[string]string{
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
				MatchLabels: map[string]string{
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
				MatchLabels: map[string]string{},
			},
			layer: LayerInfo{
				Index:     0,
				MediaType: "application/vnd.docker.image.rootfs.diff.tar.gzip",
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
				MatchLabels: map[string]string{
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
				MatchLabels: map[string]string{
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
				MatchLabels: map[string]string{
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