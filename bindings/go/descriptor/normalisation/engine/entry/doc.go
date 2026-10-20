// Package entry implements the list-of-single-key-maps serialization behind the
// legacy jsonNormalisation/v1 signing algorithm.
//
// The reference implementation is gardener's cc-utils; output must be
// byte-identical to its normalise_obj so that sha256 over the bytes reproduces
// the signed digest:
//   - repo:    https://github.com/gardener/cc-utils
//   - sign.py: https://github.com/gardener/cc-utils/blob/master/ocm/sign.py
//
// # Core mechanism
//
//   - Objects become a JSON array of single-key objects, sorted by key; the
//     engine recurses into object values only (mirroring normalise_obj).
//   - Arrays and scalars are left untouched. The OCM element lists (resources,
//     componentReferences, labels) are normalised element-wise by the json/v1
//     algorithm before they reach the engine, not by generic array recursion.
//   - Marshalling is compact: no spaces, HTML characters &<> kept literal,
//     non-ASCII escaped to \uXXXX, and non-integral floats re-spelled to match
//     Python's json.dumps (see marshal / pythoniseNumbers).
//   - Input already arrives storage-normalised: json/v1 renders the descriptor
//     through a sigs.k8s.io/yaml round-trip, so object keys are already sorted
//     and numbers already collapsed to float64 — matching what signing and
//     verification read from the OCI store.
//
// The engine is deliberately OCM-agnostic: it operates on an arbitrary any tree
// (map[string]any, []any, scalars, explicit nil). Field selection and
// OCM-specific transforms live in the json/v1 algorithm package.
//
// # Scope and gap
//
// Normalisation is purely offline and assumes resource and reference digests are
// already persisted in the descriptor. cc-utils instead resolves them on the fly
// at sign time (per-resource artifact digests, recursive reference digests), so a
// descriptor stored without those digests — e.g. real gardener shipments whose
// resources use relativeOciReference — cannot be reproduced offline by this
// engine alone; the digests must first be resolved and persisted.
package entry
