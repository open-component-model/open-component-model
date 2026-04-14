package v2_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
)

// helper to run a single-label merge with given algorithm and config.
func mergeOne(t *testing.T, algo string, cfg string, localVal, inboundVal string) (json.RawMessage, error) {
	t.Helper()
	var cfgRaw json.RawMessage
	if cfg != "" {
		cfgRaw = json.RawMessage(cfg)
	}
	spec := &v2.MergeSpec{Algorithm: algo, Config: cfgRaw}
	local := []v2.Label{{Name: "x", Value: json.RawMessage(localVal), Merge: spec}}
	inbound := []v2.Label{{Name: "x", Value: json.RawMessage(inboundVal), Merge: spec}}
	result, err := v2.MergeLabels(local, inbound)
	if err != nil {
		return nil, err
	}
	return result[0].Value, nil
}

// --- default algorithm ---

func TestDefault_EqualValues(t *testing.T) {
	val, err := mergeOne(t, "default", "", `"same"`, `"same"`)
	require.NoError(t, err)
	assert.JSONEq(t, `"same"`, string(val))
}

func TestDefault_DifferentValues_InboundWins(t *testing.T) {
	val, err := mergeOne(t, "default", `{"overwrite":"inbound"}`, `"old"`, `"new"`)
	require.NoError(t, err)
	assert.JSONEq(t, `"new"`, string(val))
}

func TestDefault_DifferentValues_DefaultIsInbound(t *testing.T) {
	val, err := mergeOne(t, "default", "", `"old"`, `"new"`)
	require.NoError(t, err)
	assert.JSONEq(t, `"new"`, string(val))
}

func TestDefault_DifferentValues_LocalWins(t *testing.T) {
	val, err := mergeOne(t, "default", `{"overwrite":"local"}`, `"old"`, `"new"`)
	require.NoError(t, err)
	assert.JSONEq(t, `"old"`, string(val))
}

func TestDefault_DifferentValues_None_Error(t *testing.T) {
	_, err := mergeOne(t, "default", `{"overwrite":"none"}`, `"old"`, `"new"`)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "conflicting")
}

func TestDefault_ComplexObjects_Equal(t *testing.T) {
	obj := `{"a":1,"b":[2,3]}`
	val, err := mergeOne(t, "default", "", obj, obj)
	require.NoError(t, err)
	assert.JSONEq(t, obj, string(val))
}

func TestDefault_InvalidConfig(t *testing.T) {
	_, err := mergeOne(t, "default", `{invalid`, `"a"`, `"b"`)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "parsing default merge config")
}

// --- simpleMapMerge algorithm ---

func TestSimpleMap_DisjointKeys(t *testing.T) {
	val, err := mergeOne(t, "simpleMapMerge", "",
		`{"a":"1"}`,
		`{"b":"2"}`,
	)
	require.NoError(t, err)
	var m map[string]any
	require.NoError(t, json.Unmarshal(val, &m))
	assert.Equal(t, "1", m["a"])
	assert.Equal(t, "2", m["b"])
}

func TestSimpleMap_SameKeysSameValues(t *testing.T) {
	val, err := mergeOne(t, "simpleMapMerge", "",
		`{"a":"1"}`,
		`{"a":"1"}`,
	)
	require.NoError(t, err)
	assert.JSONEq(t, `{"a":"1"}`, string(val))
}

func TestSimpleMap_Conflict_NoneDefault_Error(t *testing.T) {
	_, err := mergeOne(t, "simpleMapMerge", "",
		`{"a":"local"}`,
		`{"a":"inbound"}`,
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "conflicting")
}

func TestSimpleMap_Conflict_Inbound(t *testing.T) {
	val, err := mergeOne(t, "simpleMapMerge", `{"overwrite":"inbound"}`,
		`{"a":"local"}`,
		`{"a":"inbound"}`,
	)
	require.NoError(t, err)
	var m map[string]any
	require.NoError(t, json.Unmarshal(val, &m))
	assert.Equal(t, "inbound", m["a"])
}

func TestSimpleMap_Conflict_Local(t *testing.T) {
	val, err := mergeOne(t, "simpleMapMerge", `{"overwrite":"local"}`,
		`{"a":"local"}`,
		`{"a":"inbound"}`,
	)
	require.NoError(t, err)
	var m map[string]any
	require.NoError(t, json.Unmarshal(val, &m))
	assert.Equal(t, "local", m["a"])
}

