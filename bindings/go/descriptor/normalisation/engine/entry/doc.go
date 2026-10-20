// Package entry implements the list-of-single-key-maps serialization used by
// the legacy jsonNormalisation/v1 algorithm.
//
// Unlike the JCS engine (RFC 8785, see package jcs), which canonicalises to a
// JSON object, this engine recursively rewrites every JSON object into a JSON
// array of single-key objects sorted by key. Values that are themselves
// objects are recursed into; arrays and scalars are left untouched — mirroring
// cc-utils' normalise_obj, which only descends into dicts. The OCM-specific
// lists (resources, componentReferences, labels) are normalised element-wise by
// the json/v1 algorithm before they reach this engine. The result is marshalled
// compactly (no spaces, HTML characters &<> kept literal, non-ASCII escaped to
// \uXXXX) so that sha256 over the bytes matches the digests produced by
// gardener's cc-utils (ocm/sign.py, which serialises with Python json.dumps).
//
// Descriptors reach this engine via the storage representation (sigs.k8s.io/yaml
// round-trip in json/v1), so object keys are already sorted and numbers already
// collapsed to float64; the engine only re-spells non-integral floats to match
// Python's json.dumps (see marshal/pythoniseNumbers) and otherwise preserves
// order and values as received.
//
// The engine is deliberately OCM-agnostic: it operates on an arbitrary
// any tree (map[string]any, []any, scalars, explicit nil). Field selection
// and OCM-specific transforms live in the json/v1 algorithm package.
//
// Reference implementation: gardener cc-utils, https://github.com/gardener/cc-utils
// (ocm/sign.py: https://github.com/gardener/cc-utils/blob/master/ocm/sign.py).
package entry
