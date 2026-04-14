package v2

import (
	"encoding/json"
	"fmt"
	"reflect"
)

// Label Merge Algorithms
//
// During component version transfers between repositories, non-signing labels
// may differ between source (inbound) and target (local). Merge algorithms
// resolve these differences according to configurable strategies.
//
// Four algorithms are defined by the OCM specification:
//
//   - "default": binary choice — compare values, resolve conflicts via overwrite mode.
//   - "simpleMapMerge": union of map keys with configurable conflict resolution.
//   - "simpleListMerge": union of list entries (append missing from local to inbound).
//   - "mapListMerge": merge lists of maps by a key field (default: "name").
//
// Merge specifications are attached to individual labels via the Merge field.
// If no specification is present, the "default" algorithm with OverwriteInbound is used.
//
// See https://github.com/open-component-model/ocm-spec/blob/main/doc/04-extensions/04-algorithms/label-merge-algorithms.md

// maxMergeDepth limits the recursion depth for nested merge specifications
// (e.g. entries fields in simpleMapMerge and mapListMerge). This prevents
// stack overflows from malicious or misconfigured deeply-nested merge specs.
const maxMergeDepth = 32

// Algorithm name constants for the built-in merge algorithms.
const (
	// MergeAlgorithmDefault selects the default merge algorithm which does
	// a binary comparison and resolves conflicts via the configured overwrite mode.
	MergeAlgorithmDefault = "default"

	// MergeAlgorithmSimpleMapMerge merges JSON object values by taking the union
	// of keys. Conflicts on shared keys are resolved per the overwrite mode or
	// an optional nested entries merge spec.
	MergeAlgorithmSimpleMapMerge = "simpleMapMerge"

	// MergeAlgorithmSimpleListMerge merges JSON array values by appending entries
	// from the local value that are not already present in the inbound value.
	MergeAlgorithmSimpleListMerge = "simpleListMerge"

	// MergeAlgorithmMapListMerge merges JSON arrays of objects by matching entries
	// on a configurable key field (default: "name"). Unmatched local entries are
	// appended; matched entries are merged per the overwrite mode.
	MergeAlgorithmMapListMerge = "mapListMerge"
)

// MergeSpec defines the merge algorithm specification for a label.
// It configures how label values are merged during component version transfers.
//
// See https://github.com/open-component-model/ocm-spec/blob/main/doc/01-model/03-elements-sub.md#labels
//
// +k8s:deepcopy-gen=true
type MergeSpec struct {
	// Algorithm is the name of the merge algorithm. Known algorithms:
	// "default", "simpleMapMerge", "simpleListMerge", "mapListMerge".
	// If empty, "default" is assumed.
	Algorithm string `json:"algorithm,omitempty"`

	// Config is algorithm-specific configuration as raw JSON.
	Config json.RawMessage `json:"config,omitempty"`
}

// OverwriteMode controls how merge conflicts are resolved.
type OverwriteMode string

const (
	// OverwriteNone rejects the merge if values conflict.
	OverwriteNone OverwriteMode = "none"

	// OverwriteLocal preserves the local (target) value on conflict.
	OverwriteLocal OverwriteMode = "local"

	// OverwriteInbound uses the inbound (source) value on conflict.
	OverwriteInbound OverwriteMode = "inbound"
)

// DefaultMergeConfig is the configuration for the "default" merge algorithm.
type DefaultMergeConfig struct {
	// Overwrite specifies conflict resolution. Defaults to OverwriteInbound.
	Overwrite OverwriteMode `json:"overwrite,omitempty"`
}

// SimpleMapMergeConfig is the configuration for the "simpleMapMerge" algorithm.
type SimpleMapMergeConfig struct {
	// Overwrite specifies conflict resolution for shared keys. Defaults to OverwriteNone.
	Overwrite OverwriteMode `json:"overwrite,omitempty"`

	// Entries optionally specifies a nested merge specification for resolving
	// conflicting map entry values. Used when Overwrite is empty and entries differ.
	Entries *MergeSpec `json:"entries,omitempty"`
}

// SimpleListMergeConfig is the configuration for the "simpleListMerge" algorithm.
type SimpleListMergeConfig struct {
	// Overwrite specifies conflict resolution. Defaults to OverwriteNone.
	Overwrite OverwriteMode `json:"overwrite,omitempty"`
}

