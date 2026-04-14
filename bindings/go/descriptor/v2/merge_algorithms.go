package v2

import (
	"encoding/json"
	"fmt"
	"reflect"
)

// mergeDefault implements the "default" merge algorithm.
//
// It compares local and inbound values for semantic JSON equality.
// If equal, inbound is returned unchanged. On conflict, the configured
// overwrite mode determines the result. The default overwrite mode is
// OverwriteInbound.
//
// See https://github.com/open-component-model/ocm-spec/blob/main/doc/04-extensions/04-algorithms/label-merge-algorithms.md
func mergeDefault(local, inbound json.RawMessage, cfgRaw json.RawMessage) (json.RawMessage, error) {
	eq, err := jsonEqual(local, inbound)
	if err != nil {
		return nil, err
	}
	if eq {
		return inbound, nil
	}

	var cfg DefaultMergeConfig
	if len(cfgRaw) > 0 {
		if err := json.Unmarshal(cfgRaw, &cfg); err != nil {
			return nil, fmt.Errorf("parsing default merge config: %w", err)
		}
	}

	mode := cfg.Overwrite
	if mode == "" {
		mode = OverwriteInbound
	}
	return resolveOverwrite(mode, local, inbound)
}

// mergeSimpleMap implements the "simpleMapMerge" algorithm.
//
// Both values must be JSON objects (map[string]any). The result is the union
// of all keys from both maps:
//   - Keys only in inbound: kept as-is.
//   - Keys only in local: added to result.
//   - Keys in both with equal values: kept as-is.
//   - Keys in both with different values: resolved per overwrite mode, or via
//     the optional nested entries merge spec.
//
// The default overwrite mode is OverwriteNone.
//
// See https://github.com/open-component-model/ocm-spec/blob/main/doc/04-extensions/04-algorithms/label-merge-algorithms.md
func mergeSimpleMap(local, inbound json.RawMessage, cfgRaw json.RawMessage, depth int) (json.RawMessage, error) {
	var localMap, inboundMap map[string]any
	if err := json.Unmarshal(local, &localMap); err != nil {
		return nil, fmt.Errorf("simpleMapMerge: local value is not a JSON object: %w", err)
	}
	if err := json.Unmarshal(inbound, &inboundMap); err != nil {
		return nil, fmt.Errorf("simpleMapMerge: inbound value is not a JSON object: %w", err)
	}

	var cfg SimpleMapMergeConfig
	if len(cfgRaw) > 0 {
		if err := json.Unmarshal(cfgRaw, &cfg); err != nil {
			return nil, fmt.Errorf("parsing simpleMapMerge config: %w", err)
		}
	}

	result := make(map[string]any, len(inboundMap)+len(localMap))
	for k, v := range inboundMap {
		result[k] = v
	}

	for k, lv := range localMap {
		iv, exists := inboundMap[k]
		if !exists {
			result[k] = lv
			continue
		}
		if reflect.DeepEqual(lv, iv) {
			continue
		}
		resolved, err := resolveMapEntry(k, lv, iv, cfg, depth)
		if err != nil {
			return nil, err
		}
		result[k] = resolved
	}

	return json.Marshal(result)
}

// resolveMapEntry resolves a conflict on a single map key.
func resolveMapEntry(key string, local, inbound any, cfg SimpleMapMergeConfig, depth int) (any, error) {
	if cfg.Overwrite == "" && cfg.Entries != nil {
		resolved, err := mergeEntryValue(local, inbound, cfg.Entries, depth+1)
		if err != nil {
			return nil, fmt.Errorf("map key %q: %w", key, err)
		}
		return resolved, nil
	}

	mode := cfg.Overwrite
	if mode == "" {
		mode = OverwriteNone
	}

	switch mode {
	case OverwriteInbound:
		return inbound, nil
	case OverwriteLocal:
		return local, nil
	case OverwriteNone:
		return nil, fmt.Errorf("map key %q: conflicting values and overwrite mode is %q", key, OverwriteNone)
	default:
		return nil, fmt.Errorf("map key %q: unknown overwrite mode: %q", key, mode)
	}
}

// mergeSimpleList implements the "simpleListMerge" algorithm.
//
// Both values must be JSON arrays. The result contains all entries from inbound,
// plus any entries from local that are not already present (compared by
// reflect.DeepEqual on the unmarshalled values).
//
// See https://github.com/open-component-model/ocm-spec/blob/main/doc/04-extensions/04-algorithms/label-merge-algorithms.md
func mergeSimpleList(local, inbound json.RawMessage, cfgRaw json.RawMessage) (json.RawMessage, error) {
	var localList, inboundList []any
	if err := json.Unmarshal(local, &localList); err != nil {
		return nil, fmt.Errorf("simpleListMerge: local value is not a JSON array: %w", err)
	}
	if err := json.Unmarshal(inbound, &inboundList); err != nil {
		return nil, fmt.Errorf("simpleListMerge: inbound value is not a JSON array: %w", err)
	}

	// Validate config if present (currently unused beyond validation).
	if len(cfgRaw) > 0 {
		var cfg SimpleListMergeConfig
		if err := json.Unmarshal(cfgRaw, &cfg); err != nil {
			return nil, fmt.Errorf("parsing simpleListMerge config: %w", err)
		}
	}

	result := make([]any, len(inboundList))
	copy(result, inboundList)

	for _, le := range localList {
		found := false
		for _, ie := range result {
			if reflect.DeepEqual(le, ie) {
				found = true
				break
			}
		}
		if !found {
			result = append(result, le)
		}
	}

	return json.Marshal(result)
}

