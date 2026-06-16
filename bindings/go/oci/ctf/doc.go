// Package ctf implements a structurally pre-v1.1 OCI registry: tags and digests
// in artifact-index.json, but no native index of manifests by their subject
// field. Referrer support is therefore implemented via the referrers tag
// schema that the OCI distribution spec defines as the fallback for exactly
// this situation, mirroring oras-go's remote.Repository behavior so that a
// CTF behaves like any other registry without Referrers API support:
//
//   - On push of a manifest with a subject (see [repository.Push]), the
//     referrers index stored under the tag "<alg>-<hex>" of the subject digest
//     is updated with the annotated referrer descriptor.
//   - On read (see [repository.Referrers]), that index is fetched and its
//     entries are returned, optionally filtered by artifact type.
//   - [repository.Predecessors] delegates to Referrers, so
//     oras.ExtendedCopyGraph picks referrers up as predecessors and copies
//     them alongside their subject.
//
// The referrers index itself is registry-local bookkeeping: it carries no
// subject edge, is never returned as a predecessor, and is consequently not
// copied by ExtendedCopyGraph. A destination store rebuilds its own index
// when the referrer manifests are pushed into it.
//
// Reference: https://github.com/opencontainers/distribution-spec/blob/v1.1.1/spec.md#unavailable-referrers-api
//
// Concurrency: the spec notes that maintaining the referrers tag is the
// client's responsibility and that concurrent updates can lose data. Within
// a process, every repository derived from the same [Store] shares the
// store's write lock, which fully serializes the read-modify-write of the
// referrers index. Across multiple [Store] instances for the same path, or
// across processes, the CTF (like the rest of artifact-index.json handling)
// is single-writer; see also the store cache in the oci repository provider.
package ctf
