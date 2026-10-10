# FIPS 140-3 Support for OCM Signing

* **Status**: proposed
* **Deciders**: OCM Technical Steering Committee
* **Date**: 2026-10-05

Technical Story: [ocm-project#1327](https://github.com/open-component-model/ocm-project/issues/1327), part of the FIPS 140-3 epic [ocm-project#1197](https://github.com/open-component-model/ocm-project/issues/1197). User documentation: the [FIPS 140-3 reference](../../website/content/docs/reference/standards-and-regulations/fips.md). Implemented in [#3691](https://github.com/open-component-model/open-component-model/pull/3691) and [#3747](https://github.com/open-component-model/open-component-model/pull/3747).

---

## Context and Problem Statement

FIPS 140-3 is a US government standard for cryptographic modules. Regulated users may only run cryptography that executes inside a module with a FIPS 140-3 validation certificate.

OCM wants to serve these users without splitting its distribution. The epic sets three principles:

* **One artifact.** There is no separate FIPS build of the CLI or the controller. Every release is built against the certified Go Cryptographic Module, so the same binary runs in regulated and unregulated environments.
* **No surprise for existing users.** Building for FIPS must not take features away from users who never asked for FIPS.
* **Honest boundaries.** Where cryptography runs outside a validated module, OCM says so instead of hiding it.

OCM's own cryptography (hashing, RSA signing, TLS) already runs in the Go module once it is built for FIPS. Three features do not, because their cryptography lives somewhere else:

* **GPG signing** used a Go OpenPGP library outside the Go module. Some of its algorithms, and unlocking passphrase-protected keys, are never FIPS-approved.
* **Sigstore signing** runs the external `cosign` program. The upstream `cosign` release is not a FIPS build.
* **Helm chart provenance verification** uses the same OpenPGP library as the old GPG signing.

A further complication: Go offers three runtime modes. `fips140=off` disables FIPS, `fips140=on` uses approved implementations but lets non-approved algorithms still run, and `fips140=only` makes non-approved algorithms fail. A binary built for FIPS starts in `on` by default. Go describes `only` as a mode for testing and assessment, not for production.

This ADR decides how OCM signing behaves in each mode, and where the FIPS boundary of OCM ends.

## Decision Drivers

* One artifact for everyone.
* Users who do not set anything keep today's behavior.
* Users who need a hard guarantee get one, with a single, documented switch.
* GPG users, including those with passphrase-protected keys, keep a FIPS-compliant path.
* OCM does not take over the supply chain of third-party tools.
* Few new dependencies.

## Considered Options

When OCM enforces FIPS:

* **E1**: whenever FIPS mode is on
* **E2**: only in the strict mode `fips140=only`; the default mode reports instead

GPG:

* **G1**: keep the Go OpenPGP library and allow only approved key types
* **G2**: hand all GPG operations to the system GnuPG installation
* **G3**: remove GPG signing in favour of RSA
* **G4**: a separate FIPS build without GPG

Sigstore (`cosign`):

* **C1**: check whether the `cosign` in use is a FIPS build, and require one in strict mode
* **C2**: OCM builds and distributes its own FIPS `cosign`
* **C3**: run the upstream `cosign` in FIPS mode

Helm provenance:

* **H1**: refuse provenance verification in strict mode
* **H2**: verify provenance through the system GnuPG

## Decision Outcome

Chosen **E2**, **G1**, **C1** and **H1**.

Justification:

* **E2:** FIPS mode is on in every release build by default. Enforcing whenever it is on (E1) would remove features from every user, for example the automatic `cosign` download, which contradicts "no surprise for existing users". E2 keeps the default permissive and gives regulated users an explicit opt-in.
* **G1:** go-crypto runs RSA, NIST ECDSA, SHA-2 and RNG through the Go Cryptographic Module. Strict mode allows only unprotected v6 RSA/ECDSA-P keys and SHA-2 signatures. Everything else (v4 keys with SHA-1 fingerprints, EdDSA/Ed25519/Ed448 via circl, DSA, other curves, passphrase-protected keys, secret-key export from the GnuPG keyring) runs with a debug log by default and is rejected in strict mode. The GnuPG keyring stays usable as a key source (`keyringFingerprint`): gpg only exports keys; no signing or verification runs in gpg. Nothing has to ship in images or stay in lockstep with a libc base image.
* **C1:** OCM cannot vouch for an arbitrary program, but it can tell whether `cosign` was built against a frozen Go Cryptographic Module. That is enough to report in the default mode and refuse in strict mode. C2 would make OCM responsible for building and shipping a third-party tool. C3 is not compliant, because the upstream build has no validated module to switch on.
* **H1:** treating Helm provenance like the other external cryptography keeps the rules uniform. H2 would mean rebuilding Helm's provenance check around GnuPG, which can follow if users need it.

### Runtime modes

OCM follows Go's three modes and attaches one rule to each:

| Mode | Meaning for OCM |
|---|---|
| `fips140=off` | FIPS disabled. No checks. |
| `fips140=on` (default) | OCM's own cryptography runs in the validated module. Steps that leave the FIPS boundary still run and are reported in the debug log. |
| `fips140=only` | Steps that leave the FIPS boundary fail with an error that explains what to change. |

The steps covered by this rule:

* a `cosign` that is not a FIPS build, or downloading the upstream `cosign`;
* GPG keys other than unprotected v6 RSA or ECDSA P-256/P-384/P-521 keys, and exporting secret keys from the GnuPG keyring;
* Helm chart provenance verification;
* component digests that do not use SHA-256 or SHA-512, because a signature must not rest on a weak hash. In the other modes these are accepted with a warning, so existing component versions stay usable.

### GPG

GPG signing and verification run in-process with the Go OpenPGP library (`github.com/ProtonMail/go-crypto`), whose RSA, NIST ECDSA, SHA-2 and RNG run through the Go Cryptographic Module.

* `keySource` is replaced by `keyringFingerprint` and `keyringHome` in `GPGCredentials`. When set, OCM runs the `gpg` binary only to export key material (`gpg --export`, `gpg --export-secret-keys`); all signing and verification runs in OCM.
* Signatures from the previous gpg-binary and former go-crypto implementations still verify (v4 keys outside strict mode). OCM reads v4, LibrePGP v5 and RFC 9580 v6 keys; encryption-only subkeys are ignored, so GnuPG's PQC keys with Kyber subkeys work.
* Strict mode accepts exactly: OpenPGP v6 (RFC 9580) or v5 (LibrePGP) keys, both with SHA-256 fingerprints, whose primary key and signing subkeys all use RSA or ECDSA P-256/P-384/P-521 (reading a key verifies their self-signatures), unprotected secret keys, and SHA-256/384/512 signatures. It rejects exporting secret keys from the keyring.
* Hardware-token keys are not supported: `--export-secret-keys` yields only card stubs, so signing fails with a clear error.
* GnuPG creates v4 keys for RSA and ECDSA and cannot create or verify v6 keys (it implements LibrePGP), so keyring keys never pass strict mode. Strict-mode users create keys with an RFC 9580 implementation such as Sequoia (`sq key generate --profile rfc9580`).

### Sigstore

OCM keeps using the external `cosign` program. When FIPS mode is on, OCM checks whether that `cosign` was built against a frozen Go Cryptographic Module and handles the result according to the runtime mode. Operators who need strict mode supply a FIPS `cosign`, either by building it themselves with the Go FIPS settings or from a vendor.

### Where the boundary ends

Some features use non-approved hashes for purposes that are not security relevant:

* **Git** names its objects with SHA-1.
* **HTTP checksum verification** accepts MD5 and SHA-1 checksums that servers publish.

These run outside strict enforcement so they keep working in `fips140=only`. FIPS mode itself stays on, so network connections still use approved algorithms only, and every digest OCM records and signs is SHA-256.

### Keeping it that way

* A lint rule rejects imports of non-approved cryptography in OCM's own code. The GPG signing handler (`bindings/go/gpg/signing/handler`) is exempted because it enforces the FIPS 140-3 mode rules itself.
* A small set of test packages runs in strict mode on every CI run, including `bindings/go/gpg/fips140`, so a change that pulls a non-approved algorithm into signing, Git or checksum handling fails CI. All other tests run in the default mode.

## Pros and Cons of the Options

### [E1] Enforce whenever FIPS mode is on

Pros:

* Nothing runs outside the boundary on a FIPS host.

Cons:

* Every user loses features by default, because every release runs in FIPS mode.
* Users would have to switch FIPS off entirely to get their features back.

### [E2] Enforce only in strict mode

Pros:

* No change for users who do not opt in.
* One documented switch gives a hard guarantee.

Cons:

* In the default mode, steps outside the boundary are only visible in the debug log.
* Strict mode is labelled by Go as not meant for production, and other tools that inherit the setting may fail.

### [G1] Approved key types only (go-crypto in-process)

Pros:

* No external program needed for signing or verification; the GnuPG keyring stays usable as a key-export source.
* RSA, NIST ECDSA, SHA-2 and RNG run through the Go Cryptographic Module.
* Nothing has to ship in images or stay in lockstep with a libc base image.

Cons:

* No FIPS path for passphrase-protected keys.
* No hardware tokens and no gpg-agent passphrase cache (a follow-up would need a signer abstraction in the handler).
* GnuPG cannot create or verify v6 keys (it implements LibrePGP), so strict-mode users create keys with an RFC 9580 implementation such as Sequoia, and keyring keys never pass strict mode.

### [G2] System GnuPG

Pros:

* All GPG cryptography, including passphrase unlocking, runs in a validated module on supported distributions.

Cons:

* GnuPG becomes a requirement for GPG signing in every mode.
* GnuPG must ship in every image, and validated libgcrypt exists only on some distributions.

### [G3] Remove GPG

Pros:

* Signing is FIPS-native everywhere.

Cons:

* Breaks existing GPG users.

### [G4] Separate FIPS build without GPG

Pros:

* Simple.

Cons:

* Breaks the one-artifact principle; FIPS users lose GPG.

### [C1] Check `cosign`, require a FIPS build in strict mode

Pros:

* Small change; the default mode keeps the automatic download.

Cons:

* Strict mode depends on the operator providing a FIPS `cosign`.
* FIPS builds of `cosign` that use another validated library than the Go module are not recognised yet.

### [C2] OCM ships its own FIPS `cosign`

Pros:

* Strict mode works without operator effort.

Cons:

* OCM becomes responsible for building, signing and updating a third-party tool.

### [C3] Run upstream `cosign` in FIPS mode

Pros:

* No change.

Cons:

* Not compliant: the upstream build contains no validated module.

### [H1] Refuse Helm provenance in strict mode

Pros:

* Clear error, consistent with the other rules.

Cons:

* No provenance verification in strict mode.

### [H2] Helm provenance through GnuPG

Pros:

* Provenance verification inside a validated module.

Cons:

* Rebuilds a Helm feature inside OCM.

## Discovery and Distribution

* All behavior ships in the one CLI binary and the one controller image.
* The CLI `full` image is `scratch` with `ocm` and a FIPS `cosign`. The `slim` image is `scratch` with `ocm` only. The controller image is `scratch` with `manager` and a FIPS `cosign`. No image contains GnuPG or a libc. Keyring keys need a host GnuPG.
* The FIPS 140-3 reference documents the modes, the requirements for `cosign`, the exceptions and the known limitations.

Open follow-ups:

1. GPG and Sigstore verification in the controller, under the rules of this ADR.
2. Recognise FIPS builds of `cosign` that use a validated library other than the Go module.

## Conclusion

OCM ships one artifact built against the certified Go Cryptographic Module. By default nothing changes for users, and steps that leave the FIPS boundary are reported. Users who need a guarantee switch to strict mode and get a clear error for every such step. GPG signing and verification run in-process with the Go OpenPGP library, with strict-mode rules for approved key types; the GnuPG keyring stays usable as a key source. Sigstore keeps the external `cosign` with a FIPS check, and Helm provenance is refused in strict mode.
