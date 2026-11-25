# Transfer normalized component versions as an OCI graph

> **Temporary.** This is a working implementation plan for #3888 (normalized OCI layout). Remove it, or fold its
> decisions into [ADR 0034](../adr/0034_normalized_oci_layout.md), before the PR merges.

Implementation contract for reworking how signatures are carried during transfer. It replaces the
`CarryComponentSignatures` design currently on this branch.

## Problem

Transfer never copies the component version's own OCI content. The source descriptor travels as data through the
transformation graph, and `AddComponentVersion` rebuilds every manifest on the target:

```text
discovery: source repo ──GetComponentVersion──► descriptor (data)
graph:     resources: Get*/Transfer* ──► target     (OCI graphs copied with ExtendedCopyGraph → referrers survive)
           descriptor ──CEL──► {OCI,CTF}AddComponentVersion ──► AddDescriptorToStore (rebuilt from scratch)
```

The referrers of the component version's own manifest are therefore lost. With the normalized layout, those referrers
are the cosign signatures and attestations over the normalized manifest digest. The branch fixes this with an extra
node after the upload:

```text
<id>Upload (rebuild normalized manifest + access referrer)
  └─► <id>Carry  {OCI,CTF}CarryComponentSignatures
        opens the SOURCE repository again
        Predecessors(source normalized manifest) → peekArtifactType on EVERY referrer (1 GET each)
        → CopyGraph per non-access referrer (2nd GET of the same manifest)
        fails if target normalized manifest digest != source digest (detected after writing)
```

Cost: two public transformation types with 6 schemas, the public `repository.ComponentSignatureCarrier` interface, a
digest-equality check that runs after the write, and 1 + 2·n manifest GETs for n signature referrers.

## Proposed Changes / Design

Terms:

- **normalized manifest**: the manifest the version tag resolves to. It is access-free and byte-identical in every
  repository. cosign signs its digest.