func TestSimpleMap_NestedEntriesMerge(t *testing.T) {
	cfg := `{"entries":{"algorithm":"default","config":{"overwrite":"inbound"}}}`
	val, err := mergeOne(t, "simpleMapMerge", cfg,
		`{"a":"local","b":"shared"}`,
		`{"a":"inbound","b":"shared"}`,
	)
	require.NoError(t, err)
	var m map[string]any
	require.NoError(t, json.Unmarshal(val, &m))
	assert.Equal(t, "inbound", m["a"])
	assert.Equal(t, "shared", m["b"])
}

func TestSimpleMap_NotObject_Error(t *testing.T) {
	_, err := mergeOne(t, "simpleMapMerge", "", `[1,2]`, `{"a":"1"}`)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not a JSON object")
}

func TestSimpleMap_EmptyMaps(t *testing.T) {
	val, err := mergeOne(t, "simpleMapMerge", "", `{}`, `{}`)
	require.NoError(t, err)
	assert.JSONEq(t, `{}`, string(val))
}

// --- simpleListMerge algorithm ---

func TestSimpleList_BothEmpty(t *testing.T) {
	val, err := mergeOne(t, "simpleListMerge", "", `[]`, `[]`)
	require.NoError(t, err)
	assert.JSONEq(t, `[]`, string(val))
}

func TestSimpleList_DisjointEntries(t *testing.T) {
	val, err := mergeOne(t, "simpleListMerge", "",
		`["a","b"]`,
		`["c","d"]`,
	)
	require.NoError(t, err)
	var list []any
	require.NoError(t, json.Unmarshal(val, &list))
	assert.Len(t, list, 4)
	assert.Contains(t, list, "a")
	assert.Contains(t, list, "b")
	assert.Contains(t, list, "c")
	assert.Contains(t, list, "d")
}

func TestSimpleList_DuplicateNotAdded(t *testing.T) {
	val, err := mergeOne(t, "simpleListMerge", "",
		`["a","b"]`,
		`["a","c"]`,
	)
	require.NoError(t, err)
	var list []any
	require.NoError(t, json.Unmarshal(val, &list))
	assert.Len(t, list, 3) // a, c from inbound + b from local
}

func TestSimpleList_ComplexEntries(t *testing.T) {
	val, err := mergeOne(t, "simpleListMerge", "",
		`[{"name":"a","val":1},{"name":"b","val":2}]`,
		`[{"name":"a","val":1}]`,
	)
	require.NoError(t, err)
	var list []any
	require.NoError(t, json.Unmarshal(val, &list))
	assert.Len(t, list, 2)
}

func TestSimpleList_NotArray_Error(t *testing.T) {
	_, err := mergeOne(t, "simpleListMerge", "", `"not-list"`, `[]`)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not a JSON array")
}

func TestSimpleList_InboundOrderPreserved(t *testing.T) {
	val, err := mergeOne(t, "simpleListMerge", "",
		`["x"]`,
		`["c","b","a"]`,
	)
	require.NoError(t, err)
	var list []string
	require.NoError(t, json.Unmarshal(val, &list))
	// Inbound order first, then local additions
	assert.Equal(t, []string{"c", "b", "a", "x"}, list)
}

// --- mapListMerge algorithm ---

func TestMapList_BothEmpty(t *testing.T) {
	val, err := mergeOne(t, "mapListMerge", "", `[]`, `[]`)
	require.NoError(t, err)
	assert.JSONEq(t, `[]`, string(val))
}

func TestMapList_UnmatchedLocalAppended(t *testing.T) {
	val, err := mergeOne(t, "mapListMerge", "",
		`[{"name":"a","v":1}]`,
		`[{"name":"b","v":2}]`,
	)
	require.NoError(t, err)
	var list []map[string]any
	require.NoError(t, json.Unmarshal(val, &list))
	require.Len(t, list, 2)

	names := []string{list[0]["name"].(string), list[1]["name"].(string)}
	assert.Contains(t, names, "a")
	assert.Contains(t, names, "b")
}

func TestMapList_MatchedSameValue(t *testing.T) {
	val, err := mergeOne(t, "mapListMerge", "",
		`[{"name":"a","v":1}]`,
		`[{"name":"a","v":1}]`,
	)
	require.NoError(t, err)
	assert.JSONEq(t, `[{"name":"a","v":1}]`, string(val))
}

