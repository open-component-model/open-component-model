// Package v4alpha2 provides JSON normalisation for Open Component Model
// descriptors. It is the successor to jsonNormalisation/v4alpha1 and the default
// normalisation for new signatures.
//
// It shares all field selection and canonicalisation with v4alpha1 (via the
// internal/common primitives) and differs only in label handling, in two ways:
//
//   - label.version is signed. v4alpha1 drops it; legacy jsonNormalisation/v3,
//     cc-utils and jsonNormalisation/v1 all sign it. v4alpha2 re-converges with
//     that intent. This closes a gap, not a new policy: a label is still signed
//     only when it sets signing: true (opt-in is unchanged).
//   - reference labels are filtered like every other label position. v4alpha1
//     keyed the reference rule "references" while the descriptor JSON key is
//     componentReferences, so it never matched and non-signing reference labels
//     were signed; v4alpha2 applies the full signing-only label rule under the
//     correct key. This deliberately diverges from legacy v3, which had the same
//     key bug and emitted reference labels unfiltered.
//
// Because these change the signed bytes, v4alpha2 is a separate registered
// algorithm; v4alpha1 stays frozen so existing signatures keep verifying.
package v4alpha2
