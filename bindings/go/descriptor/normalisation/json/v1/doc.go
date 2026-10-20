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
// OCM v1, which routes every number through float64; cc-utils is the primary
// compatibility target. The one value this cannot reproduce is an integral
// float such as 1.0: the store read (sigs.k8s.io/yaml) collapses it to 1 before
// normalisation runs, while cc-utils keeps 1.0.
//
// # Compatibility: cc-utils vs OCM v1
//
// jsonNormalisation/v1 also has a Go implementation in OCM v1
// (api/ocm/compdesc/normalizations/jsonv1 + legacy). It is NOT byte-identical to
// cc-utils. We deliberately align with cc-utils, since it signed the gardener
// artifacts OCM v2 must verify; where the two disagree we match cc-utils and
// knowingly diverge from OCM v1. An OCM-v1-signed descriptor that exercises one
// of these cases will not verify:
//
//   - Numbers: cc-utils' Python spellings (above), not OCM v1's float64.
//   - HTML characters &, < and > are kept literal (done in the entry engine);
//     OCM v1 escapes them to \u0026, \u003c, \u003e.
//   - Provider: a bare provider string is emitted as-is; OCM v1 applies
//     LabelExcludes to provider labels — we do not, matching cc-utils.
//   - Duplicate-identity version injection scans forward only, skips a resource
//     that already has extraIdentity.version, and keys on the full
//     extraIdentity — as cc-utils does; OCM v1 also scans backward and
//     overwrites an existing version.
//   - A resource whose access type is none keeps its digest, as cc-utils does;
//     OCM v1 drops it (MapResourcesWithNoneAccess). (A resource with no access
//     at all would have its digest dropped by cc-utils too, but ConvertFromV2
//     rejects that upstream, so it never reaches normalisation.)
package v1
