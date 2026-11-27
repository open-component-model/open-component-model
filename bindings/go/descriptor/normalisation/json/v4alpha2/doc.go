// Package v4alpha2 provides JSON normalisation for Open Component Model
// descriptors. It is a faithful re-implementation of legacy
// jsonNormalisation/v3, except for null-valued entries (see Null values
// below), and the algorithm the "jsonNormalisation/v3" alias resolves to (see
// algorithms.LegacyV3).
//
// It shares all field selection and canonicalisation with v4alpha1 (via the
// internal/common primitives) and differs only in label handling, in one way:
//
//   - label.version is signed again. v4alpha1 drops it everywhere (including on
//     reference labels); legacy jsonNormalisation/v3, cc-utils and
//     jsonNormalisation/v1 all sign it. Keeping it makes v4alpha2 byte-identical
//     to v3 except for null-valued entries, so v3 signatures over versioned
//     labels verify. A label is still signed only when it sets signing: true
//     (opt-in is unchanged).
//
// Reference labels stay unfiltered, exactly as v3 emitted them (v3's reference
// rule was keyed "references" while the JSON key is "componentReferences", so it
// never matched). Signing-only filtering of reference labels is the deliberate
// forward change shipped in jsonNormalisation/v4alpha3.
//
// Because keeping label.version changes the signed bytes relative to v4alpha1,
// v4alpha2 is a separate registered algorithm; v4alpha1 stays frozen so existing
// signatures keep verifying.
//
// Null values: legacy v3 keeps a null-valued map entry, such as a signed label
// with value: null (at any depth of the value, and on reference labels), as
// "name":null; the registered v4alpha2 algorithm drops it, as every
// v4alpha* algorithm does. v4alpha2 is not changed, because OCM v2 records
// digests minted with the jsonNormalisation/v3 alias as v4alpha2. Instead,
// NormaliseLegacyV3 produces the v3 bytes, and only digests that record
// "jsonNormalisation/v3" are verified against it (see algorithms.Verify).
//
// Known gap: legacy v3 emits reference labels unfiltered, so a reference
// label's merge attribute is part of its signed bytes (at every other position
// only name, version, value and signing are signed). Neither v2.Label nor
// runtime.Label carries merge, so it is dropped on decode and a v3 signature
// over a reference label with merge does not verify (fails closed). Closing it
// needs merge on the label types.
package v4alpha2
