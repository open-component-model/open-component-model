// Package v1 implements the legacy jsonNormalisation/v1 component descriptor
// normalisation, byte-compatible with gardener's cc-utils (ocm/sign.py). It
// exists so OCM v2 can verify signatures produced by that tooling.
//
// It operates on the v2 descriptor produced by descruntime.ConvertToV2 and
// serialises via the entry engine (package engine/entry), NOT the JCS
// canonicalizer used by jsonNormalisation/v4alpha1. The two algorithms differ
// fundamentally and share no serialization:
//
//   - v4alpha1 canonicalises to a JSON object (RFC 8785); v1 serialises to a
//     nested list-of-single-key-maps sorted by key.
//   - v1 keeps explicit nulls (e.g. extraIdentity: null); JCS drops nil fields.
//   - v1 keeps the meta section ({"meta":[{"schemaVersion":"v2"}]}); v4alpha1
//     excludes it.
//   - v1 emits a bare provider string as-is ({"provider":"ACME"}); v4alpha1
//     wraps it as a map.
//   - v1 canonicalises creationTime the way cc-utils does: the wall-clock time
//     is rounded to the second and relabelled with a literal Z, WITHOUT
//     converting the offset to UTC (07:16:30.6+02:00 → 07:16:31Z); v4alpha1
//     hashes the raw string.
//   - v1 canonicalises resource types through cc-utils' ArtefactType alias
//     table (optional /v1 suffix stripped; e.g. helmChart → helmChart/v1,
//     ociImage/v1 → ociImage, filesystem → directoryTree); unknown types are
//     kept verbatim.
//
// Field selection (drop repositoryContexts/sources/access/srcRefs/signatures/
// nestedDigests, default empty resources/componentReferences to [], inject an
// explicit null extraIdentity, keep only signing labels, duplicate-identity
// version injection) is done on the map[string]any in this package; the entry
// engine stays minimal and OCM-agnostic. Persisted digests, including the
// EXCLUDE-FROM-SIGNATURE sentinel, are kept verbatim — this is a pure, offline
// serialization of the stored descriptor with no digest resolution or graph
// recursion.
//
// Numbers in signing-label values are decoded with UseNumber and re-spelled to
// Python's json.dumps form: integer tokens keep full precision (matching
// cc-utils' arbitrary-precision ints) and fractional values use Python's float
// repr (1e-05, not Go's 0.00001). These spellings intentionally DIVERGE from
// OCM v1, which routes every number through float64; the OCM v1 variant (see
// below) uses the float64 spelling.
//
// # Storage-read limitations
//
// Two cc-utils cases cannot be reproduced, both because OCM always reads a
// descriptor from storage through sigs.k8s.io/yaml, which rewrites it before
// normalisation runs. There is no reachable OCM signing path that bypasses this
// read, so the original bytes are already gone when normalisation starts — a
// cc-utils signature exercising either case will not verify:
//
//   - An integral float such as 1.0 is collapsed to 1, while cc-utils keeps 1.0.
//   - Object keys nested inside a list-valued signing label are sorted
//     (e.g. [{"a2":1,"a10":2}] is read back as [{"a10":2,"a2":1}]), while
//     cc-utils preserves source order. The storage read sorts keys everywhere,
//     including inside arrays, so no order survives to be preserved here.
//
// # Compatibility: cc-utils vs OCM v1
//
// jsonNormalisation/v1 also has a Go implementation in OCM v1
// (api/ocm/compdesc/normalizations/jsonv1 + legacy). It is NOT byte-identical to
// cc-utils. The registered algorithm (Normalise) targets cc-utils, since it
// signed the gardener artifacts OCM v2 must verify. NormaliseOCMv1 reproduces
// the OCM v1 variant, and algorithms.Verify tries it when the cc-utils digest
// does not match, so signatures from both tools verify. If neither matches,
// verification fails with a digest mismatch that names the OCM v1 variant and
// recommends re-signing.
//
// The fallback runs only when every number in the descriptor survives a
// float64 round trip exactly (OCMv1Applicable: each number token must equal,
// by value, the shortest spelling of its float64, so 0.00001 and 2^53 pass but
// 2^53+1 and 0.10000000000000001 do not). OCM v1 maps different numbers such
// as 2^53 and 2^53+1 to the same bytes; trying the fallback for them would let
// a changed number pass a cc-utils signature. This is deliberate, so that
// cc-utils signatures stay exact: an OCM v1 signature over such a number is
// not verified and fails with a digest mismatch that says the descriptor holds
// a number the OCM v1 variant cannot represent exactly and recommends
// re-signing.
//
// For the same reason the fallback is not tried when the OCM v1 variant drops
// unsigned provider labels and the result equals the cc-utils form of the
// descriptor without them (for example a provider whose only labels are
// unsigned, and nothing else that differs between the variants). Those bytes
// could be a cc-utils signature, so accepting them would let a provider label
// be added to a cc-utils-signed descriptor; such an OCM v1 signature fails
// with a re-sign error instead.
//
// The OCM v1 variant differs in:
//
//   - Resource types: kept verbatim, not canonicalised through the cc-utils
//     ArtefactType table.
//   - Provider labels: filtered to signing labels like all other labels.
//   - Numbers: decoded as float64 and spelled by Go's encoding/json (0.00001;
//     integers above 2^53 lose precision).
//   - HTML characters &, < and > are escaped to \u0026, \u003c, \u003e,
//     and the line terminators U+2028 and U+2029 to \u2028 and \u2029.
//   - Objects inside arrays, such as a label value [{"b":1,"a":2}], are
//     rewritten to the entry form ([[{"a":2},{"b":1}]]); cc-utils keeps them
//     as they are. Those bytes are also the cc-utils form of a value holding
//     [[{"a":2},{"b":1}]], so the verify fallback refuses such descriptors
//     (ErrAmbiguousArrayObjects) and an OCM v1 signature over them needs a
//     re-sign.
//
// Not reproduced in the OCM v1 variant (a descriptor exercising them verifies
// only if signed by cc-utils, or fails with the error above):
//
//   - Duplicate-identity version injection: OCM v1 also scans backward and
//     overwrites an existing version; this package follows cc-utils.
//   - A resource whose access type is none keeps its digest; OCM v1 drops it
//     (MapResourcesWithNoneAccess).
package v1