func TestMapList_Conflict_NoneDefault_Error(t *testing.T) {
	_, err := mergeOne(t, "mapListMerge", "",
		`[{"name":"a","v":1}]`,
		`[{"name":"a","v":2}]`,
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "conflicting")
}

func TestMapList_Conflict_Inbound(t *testing.T) {
	val, err := mergeOne(t, "mapListMerge", `{"overwrite":"inbound"}`,
		`[{"name":"a","v":"local"}]`,
		`[{"name":"a","v":"inbound"}]`,
	)
	require.NoError(t, err)
	var list []map[string]any
	require.NoError(t, json.Unmarshal(val, &list))
	require.Len(t, list, 1)
	assert.Equal(t, "inbound", list[0]["v"])
}

func TestMapList_Conflict_Local(t *testing.T) {
	val, err := mergeOne(t, "mapListMerge", `{"overwrite":"local"}`,
		`[{"name":"a","v":"local"}]`,
		`[{"name":"a","v":"inbound"}]`,
	)
	require.NoError(t, err)
	var list []map[string]any
	require.NoError(t, json.Unmarshal(val, &list))
	require.Len(t, list, 1)
	assert.Equal(t, "local", list[0]["v"])
}

func TestMapList_CustomKeyField(t *testing.T) {
	val, err := mergeOne(t, "mapListMerge", `{"keyField":"id","overwrite":"inbound"}`,
		`[{"id":"x","v":"local"}]`,
		`[{"id":"x","v":"inbound"}]`,
	)
	require.NoError(t, err)
	var list []map[string]any
	require.NoError(t, json.Unmarshal(val, &list))
	require.Len(t, list, 1)
	assert.Equal(t, "inbound", list[0]["v"])
}

func TestMapList_MissingKeyField_Error(t *testing.T) {
	_, err := mergeOne(t, "mapListMerge", "",
		`[{"v":1}]`,
		`[{"name":"a"}]`,
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing key field")
}

func TestMapList_NotArrayOfObjects_Error(t *testing.T) {
	_, err := mergeOne(t, "mapListMerge", "", `"not-list"`, `[]`)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not a JSON array")
}

func TestMapList_NestedEntriesMerge(t *testing.T) {
	cfg := `{"entries":{"algorithm":"simpleMapMerge","config":{"overwrite":"inbound"}}}`
	val, err := mergeOne(t, "mapListMerge", cfg,
		`[{"name":"a","x":"local","y":"shared"}]`,
		`[{"name":"a","x":"inbound","y":"shared"}]`,
	)
	require.NoError(t, err)
	var list []map[string]any
	require.NoError(t, json.Unmarshal(val, &list))
	require.Len(t, list, 1)
	assert.Equal(t, "inbound", list[0]["x"])
	assert.Equal(t, "shared", list[0]["y"])
}

func TestMapList_Mixed_MatchedAndUnmatched(t *testing.T) {
	val, err := mergeOne(t, "mapListMerge", `{"overwrite":"inbound"}`,
		`[{"name":"a","v":"local-a"},{"name":"c","v":"local-c"}]`,
		`[{"name":"a","v":"inbound-a"},{"name":"b","v":"inbound-b"}]`,
	)
	require.NoError(t, err)
	var list []map[string]any
	require.NoError(t, json.Unmarshal(val, &list))
	require.Len(t, list, 3)

	byName := make(map[string]map[string]any)
	for _, e := range list {
		byName[e["name"].(string)] = e
	}
	assert.Equal(t, "inbound-a", byName["a"]["v"]) // matched, inbound wins
	assert.Equal(t, "inbound-b", byName["b"]["v"]) // inbound only
	assert.Equal(t, "local-c", byName["c"]["v"])    // local only, appended
}

// --- duplicate key detection ---

func TestMapList_DuplicateKeyInInbound_Error(t *testing.T) {
	_, err := mergeOne(t, "mapListMerge", "",
		`[{"name":"a","v":1}]`,
		`[{"name":"a","v":1},{"name":"a","v":2}]`,
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "duplicate key")
}

// --- non-string key field ---

func TestMapList_NonStringKeyField_Error(t *testing.T) {
	_, err := mergeOne(t, "mapListMerge", "",
		`[{"name":123}]`,
		`[{"name":"a"}]`,
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "non-string value")
}

// --- simpleListMerge config validation ---

func TestSimpleList_InvalidConfig_Error(t *testing.T) {
	_, err := mergeOne(t, "simpleListMerge", `{invalid`, `[]`, `[]`)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "parsing simpleListMerge config")
}
