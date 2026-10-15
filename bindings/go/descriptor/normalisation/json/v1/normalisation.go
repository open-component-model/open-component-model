package v1

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	norms "ocm.software/open-component-model/bindings/go/descriptor/normalisation"
	"ocm.software/open-component-model/bindings/go/descriptor/normalisation/engine/entry"
	descruntime "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// Algorithm is the registered name for this normalisation algorithm. It
// identifies the legacy gardener/cc-utils serialization (ocm/sign.py) that
// precedes jsonNormalisation/v4alpha1.
const Algorithm = "jsonNormalisation/v1"

// init registers the normalisation algorithm so that a blank import of this
// package makes jsonNormalisation/v1 resolvable from the signing registry.
func init() {
	norms.Normalisations.Register(Algorithm, algo{})
}

// algo implements the normalisation interface for the legacy entry-list
// serialization.
type algo struct{}

// Normalise converts the descriptor to its v2 form, applies the v1 field
// selection and transforms on the resulting dictionary, and serialises it via
// the entry engine (list-of-single-key-maps). The output is byte-identical to
// cc-utils' jsonNormalisation/v1.
func (algo) Normalise(cd *descruntime.Descriptor) ([]byte, error) {
	scheme := runtime.NewScheme(runtime.WithAllowUnknown())
	desc, err := descruntime.ConvertToV2(scheme, cd)
	if err != nil {
		return nil, err
	}

	m, err := toMap(desc)
	if err != nil {
		return nil, err
	}

	out, err := transformDescriptor(m)
	if err != nil {
		return nil, err
	}
	return entry.Normalise(out)
}

// toMap marshals the v2 descriptor and unmarshals it into a generic map. This
// expands Label.Value (json.RawMessage) into the real nested tree and lets the
// transforms operate uniformly on map[string]any.
func toMap(desc *v2.Descriptor) (map[string]any, error) {
	data, err := json.Marshal(desc)
	if err != nil {
		return nil, err
	}
	// UseNumber keeps numeric label values in their original textual form
	// (large integers, trailing zeros) instead of collapsing them to float64,
	// so the re-marshalled bytes stay identical to cc-utils' output.
	var m map[string]any
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	if err := dec.Decode(&m); err != nil {
		return nil, err
	}
	// Replace the (order-lost) expanded label values with their original raw
	// bytes so that arrays nested in a label value keep source key order, which
	// cc-utils preserves (it never recurses into arrays).
	preserveRawLabelValues(m, desc)
	return m, nil
}

// preserveRawLabelValues overwrites each label's expanded "value" in the generic
// map with the verbatim json.RawMessage from the descriptor. Label order and
// count match between the marshalled map and the descriptor, so a positional
// walk is exact.
func preserveRawLabelValues(m map[string]any, desc *v2.Descriptor) {
	comp, ok := m["component"].(map[string]any)
	if !ok {
		return
	}
	setRawLabelValues(comp["labels"], desc.Component.Labels)
	if resources, ok := comp["resources"].([]any); ok {
		for i, r := range resources {
			if i >= len(desc.Component.Resources) {
				break
			}
			if res, ok := r.(map[string]any); ok {
				setRawLabelValues(res["labels"], desc.Component.Resources[i].Labels)
			}
		}
	}
	if refs, ok := comp["componentReferences"].([]any); ok {
		for i, r := range refs {
			if i >= len(desc.Component.References) {
				break
			}
			if ref, ok := r.(map[string]any); ok {
				setRawLabelValues(ref["labels"], desc.Component.References[i].Labels)
			}
		}
	}
}

func setRawLabelValues(labelsAny any, src []v2.Label) {
	labels, ok := labelsAny.([]any)
	if !ok {
		return
	}
	for j, l := range labels {
		if j >= len(src) {
			break
		}
		if lm, ok := l.(map[string]any); ok {
			lm["value"] = src[j].Value
		}
	}
}