// MapListMergeConfig is the configuration for the "mapListMerge" algorithm.
type MapListMergeConfig struct {
	// KeyField identifies entries in the list by this map key. Defaults to "name".
	KeyField string `json:"keyField,omitempty"`

	// Overwrite specifies conflict resolution for matched entries. Defaults to OverwriteNone.
	Overwrite OverwriteMode `json:"overwrite,omitempty"`

	// Entries optionally specifies a nested merge specification for resolving
	// conflicting matched entry values.
	Entries *MergeSpec `json:"entries,omitempty"`
}

// MergeLabels merges two sets of labels (local and inbound) using the merge
// specifications attached to each label. Labels are matched by name.
//
// Labels present only in local are preserved. Labels present only in inbound
// are added. Labels present in both are merged according to their merge spec.
//
// Neither input slice is mutated; a new slice is always returned.
func MergeLabels(local, inbound []Label) ([]Label, error) {
	localByName := make(map[string]Label, len(local))
	for _, l := range local {
		localByName[l.Name] = l
	}

	seen := make(map[string]bool, len(inbound))
	result := make([]Label, 0, len(local)+len(inbound))

	for _, il := range inbound {
		seen[il.Name] = true
		ll, exists := localByName[il.Name]
		if !exists {
			result = append(result, il)
			continue
		}
		merged, err := mergeLabel(ll, il)
		if err != nil {
			return nil, fmt.Errorf("label %q: %w", il.Name, err)
		}
		result = append(result, merged)
	}

	for _, ll := range local {
		if !seen[ll.Name] {
			result = append(result, ll)
		}
	}

	return result, nil
}

// mergeLabel merges a single local and inbound label pair.
// The effective merge spec is determined by: inbound > local > default.
// The result always carries inbound metadata (Signing, Version, Merge).
func mergeLabel(local, inbound Label) (Label, error) {
	spec := effectiveMergeSpec(local.Merge, inbound.Merge)
	algo := spec.Algorithm
	if algo == "" {
		algo = MergeAlgorithmDefault
	}

	merged, err := mergeValueAtDepth(local.Value, inbound.Value, algo, spec.Config, 0)
	if err != nil {
		return Label{}, err
	}

	return Label{
		Name:    inbound.Name,
		Value:   merged,
		Signing: inbound.Signing,
		Version: inbound.Version,
		Merge:   inbound.Merge,
	}, nil
}

// effectiveMergeSpec returns the merge spec to use.
// Prefers inbound, falls back to local, then default (inbound overwrite).
func effectiveMergeSpec(local, inbound *MergeSpec) MergeSpec {
	if inbound != nil {
		return *inbound
	}
	if local != nil {
		return *local
	}
	return MergeSpec{Algorithm: MergeAlgorithmDefault}
}

// mergeValueAtDepth dispatches to the appropriate algorithm with recursion tracking.
func mergeValueAtDepth(local, inbound json.RawMessage, algo string, cfg json.RawMessage, depth int) (json.RawMessage, error) {
	if depth > maxMergeDepth {
		return nil, fmt.Errorf("merge recursion depth exceeded (max %d)", maxMergeDepth)
	}
	switch algo {
	case MergeAlgorithmDefault:
		return mergeDefault(local, inbound, cfg)
	case MergeAlgorithmSimpleMapMerge:
		return mergeSimpleMap(local, inbound, cfg, depth)
	case MergeAlgorithmSimpleListMerge:
		return mergeSimpleList(local, inbound, cfg)
	case MergeAlgorithmMapListMerge:
		return mergeMapList(local, inbound, cfg, depth)
	default:
		return nil, fmt.Errorf("unknown merge algorithm: %q", algo)
	}
}

// jsonEqual compares two json.RawMessage values for semantic equality.
func jsonEqual(a, b json.RawMessage) (bool, error) {
	var va, vb any
	if err := json.Unmarshal(a, &va); err != nil {
		return false, fmt.Errorf("unmarshalling local value: %w", err)
	}
	if err := json.Unmarshal(b, &vb); err != nil {
		return false, fmt.Errorf("unmarshalling inbound value: %w", err)
	}
	return reflect.DeepEqual(va, vb), nil
}

// resolveOverwrite resolves a conflict using the given overwrite mode.
// Callers must ensure mode is a valid OverwriteMode constant; empty string is rejected.
func resolveOverwrite(mode OverwriteMode, local, inbound json.RawMessage) (json.RawMessage, error) {
	switch mode {
	case OverwriteInbound:
		return inbound, nil
	case OverwriteLocal:
		return local, nil
	case OverwriteNone:
		return nil, fmt.Errorf("conflicting values and overwrite mode is %q", OverwriteNone)
	default:
		return nil, fmt.Errorf("unknown overwrite mode: %q", mode)
	}
}
