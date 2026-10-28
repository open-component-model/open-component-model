// Package v4alpha3 provides JSON normalisation for Open Component Model
// descriptors. It is the default normalisation for new signatures and the
// forward-looking member of the v4 family: jsonNormalisation/v4alpha2 realigns
// with legacy v3 (keeps label.version, reference labels unfiltered), and
// v4alpha3 additionally filters reference labels signing-only.
//
// It shares all field selection and canonicalisation with v4alpha1 (via the
// internal/common primitives) and differs only in label handling, in two ways:
//
//   - label.version is signed. v4alpha1 drops it; legacy jsonNormalisation/v3,
//     cc-utils and jsonNormalisation/v1 all sign it. v4alpha3 re-converges with
//     that intent. This closes a gap, not a new policy: a label is still signed
//     only when it sets signing: true (opt-in is unchanged).
//   - reference labels are filtered signing-only like every other label
//     position. Legacy v3 keyed its reference rule "references" while the
//     descriptor JSON key is componentReferences, so the rule never matched
//     and every reference label was signed; v4alpha2 reproduces that, and
//     v4alpha1 only strips label.version from reference labels. v4alpha3
//     applies the full signing-only label rule under the correct key.
//
// Because these change the signed bytes, v4alpha3 is a separate registered
// algorithm; v4alpha1 stays frozen so existing signatures keep verifying.
package v4alpha3