// transformDescriptor keeps only meta and component (dropping signatures and
// nestedDigests) and applies the component transforms. meta is kept verbatim.
func transformDescriptor(m map[string]any) (map[string]any, error) {
	out := map[string]any{}
	if meta, ok := m["meta"]; ok {
		out["meta"] = meta
	}
	comp, ok := m["component"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("descriptor is missing a component object")
	}
	if err := transformComponent(comp); err != nil {
		return nil, err
	}
	out["component"] = comp
	return out, nil
}

// transformComponent applies the v1 field selection to the component dict in
// place.
func transformComponent(c map[string]any) error {
	delete(c, "repositoryContexts")
	delete(c, "sources")

	defaultEmptyArray(c, "resources")
	defaultEmptyArray(c, "componentReferences")

	normaliseProvider(c)

	if err := normaliseCreationTime(c); err != nil {
		return err
	}

	if err := normaliseElementLabels(c); err != nil {
		return err
	}

	resources, _ := c["resources"].([]any)
	if err := injectDuplicateIdentityVersion(resources); err != nil {
		return err
	}
	for _, r := range resources {
		if res, ok := r.(map[string]any); ok {
			if err := transformResource(res); err != nil {
				return err
			}
		}
	}
	c["resources"] = transformElementList(resources)

	refs, _ := c["componentReferences"].([]any)
	for _, r := range refs {
		if ref, ok := r.(map[string]any); ok {
			if err := transformReference(ref); err != nil {
				return err
			}
		}
	}
	c["componentReferences"] = transformElementList(refs)
	return nil
}

// transformElementList normalises each element of an OCM list (resources,
// componentReferences, labels) into the entry form, leaving the list itself as
// a raw []any. This mirrors cc-utils, which normalises these lists one element
// at a time rather than recursing into arrays generically.
func transformElementList(list []any) []any {
	out := make([]any, len(list))
	for i, e := range list {
		out[i] = entry.Transform(e)
	}
	return out
}

// transformResource drops the transport-only fields, forces an explicit
// extraIdentity null when empty, and applies the label rule. The digest is kept
// verbatim, including the EXCLUDE-FROM-SIGNATURE sentinel. (cc-utils drops the
// digest for a resource with no access, but ConvertFromV2 rejects an access-less
// resource upstream, so that case never reaches normalisation here.)
func transformResource(res map[string]any) error {
	delete(res, "access")
	delete(res, "srcRefs")
	nullExtraIdentity(res)
	return normaliseElementLabels(res)
}

// transformReference forces an explicit extraIdentity null when empty and
// applies the label rule. The digest is the persisted recursive v1 digest of
// the child and is kept verbatim.
func transformReference(ref map[string]any) error {
	nullExtraIdentity(ref)
	return normaliseElementLabels(ref)
}

// defaultEmptyArray coerces a null or absent field to an empty array so that
// resources/componentReferences serialise as [] rather than null.
func defaultEmptyArray(m map[string]any, key string) {
	if v, ok := m[key]; !ok || v == nil {
		m[key] = []any{}
	}
}

// normaliseProvider re-emits a structured provider (a JSON-object string
// produced by ConvertToV2) as the original object; a bare provider string is
// kept as-is.
func normaliseProvider(c map[string]any) {
	s, ok := c["provider"].(string)
	if !ok {
		return
	}
	if !strings.HasPrefix(strings.TrimSpace(s), "{") {
		return
	}
	var obj map[string]any
	if err := json.Unmarshal([]byte(s), &obj); err == nil {
		c["provider"] = obj
	}
}

// normaliseCreationTime canonicalises a present creationTime string through the
// v2 Timestamp type (RFC3339 → UTC, rounded to the second, …Z). An absent or
// empty value was already dropped by omitempty.
func normaliseCreationTime(c map[string]any) error {
	s, ok := c["creationTime"].(string)
	if !ok || s == "" {
		return nil
	}
	var ts v2.Timestamp
	if err := ts.UnmarshalJSON([]byte(strconv.Quote(s))); err != nil {
		return fmt.Errorf("parsing creationTime %q: %w", s, err)
	}
	b, err := ts.MarshalJSON()
	if err != nil {
		return fmt.Errorf("formatting creationTime %q: %w", s, err)
	}
	var canon string
	if err := json.Unmarshal(b, &canon); err != nil {
		return fmt.Errorf("decoding creationTime %q: %w", s, err)
	}
	c["creationTime"] = canon
	return nil
}

