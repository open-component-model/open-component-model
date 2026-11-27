package v1

import (
	jsonstd "encoding/json"
	"encoding/json/v2"
	"errors"
	"fmt"
	"math/big"
	"strconv"
	"strings"
	"time"

	"sigs.k8s.io/yaml"

	norms "ocm.software/open-component-model/bindings/go/descriptor/normalisation"
	"ocm.software/open-component-model/bindings/go/descriptor/normalisation/engine/entry"
	descruntime "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// Algorithm is the registered name for this normalisation algorithm. It
// identifies the legacy gardener/cc-utils serialization (ocm/sign.py) that
// precedes jsonNormalisation/v4alpha1. It is verify-only: existing v1
// signatures, pins and reference digests are verified, but a new v1 signature
// must never be minted (enforced at the signing boundary by
// signing.EnsureSignable). Use jsonNormalisation/v4alpha3 for new signatures.
//
// Deprecated: jsonNormalisation/v1 is a legacy cc-utils compatibility format
// kept for verification only; do not use it to produce new signatures (use
// jsonNormalisation/v4alpha3). It stays registered so existing signatures and
// pins remain verifiable.
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
	return normalise(cd, ccUtils)
}

// NormaliseOCMv1 serialises the descriptor the way the OCM v1 CLI computes
// jsonNormalisation/v1. It differs from the cc-utils form returned by the
// registered algorithm in resource types (kept verbatim), provider labels
// (filtered to signing labels), objects inside arrays (rewritten to the entry
// form), and number and string spelling (Go's encoding/json: float64 numbers,
// HTML characters and U+2028/U+2029 escaped). algorithms.Verify uses it as a
// fallback when the cc-utils digest does not match, but only if
// OCMv1Applicable accepts the descriptor: every number must survive the
// float64 round trip exactly, otherwise two different numbers could share one
// OCM v1 form and a cc-utils signature would stop being exact.
func NormaliseOCMv1(cd *descruntime.Descriptor) ([]byte, error) {
	return normalise(cd, ocmV1)
}

// ErrInexactNumber is returned by OCMv1Applicable when the descriptor holds a
// number the OCM v1 variant (float64) cannot represent exactly.
var ErrInexactNumber = errors.New("descriptor contains a number the OCM v1 variant of " + Algorithm + " cannot represent exactly")

// OCMv1Applicable reports whether the OCM v1 variant can serialise the
// descriptor without losing information. It walks every number token (labels,
// extraIdentity, the provider object, everything under component) and requires
// that the token is the shortest spelling of its own float64, compared by value
// (1e-05 and 0.00001 are equal). It rejects, for example, 9007199254740993 and
// 0.10000000000000001, which OCM v1 would round to a neighbouring number; the
// returned error wraps ErrInexactNumber. It also rejects, with
// ErrAmbiguousArrayObjects, a descriptor with an object inside an array of a
// signed value, and, with ErrAmbiguousProviderLabels, a descriptor whose OCM v1
// form is the cc-utils form of the descriptor without its unsigned provider
// labels.
func OCMv1Applicable(cd *descruntime.Descriptor) error {
	scheme := runtime.NewScheme(runtime.WithAllowUnknown())
	desc, err := descruntime.ConvertToV2(scheme, cd)
	if err != nil {
		return err
	}
	m, err := toMap(desc, ccUtils)
	if err != nil {
		return err
	}
	if comp, ok := m["component"].(map[string]any); ok {
		if s, ok := comp["provider"].(string); ok {
			var obj any
			if err := json.Unmarshal([]byte(s), &obj, runtime.PreserveJSONNumbers); err == nil {
				comp["provider"] = obj
			}
		}
	}
	if err := checkExactNumbers(m); err != nil {
		return err
	}
	if err := checkNoObjectsInArrays(m); err != nil {
		return err
	}
	return checkUnambiguousProvider(cd)
}

// ErrAmbiguousArrayObjects is returned by OCMv1Applicable when a signed value
// holds an object inside an array.
var ErrAmbiguousArrayObjects = errors.New("the OCM v1 variant of " + Algorithm + " rewrites objects inside arrays, and the result is also the cc-utils form of a descriptor holding the rewritten arrays")

