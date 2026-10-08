# Shared Resource Digest Rules

* **Status**: proposed
* **Deciders**: OCM Maintainer Team
* **Date**: 2026-10-07

Technical Story: #3674 (OCI digests relabelled `ociArtifactDigest/v1` → `genericBlobDigest/v1` on upload broke signatures), #3643 (OCI→HTTP transfer compares a layout-tar hash with a manifest digest; its transfer-output side is [out of scope](#out-of-scope)), #3681 (the fix, which reviewers found correct but messily integrated).

## Context and Problem Statement

A resource digest is a triple: hash algorithm, normalisation algorithm, and value. All three fields are signed. The spec defines three hash names, `SHA-256`, `SHA-512` and `NO-DIGEST` ([digest-algorithms](https://github.com/open-component-model/ocm-spec/blob/main/doc/04-extensions/04-algorithms/digest-algorithms.md)). It defines two normalisations: `genericBlobDigest/v1` (G, the bytes of the blob) and `ociArtifactDigest/v1` (O, the digest of the OCI manifest or index) ([artifact-normalization-types](https://github.com/open-component-model/ocm-spec/blob/main/doc/04-extensions/04-algorithms/artifact-normalization-types.md)).

Each binding handles that triple on its own. Paths are relative to `bindings/go/`. Unless a line says "main" (upstream `57e5d8c50`), it refers to the #3681 head `ec1efe0bd`.

* **Every OCI resource that v2 has created so far is labelled G.** On main, `oci/internal/digest.Apply` hard-codes `genericBlobDigest/v1` for every OCI root (main `oci/internal/digest/digest.go:28-37`). That covers `ProcessResourceDigest` (main `oci/repository.go:339`), both upload paths (main `:667`, `:1168`), packing (main `oci/internal/pack/pack.go:363`) and Helm `oci://` charts (main `helm/digest/digest.go:286`). This "historical G on an OCI root" is not an edge case. It is the label of every v2-created OCI resource that exists today.
* **The decision logic is copied.** `oci/repository.go:337-346`, `:721-729` and `:1176-1185` contain the same complete-or-verify block. `oci/internal/pack/pack.go:197-201`, `:243-256` and `:396-403` contain variants of it. `helm/digest/digest.go:297-321` implements the same logic again with different rules. Because `oci/internal/digest` is an internal package, helm and transfer cannot reuse it.
* **Hash names have no single answer.** Each binding uses its own spelling rule (see [Hash names today](#hash-names-today)). In signing, `isApprovedDigestHash` and `getSupportedHash` disagree on `sha256` (`signing/digest.go:218-225` vs `:256-270`). s3, git and github *rewrite* complete digests to their own spelling (`s3/repository/resource_repository.go:233-236`, `github/digest/digest.go:172-175`, `git/repository/resource_repository.go:192-196`).
* **The content kind is rediscovered at each step.** `oci/blob/artifact_blob.go:129-157` checks five sources (including type assertions on wrapped blobs) to decide whether content is an OCI layout. `oci/internal/pack/pack.go:66-84` decides again, and the two decisions can disagree. `oci/blob/update.go:20` decides a third time.
* **Only two places handle excluded resources.** The spec allows a resource to be left out of the signature by recording the triple `NO-DIGEST` / `EXCLUDE-FROM-SIGNATURE` / `NO-DIGEST`. That triple is the way to reference volatile content (ocm-spec `41ab62f5`: `04-extensions/04-algorithms/digest-algorithms.md`, `artifact-normalization-types.md`, `01-model/07-extensions.md:529-533`, `02-processing/05-component-descriptor-normalization.md:78-83`). The library has the constants (`descriptor/runtime/descriptor.go:16-21`, `NewExcludeFromSignatureDigest` at `:454`). Only signing (`signing/digest.go:231`) and `repository/verify_digest.go:23` detect the triple, both ignoring case and with either field being enough. Every other binding treats it as a foreign label or an unknown hash. The constructor calls the digest processor unconditionally (`constructor/construct.go:510`).
* **Blob handling mixes concerns.** `ArtifactBlob.Digest()` mixes the byte checksum with signed metadata (`oci/blob/artifact_blob.go:84-120`). Defaulting runs twice (`update.go:16-37`, `artifact_blob.go:51-68`). `repository.VerifyDownload` accepts only SHA-256 (`repository/verify_digest.go:34`).

## Decision Drivers

1. **Existing digests never break.** Any digest accepted today stays accepted with the same verification result. A complete recorded digest is never relabelled, re-spelled or recomputed. No entry point becomes stricter than it is today.
2. One place answers which hash names and normalisations are valid.
3. The shared package has no OCI knowledge. Each binding owns the meaning of what it computes.
4. House style: plain string constants, small pure functions, sentinel errors, table tests, and no new optional blob interface.
5. Small PRs that are easy to review. Only Go API breaks carry `!`.

## Considered Options

* **Option 1**: a shared `bindings/go/digest` package. It holds the spec vocabulary and a pure `Check` that compares the recorded digest with what the binding computed, under a rule the binding supplies.
* **Option 2**: a v1-style digester registry keyed by the recorded (hash, normalisation) pair and by media type.
* **Option 3**: consolidate only the hash names and helpers.
* Rejected without a full option: modelling normalisations or hash names as `runtime.Type`s in a `runtime.Scheme`. A Scheme maps type names to Go prototypes so that typed specs can be decoded. Here, there is no per-normalisation behaviour to convert into, the aliases are exact-match only, and collapsing aliases to the canonical name (what a Scheme does) would invite rewriting recorded labels. Normalisations stay plain strings, as in `descriptor/normalisation` (`type Algorithm = string`) and `runtime.Digest.NormalisationAlgorithm`.

## Decision Outcome

Chosen [Option 1](#option-1): "Shared digest package with binding-supplied rules". Option 3 ships as its first step.

Justification:

* One pure decision function replaces the copied blocks and is tested against the behaviour table below.
* One `ParseHashAlgorithm`/`FormatHashAlgorithm` pair replaces every mapping and answers "where do we accept these?".
* The rule is chosen from **what the binding computed**, never from the recorded label. This is the opposite of v1 (see [Option 2](#option-2-v1-style-digester-registry)), and it is required because every v2-created OCI resource carries G.
* Dispatch per access type already exists in the `digestprocessor` plugin registry, so no second registry is needed.

### Option 1

#### Description

`bindings/go/digest` owns the spec vocabulary: `ParseHashAlgorithm`/`FormatHashAlgorithm` on `crypto.Hash`, the `genericBlobDigest/v1` constant, and `Check`. It operates on the existing `descriptor/runtime.Digest` (`descriptor/runtime/descriptor.go:284`) and does not move or duplicate it.

**The kind comes from what was computed.** A binding that resolved a manifest or index root uses `ArtifactRule`. A binding that hashed raw bytes uses `BlobRule`. The recorded `normalisationAlgorithm` is only *validated* against that rule (canonical or `Accepted`, otherwise foreign) and is never used to choose the rule. Each binding picks the rule once, where content enters it:

* **Helm:** the `oci://` check stays in `helm/digest` as that entry point. `oci://` charts use `ArtifactRule` and HTTP charts use `BlobRule`.
* **OCI local blobs:** `ArtifactBlob` carries the rule, so `pack` does not inspect the blob again.

**`Check`.**

* It has no side effects, and an error means reject.
* `Result.Digest` is the triple the caller stores: for `Keep` and `KeepUnverified` it is the recorded triple unchanged; for `Generate` and `Complete` it is a new triple.
* **Never rewrite** is enforced inside `Check`, not left to each rule. A complete recorded digest always comes back byte for byte, whatever its label, spelling or value case.

**Excluded resources (`NO-DIGEST`).** The exclusion check runs first in `Check`, before any rule, mode or hash logic.

* If `IsExcluded(recorded)` is true, `Check` returns the dedicated decision `Excluded`, with `Result.Digest` equal to the recorded triple byte for byte. It ignores `computed` (a zero `Computed` is valid), `rule` and `mode`, and never returns an error for it.
* `Excluded` is a decision of its own rather than `Keep` or `KeepUnverified`. `Keep` means "verified against the content". `KeepUnverified` means "foreign label tolerated in Lenient mode". An excluded resource is neither. Callers need to know that no content was hashed, so they can skip downloads and log accurately, and so the behaviour table can list the case separately.
* Callers **should not compute anything** for an excluded resource, because its content is volatile by definition. Each entry point guards with `ocmdigest.IsExcluded` before downloading or hashing. `Check` handles the case on its own as well, so a caller that computes anyway is still safe.
* The binding never hashes, compares, completes or rewrites an excluded triple, and never changes its label. This applies to:
  * adding a component version by reference (the constructor skips the digest processor) and by value (input → local blob);
  * getting a component version;
  * every transfer path (OCI→OCI, local blobs, and uploads to non-OCI targets);
  * download.
* `IsExcluded` matches signing exactly: ignoring case, and **either** `hashAlgorithm == NO-DIGEST` **or** `normalisationAlgorithm == EXCLUDE-FROM-SIGNATURE` is enough. An invalid exclude triple is therefore also `Excluded` and kept as recorded, including:
  * `NO-DIGEST` with another normalisation;
  * `EXCLUDE-FROM-SIGNATURE` with a real hash;
  * a partial triple.

  This is never stricter than today. Signing's own completeness check (`signing/digest.go:173-180`) still rejects a *partial* triple at signing time, exactly as it does today. `Check` does not complete it.

**Hash names.**

* `ParseHashAlgorithm` ignores case and dashes and accepts the union of what any binding accepts today. That union is the floor, so a binding has nothing to add.
* `FormatHashAlgorithm` returns `SHA-256` or `SHA-512` and is only used for new or completed triples.
* `NO-DIGEST` is a spec hash name, but it has no `crypto.Hash`. `ParseHashAlgorithm("NO-DIGEST")` (any case) returns the sentinel `ErrNoDigest` (`errors.Is`) and never maps it to a hash. Callers check `IsExcluded` before parsing, in the same order signing uses today (`ValidateDigestHashAlgorithms` skips excluded resources before checking the hash, `signing/digest.go:202`). The sentinel only exists so that a caller that forgets the check gets a precise error instead of "unknown hash".

**`Mode` is chosen per entry point** so that each entry point keeps its current behaviour (see [Modes per entry point](#modes-per-entry-point)).

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

The three copied blocks in `oci/repository.go` (`:337`, `:721`, `:1176`) each become:

```go
if ocmdigest.IsExcluded(res.Digest) {
    return nil // volatile content: never resolved, hashed, compared or rewritten (Check would return Excluded)
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

The package imports only the stdlib, `bindings/go/runtime` and `bindings/go/descriptor/runtime`. `descriptor/runtime` depends only on `runtime` and `descriptor/v2`, and the existing `descriptor-runtime` and `runtime` depguard rules forbid any new imports there, so no import cycle is possible. The proposed `golangci.yml` rule follows the style of the existing lax rules:

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

`bindings/go/digest` must also be added to the allow lists of oci, helm, transfer, wget, s3, git, github, repository, signing, constructor, cli and kubernetes-controller.

### Hash names today

This table shows, per site, which `hashAlgorithm` spellings are accepted when a digest is recorded and which spelling is emitted for a new complete digest, at `ec1efe0bd`. A table test asserts that `ParseHashAlgorithm` accepts every spelling in the "Accepts" column.

| Site | Accepts (recorded) | Emits (new, complete) |
|---|---|---|
| `oci/internal/digest/digest.go:100-118` (process, upload, pack) | SHA-256/SHA-512, case-insensitive, dash optional | `SHA-256`, `SHA-512` |
| `oci/blob/artifact_blob.go:205-216` (ordinary blob G) | exact `SHA-256`, `SHA-512` | `SHA-256`, `SHA-512` (`:198-203`) |
| `helm/digest/digest.go:301-330` | complete: exact `SHA-256`; partial: case-insensitive, dash optional; SHA-256 only | `SHA-256` |
| `helm/transformation/convert_helm_chart_to_oci.go:122-130` | — | `SHA-256`, `SHA-512` |
| `transfer/internal/repositoryupload/digest.go:16,25` | exact `SHA-256`, `SHA-512` | `SHA-256`, `SHA-512` |
| `repository/verify_digest.go:34` (`VerifyDownload`) | SHA-256, case-insensitive, dash optional | — |
| `signing/digest.go:218-225` (`isApprovedDigestHash`) | SHA-256/SHA-512, case-insensitive, dash optional | — |
| `signing/digest.go:256-270` (`getSupportedHash`) | upper-cased `SHA-256`/`SHA-512` (`sha-256` yes, `sha256` no) | `SHA-256`, `SHA-512` |
| `wget/repository/resource_repository.go:246` (download), `:314` (peek) | exact `SHA-256`; peek: `SHA-256`/`SHA-512` case-insensitive | `SHA-256`; peek also `SHA-512` |
| `wget/input/method.go:175`, `wget/transformation/http_streaming.go:194` | case-insensitive `SHA-256` / exact `SHA-256` | `SHA-256` |
| s3 `:223`, git `:236`, github `:128` | `SHA-256`, case-insensitive (`sha256` no) | `SHA-256` (and re-spell complete digests) |
| `constructor/construct.go:763` (reference digests, unchanged) | exact triple compare (`:708`) | `SHA-256` |

The union is SHA-256 and SHA-512, case-insensitive, with an optional dash. Upstream main is narrower or equal at every site, for example exact `SHA-256` in main's OCI `Verify` and helm `verifyDigest`.

Every site already emits `SHA-256`/`SHA-512` for new *complete* digests. The only lowercase writer is main's helm→OCI conversion. It writes `HashAlgorithm: string(desc.Digest.Algorithm())` (`sha256`) without a normalisation (main `helm/transformation/convert_helm_chart_to_oci.go:121-124`), which is an *incomplete* digest. Main's upload and pack then overwrite that incomplete digest with `Apply`, which emits `SHA-256` (main `oci/repository.go:665-669`, `pack.go:361-365`). So `FormatHashAlgorithm` returning `SHA-256` changes no emitted complete digest.

### Modes per entry point

Each entry point keeps today's behaviour for a complete digest whose label is foreign to the rule.

| Entry point | Today | Mode |
|---|---|---|
| OCI `ProcessResourceDigest`, `UploadResource`, `UploadResourceStream` (`oci/repository.go:344,726,1183`) | reject | Strict |
| OCI layout and layer packing (`oci/internal/pack/pack.go:251-255`, `:197`; `artifact_blob.go:122-127`) | keep unverified | Lenient |
| helm `ProcessResourceDigest` (`helm/digest/digest.go:302-305`) | reject | Strict |
| wget processing (`:249`, `:317`), input (`method.go:178`), streaming upload (`http_streaming.go:197`) | reject | Strict |
| s3, git, github `ProcessResourceDigest` | reject | Strict |
| transfer `expectedDigest` for non-OCI sources (`transfer/.../digest.go:29`) | reject | Strict |
| `repository.VerifyDownload` (wget, s3, github downloads) | label ignored; bytes verified | unchanged; only hash parsing moves to `ParseHashAlgorithm` |
| OCI `GetComponentVersion`, `GetLocalResource`, `Download*` | not checked against `res.Digest` | unchanged; out of scope |

The asymmetry is intentional: packing a local blob has always preserved foreign complete digests (`pack_test.go:923`), and processing and upload have always rejected them.

### Excluded resources per entry point

Verified locally by the tests named below, run against the production code of the #3681 head `ec1efe0bd`. Each test records the exclude triple through one path and asserts the target behaviour. The tests land together with the implementation.

| Entry point | Exclude triple today | Afterwards | Test |
|---|---|---|---|
| Add by reference: the constructor calls the digest processor without checking the triple (`constructor/construct.go:488-510`) | fails in every processor (see the rows below) | processor not called; triple kept | `constructor` `TestConstructWithResourceDigestExcludedFromSignature` |
| OCI `ProcessResourceDigest` (`oci/repository.go:344`) | fails: "unsupported OCI artifact normalisation algorithm: EXCLUDE-FROM-SIGNATURE" | kept; root not compared | `oci` `TestRepository_ExcludedFromSignatureDigest/ProcessResourceDigest OCI image` |
| helm `ProcessResourceDigest` (`helm/digest/digest.go:121,302`; `:203` if the index has no digest, after fetching the index) | fails: normalisation mismatch, or no index digest | kept; index not fetched | `helm/digest` `TestProcessResourceDigest_ExcludedFromSignature` |
| wget `ProcessResourceDigest` (`wget/repository/resource_repository.go:201,246`) | fails: skip and prefer modes download and hash, then report a hash mismatch; require mode reports "no advertised checksum" | kept; nothing downloaded | `wget/repository` `TestProcessResourceDigest_ExcludedFromSignature` |
| s3 `ProcessResourceDigest` (`s3/repository/resource_repository.go:197,223`) | fails after downloading: hash mismatch | kept; nothing downloaded | `s3/repository` `Test_ProcessResourceDigest_ExcludedFromSignature` |
| git `ProcessResourceDigest` (`git/repository/resource_repository.go:141,236`; `:192` would overwrite) | fails after building the archive: hash mismatch | kept; no archive built | `git/repository` `TestResourceDigestExcludedFromSignature` |
| github digest processor (`github/digest/digest.go:129`) | fails: hash mismatch | kept | `github/digest` `TestDigestProcessor_ExcludedFromSignature` |
| Add by value: `AddLocalResource`, plain blob and OCI layout (`pack.go:197,243-256,396`) | **kept** | kept, without computing | `oci` `TestRepository_ExcludedFromSignatureDigest/AddLocalResource plain blob`, `/AddLocalResource OCI layout` |
| Get component version and v2 round trip | **kept** | unchanged | `oci` `TestRepository_GetComponentVersion_ExcludedFromSignatureDigest` |
| OCI→OCI transfer of a local blob (store as local blob, add and read the component version) | **kept** | unchanged | `oci` `TestRepository_GetComponentVersion_ExcludedFromSignatureDigest/local blob` |
| OCI `UploadResource` (`oci/repository.go:726`), `UploadResourceStream` (`:1183`) | fails: unsupported OCI normalisation | kept; root not compared | `oci` `TestRepository_ExcludedFromSignatureDigest/UploadResource`, `/UploadResourceStream` |
| Transfer to non-OCI targets, plain blob source (`transfer/internal/repositoryupload/digest.go:27`) | fails: "unsupported hash algorithm: expected SHA-256 or SHA-512, got NO-DIGEST" | kept; no expected digest | `repositoryupload` `TestUploadDigestExcludedFromSignature/plain blob source` |
| Transfer to non-OCI targets, OCI source (`digest.go:63-70`) | **rewritten** to `SHA-256` / `genericBlobDigest/v1` / hash of the uploaded bytes | kept | `repositoryupload` `TestUploadDigestExcludedFromSignature/OCI artifact source` |
| helm→OCI conversion (`helm/transformation/convert_helm_chart_to_oci.go:133-137`) | **rewritten** to `SHA-256` / `ociArtifactDigest/v1` / manifest digest, no error | kept | `helm/transformation` `TestConvertHelmChartToOCI_ExcludedFromSignature` |
| wget streaming upload (`wget/transformation/http_streaming.go:194`) | fails: unsupported hash (from the code; no test) | kept | none yet |
| Download: `repository.VerifyDownload` (`verify_digest.go:23`); OCI `Download*` | passed through without verification; OCI not checked | unchanged | `repository` `TestVerifyDownload` (`repository/verify_test.go:60`) |
| Signing and verification (`signing/digest.go:202,231`) | content left out of the signature; the triple itself is signed | unchanged; uses `ocmdigest.IsExcluded` | `signing` `TestSignVerifyWithResourceExcludedFromSignature` |

## Behaviour (R1–R14)

Test references are to `ec1efe0bd`. Rows marked *proposed* are pending maintainer agreement.

| Rule | Recorded digest | Decision | Evidence |
|---|---|---|---|
| R1 | absent or empty | **Generate**: `rule.Normalisation`, `FormatHashAlgorithm` (G for bytes, O for an OCI root) | `pack_test.go:744-745`; `repository_test.go:2513,2631` |
| R2 | incomplete, the fields that are set match | **Complete** with `rule.Normalisation` | `oci/internal/digest/digest_test.go:88-113`; `repository_test.go:2544-2566`; `pack_test.go:746` |
| R3 | incomplete, label in `Accepted` | **Complete**, relabelled to canonical; an incomplete digest cannot be signed (`signing/digest.go:173-180`) | `digest_test.go:100`; `helm/digest/digest_internal_test.go:42-46` |
| R4 | incomplete, label foreign to the rule (O on bytes) | reject | `digest_test.go:104` |
| R5 | complete, canonical or `Accepted` label | verify hash and value against the computed digest, then **Keep** byte for byte | `repository_test.go:2515-2541,2633` |
| R6 | complete G on an OCI root, i.e. every OCI resource v2 created before this change | **resolved:** G is in `ArtifactRule.Accepted` *indefinitely*. It is verified against the root (never against tar bytes) and **kept**, never relabelled. | `repository_test.go:144-204`; `digest_test.go:35`; CLI `transfer_oci_artifact_integration_test.go:716-890` |
| R7 | complete, foreign label | **resolved (proposed):** Strict where it is rejected today, Lenient in packing as today; never rewritten | `repository_test.go:2529-2531`; helm `digest_internal_test.go:54-58`; `pack_test.go:923` |
| R8 | hash spelling | **resolved (proposed):** `ParseHashAlgorithm` (the union); complete digests keep their spelling; new and completed digests use `FormatHashAlgorithm` | `digest_test.go:34,96`; `repository_test.go:2534-2536`; helm `:48-52` |
| R9 | hash set | SHA-256 and SHA-512; SHA-384 is rejected for resources (sources may carry it); helm gains SHA-512 (a widening) | `repository_test.go:2649-2675` |
| R10 | any rejection | happens before any destination write: no root copied, tag untouched, no push; a target-access pin must match the root | `repository_test.go:2615-2625,2667,2746-2749`; `pack_test.go:1012-1013` |
| R11 | input | `Check` never mutates; callers store `Result.Digest` on their own copy | `repository_test.go:2615,2731` |
| R12 | layout archive checksum | kept separate from the resource digest; never compared with it or substituted for it | `artifact_blob_test.go:462-527` |
| R13 | ordinary blob, G or unlabelled | is the byte checksum and must match the bytes; re-verified when buffered | `artifact_blob_test.go:529-645`; `pack_test.go:121-143,1014` |
| R14 | exclude triple (`NO-DIGEST` hash **or** `EXCLUDE-FROM-SIGNATURE` normalisation, ignoring case, including invalid and partial forms) | **Excluded**, checked before R1–R13: no hashing or comparison, never completed, relabelled or rewritten; `computed`, `rule` and `mode` ignored | see [Excluded resources per entry point](#excluded-resources-per-entry-point); `TestSignVerifyWithResourceExcludedFromSignature` |

### Compatibility audit

In-scope changes only, checked against Driver 1:

* **Widenings (rejected today, accepted afterwards):**
  * all spelling variants at every site, including `sha256` in `getSupportedHash`;
  * `sha256` on a complete G for local blobs (`artifact_blob.go:206` errors today);
  * SHA-512 in helm and `VerifyDownload`;
  * upper-case or `sha256:`-prefixed values at OCI;
  * case-insensitive normalisation labels (s3, git and github already accept them);
  * `ociArtefactDigest/v1`;
  * incomplete digests in s3, git, github and wget, which are completed instead of rejected;
  * the exclude triple (R14) when adding by reference (all six digest processors), in OCI `ProcessResourceDigest`, `UploadResource` and `UploadResourceStream`, in transfer from plain blob sources and in wget streaming upload. All of these fail today and keep it afterwards. Invalid and partial exclude forms are kept as recorded rather than rejected. Signing still rejects a partial one at signing time, as today.
* **Why R14 keeps the triple, never replaces it:** the exclude triple is itself part of the signed descriptor. Replacing it with a real digest makes verification fail with "digest mismatch" (`TestSignVerifyWithResourceExcludedFromSignature`). Any rewrite of the marker therefore breaks existing signatures.
* **Changed in two otherwise out-of-scope paths, both needing maintainer agreement:**
  * Transfer of an OCI source to a non-OCI target rewrites the exclude triple to `SHA-256` / `genericBlobDigest/v1` / hash of the uploaded bytes (`transfer/internal/repositoryupload/digest.go:63-70`).
  * The helm→OCI conversion rewrites it to `SHA-256` / `ociArtifactDigest/v1` / manifest digest (`helm/transformation/convert_helm_chart_to_oci.go:133-137`).

  With R14 both keep the triple. Only excluded resources are affected; the rest of both output paths stays [out of scope](#out-of-scope). Both turn a signature-breaking rewrite into a keep, so neither can break a digest that is accepted today. Both change the output for those resources.
* **Unchanged:**
  * Historical G is kept (R6). Completion follows R2 and R3. R4 is unchanged, and R7 keeps each entry point's current behaviour.
  * OCI→OCI transfer keeps the source digest (`oci/repository.go:665`; CLI `transfer_oci_artifact_integration_test.go:716-890`).
  * The transfer output path (`transfer/internal/repositoryupload/digest.go:58-72`) is unchanged apart from hash-name parsing and the excluded-resource keep described above.
  * The R13 byte check only moves *before* the push. The push already rejects mismatched bytes (`pack_test.go:1014`).
  * Removing the `update.go` defaulting yields the same triple through `Generate`, guarded by the R1 tests.
  * Exclude triples on add by value, get, OCI→OCI transfer of local blobs, download and signing (R14), all verified by the tests above.
* **Stricter: none relative to the #3681 head.**
* **Stricter relative to upstream main, flagged:** R2 rejects an *incomplete* digest whose set fields contradict the computed root. Main's upload and pack instead overwrite any incomplete digest silently (main `oci/repository.go:665-669`, `:1166-1170`; `pack.go:361-365`). Incomplete digests cannot carry a signature, but an input that main accepts would be rejected. This needs maintainer agreement: either keep R2 as validated in #3681, or have `Check` treat contradictory incomplete digests as `Generate`.
* **Residual:** completing an incomplete digest changes bytes that a parent component reference digest could cover. Main and OCI already do this today.

## Public API impact

* **New:** `bindings/go/digest` (including `IsExcluded`, `ErrNoDigest` and the `Excluded` decision); `bindings/go/oci/digest` (`ArtifactRule`, `BlobRule`, `FromOCI`, root computation).
* **Removed (internal or unexported):**
  * `oci/internal/digest`;
  * the local constants and mappings in helm, transfer, wget, s3, git and github;
  * `signing.isApprovedDigestHash` and `getSupportedHash`, both replaced by `ParseHashAlgorithm` (a widening for `getSupportedHash`).
* **Digests:** no accepted digest changes or fails, except the flagged R2 point relative to main.
* **Breaking Go API (`!`, approved in principle):** in `oci/blob`:
  * `NewArtifactBlob` takes the rule and stops mutating its argument.
  * `Digest()` is a byte checksum only.
  * `SetPrecalculatedDigest` and the defaulting in `UpdateArtifactWithInformationFromBlob` are removed.

## Migration plan

The ADR and the implementation land in separate PRs. The split happens after the design has been validated on #3681 (see below). The implementation is sliced into:

1. **Hash names.** Add `bindings/go/digest` (`ParseHashAlgorithm`, `FormatHashAlgorithm`, constants), the spelling table test and the depguard rule. Migrate signing (`signing/digest.go:194-270`), `repository/verify_digest.go`, transfer, helm and the local constants. s3, git and github stop re-spelling complete digests.
2. **`Check`, `Rule`, `Mode`.** Add `oci/digest` (`ArtifactRule`, `BlobRule`, `FromOCI`). Replace `oci/internal/digest`, the three `oci/repository.go` blocks, the pack verify/complete code (verifying *after* completion), helm `verifyDigest` and transfer `expectedDigest`, each with the mode from the table above. Add the `IsExcluded` guard (R14) at every entry point in [Excluded resources per entry point](#excluded-resources-per-entry-point), including the constructor before it calls the digest processor, which also covers external digest-processor plugins. Move the pack digest tests into `pack_test.go`.
3. **`feat(oci)!`.** The rule is chosen once at entry and carried on `ArtifactBlob`. `Digest()` becomes a byte checksum, verified before the push. Delete the double defaulting, `isOCILayout` and `SetPrecalculatedDigest`.
4. **Later:** the ADR 0031 chunked rule in `oci/digest`. It is chosen from what is computed (the logical content behind a chunk manifest), never from the stored shape.

A separate issue covers the wget peek path recording SHA-512 while `VerifyDownload` accepts only SHA-256 (`wget/repository/resource_repository.go:285,329-333`; found by static analysis only). Slice 1 fixes it by widening.

`website/content/docs/reference/oci-resource-digests.md` is updated in place with slice 2.

### Out of scope

* Verifying OCI downloads against `res.Digest`.
* The digest that transfer writes for OCI sources uploaded to non-OCI targets (Artifactory, Nexus, HTTP; `transfer/internal/repositoryupload/digest.go:58-72`), and the digest written by the helm→OCI transformation (`helm/transformation/convert_helm_chart_to_oci.go:122-137`). OCI→OCI transfer keeps the digest and is unaffected.

## What happens to #3681

Until the split, PR #3681 validates this ADR. It carries the ADR plus an implementation of `bindings/go/digest`, `Check`/`Rule`/`Mode` and `oci/digest` (slices 1–2), which proves the design against #3681's tests: `oci/internal/digest`, `oci/repository_test.go`, `pack_test.go`, `artifact_blob_test.go`, helm `digest_internal_test.go` and the CLI transfer integration test. These tests form the regression suite and must pass unchanged, apart from the move into `pack_test.go`. Once the design is validated, the ADR and the implementation are split into separate PRs, and slice 3 follows on its own.

## Review feedback

| Feedback on #3681 | Decision |
|---|---|
| Jakob, CHANGES_REQUESTED: "quite some interface assertions" in blob handling | The rule is chosen once, from what the binding computed, and carried on the concrete `ArtifactBlob`. The kind-rediscovering assertions in `artifact_blob.go:136-155` and `pack.go:69` go away, and no new blob interface is added (slice 3). |
| Jakob, `oci/blob/update.go:20` "looks very very wrong" (a hack) | The defaulting in `UpdateArtifactWithInformationFromBlob` is deleted; Generate and Complete go through `Check` after packing (slices 2–3). |
| Jakob, `oci/internal/digest/digest.go:102`: "where do we actually accept these as hash algorithms?" / "should be standardized" | `ocmdigest.ParseHashAlgorithm` is the one place (R8, slice 1). |
| Jakob (Slack): "the code is actually correct … but the way its integrated is very messy because we dont have clean abstractions in the OCI package" | The behaviour is kept (R1–R14, Driver 1). The structure moves to `digest` (vocabulary) and `oci/digest` (OCI rules). |
| Frederic, `helm/digest/digest.go:303` "too complicated" | Helm calls `Check` with `ArtifactRule` (`oci://`) or `BlobRule` (HTTP). The legacy exception becomes data (`Accepted`), not chained `if`s (slice 2). |
| Skarlso, `pack.go:201`: verify after fill-in, so incomplete digests are not rejected | `Check` decides Complete vs Keep before verifying; pack verifies only complete digests (R2, R13). |
| Skarlso, `digest.go:106`: use `sameHashAlgorithm` | Superseded by `ParseHashAlgorithm` for complete and incomplete digests alike. |
| Skarlso: "Helm is also using this" | Helm uses the public `oci/digest` rules (slice 2). |
| Skarlso: "should be an ADR" | This ADR. |
| Skarlso: "breaking change, needs `!`" | No digest breaks (Driver 1). Only the `oci/blob` Go API change (slice 3) carries `!`. |
| Piotr: `pack/digest_test.go` belongs in `pack_test.go` | Moved in slice 2. |

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

* v1 picks the digester from the *recorded* label. In v2 that label is G on every OCI resource created before this change (main `oci/internal/digest/digest.go:28-37`), so dispatching on it would hash tar bytes instead of the root and break every such digest.
* It adds a second dispatch axis next to the `digestprocessor` registry, with global mutable state for a closed set of two.

### [Option 3] Consolidate hash names only

Pros:

* Small and low risk; it answers the hash-name question.

Cons:

* The copied decision blocks and the kind rediscovery remain, so it does not address the main review objection. It is used only as slice 1.

## Conclusion

Digest rules become plain data owned by the bindings. The rule is chosen from what was computed, and one pure `Check` in `bindings/go/digest` decides what to do. `ParseHashAlgorithm` accepts the union of everything accepted today, complete recorded digests are never rewritten, historical G on OCI roots is accepted indefinitely, excluded (`NO-DIGEST`) resources are kept without ever being hashed, and no entry point becomes stricter than at the #3681 head.
