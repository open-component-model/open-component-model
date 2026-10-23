// Package v4alpha2 provides JSON normalisation for Open Component Model
// descriptors. It is a faithful re-implementation of legacy
// jsonNormalisation/v3 and the algorithm the "jsonNormalisation/v3" alias
// resolves to (see signing.LegacyNormalisationAlgo).
//
// It shares all field selection and canonicalisation with v4alpha1 (via the
// internal/common primitives) and differs only in label handling, in one way:
//
//   - label.version is signed again. v4alpha1 drops it everywhere (including on
//     reference labels); legacy jsonNormalisation/v3, cc-utils and
//     jsonNormalisation/v1 all sign it. Keeping it makes v4alpha2 byte-identical
//     to v3, which also closes the verification gap for existing v3 signatures
//     over versioned labels (v3 → v4alpha1 previously diverged). A label is
//     still signed only when it sets signing: true (opt-in is unchanged).
//
// Reference labels stay unfiltered, exactly as v3 emitted them (v3's reference
// rule was keyed "references" while the JSON key is "componentReferences", so it
// never matched). Signing-only filtering of reference labels is the deliberate
// forward change shipped in jsonNormalisation/v4alpha3.
//
// Because keeping label.version changes the signed bytes relative to v4alpha1,
// v4alpha2 is a separate registered algorithm; v4alpha1 stays frozen so existing
// signatures keep verifying.
package v4alpha2
