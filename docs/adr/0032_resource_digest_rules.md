# Shared Resource Digest Rules

* **Status**: proposed
* **Deciders**: OCM Maintainer Team
* **Date**: 2026-10-07

Technical Story: [#3674](https://github.com/open-component-model/open-component-model/issues/3674) (OCI digests relabelled `ociArtifactDigest/v1` → `genericBlobDigest/v1` on upload broke signatures) and [#3643](https://github.com/open-component-model/open-component-model/pull/3643) (OCI→HTTP transfer compares a layout-tar hash with a manifest digest; its transfer-output side is [out of scope](#out-of-scope)). Both are fixed on main by [#3681](https://github.com/open-component-model/open-component-model/pull/3681) (`836cdfaf3`, "fix(oci): digest compatibility"). Its review found the behaviour correct but the integration messy, and asked for this ADR. The implementation follows in its own PRs once this ADR is accepted.

---

## Context and Problem Statement

A resource digest is a triple: hash algorithm, normalisation algorithm, and value. All three fields are signed. The spec defines three hash names, `SHA-256`, `SHA-512` and `NO-DIGEST` ([digest-algorithms](https://github.com/open-component-model/ocm-spec/blob/main/doc/04-extensions/04-algorithms/digest-algorithms.md)). It defines two normalisations: `genericBlobDigest/v1` (G, the bytes of the blob) and `ociArtifactDigest/v1` (O, the digest of the OCI manifest or index) ([artifact-normalization-types](https://github.com/open-component-model/ocm-spec/blob/main/doc/04-extensions/04-algorithms/artifact-normalization-types.md)).

Each binding handles that triple on its own. Paths below are relative to `bindings/go/` on main (`2c0c9c069`).

* **Every OCI resource that v2 created before #3681 is labelled G.** Until `836cdfaf3`, `oci/internal/digest.Apply` hard-coded `genericBlobDigest/v1` for every OCI root: in `ProcessResourceDigest`, both upload paths, packing and Helm `oci://` charts. This "historical G on an OCI root" is not an edge case. It is the label of every v2-created OCI resource that exists today.
* **The decision logic is copied.** `ProcessResourceDigest`, `UploadResource` and `UploadResourceStream` in `oci/repository.go` each contain the same complete-or-verify block. `oci/internal/pack/pack.go` (`finishResourceLayer`, `verifyLayoutRootDigest`, `updateArtifactAccess`) contains variants of it. `helm/digest/digest.go` (`verifyDigest`) implements it again with different rules. Because `oci/internal/digest` is internal, helm and transfer cannot reuse it.
* **Hash names have no single answer.** Each binding uses its own spelling rule (see [Hash names today](#hash-names-today)). In `signing/digest.go`, `isApprovedDigestHash` and `getSupportedHash` disagree on `sha256`. s3, git and github *rewrite* complete digests to their own spelling.
* **The content kind is rediscovered at each step.** `isOCILayout` in `oci/blob/artifact_blob.go` checks five sources, including type assertions on wrapped blobs, to decide whether content is an OCI layout. `pack.ResourceLocalBlob` decides again, and the two decisions can disagree. `oci/blob/update.go` decides a third time.
* **Only two places handle excluded resources.** The spec lets a resource be left out of the signature by recording the triple `NO-DIGEST` / `EXCLUDE-FROM-SIGNATURE` / `NO-DIGEST`; it is the way to reference volatile content. The library has the constants and `NewExcludeFromSignatureDigest` (`descriptor/runtime/descriptor.go`). Only signing (`isExcludedFromSignature`) and `repository/verify_digest.go` detect the triple, both ignoring case and with either field being enough. Every other binding treats it as a foreign label or an unknown hash, and the constructor calls the digest processor unconditionally.
* **Blob handling mixes concerns.** `ArtifactBlob.Digest()` mixes the byte checksum with signed metadata. Defaulting runs twice (`UpdateArtifactWithInformationFromBlob` and `NewArtifactBlobWithMediaType`). `repository.VerifyDownload` accepts only SHA-256.

### Hash names today

| Site | Accepts (recorded) | Emits (new, complete) |
|---|---|---|
| `oci/internal/digest` (process, upload, pack) | SHA-256/SHA-512, case-insensitive, dash optional | `SHA-256`, `SHA-512` |
| `oci/blob/artifact_blob.go` (ordinary blob, G) | exact `SHA-256`, `SHA-512` | `SHA-256`, `SHA-512` |
| `helm/digest` | complete: exact `SHA-256`; partial: case-insensitive, dash optional; SHA-256 only | `SHA-256` |
| `helm/transformation` (helm→OCI) | — | `SHA-256`, `SHA-512` |
| `transfer/internal/repositoryupload` | exact `SHA-256`, `SHA-512` | `SHA-256`, `SHA-512` |
| `repository.VerifyDownload` | SHA-256, case-insensitive, dash optional | — |
| `signing` `isApprovedDigestHash` | SHA-256/SHA-512, case-insensitive, dash optional | — |
| `signing` `getSupportedHash` | upper-cased `SHA-256`/`SHA-512` (`sha-256` yes, `sha256` no) | `SHA-256`, `SHA-512` |
| `wget/repository` | download: exact `SHA-256`; peek: `SHA-256`/`SHA-512` case-insensitive | `SHA-256`; peek also `SHA-512` |
| `wget/input`, `wget/transformation` | case-insensitive `SHA-256` / exact `SHA-256` | `SHA-256` |
| s3, git, github | `SHA-256`, case-insensitive (`sha256` no) | `SHA-256`, and re-spell complete digests |

The union is SHA-256 and SHA-512, case-insensitive, with an optional dash. Every site already emits `SHA-256`/`SHA-512` for new complete digests, so a single formatter changes no emitted digest.

## Decision Drivers

* **Existing digests never break.** Any digest accepted today stays accepted with the same verification result. A complete recorded digest is never relabelled, re-spelled or recomputed. No entry point becomes stricter than it is on main.
* One place answers which hash names and normalisations are valid.
* The shared package has no OCI knowledge. Each binding owns the meaning of what it computes.
* House style: plain string constants, small pure functions, sentinel errors, table tests, and no new optional blob interface.
* Small PRs that are easy to review. Only Go API breaks carry `!`.

## Considered Options

* **Option 1**: a shared `bindings/go/digest` package. It holds the spec vocabulary and a pure `Check` that compares the recorded digest with what the binding computed, under a rule the binding supplies.
* **Option 2**: a v1-style digester registry keyed by the recorded (hash, normalisation) pair and by media type.
* **Option 3**: consolidate only the hash names and helpers.

Rejected without a full option: modelling normalisations or hash names as `runtime.Type`s in a `runtime.Scheme`. A Scheme maps type names to Go prototypes for decoding typed specs. Here there is no per-normalisation behaviour to convert into, the aliases are exact-match only, and collapsing aliases to the canonical name (what a Scheme does) would invite rewriting recorded labels. Normalisations stay plain strings, as in `descriptor/normalisation` (`type Algorithm = string`) and `runtime.Digest.NormalisationAlgorithm`.

## Decision Outcome

Chosen [Option 1](#option-1): "Shared digest package with binding-supplied rules". Option 3 ships as its first step.

Justification:

* One pure decision function replaces the copied blocks and is tested against the [behaviour table](#behaviour).
* One `ParseHashAlgorithm`/`FormatHashAlgorithm` pair replaces every mapping and answers "where do we accept these?".
* The rule is chosen from **what the binding computed**, never from the recorded label. This is the opposite of v1 (see [Option 2](#option-2-v1-style-digester-registry)), and it is required because every v2-created OCI resource carries G.
* Dispatch per access type already exists in the `digestprocessor` plugin registry, so no second registry is needed.

### Option 1

#### Description

`bindings/go/digest` (imported as `ocmdigest` where it clashes with go-digest) owns the spec vocabulary: `ParseHashAlgorithm`/`FormatHashAlgorithm` on `crypto.Hash`, the `genericBlobDigest/v1` constant, `IsExcluded` and `Check`. It operates on the existing `descriptor/runtime.Digest` and does not move or duplicate it.

**The rule comes from what was computed.** A binding that resolved a manifest or index root uses `ArtifactRule`; a binding that hashed raw bytes uses `BlobRule`. The recorded `normalisationAlgorithm` is only *validated* against that rule (canonical or `Accepted`, otherwise foreign) and never chooses it. Each binding picks the rule once, where content enters it: Helm `oci://` charts use `ArtifactRule` and HTTP charts `BlobRule`; for OCI local blobs `ArtifactBlob` carries the rule, so `pack` does not inspect the blob again.

**`Check`** has no side effects, and an error means reject. `Result.Digest` is the triple the caller stores: the recorded triple unchanged for `Keep`, `KeepUnverified` and `Excluded`, a new triple for `Generate` and `Complete`. **Never rewrite** is enforced inside `Check`, not left to each rule: a complete recorded digest always comes back byte for byte, whatever its label, spelling or value case.

**Excluded resources (`NO-DIGEST`)** are handled first, before any rule, mode or hash logic:

* `IsExcluded` matches signing exactly: ignoring case, **either** `hashAlgorithm == NO-DIGEST` **or** `normalisationAlgorithm == EXCLUDE-FROM-SIGNATURE` is enough. Invalid and partial exclude triples are therefore also excluded and kept as recorded. Signing still rejects a partial triple at signing time, as today; `Check` does not complete it.
* `Check` returns the dedicated decision `Excluded` and ignores `computed` (a zero `Computed` is valid), `rule` and `mode`. It is not `Keep` ("verified against the content") or `KeepUnverified` ("foreign label tolerated"): callers need to know that nothing was hashed, so they can skip downloads and log accurately.
* Each entry point, including the constructor before it calls the digest processor, guards with `ocmdigest.IsExcluded` and computes nothing, because the content is volatile by definition. `Check` handling the case as well keeps a caller that computes anyway safe.
* The triple is never hashed, compared, completed, relabelled or rewritten: when adding a component version by reference or by value, getting it, in every transfer path and on download. The triple is itself signed, so replacing it with a real digest breaks verification with "digest mismatch".

**Hash names.** `ParseHashAlgorithm` ignores case and dashes and accepts the union of what any binding accepts today; that union is the floor, so a binding has nothing to add. `FormatHashAlgorithm` returns `SHA-256` or `SHA-512` and is only used for new or completed triples. `NO-DIGEST` has no `crypto.Hash`: `ParseHashAlgorithm` returns the sentinel `ErrNoDigest` for it, so a caller that forgets the `IsExcluded` check gets a precise error instead of "unknown hash". This is the order signing uses today (`ValidateDigestHashAlgorithms` skips excluded resources before checking the hash).

**`Mode`** is chosen per entry point so that each keeps its current behaviour (see [Modes per entry point](#modes-per-entry-point)).

#### Contract

```go
// bindings/go/digest (import as ocmdigest where it clashes with go-digest)
const GenericBlobDigestV1 = "genericBlobDigest/v1"
var ErrNoDigest = errors.New("NO-DIGEST has no hash algorithm")   // returned by ParseHashAlgorithm
func IsExcluded(d *runtime.Digest) bool                          // NO-DIGEST hash OR EXCLUDE-FROM-SIGNATURE, case-insensitive (as signing)
func ParseHashAlgorithm(name string) (crypto.Hash, error) // case- and dash-insensitive; SHA-256, SHA-512; NO-DIGEST → ErrNoDigest
func FormatHashAlgorithm(h crypto.Hash) string            // "SHA-256", "SHA-512"; only for new/completed triples
type Computed struct { Hash crypto.Hash; Value string }  // what the binding hashed; zero for excluded resources
type Rule struct { Normalisation string; Accepted []string } // canonical label + exact-match labels kept as recorded
type Mode int // Strict (reject a complete foreign label) | Lenient (keep it unverified)
type Result struct { Digest runtime.Digest; Decision Decision } // Generate | Complete | Keep | KeepUnverified | Excluded
func Check(recorded *runtime.Digest, computed Computed, rule Rule, mode Mode) (Result, error)

// bindings/go/oci/digest
const ArtifactDigestV1 = "ociArtifactDigest/v1"
var ArtifactRule = ocmdigest.Rule{Normalisation: ArtifactDigestV1, Accepted: []string{"ociArtefactDigest/v1", ocmdigest.GenericBlobDigestV1}}
var BlobRule = ocmdigest.Rule{Normalisation: ocmdigest.GenericBlobDigestV1} // non-OCI bindings write the same literal
func FromOCI(d godigest.Digest) (ocmdigest.Computed, error)               // go-digest → crypto.Hash; SHA-384 rejected
```

Each of the three copied blocks in `oci/repository.go` becomes:

```go
if ocmdigest.IsExcluded(res.Digest) {
    return nil // volatile content: never resolved, hashed, compared or rewritten
}
computed, err := ocidigest.FromOCI(root.Digest)
if err != nil {
    return fmt.Errorf("resource %q: %w", res.ToIdentity(), err)
}
result, err := ocmdigest.Check(res.Digest, computed, ocidigest.ArtifactRule, ocmdigest.Strict)
if err != nil {
    return fmt.Errorf("resource %q: %w", res.ToIdentity(), err)
}
res.Digest = &result.Digest // res is already a deep copy; Keep returns the recorded triple unchanged
```

The package imports only the stdlib, `bindings/go/runtime` and `bindings/go/descriptor/runtime`, whose existing depguard rules already forbid new imports, so no import cycle is possible. A new lax depguard rule enforces the layering, and `bindings/go/digest` is added to the allow lists of oci, helm, transfer, wget, s3, git, github, repository, signing, constructor, cli and kubernetes-controller:

```yaml
        digest:
          list-mode: lax
          files: ["**/bindings/go/digest/**"]
          allow:
            - "ocm.software/open-component-model/bindings/go/digest"
            - "ocm.software/open-component-model/bindings/go/descriptor/runtime"
            - "ocm.software/open-component-model/bindings/go/runtime"
          deny:
            - pkg: "ocm.software/open-component-model/bindings/go/"
              desc: "digest must not import bindings outside its allowed set (no OCI knowledge)"
            - pkg: "github.com/opencontainers/go-digest"
              desc: "digest speaks crypto.Hash; go-digest conversion belongs to the OCI binding"
            - pkg: "oras.land/oras-go"
              desc: "digest must not depend on OCI transport"
```

#### Behaviour

`Check` and its callers implement this table. The tests from #3681 (`oci/internal/digest`, `oci/repository_test.go`, `pack_test.go`, `artifact_blob_test.go`, helm `digest_internal_test.go` and the CLI `transfer_oci_artifact_integration_test.go`) are the regression suite and must pass unchanged.

| Rule | Recorded digest | Decision |
|---|---|---|
| R1 | absent or empty | **Generate** with `rule.Normalisation` and `FormatHashAlgorithm` (G for bytes, O for an OCI root) |
| R2 | incomplete, the fields that are set match | **Complete** with `rule.Normalisation` |
| R3 | incomplete, label in `Accepted` | **Complete**, relabelled to canonical; an incomplete digest cannot carry a signature |
| R4 | incomplete, label foreign to the rule (O on bytes) | reject |
| R5 | complete, canonical or `Accepted` label | verify hash and value against the computed digest, then **Keep** byte for byte |
| R6 | complete G on an OCI root (every OCI resource v2 created before #3681) | G stays in `ArtifactRule.Accepted` indefinitely; verified against the root, never against tar bytes, and **kept** |
| R7 | complete, foreign label | Strict where it is rejected today, Lenient (`KeepUnverified`) in packing as today; never rewritten |
| R8 | hash spelling | parsed by `ParseHashAlgorithm`; complete digests keep their spelling; new and completed ones use `FormatHashAlgorithm` |
| R9 | hash set | SHA-256 and SHA-512; SHA-384 rejected for resources; helm gains SHA-512 |
| R10 | any rejection | happens before any destination write: no root copied, tag untouched, no push; a target-access pin must match the root |
| R11 | input | `Check` never mutates; callers store `Result.Digest` on their own copy |
| R12 | layout archive checksum | kept separate from the resource digest; never compared with it or substituted for it |
| R13 | ordinary blob, G or unlabelled | is the byte checksum and must match the bytes; re-verified when buffered |
| R14 | exclude triple (either field, ignoring case, including invalid and partial forms) | **Excluded**, checked before R1–R13; never hashed, compared, completed or rewritten |

#### Modes per entry point

| Entry point | Complete foreign label today | Mode |
|---|---|---|
| OCI `ProcessResourceDigest`, `UploadResource`, `UploadResourceStream` | reject | Strict |
| OCI layout and layer packing | keep unverified | Lenient |
| helm, wget (processing, input, streaming upload), s3, git, github `ProcessResourceDigest` | reject | Strict |
| transfer `expectedDigest` for non-OCI sources | reject | Strict |
| `repository.VerifyDownload` (wget, s3, github downloads) | label ignored; bytes verified | unchanged; only hash parsing moves to `ParseHashAlgorithm` |
| OCI `GetComponentVersion`, `GetLocalResource`, `Download*` | not checked against `res.Digest` | unchanged; [out of scope](#out-of-scope) |

The asymmetry is intentional: packing a local blob has always preserved foreign complete digests, and processing and upload have always rejected them.

#### Excluded resources per entry point

| Entry point | Exclude triple on main | Afterwards |
|---|---|---|
| Add by reference (constructor → digest processor) | fails in every processor below | processor not called; triple kept |
| OCI `ProcessResourceDigest`, `UploadResource`, `UploadResourceStream` | fails: unsupported OCI artifact normalisation | kept; root not compared |
| helm `ProcessResourceDigest` | fails: normalisation mismatch, or no index digest | kept; index not fetched |
| wget `ProcessResourceDigest` | fails after downloading and hashing, or "no advertised checksum" | kept; nothing downloaded |
| s3, git, github `ProcessResourceDigest` | fails after downloading or archiving: hash mismatch | kept; nothing downloaded or built |
| wget streaming upload | fails: unsupported hash | kept |
| Transfer to non-OCI targets, plain blob source | fails: unsupported hash algorithm `NO-DIGEST` | kept; no expected digest |
| Transfer to non-OCI targets, OCI source | **rewritten** to `SHA-256` / `genericBlobDigest/v1` / hash of the uploaded bytes | kept |
| helm→OCI conversion | **rewritten** to `SHA-256` / `ociArtifactDigest/v1` / manifest digest | kept |
| Add by value (`AddLocalResource`), get, OCI→OCI transfer, download, signing | kept | unchanged; no computation |

### Compatibility

Checked against the first decision driver:

* **Widenings** (rejected today, accepted afterwards): all spelling variants at every site, including `sha256` in `getSupportedHash` and on a complete G for local blobs; SHA-512 in helm and `VerifyDownload`; upper-case or `sha256:`-prefixed values at OCI; case-insensitive normalisation labels (s3, git and github already accept them); `ociArtefactDigest/v1`; incomplete digests in s3, git, github and wget, which are completed instead of rejected; and the exclude triple wherever it fails today.
* **Changed output in two otherwise out-of-scope paths:** transfer of an OCI source to a non-OCI target, and the helm→OCI conversion, keep the exclude triple instead of rewriting it. Both turn a signature-breaking rewrite into a keep, so neither can break a digest accepted today, but both change the output for excluded resources. This needs maintainer agreement.
* **Unchanged:** historical G is kept (R6); completion (R2, R3) and R4 behave as on main, and R7 keeps each entry point's current mode. OCI→OCI transfer keeps the source digest. The R13 byte check only moves *before* the push, which already rejects mismatched bytes. Removing the `update.go` defaulting yields the same triple through `Generate`.
* **Stricter:** none relative to main.
* **Residual:** completing an incomplete digest changes bytes that a parent component reference digest could cover. Main already does this.

### Out of scope

* Verifying OCI downloads against `res.Digest`.
* The digest that transfer writes for OCI sources uploaded to non-OCI targets (Artifactory, Nexus, HTTP; `transfer/internal/repositoryupload`) and the digest written by the helm→OCI transformation (`helm/transformation`), except for the excluded-resource keep above. OCI→OCI transfer keeps the digest and is unaffected.

## Pros and Cons of the Options

### [Option 1] Shared package with `Check`, `Rule` and `Mode`

Pros:

* One tested decision function, with the never-rewrite guarantee enforced in one place.
* One hash-name parser and one formatter replace every mapping.
* The rule follows from what was computed, so the G label on every existing v2 OCI resource cannot misroute it.
* No OCI knowledge in shared code; new rules (chunking) are plain values; depguard enforces the layering.

Cons:

* It touches about ten packages. Strict and Lenient coexist by design.
* External plugins still implement `ProcessResourceDigest` themselves; the behaviour table is their contract.

### [Option 2] v1-style digester registry

Pros:

* Open to new normalisations and close to the spec's "digest handler per media type".

Cons:

* v1 picks the digester from the *recorded* label. In v2 that label is G on every OCI resource created before #3681, so dispatching on it would hash tar bytes instead of the root and break every such digest.
* It adds a second dispatch axis next to the `digestprocessor` registry, with global mutable state for a closed set of two.

### [Option 3] Consolidate hash names only

Pros:

* Small and low risk; it answers the hash-name question.

Cons:

* The copied decision blocks and the kind rediscovery remain, so it does not address the main review objection. It is used only as the first step of Option 1.

## Discovery and Distribution

The ADR lands first; the implementation follows in separate PRs:

1. **Hash names.** Add `bindings/go/digest` (`ParseHashAlgorithm`, `FormatHashAlgorithm`, constants), a table test asserting every spelling in [Hash names today](#hash-names-today), and the depguard rule. Migrate signing, `repository/verify_digest.go`, transfer, helm and the local constants. s3, git and github stop re-spelling complete digests.
2. **`Check`, `Rule`, `Mode`.** Add `oci/digest` (`ArtifactRule`, `BlobRule`, `FromOCI`). Replace `oci/internal/digest`, the three `oci/repository.go` blocks, the pack verify/complete code (verifying *after* completion), helm `verifyDigest` and transfer `expectedDigest`, each with its mode. Add the `IsExcluded` guard at every entry point, including the constructor, which also covers external digest-processor plugins. Each row of [Excluded resources per entry point](#excluded-resources-per-entry-point) gets a regression test. `website/content/docs/reference/oci-resource-digests.md` is updated in place.
3. **`feat(oci)!`.** The rule is chosen once at entry and carried on `ArtifactBlob`. `Digest()` becomes a byte checksum, verified before the push. Delete the double defaulting, `isOCILayout` and `SetPrecalculatedDigest`.
4. **Later:** the [ADR 0031](0031_oci_local_blob_chunking.md) chunked rule in `oci/digest`, chosen from the logical content behind a chunk manifest, never from the stored shape.

Public API impact:

* **New:** `bindings/go/digest` and `bindings/go/oci/digest`.
* **Removed (internal or unexported):** `oci/internal/digest`; the local constants and mappings in helm, transfer, wget, s3, git and github; `signing.isApprovedDigestHash` and `getSupportedHash`.
* **Breaking Go API (`!`, approved in principle)** in `oci/blob`: `NewArtifactBlob` takes the rule and stops mutating its argument; `Digest()` is a byte checksum only; `SetPrecalculatedDigest` and the defaulting in `UpdateArtifactWithInformationFromBlob` are removed.

Open follow-ups:

1. Maintainer agreement on keeping the exclude triple in the two out-of-scope output paths (see [Compatibility](#compatibility)).
2. The wget peek path can record SHA-512 while `VerifyDownload` accepts only SHA-256 (found by static analysis only). Step 1 fixes it by widening; a separate issue tracks it.

## Conclusion

Digest rules become plain data owned by the bindings. The rule is chosen from what was computed, and one pure `Check` in `bindings/go/digest` decides what to do. `ParseHashAlgorithm` accepts the union of everything accepted today, complete recorded digests are never rewritten, historical G on OCI roots is accepted indefinitely, excluded (`NO-DIGEST`) resources are kept without ever being hashed, and no entry point becomes stricter than on main.
