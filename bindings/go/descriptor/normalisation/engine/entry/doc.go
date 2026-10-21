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
//   - Arrays and scalars are left untouched; the engine never descends into an
//     array. cc-utils only normalises the named lists (resources,
//     componentReferences, labels) element-wise — any other array, e.g. a
//     list-valued signing label, must stay raw. Being OCM-agnostic the engine
//     can't tell them apart, so json/v1 transforms those element lists itself
//     (via Transform) before calling Normalise.
//   - Marshalling is compact: no spaces, HTML characters &<> kept literal,
//     non-ASCII escaped to \uXXXX, and non-integral floats re-spelled to match
//     Python's json.dumps (see marshal / pythoniseNumbers).
//   - Input already arrives storage-normalised: json/v1 renders the descriptor
//     through a sigs.k8s.io/yaml round-trip, so object keys are already sorted
//     and an integral float such as 1.0 is already collapsed to 1 — matching
//     what signing and verification read from the OCI store. json/v1 decodes
//     with UseNumber, so numbers arrive as json.Number and integers keep full
//     precision.
//
// The engine is deliberately OCM-agnostic: it operates on an arbitrary any tree
// (map[string]any, []any, scalars, explicit nil). Field selection and
// OCM-specific transforms live in the json/v1 algorithm package.
//
// # Example
//
// Each object is rewritten into an array of single-key objects sorted by key,
// recursing into object values; arrays and scalars are copied as-is. So
//
//	{"b": 2, "a": {"d": 4, "c": 3}, "l": [3, 1]}
//
// normalises to
//
//	[{"a":[{"c":3},{"d":4}]},{"b":2},{"l":[3,1]}]
//
// Note how a/b and the nested c/d are sorted and each wrapped in its own
// single-key object, while the array l keeps its order and stays a plain array
// (the engine never descends into or rewrites arrays).
//
// # Compatibility: cc-utils vs OCM v1
//
// jsonNormalisation/v1 has two legacy implementations that are NOT
// byte-identical: gardener's cc-utils (Python) and OCM v1 (Go). We deliberately
// target cc-utils, because it produced the signatures OCM v2 needs to verify;
// where the two disagree we match cc-utils and accept divergence from OCM v1.
//
// Engine-level differences from OCM v1:
//
//   - Numbers are re-spelled to Python's json.dumps form (see pythoniseNumbers):
//     big integers keep full precision and fractional values use Python's repr
//     (1e-05, not 0.00001). OCM v1 routes every number through float64. Neither
//     target can reproduce an integral float like 1.0 offline: the storage read
//     (sigs.k8s.io/yaml) collapses it to 1 before normalisation, while cc-utils
//     keeps 1.0.
//   - HTML characters &, < and > are kept literal (Python json.dumps default);
//     OCM v1 escapes them to \u0026, \u003c, \u003e.
//
// Field-selection divergences from OCM v1 (provider labels, duplicate-identity
// injection, none-access resources, …) are decided in the json/v1 algorithm
// package; see its package doc. Normalisation itself is a pure offline
// serialization that assumes digests are already persisted (no resolution).
package entry