// nullExtraIdentity injects an explicit null extraIdentity when it is absent or
// empty, so the normalised form always carries the key.
func nullExtraIdentity(m map[string]any) {
	ei, ok := m["extraIdentity"]
	if !ok {
		m["extraIdentity"] = nil
		return
	}
	if em, ok := ei.(map[string]any); ok && len(em) == 0 {
		m["extraIdentity"] = nil
	}
}

// normaliseElementLabels applies the label rule to the element's labels field,
// dropping the field entirely when no signing labels remain. Each kept label's
// value is normalised through entry.TransformRaw so that arrays keep source key
// order.
func normaliseElementLabels(m map[string]any) error {
	labels, ok := m["labels"].([]any)
	if !ok {
		return nil
	}
	kept := make([]any, 0, len(labels))
	for _, l := range labels {
		lm, ok := l.(map[string]any)
		if !ok {
			continue
		}
		sig := lm["signing"]
		if sig != true && sig != "true" {
			continue
		}
		value, err := normaliseLabelValue(lm["value"])
		if err != nil {
			return err
		}
		label := map[string]any{
			"name":    lm["name"],
			"value":   value,
			"signing": lm["signing"],
		}
		if ver, ok := lm["version"].(string); ok && ver != "" {
			label["version"] = ver
		}
		kept = append(kept, label)
	}
	if len(kept) == 0 {
		delete(m, "labels")
		return nil
	}
	m["labels"] = transformElementList(kept)
	return nil
}

// normaliseLabelValue normalises a label value. preserveRawLabelValues leaves it
// as a json.RawMessage so order is kept; a non-raw value (should not occur) is
// handled via the generic engine transform.
func normaliseLabelValue(v any) (any, error) {
	if raw, ok := v.(json.RawMessage); ok {
		return entry.TransformRaw(raw)
	}
	return entry.Transform(v), nil
}

// injectDuplicateIdentityVersion mirrors cc-utils' OCM-cli compat behaviour:
// for a resource whose identity (name + full extraIdentity) is shared by a
// later peer, and which has no version in its extraIdentity, the resource
// version is injected as extraIdentity["version"]. The last member of a
// duplicate group is left unchanged. It must run before extraIdentity is
// nulled, as it reads the raw extraIdentity.
func injectDuplicateIdentityVersion(resources []any) error {
	ids := make([]string, len(resources))
	for i, r := range resources {
		res, _ := r.(map[string]any)
		id, err := resourceIdentity(res)
		if err != nil {
			return err
		}
		ids[i] = id
	}
	for i, r := range resources {
		res, ok := r.(map[string]any)
		if !ok {
			continue
		}
		ei, _ := res["extraIdentity"].(map[string]any)
		if _, hasVersion := ei["version"]; hasVersion {
			continue
		}
		for j := i + 1; j < len(resources); j++ {
			if ids[j] == ids[i] {
				if ei == nil {
					ei = map[string]any{}
					res["extraIdentity"] = ei
				}
				ei["version"] = res["version"]
				break
			}
		}
	}
	return nil
}

// resourceIdentity builds a stable key from a resource's name and full
// extraIdentity, matching cc-utils' Resource.identity() (which includes an
// explicit extraIdentity["version"]). An absent extraIdentity is treated as
// empty. Name and extraIdentity are encoded as structured JSON (not
// delimiter-joined) so identity values containing ';' or '=' cannot collide
// across distinct resources.
func resourceIdentity(res map[string]any) (string, error) {
	if res == nil {
		return "", nil
	}
	ei := map[string]any{}
	if m, ok := res["extraIdentity"].(map[string]any); ok {
		for k, v := range m {
			ei[k] = v
		}
	}
	b, err := json.Marshal(map[string]any{"name": res["name"], "extraIdentity": ei})
	if err != nil {
		return "", fmt.Errorf("encoding resource identity: %w", err)
	}
	return string(b), nil
}
