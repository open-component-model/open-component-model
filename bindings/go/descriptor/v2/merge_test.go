package v2_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
)

// j is a shorthand for creating json.RawMessage from a JSON literal.
func j(s string) json.RawMessage {
	return json.RawMessage(s)
}

func TestMergeSpec_JSON_Roundtrip(t *testing.T) {
	spec := v2.MergeSpec{
		Algorithm: v2.MergeAlgorithmSimpleMapMerge,
		Config:    j(`{"overwrite":"local"}`),
	}
	data, err := json.Marshal(spec)
	require.NoError(t, err)

	var decoded v2.MergeSpec
	require.NoError(t, json.Unmarshal(data, &decoded))
	assert.Equal(t, spec.Algorithm, decoded.Algorithm)
	assert.JSONEq(t, string(spec.Config), string(decoded.Config))
}

func TestLabel_WithMerge_JSON_Roundtrip(t *testing.T) {
	label := v2.Label{
		Name:    "routing-slip",
		Value:   j(`{"dest":"prod"}`),
		Version: "v1",
		Merge: &v2.MergeSpec{
			Algorithm: v2.MergeAlgorithmDefault,
			Config:    j(`{"overwrite":"inbound"}`),
		},
	}
	data, err := json.Marshal(label)
	require.NoError(t, err)

	var decoded v2.Label
	require.NoError(t, json.Unmarshal(data, &decoded))
	assert.Equal(t, label.Name, decoded.Name)
	require.NotNil(t, decoded.Merge)
	assert.Equal(t, v2.MergeAlgorithmDefault, decoded.Merge.Algorithm)
}

func TestLabel_WithoutMerge_BackwardCompat(t *testing.T) {
	raw := `{"name":"test","value":"hello"}`
	var label v2.Label
	require.NoError(t, json.Unmarshal([]byte(raw), &label))
	assert.Nil(t, label.Merge)
}

func TestMergeSpec_DeepCopy(t *testing.T) {
	spec := &v2.MergeSpec{
		Algorithm: "default",
		Config:    j(`{"overwrite":"local"}`),
	}
	cp := spec.DeepCopy()
	assert.Equal(t, spec.Algorithm, cp.Algorithm)
	assert.Equal(t, string(spec.Config), string(cp.Config))

	// Mutate copy, verify original unchanged
	cp.Config[0] = 'X'
	assert.NotEqual(t, string(spec.Config), string(cp.Config))
}

func TestLabel_DeepCopy_WithMerge(t *testing.T) {
	label := &v2.Label{
		Name:  "test",
		Value: j(`"val"`),
		Merge: &v2.MergeSpec{
			Algorithm: "default",
			Config:    j(`{}`),
		},
	}
	cp := label.DeepCopy()
	require.NotNil(t, cp.Merge)
	assert.Equal(t, "default", cp.Merge.Algorithm)

	// Verify independence
	cp.Merge.Algorithm = "changed"
	assert.Equal(t, "default", label.Merge.Algorithm)
}

func TestMergeLabels_BothEmpty(t *testing.T) {
	result, err := v2.MergeLabels(nil, nil)
	require.NoError(t, err)
	assert.Empty(t, result)
}

func TestMergeLabels_LocalOnly(t *testing.T) {
	local := []v2.Label{
		{Name: "a", Value: j(`"val-a"`)},
	}
	result, err := v2.MergeLabels(local, nil)
	require.NoError(t, err)
	require.Len(t, result, 1)
	assert.Equal(t, "a", result[0].Name)
}

func TestMergeLabels_InboundOnly(t *testing.T) {
	inbound := []v2.Label{
		{Name: "b", Value: j(`"val-b"`)},
	}
	result, err := v2.MergeLabels(nil, inbound)
	require.NoError(t, err)
	require.Len(t, result, 1)
	assert.Equal(t, "b", result[0].Name)
}

func TestMergeLabels_MatchedIdentical(t *testing.T) {
	labels := []v2.Label{
		{Name: "a", Value: j(`"same"`)},
	}
	result, err := v2.MergeLabels(labels, labels)
	require.NoError(t, err)
	require.Len(t, result, 1)
	assert.Equal(t, "a", result[0].Name)
}

