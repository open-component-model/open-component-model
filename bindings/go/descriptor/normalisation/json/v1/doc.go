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
//   - v1 canonicalises creationTime through the v2 Timestamp type (RFC3339 →
//     UTC, rounded to the second, trailing Z); v4alpha1 hashes the raw string.
//
// Field selection (drop repositoryContexts/sources/access/srcRefs/signatures/
// nestedDigests, default empty resources/componentReferences to [], inject an
// explicit null extraIdentity, keep only signing labels, duplicate-identity
// version injection) is done on the map[string]any in this package; the entry
// engine stays minimal and OCM-agnostic. Persisted digests, including the
// EXCLUDE-FROM-SIGNATURE sentinel, are kept verbatim — this is a pure, offline
// serialization of the stored descriptor with no digest resolution or graph
// recursion.
package v1