- **access referrer**: a referrer of the normalized manifest with artifact type `ArtifactTypeAccessDescriptor` (an
  image manifest, or an image index when OCI-native local blobs exist). It holds the access-bearing descriptor and the
  local blobs. It has no tag. Each write stamps it with an `org.opencontainers.image.created` annotation, and every
  write (re-add, e.g. `ocm sign`; a transfer) adds a new one while the older ones stay listed as referrers. Reads
  select the newest by `created` from the referrers listing and validate only that one (see
  [Read path](#read-path-select-the-access-referrer-from-the-listing)).
- **signature referrers**: every other referrer of the normalized manifest, transitively (cosign signatures,
  attestations, referrers of those).

Design: for a normalized source where source and target are OCI or CTF, one fused transformation replaces
`{OCI,CTF}AddComponentVersion`. It streams the normalized manifest with its signature referrers from the source store
into the target store with a single `oras.ExtendedCopyGraph`, then writes the target's own access referrer. There is
no intermediate file. This follows the fused local blob transfers (`oci/transformer/transfer_local_blob.go`).

```text
<id>Upload  Transfer{OCI,CTF}ComponentVersionTo{OCI,CTF}
  source: repository, component, version, digest (pin from discovery)
  repository: target        descriptor: same CEL as today's Upload (env descriptor + resource outputs)

  1. source.DownloadComponentVersionGraph → ResourceStream (normalized manifest + blobs + signature referrers)
  2. bind check: normalize(descriptor) == normalized layer bytes from the graph        (before any write)
  3. subject check: normalized manifest subject matches target referrer tracking policy (before any write)
  4. ExtendedCopyGraph(source store → target store, normalized manifest)
  5. push target access referrer (subject = normalized manifest, created = now)
  6. tag normalized manifest with the version   (tagIdempotent, as in AddComponentVersion)
```

### Fixed requirements

1. **Graph content.** The copy contains the normalized manifest, its config and layer, the component index manifest if
   it is the subject, and every transitive signature referrer. Source access referrers are never copied, whether
   current or superseded. Besides the size cost, this matters for selection: reads pick the newest access referrer by
   `created`, and timestamps from another writer's clock can be ahead of the target's. A copied source access
   referrer could then win on the target and point readers at source accesses.
2. **Bind check before writing.** `normalizedlayout.VerifyNormalizedMatchesAccess(<normalized layer bytes from the
   source graph>, <transferred descriptor>)` must pass before anything is written to the target. Failure is an error
   naming the component version ("descriptor changed during transfer; source signatures do not apply"). There is no
   fallback to the rebuild path.
3. **Referrer tracking policy.** The copied normalized manifest keeps the source's `subject`. If its presence doesn't
   match the target's `referrerTrackingPolicy`, fail before writing: the target needs a subject
   (`ReferrerTrackingPolicyByIndexAndSubject`) and the source has none, or the other way round.
4. **Write order.** Graph copy, then access referrer, then version tag. A version must never be tagged before its
   signature referrers and access referrer exist on the target.
5. **Rebuild path unchanged.** `{OCI,CTF}AddComponentVersion` and `AddComponentVersion` stay as they are (`ocm add
   cv`, `ocm sign` re-adds, default-layout sources, non-OCI/CTF sources).
6. **Default-layout target.** If the target is configured for the default (v2) layout, the fused transformation skips
   the graph and writes like `AddComponentVersion`. It logs a warning that the signature referrers of the normalized
   source are not carried, with the `componentVersionLayout: normalized` hint. Do not fail.
7. **Failure semantics.** Any failure to open the source, discover or filter referrers, or copy fails the transfer.
8. **Request cost.** Filtering must not fetch a referrer manifest when its descriptor's `artifactType` already
   identifies it (see below).
9. **Pinning.** `source.digest` is set from `discoveryValue.Pin` when present, and the source is opened through
   `repository.PinRepository`, so the graph is read from the artifact the descriptor was read from.
10. **Always write the access referrer.** Every run writes a new, timestamped access referrer, including reruns into a
    target that already holds an equal one. Do not read the target's referrers to skip the write. The resulting
    duplication is accepted. Selection on read (newest `created`) handles it.

### Filtering access referrers: request cost

The branch's `peekArtifactType` (`oci/store_normalized_read.go`) is one manifest GET per referrer. In oras-go v3,
`ExtendedCopyGraph` calls `FindPredecessors` in `findRoots` on the raw source. The caching proxy only exists
afterwards, inside `copyGraph`, so a peek is never reused by the copy.

Decide per referrer, in this order:

1. **`desc.ArtifactType` non-empty → decide from it.** This holds for the OCI 1.1 Referrers API (required by
   distribution-spec), for referrers-tag-schema indexes written by oras/OCM/cosign v3, and for CTF
   (`oci/ctf/referrers.go`, `referrerFromManifest`).
2. **Otherwise peek**, only for image manifest / index media types. This is rare: a fallback index written by tooling
   that omits `artifactType`. Return peek errors; never swallow them. The read path ignores such entries, but the copy
   must not: the target registry derives `artifactType` from the copied manifest, so an unpeeked access referrer would
   become a selectable access referrer on the target.

Net: no extra requests on conformant registries and CTF, versus 1 + 2·n on the branch.

### Source read cost (prefetch + transfer)

With the read path below, discovery performs a constant normalized read, independent of the number of signatures,
attestations and superseded access referrers:

1. resolve the version tag
2. GET normalized manifest (needed to detect the layout and to find the normalized layer)
3. GET normalized layer (needed for the bind check, for every reader)
4. list referrers
5. GET the newest access referrer's manifest (+ inner manifest for the index form)
6. GET its access descriptor layer

The fused node re-reads the normalized manifest and layer. With the CLI provider, these are blob/reference-cache hits:
`cli/internal/plugin/builtin/oci/register.go` enables both caches, the cache's default accept list covers manifests,
`componentConfig.MediaType` and the `component-descriptor` prefix, and discovery and the builder share
`pm.ComponentVersionRepositoryRegistry`.

Remaining network cost per component version and target:

- Referrers listings: `cache.CachedStore` does not cache `Referrers`/`Predecessors`. Besides discovery's one listing,
  each target's fused node lists the normalized manifest once and every signature referrer once (`findRoots` walks
  predecessors transitively; those listings are usually empty), so 1 + s per target for s signature referrers. A
  run-scoped in-memory memo could save those; it is deliberately left out for now.
- the normalized config, which the read never fetches
- the signature referrers' manifests and layers, which are inherent to copying them

### Read path: select the access referrer from the listing

A separate change in `getNormalizedComponentVersion` (`oci/store_normalized_read.go`, the code from "Remove access
tag"). It can land before or independently of the transfer rework. Today the read fetches every referrer manifest
(`loadAccessReferrer` fetches before it checks `artifactType`), validates every access referrer, and falls back to
the newest *valid* one. That costs 4 + (all referrers) + 1–2 per access referrer, and a broken or tampered newest
write silently yields an older descriptor.

Fixed requirements:

1. **Select from the listing only.** Candidates are the referrer descriptors with `artifactType ==
   ArtifactTypeAccessDescriptor`. Entries without `artifactType` are ignored. Every OCM write path sets it, and the
   distribution-spec requires it in Referrers responses and referrers-tag-schema indexes, as does OCM's CTF index
   (`oci/ctf/referrers.go`, `referrerFromManifest`).
2. **Newest wins, from the listing's annotations.** Order by `org.opencontainers.image.created` taken from the
   descriptor's annotations (required in Referrers responses: "The descriptors MUST include annotations from the image
   manifest or index"). A missing or malformed value sorts oldest; ties break by lowest digest.
3. **Validate only the selected one.** Fetch and validate the newest candidate as `loadAccessReferrer` does today
   (subject, bind check, `ValidateDigests`). If it is invalid, the read fails with an error naming its digest. There is
   no fallback to an older access referrer. The next write repairs the version by adding a newer one.
4. **No access referrer** in the listing: fail as today ("no access referrer found").

Tests: extend the existing `TestNormalizedLayout_AccessReferrerSelection` (`oci/normalized_layout_test.go`).

- newest by `created` wins
- an invalid newest one fails the read (no fallback)
- an entry without `artifactType` is ignored
- with a counting store, only the selected access referrer is fetched, not signature referrers or superseded access
  referrers

No caching work is in scope.

## Implementation / API Changes

Package `oci` (`bindings/go/oci`):

- New file `component_graph.go`:
  - `func (repo *Repository) componentGraphCopyOptions() oras.ExtendedCopyGraphOptions` wraps
    `extendedCopyGraphOptions()` (`missing_subjects.go`: `skipMissingSubject` + `skipMissingReferrers`). Its
    `FindPredecessors` applies the two-step filter above.
  - `func (repo *Repository) DownloadComponentVersionGraph(ctx, component, version string)
    (*ocistream.OCIResourceStream, error)`: `getStore` → `Resolve` → require `getStoredLayout == LayoutNormalized` →
    return `&ocistream.OCIResourceStream{ReadOnlyGraphStorage: graph, Descriptor: normalizedManifest,
    ExtendedCopyOpts: repo.componentGraphCopyOptions(), TempDir: repo.tempDir, Tags: []string{version}}`. The stream
    is read in place by the target; no `LayoutBlob` and no tar.
  - `func (repo *Repository) AddComponentVersionFromGraph(ctx, desc *descriptor.Descriptor, graph
    ocistream.ResourceStream) error`: same flow as `AddComponentVersion` (tag validation, `checkPinnedTag`, local blob
    scan, `tagIdempotent`), but passes `AddDescriptorOptions.NormalizedGraph`. For `repo.layout != LayoutNormalized`
    it applies requirement 6 and delegates to `AddComponentVersion`.
- `store_descriptor.go`:
  - Add `AddDescriptorOptions.NormalizedGraph ocistream.ResourceStream`.
  - Split `addNormalizedLayout` into `pushNormalizedManifest` (today's config/layer/manifest pushes, the rebuild path)
    and `pushAccessReferrer` (today's access manifest/index push, including the `created` stamp).
  - Copy path: fetch the normalized layer from `graph` (`graph.Root()` is the normalized manifest), apply
    requirements 2 and 3, run `oras.ExtendedCopyGraph(ctx, graph, store, graph.Root(), <the stream's
    ExtendedCopyOpts>)`, then `pushAccessReferrer`.
- `repository.go`: delete `CarryComponentSignatures` and the `ComponentSignatureCarrier` assertion.

Package `oci/spec/transformation/v1alpha1`:

- New file `transfer_component_version.go` with four types, one per path, following `transfer_local_blob.go`:
  `TransferOCIComponentVersionToOCI`, `TransferOCIComponentVersionToCTF`, `TransferCTFComponentVersionToOCI`,
  `TransferCTFComponentVersionToCTF`.
  - Sources: `OCIComponentVersionSource` / `CTFComponentVersionSource` (`repository` concretely typed, `component`,
    `version`, `digest,omitempty`).
  - Spec: `source`, `repository` (the concrete target type), `descriptor *v2.Descriptor`.
  - Output: empty `TransferComponentVersionOutput`.
  - Register them in `scheme.go`.
- Delete `oci_carry_component_signatures.go`, `ctf_carry_component_signatures.go` and their schemas.
- Run `task generate`.

Package `oci/transformer`:

- New `transfer_component_version.go` with a `TransferComponentVersion` transformer handling the four types:
  - Open the source with `componentVersionRepository` + `repository.PinRepository` (as `localBlobSource.open`), and
    the target with `componentVersionRepository`.
  - Assert the unexported local interfaces `componentGraphSource{ DownloadComponentVersionGraph }` and
    `componentGraphTarget{ AddComponentVersionFromGraph }`. Either assertion failing is an error.
  - Convert the descriptor (`descriptor.ConvertFromV2`) and call `AddComponentVersionFromGraph`.
- Delete `carry_component_signatures.go`.

Package `repository`: delete `ComponentSignatureCarrier` and keep `ComponentVersionLayoutDetector`. No new public
interfaces.

Package `transfer/internal`:

- `graph.go` (`fillGraphDefinitionWithPrefetchedComponents`, `addUploadTransformation`): if `val.SourceLayout ==
  layout.ComponentVersionLayoutNormalized` and `val.SourceRepository` and the target are OCI/CTF, emit the fused type
  instead of the Add type. Keep the ID `<id>Upload`, the label (`uploadLabel`) and the `descriptor` CEL spec
  (`buildDescriptorSpec`). Add `source: {repository, component, version, digest: val.Pin}`. Otherwise keep
  `chooseAddType` unchanged.
- `layout.go`: replace `addCarryTransformation` / `chooseCarryType` with `chooseTransferComponentVersionType(source,
  target runtime.Type)`. Keep the warning for an unknown `SourceRepository`, which falls back to the Add type.
- `labels.go`: delete `carryLabel`.
- `builder.go`: register the four fused types with the `TransferComponentVersion` transformer and remove the carry
  registrations.
- `cli/cmd/transfer/component-version/cmd.go`: update the graph description text (lines ~98, ~125).

Pitfalls:

- The copy must use the copy options the source stream carries (`OCIResourceStream.ExtendedCopyOpts`, i.e. the
  source's `componentGraphCopyOptions()`), not the target's `extendedCopyGraphOptions()`. The existing fast path in
  `uploadOCIImage` (`oci/repository.go`) copies a `ResourceStream` with the target's options. Copying the same way here
  would drop the access-referrer filter and pull source access referrers onto the target.
- `findRoots` walks predecessors transitively. The filter applies at every level.
- One `ExtendedCopyGraph` from the normalized manifest copies the manifest, its blobs and all signature referrers. Do
  not loop `oras.CopyGraph` over referrers.
- Do not try to drop or replace the access referrer with `PreCopy` + `oras.SkipNode`. `copyGraph` copies a node's
  successors (local blobs, the source access descriptor layer, the config) before `PreCopy` runs, and `SkipNode`
  requires the node to already exist in the target. Filtering in `FindPredecessors` is the only point where nothing
  below the access referrer is fetched or pushed.
- Do not copy the graph and delete the source access referrer on the target afterwards. Registries often reject DELETE
  (immutable registries are why `tagIdempotent` exists), and the copy would pull source-only local blobs.
- An access index is an image index with artifact type `ArtifactTypeAccessDescriptor`. The filter must check index
  media types as well as manifests.
- With multiple targets, each target node reads the source graph itself. Do not share an opened source stream across
  nodes.
- New code uses `github.com/oras-project/oras-go/v3`, like `missing_subjects.go`. `store_normalized_read.go` (incl.
  `peekArtifactType`) still imports `oras.land/oras-go/v2`; move it to v3 when touching it.

## Tests

Seams (2):

1. **Primary: transfer graph execution.** `transfer.BuildGraphDefinition` → `transfer.NewDefaultBuilder(...)
   .BuildAndCheck(tgd)` → `graph.Process(ctx)`. This is the API of
   `transfer/integration/normalized_layout_integration_test.go`. Unit variant: CTF source → CTF target in
   `transfer/internal`, no Docker. Integration variant: the existing two-registry test.
2. **Request count: `oci` package.** `componentGraphCopyOptions().FindPredecessors` against a counting
   `content.ReadOnlyGraphStorage` wrapper over a CTF store, because request cost is invisible at seam 1.

Scenarios:

- Seam 1, CTF→CTF, normalized source with a cosign-type signature and an attestation (`pushReferrerManifest`,
  `oci/normalized_layout_test.go`):
  - The target tag resolves to the same digest as the source.
  - Signature and attestation exist on the target, byte-identical.
  - The source access referrer digest does not exist on the target.
  - The target has exactly one access referrer (its own), and `GetComponentVersion` on the target selects it and
    passes the bind check.
- Seam 1, source re-added once (a superseded access referrer exists): neither source access referrer is copied.
- Seam 1, source access referrer stamped with a `created` time in the future (clock skew): it is not copied, and the
  target read selects the target's access referrer.
- Seam 1, source with an OCI-native local blob (access index form): the index is not copied; the target writes its
  own.
- Seam 1, an uploader that changes a normalized field (e.g. a resource digest): the transfer fails with the bind-check
  error, and the target has no version tag and no signature referrers.
- Seam 1, source `ReferrerTrackingPolicyByIndexAndSubject`, target `None`: fails before writing (no tag on the
  target).
- Seam 1, default-layout target: succeeds with a warning; signatures are absent.
- Seam 1, multi-target: both targets get the signatures.
- Seam 1, graph definition: a normalized OCI/CTF source yields a `Transfer*ComponentVersionTo*` node with ID
  `<id>Upload` and `source.digest == Pin`. A default-layout source still yields `{OCI,CTF}AddComponentVersion`.
  Replace `TestBuildGraphDefinition_CarryComponentSignatures` (`transfer/internal/layout_test.go`).
- Seam 2:
  - Referrers with `artifactType` set, access and non-access: 0 fetches.
  - Empty `artifactType`: exactly 1 fetch.
  - A peek error is returned.
- Delete `TestNormalizedLayout_CarryComponentSignatures` (`oci/normalized_layout_test.go`) without a replacement at the
  `oci` level. Its cases (copy, digest mismatch → bind-check failure, default-layout target) are covered at seam 1, and
  `AddComponentVersionFromGraph` gets no tests of its own.
- Existing integration test `Test_Integration_TransferNormalized_OCIToOCI_CarriesSignatureReferrer`: assertions
  unchanged, rename "Carries" → "Copies".

## Documentation / Verification

- `docs/adr/0034_normalized_oci_layout.md`, Transfer section: replace the `<id>Carry` paragraphs with the fused node,
  the bind and subject checks before writing, the filter order, and the copy-from-source-store data path. Remove
  `ComponentSignatureCarrier` and the carry types from the API list.
- Fix the PR body's "transfers inherit the source layout by default"; the ADR says the source layout is not
  inherited.
- `website/content/docs/concepts/transfer-concept.md` and `signing-and-verification-concept.md`: update any mention
  of the carry node.
- Regenerated CLI reference for `ocm transfer component-version` (graph description).
- Conformance `conformance/scenarios/cosign-normalized`: must pass unchanged. It is the end-to-end proof that
  `cosign verify` works on the target.

## Non-goals

- Changing the default (v2) layout transfer path or `{OCI,CTF}AddComponentVersion`.
- Copying cosign legacy tag-scheme signatures (`sha256-<hex>.sig`/`.att`), which are not referrers.
- Converting layouts during transfer beyond requirement 6.
- Caching `Referrers`/`Predecessors`, or any other cache change.
- Verifying cosign signatures inside OCM, and digest pinning of component references (#3889).
- Cleanup of superseded access referrers on the source or the target.
- Deduplicating access referrers on rerun, or making them deterministic (dropping the `created` timestamp). Either
  would need a read before the write or a rule for picking among differing access referrers.