func TestMergeLabels_DefaultMerge_InboundWins(t *testing.T) {
	local := []v2.Label{
		{Name: "a", Value: j(`"old"`)},
	}
	inbound := []v2.Label{
		{Name: "a", Value: j(`"new"`)},
	}
	result, err := v2.MergeLabels(local, inbound)
	require.NoError(t, err)
	require.Len(t, result, 1)

	var val string
	require.NoError(t, json.Unmarshal(result[0].Value, &val))
	assert.Equal(t, "new", val)
}

func TestMergeLabels_ExplicitNone_Error(t *testing.T) {
	local := []v2.Label{
		{
			Name:  "a",
			Value: j(`"old"`),
			Merge: &v2.MergeSpec{
				Algorithm: v2.MergeAlgorithmDefault,
				Config:    j(`{"overwrite":"none"}`),
			},
		},
	}
	inbound := []v2.Label{
		{
			Name:  "a",
			Value: j(`"new"`),
			Merge: &v2.MergeSpec{
				Algorithm: v2.MergeAlgorithmDefault,
				Config:    j(`{"overwrite":"none"}`),
			},
		},
	}
	_, err := v2.MergeLabels(local, inbound)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "conflicting")
}

func TestMergeLabels_Mixed(t *testing.T) {
	local := []v2.Label{
		{Name: "shared", Value: j(`"local-val"`)},
		{Name: "local-only", Value: j(`"lo"`)},
	}
	inbound := []v2.Label{
		{Name: "shared", Value: j(`"inbound-val"`)},
		{Name: "inbound-only", Value: j(`"io"`)},
	}

	result, err := v2.MergeLabels(local, inbound)
	require.NoError(t, err)
	require.Len(t, result, 3)

	names := make(map[string]bool)
	for _, l := range result {
		names[l.Name] = true
	}
	assert.True(t, names["shared"])
	assert.True(t, names["local-only"])
	assert.True(t, names["inbound-only"])
}

func TestMergeLabels_InboundSpecPreferred(t *testing.T) {
	local := []v2.Label{
		{
			Name:  "a",
			Value: j(`"old"`),
			Merge: &v2.MergeSpec{
				Algorithm: v2.MergeAlgorithmDefault,
				Config:    j(`{"overwrite":"local"}`),
			},
		},
	}
	inbound := []v2.Label{
		{
			Name:  "a",
			Value: j(`"new"`),
			Merge: &v2.MergeSpec{
				Algorithm: v2.MergeAlgorithmDefault,
				Config:    j(`{"overwrite":"inbound"}`),
			},
		},
	}
	result, err := v2.MergeLabels(local, inbound)
	require.NoError(t, err)
	require.Len(t, result, 1)

	var val string
	require.NoError(t, json.Unmarshal(result[0].Value, &val))
	assert.Equal(t, "new", val)
}

func TestMergeLabels_DoesNotMutateInput(t *testing.T) {
	local := []v2.Label{
		{Name: "a", Value: j(`"old"`)},
	}
	inbound := []v2.Label{
		{Name: "a", Value: j(`"new"`)},
		{Name: "b", Value: j(`"b-val"`)},
	}

	origLocalLen := len(local)
	origInboundLen := len(inbound)

	_, err := v2.MergeLabels(local, inbound)
	require.NoError(t, err)

	assert.Len(t, local, origLocalLen)
	assert.Len(t, inbound, origInboundLen)
}

func TestMergeLabels_UnknownAlgorithm(t *testing.T) {
	local := []v2.Label{
		{
			Name:  "a",
			Value: j(`"old"`),
			Merge: &v2.MergeSpec{Algorithm: "doesNotExist"},
		},
	}
	inbound := []v2.Label{
		{
			Name:  "a",
			Value: j(`"new"`),
			Merge: &v2.MergeSpec{Algorithm: "doesNotExist"},
		},
	}
	_, err := v2.MergeLabels(local, inbound)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown merge algorithm")
}

func TestMergeLabels_PreservesOrder(t *testing.T) {
	local := []v2.Label{
		{Name: "z", Value: j(`"z"`)},
		{Name: "a", Value: j(`"a"`)},
	}
	inbound := []v2.Label{
		{Name: "m", Value: j(`"m"`)},
		{Name: "a", Value: j(`"a-new"`)},
	}

	result, err := v2.MergeLabels(local, inbound)
	require.NoError(t, err)
	require.Len(t, result, 3)
	// Inbound order first, then local-only
	assert.Equal(t, "m", result[0].Name)
	assert.Equal(t, "a", result[1].Name)
	assert.Equal(t, "z", result[2].Name)
}