// checkNoObjectsInArrays rejects a descriptor with an object inside an array of
// a signed value (a label value, a digest). OCM v1 rewrites
// [{"b":1,"a":2}] to [[{"a":2},{"b":1}]], which is also how cc-utils serialises
// a descriptor holding [[{"a":2},{"b":1}]], so accepting the fallback would let
// a cc-utils-signed value change shape. The element lists (resources,
// componentReferences, labels) are structure, not values, and are walked into.
func checkNoObjectsInArrays(m map[string]any) error {
	comp, ok := m["component"].(map[string]any)
	if !ok {
		return nil
	}
	for k, v := range comp {
		switch k {
		case "sources", "repositoryContexts":
			// not part of the normalised bytes
		case "resources", "componentReferences":
			list, _ := v.([]any)
			for _, e := range list {
				if err := checkElementFields(k, e); err != nil {
					return err
				}
			}
		case "labels":
			if err := checkLabels(k, v); err != nil {
				return err
			}
		case "provider":
			if err := checkElementFields(k, v); err != nil {
				return err
			}
		default:
			if err := checkValue(k, v, false); err != nil {
				return err
			}
		}
	}
	return nil
}

func checkElementFields(path string, e any) error {
	el, ok := e.(map[string]any)
	if !ok {
		return checkValue(path, e, false)
	}
	for k, v := range el {
		var err error
		switch k {
		case "access", "srcRefs":
			// not part of the normalised bytes
		case "labels":
			err = checkLabels(path+".labels", v)
		default:
			err = checkValue(path+"."+k, v, false)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

func checkLabels(path string, v any) error {
	list, ok := v.([]any)
	if !ok {
		return checkValue(path, v, false)
	}
	for _, l := range list {
		label, ok := l.(map[string]any)
		if !ok {
			return checkValue(path, l, false)
		}
		for k, lv := range label {
			if err := checkValue(path+"."+k, lv, false); err != nil {
				return err
			}
		}
	}
	return nil
}

func checkValue(path string, v any, inArray bool) error {
	switch t := v.(type) {
	case map[string]any:
		if inArray {
			return fmt.Errorf("%w: %s", ErrAmbiguousArrayObjects, path)
		}
		for k, e := range t {
			if err := checkValue(path+"."+k, e, false); err != nil {
				return err
			}
		}
	case []any:
		for _, e := range t {
			if err := checkValue(path, e, true); err != nil {
				return err
			}
		}
	}
	return nil
}

// ErrAmbiguousProviderLabels is returned by OCMv1Applicable when the OCM v1
// variant drops unsigned provider labels and the result is also the cc-utils
// form of the descriptor without them.
var ErrAmbiguousProviderLabels = errors.New("the OCM v1 variant of " + Algorithm + " drops the unsigned provider labels, and the result is also the cc-utils form of the descriptor without them")

// checkUnambiguousProvider rejects a descriptor whose OCM v1 form equals the
// cc-utils form of the same descriptor with its provider labels filtered to
// signing labels. cc-utils signs every provider label, OCM v1 only the signing
// ones, so such bytes could be a cc-utils signature over the descriptor without
// the unsigned provider labels: accepting the fallback would let a provider
// label be injected into a cc-utils-signed descriptor.
func checkUnambiguousProvider(cd *descruntime.Descriptor) error {
	labels := cd.Component.Provider.Labels
	signing := make([]descruntime.Label, 0, len(labels))
	for _, l := range labels {
		if l.Signing {
			signing = append(signing, l)
		}
	}
	if len(signing) == len(labels) {
		return nil
	}
	ocm, err := normalise(cd, ocmV1)
	if err != nil {
		return err
	}
	// With no signing label left, the original may have had an empty label
	// list (a structured provider) or none at all (a bare provider string).
	candidates := [][]descruntime.Label{signing}
	if len(signing) == 0 {
		candidates = append(candidates, nil)
	}
	for _, kept := range candidates {
		stripped := *cd
		stripped.Component.Provider.Labels = kept
		cc, err := normalise(&stripped, ccUtils)
		if err != nil {
			return err
		}
		if string(ocm) == string(cc) {
			return ErrAmbiguousProviderLabels
		}
	}
	return nil
}

func checkExactNumbers(v any) error {
	switch t := v.(type) {
	case map[string]any:
		for _, e := range t {
			if err := checkExactNumbers(e); err != nil {
				return err
			}
		}
	case []any:
		for _, e := range t {
			if err := checkExactNumbers(e); err != nil {
				return err
			}
		}
	case jsonstd.Number:
		if !float64Exact(string(t)) {
			return fmt.Errorf("%w: %s", ErrInexactNumber, t)
		}
	}
	return nil
}

// float64Exact reports whether token has the same value as the shortest
// spelling of its float64.
func float64Exact(token string) bool {
	f, err := strconv.ParseFloat(token, 64)
	if err != nil {
		return false
	}
	want, ok := new(big.Rat).SetString(token)
	if !ok {
		return false
	}
	got, ok := new(big.Rat).SetString(strconv.FormatFloat(f, 'g', -1, 64))
	if !ok {
		return false
	}
	return want.Cmp(got) == 0
}

// variant selects which legacy implementation of jsonNormalisation/v1 is
// reproduced.
type variant int

const (
	ccUtils variant = iota
	ocmV1
)

func normalise(cd *descruntime.Descriptor, v variant) ([]byte, error) {
	scheme := runtime.NewScheme(runtime.WithAllowUnknown())
	desc, err := descruntime.ConvertToV2(scheme, cd)
	if err != nil {
		return nil, err
	}

	m, err := toMap(desc, v)
	if err != nil {
		return nil, err
	}

	out, err := transformDescriptor(m, v)
	if err != nil {
		return nil, err
	}
	if v == ocmV1 {
		return entry.NormaliseGoJSON(out)
	}
	return entry.Normalise(out)
}

// toMap renders the v2 descriptor into the generic map the transforms operate
// on, modelling the storage round-trip rather than the in-memory bytes. The OCI
// store writes descriptors with yaml.Marshal and reads them back with
// sigs.k8s.io/yaml (oci/spec/descriptor/{encode,decode}.go), which sorts object
// keys. Signing and verification always operate on this stored form, so we
// normalise through the same representation. Expanding Label.Value
// (json.RawMessage) also falls out of this for free.
//
// Numbers are decoded with [runtime.PreserveJSONNumbers] so integer label values keep full
// precision (cc-utils serialises them via Python json.dumps, which never loses
// precision); the entry engine then re-spells them to match Python. The one
// value this cannot recover is an integral float such as 1.0: YAMLToJSON in the
// store read collapses it to 1 before normalisation runs, whereas cc-utils
// keeps 1.0 — see the engine's number handling and the divergence tests.
func toMap(desc *v2.Descriptor, v variant) (map[string]any, error) {
	y, err := yaml.Marshal(desc)
	if err != nil {
		return nil, err
	}
	data, err := yaml.YAMLToJSON(y)
	if err != nil {
		return nil, err
	}
	var m map[string]any
	// OCM v1 decodes every number into float64, so it keeps the default here.
	var opts []json.Options
	if v == ccUtils {
		opts = append(opts, runtime.PreserveJSONNumbers)
	}
	if err := json.Unmarshal(data, &m, opts...); err != nil {
		return nil, err
	}
	return m, nil
}

// transformDescriptor keeps only meta and component (dropping signatures and
// nestedDigests) and applies the component transforms. meta is kept verbatim.
func transformDescriptor(m map[string]any, v variant) (map[string]any, error) {
	out := map[string]any{}
	if meta, ok := m["meta"]; ok {
		out["meta"] = meta
	}
	comp, ok := m["component"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("descriptor is missing a component object")
	}
	if err := transformComponent(comp, v); err != nil {
		return nil, err
	}
	out["component"] = comp
	return out, nil
}

// transformComponent applies the v1 field selection to the component dict in
// place.
func transformComponent(c map[string]any, v variant) error {
	delete(c, "repositoryContexts")
	delete(c, "sources")

	defaultEmptyArray(c, "resources")
	defaultEmptyArray(c, "componentReferences")

	normaliseProvider(c, v)

	if err := normaliseCreationTime(c); err != nil {
		return err
	}

	normaliseElementLabels(c, v)

	resources, _ := c["resources"].([]any)
	if err := injectDuplicateIdentityVersion(resources); err != nil {
		return err
	}
	for _, r := range resources {
		if res, ok := r.(map[string]any); ok {
			transformResource(res, v)
		}
	}
	c["resources"] = transformElementList(resources, v)

	refs, _ := c["componentReferences"].([]any)
	for _, r := range refs {
		if ref, ok := r.(map[string]any); ok {
			transformReference(ref, v)
		}
	}
	c["componentReferences"] = transformElementList(refs, v)
	return nil
}

// transformElementList normalises each element of an OCM list (resources,
// componentReferences, labels) into the entry form, leaving the list itself as
// a raw []any. This mirrors cc-utils, which normalises these lists one element
// at a time rather than recursing into arrays generically. The OCM v1 variant
// keeps the list raw: entry.NormaliseGoJSON descends into every array.
func transformElementList(list []any, v variant) []any {
	if v == ocmV1 {
		return list
	}
	out := make([]any, len(list))
	for i, e := range list {
		out[i] = entry.Transform(e)
	}
	return out
}

// ccutilsArtefactTypes mirrors cc-utils' ArtefactType alias table
// (ocm/__init__.py): on load it canonicalises resource types to the enum's
// serialised value. OCM v2 stores the type verbatim, so to match a cc-utils
// signature we apply the same canonicalisation. Keys are the non-"/v1" spelling
// (cc-utils strips an optional "/v1" before resolving); each maps to the enum's
// serialised value. canonicaliseArtefactType does the lookup.
var ccutilsArtefactTypes = map[string]string{
	"blob":            "blob/v1",
	"cosignSignature": "cosignSignature",
	"directoryTree":   "directoryTree",
	"executable":      "executable",
	"filesystem":      "directoryTree",
	"git":             "git",
	"helmChart":       "helmChart/v1",
	"npmPackage":      "npmPackage",
	"ociArtifact":     "ociArtifact/v1",
	"ociImage":        "ociImage",
	"sbom":            "sbom",
}

// canonicaliseArtefactType mirrors cc-utils' enum_or_string(enum_type=
// ArtefactType, omit_v1_version=True) (ocm/__init__.py): a trailing "/v1" is
// optional and stripped before resolving, a resolved type is replaced by the
// enum's canonical spelling, and an unresolved type is kept verbatim (so an
// unknown "custom/v1" stays "custom/v1").
func canonicaliseArtefactType(t string) string {
	if stripped, hadSuffix := strings.CutSuffix(t, "/v1"); hadSuffix {
		if canon, ok := ccutilsArtefactTypes[stripped]; ok {
			return canon
		}
		return t
	}
	if canon, ok := ccutilsArtefactTypes[t]; ok {
		return canon
	}
	return t
}

// transformResource drops the transport-only fields, canonicalises the resource
// type to cc-utils' spelling, forces an explicit extraIdentity null when empty,
// and applies the label rule. The digest is kept verbatim, including the
// EXCLUDE-FROM-SIGNATURE sentinel. (cc-utils drops the digest for a resource
// with no access, but ConvertFromV2 rejects an access-less resource upstream,
// so that case never reaches normalisation here.)
func transformResource(res map[string]any, v variant) {
	delete(res, "access")
	delete(res, "srcRefs")
	if t, ok := res["type"].(string); ok && v == ccUtils {
		res["type"] = canonicaliseArtefactType(t)
	}
	nullExtraIdentity(res)
	normaliseElementLabels(res, v)
}

// transformReference forces an explicit extraIdentity null when empty and
// applies the label rule. The digest is the persisted recursive v1 digest of
// the child and is kept verbatim.
func transformReference(ref map[string]any, v variant) {
	nullExtraIdentity(ref)
	normaliseElementLabels(ref, v)
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
//
// For the OCM v1 variant the provider labels are filtered like every other
// label list (signing labels only); cc-utils leaves them untouched.
func normaliseProvider(c map[string]any, v variant) {
	s, ok := c["provider"].(string)
	if !ok {
		return
	}
	if !strings.HasPrefix(strings.TrimSpace(s), "{") {
		return
	}
	var obj map[string]any
	if err := json.Unmarshal([]byte(s), &obj); err == nil {
		if v == ocmV1 {
			normaliseElementLabels(obj, v)
		}
		c["provider"] = obj
	}
}

// normaliseCreationTime canonicalises a present creationTime the way cc-utils
// (ocm/sign.py) does: parse the RFC3339 value, round it to the second, and
// format the WALL-CLOCK time with a literal "Z" — the UTC offset is not
// converted, only relabelled. So 2026-06-19T07:16:30.6+02:00 becomes
// 2026-06-19T07:16:31Z (not 05:16:31Z). cc-utils rounds up when the fractional
// part is >= 0.5s (time.Round rounds half away from zero, which agrees for the
// microsecond-precision timestamps the store emits). An absent or empty value
// was already dropped by omitempty.
func normaliseCreationTime(c map[string]any) error {
	s, ok := c["creationTime"].(string)
	if !ok || s == "" {
		return nil
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return fmt.Errorf("parsing creationTime %q: %w", s, err)
	}
	t = t.Round(time.Second)
	c["creationTime"] = t.Format("2006-01-02T15:04:05") + "Z"
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
// dropping the field entirely when no signing labels remain.
func normaliseElementLabels(m map[string]any, v variant) {
	labels, ok := m["labels"].([]any)
	if !ok {
		return
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
		label := map[string]any{
			"name":    lm["name"],
			"value":   lm["value"],
			"signing": lm["signing"],
		}
		if ver, ok := lm["version"].(string); ok && ver != "" {
			label["version"] = ver
		}
		kept = append(kept, label)
	}
	if len(kept) == 0 {
		delete(m, "labels")
		return
	}
	m["labels"] = transformElementList(kept, v)
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
	b, err := json.Marshal(map[string]any{"name": res["name"], "extraIdentity": ei}, runtime.JSONOptions)
	if err != nil {
		return "", fmt.Errorf("encoding resource identity: %w", err)
	}
	return string(b), nil
}