// mergeMapList implements the "mapListMerge" algorithm.
//
// Both values must be JSON arrays of objects. Entries are matched by a key field
// (default: "name"). The result contains:
//   - All inbound entries (updated if local has a matching entry with different values).
//   - Local entries with no match in inbound are appended.
//   - Matched entries with different values are resolved per overwrite mode or
//     the optional nested entries merge spec.
//
// The default overwrite mode is OverwriteNone.
//
// See https://github.com/open-component-model/ocm-spec/blob/main/doc/04-extensions/04-algorithms/label-merge-algorithms.md
func mergeMapList(local, inbound json.RawMessage, cfgRaw json.RawMessage, depth int) (json.RawMessage, error) {
	var localList, inboundList []map[string]any
	if err := json.Unmarshal(local, &localList); err != nil {
		return nil, fmt.Errorf("mapListMerge: local value is not a JSON array of objects: %w", err)
	}
	if err := json.Unmarshal(inbound, &inboundList); err != nil {
		return nil, fmt.Errorf("mapListMerge: inbound value is not a JSON array of objects: %w", err)
	}

	var cfg MapListMergeConfig
	if len(cfgRaw) > 0 {
		if err := json.Unmarshal(cfgRaw, &cfg); err != nil {
			return nil, fmt.Errorf("parsing mapListMerge config: %w", err)
		}
	}
	keyField := cfg.KeyField
	if keyField == "" {
		keyField = "name"
	}

	inboundIndex := make(map[string]int, len(inboundList))
	for i, entry := range inboundList {
		kv, err := extractKeyValue(entry, keyField)
		if err != nil {
			return nil, fmt.Errorf("mapListMerge: inbound entry %d: %w", i, err)
		}
		if _, dup := inboundIndex[kv]; dup {
			return nil, fmt.Errorf("mapListMerge: duplicate key %q in inbound list", kv)
		}
		inboundIndex[kv] = i
	}

	result := make([]map[string]any, len(inboundList))
	for i, entry := range inboundList {
		cp := make(map[string]any, len(entry))
		for k, v := range entry {
			cp[k] = v
		}
		result[i] = cp
	}

	for i, le := range localList {
		kv, err := extractKeyValue(le, keyField)
		if err != nil {
			return nil, fmt.Errorf("mapListMerge: local entry %d: %w", i, err)
		}
		idx, matched := inboundIndex[kv]
		if !matched {
			result = append(result, le)
			continue
		}
		if reflect.DeepEqual(le, inboundList[idx]) {
			continue
		}
		resolved, err := resolveMapListEntry(kv, le, inboundList[idx], cfg, depth)
		if err != nil {
			return nil, err
		}
		result[idx] = resolved
	}

	return json.Marshal(result)
}

// extractKeyValue extracts the key field value from a map entry as a string.
// Only string key field values are supported; other types are rejected.
func extractKeyValue(entry map[string]any, keyField string) (string, error) {
	v, ok := entry[keyField]
	if !ok {
		return "", fmt.Errorf("missing key field %q", keyField)
	}
	s, ok := v.(string)
	if !ok {
		return "", fmt.Errorf("key field %q has non-string value of type %T", keyField, v)
	}
	return s, nil
}

// resolveMapListEntry resolves a conflict on a matched map list entry.
func resolveMapListEntry(key string, local, inbound map[string]any, cfg MapListMergeConfig, depth int) (map[string]any, error) {
	if cfg.Overwrite == "" && cfg.Entries != nil {
		resolved, err := mergeEntryValue(local, inbound, cfg.Entries, depth+1)
		if err != nil {
			return nil, fmt.Errorf("map list entry %q: %w", key, err)
		}
		m, ok := resolved.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("map list entry %q: nested merge did not produce a map", key)
		}
		return m, nil
	}

	mode := cfg.Overwrite
	if mode == "" {
		mode = OverwriteNone
	}

	switch mode {
	case OverwriteInbound:
		return inbound, nil
	case OverwriteLocal:
		return local, nil
	case OverwriteNone:
		return nil, fmt.Errorf("map list entry %q: conflicting values and overwrite mode is %q", key, OverwriteNone)
	default:
		return nil, fmt.Errorf("map list entry %q: unknown overwrite mode: %q", key, mode)
	}
}

// mergeEntryValue performs a nested merge of two values using a MergeSpec.
// The depth parameter tracks recursion depth to prevent stack overflows.
func mergeEntryValue(local, inbound any, spec *MergeSpec, depth int) (any, error) {
	localRaw, err := json.Marshal(local)
	if err != nil {
		return nil, fmt.Errorf("marshalling local for nested merge: %w", err)
	}
	inboundRaw, err := json.Marshal(inbound)
	if err != nil {
		return nil, fmt.Errorf("marshalling inbound for nested merge: %w", err)
	}

	algo := spec.Algorithm
	if algo == "" {
		algo = MergeAlgorithmDefault
	}

	merged, err := mergeValueAtDepth(localRaw, inboundRaw, algo, spec.Config, depth)
	if err != nil {
		return nil, err
	}

	var result any
	if err := json.Unmarshal(merged, &result); err != nil {
		return nil, fmt.Errorf("unmarshalling nested merge result: %w", err)
	}
	return result, nil
}
