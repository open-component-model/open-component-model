# Pinned-Verification Integrity Model

* **Status**: proposed
* **Deciders**: OCM Maintainer Team
* **Date**: 2026-10-09

## Context and Problem Statement

`ocm verify` establishes trust at a point in time (T0). A subsequent command that acts on the same component — `ocm transfer`, `ocm get componentversion`, `ocm download resource` — runs at a later point (T1) and re-fetches content from the repository. Nothing binds the content seen at T1 to what was verified at T0: between the two, an attacker with write access to the repository can tamper with descriptors or resource blobs. This is a time-of-check/time-of-use (TOCTOU) gap in the `verify → act` pipeline.

OCM signing is already a Merkle tree. `Reference.Digest` and resource `Digest` (`bindings/go/descriptor/runtime/descriptor.go`) are embedded in each component descriptor, and the root's normalized signed digest transitively commits to them. The transfer walk already exercises this: `bindings/go/transfer/internal/discovery.go` records each reference's expected digest in `discoveredDigests` and verifies every fetched child via `signing.VerifyDigestMatchesDescriptor` (`bindings/go/signing/digest.go`). The one node with no parent reference to anchor it is the **root**.

The question this ADR settles is **what integrity anchor is carried from `verify` into the consuming command, and what exactly it must bind** so that the consuming command can re-establish integrity at T1 without re-running signature verification and without any trust material.

This ADR covers only the integrity model for commands invoked **on a root** (the root pin anchors the existing downward walk). Making genuinely rootless non-root invocations verifiable (per-node signatures) is a separate, larger decision with its own ADR and is explicitly out of scope here.

## Decision Drivers

* **Close the TOCTOU window** for the `verify → act` pipeline without weakening the existing signature model.
* **No trust material at T1.** The consuming command should need no keys, certificates, or verifier configuration — only a cheap integrity check against the anchor produced at T0.
* **Resistance to downgrade/coverage attacks.** At T1 the re-fetched descriptor is attacker-controlled; the anchor must not let the attacker choose how it is checked.
* **Reuse existing machinery.** The reference-digest walk already exists; prefer seeding it over inventing a parallel mechanism.
* **Composability / good UX.** The anchor must be scriptable and pipeable (`ocm verify … | ocm transfer …`).

## Considered Options

### Option 1: Self-describing pin binding normalisation + hash algorithm + value (typed config)

#### High-Level Design
`verify`, after a successful verification, emits a typed configuration document (a *pin*) carrying, per component identity, the full digest specification of the verified root: normalisation algorithm, hash algorithm, and hex value — i.e. the existing `descruntime.Digest`. The consuming command receives the pin via the existing stdin-config mechanism (`--config -`), seeds `discoveredDigests[rootIdentity]`, and the existing walk verifies the root exactly like any child, propagating trust down the tree. No signature verification and no trust material are needed at T1 — only normalise-and-hash plus a byte compare.

#### Structural / API Changes
* New typed config, e.g. `PinnedDigests/v1alpha1`, under `bindings/go/configuration/` (sibling of the existing `checksum` config). Value per entry is `descruntime.Digest`.
* `verify` gains a flag (e.g. `--emit-pin`) that writes the pin document to stdout, derived from the verified `descruntime.Signature`.
* Transfer seeds the root entry of `discoveredDigests` from the pin before discovery; `get cv` verifies the fetched root against the pin before output.

#### Pros and Cons
* **Pros**: binds *what the digest covers* (normalisation) as well as the hash, so neither can be chosen by the attacker at T1; self-describing, so it survives normalisation-algorithm version bumps; reuses the walk; composes over stdin config.
* **Cons**: the anchor is a small structured document rather than a one-line token; a new (versioned) config type to maintain.

### Option 2: Bare `algo:hex` pin with a hardcoded canonical normalisation

#### High-Level Design
Carry only `hashAlgorithm:value` (go-digest style). Both `verify` and the walk always normalise under a single canonical algorithm fixed in code, never read from the descriptor.

#### Structural / API Changes
* A compact string flag/output instead of a config type.
* A constant canonical normalisation at producer and consumer.

#### Pros and Cons
* **Pros**: smallest possible anchor; trivially pipeable.
* **Cons**: couples both sides to one normalisation version — a pin emitted under vN silently fails against a consumer defaulting to vN+1, a real migration hazard; the canonical choice is implicit and easy to drift.

### Option 3: Re-run signature verification inside each consuming command

#### High-Level Design
Instead of a pin, each consuming command re-verifies the signature itself at T1.

#### Pros and Cons
* **Pros**: no new artifact; always current.
* **Cons**: requires trust material (keys/verifier config) wherever the command runs; repeats cryptographic work; does not express "the exact thing I verified a moment ago" and still races unless folded into one atomic operation.

### Option 4: Persisted `checksum.db` lookup table built during verify

#### High-Level Design
`verify` writes a lookup table of digests for the whole tree to disk; consuming commands read it.

#### Pros and Cons
* **Pros**: covers the whole tree in one pass.
* **Cons**: adds an on-disk artifact that is itself a tamper target; makes verify eagerly walk the whole tree (expensive) even when the consumer touches few nodes; the pinned-root + lazy-walk approach gets the same guarantee without persistence.

## Decision Outcome

Chosen Option: **Option 1 — self-describing pin binding normalisation + hash algorithm + value, delivered as typed stdin config.**

### Justification
At T1 there is no signature verification — only the pin and an attacker-controlled, re-fetched descriptor. The anchor must therefore be checkable without trust material *and* leave the attacker no freedom in how the check is performed:

* Binding only the hash **value** is insufficient. The attacker controls the descriptor's declared digest fields, so a walk that reads the hash algorithm from the descriptor can be **downgraded** to a weak hash (e.g. a chosen-prefix SHA-1 collision). The pin must bind the hash algorithm.
* Binding only `algo:hex` is still insufficient. Normalisation is a canonicalisation that deliberately discards variation (field ordering, `EXCLUDE-FROM-SIGNATURE` fields, `jsonNormalisation/v1` vs later versions). It defines **what the digest covers**. If the walk reads the normalisation from the descriptor, the attacker selects a weaker projection whose equivalence class lets them tamper a covered field while the normalised bytes — and thus the hash — still match. This is a coverage downgrade, not a preimage attack, so a strong hash does not prevent it. The pin must bind the normalisation.

Binding all three closes both downgrades. A self-describing pin (Option 1) is preferred over a hardcoded canonical normalisation (Option 2) because it survives normalisation-version evolution without a silent-failure migration hazard. Re-verification (Option 3) reintroduces trust material at T1 and does not express "exactly what I just verified." A persisted `checksum.db` (Option 4) adds a tamper target and forces an eager full-tree walk; seeding the already-existing lazy walk from a pinned root yields the same guarantee without persistence.

### Consequences / Trade-offs
* A new versioned config type (`PinnedDigests/v1alpha1`) and an `emit-pin` output on `verify`; deepcopy and JSON schema are generated.
* The consuming command performs a normalise-and-hash plus byte compare per node it touches — cheap, no crypto, no trust material. Mismatch is a hard failure.
* Delivery over `--config -` makes `ocm verify … | ocm transfer … --config -` the natural pipeline; a dedicated flag is the scriptable fallback.
* The pin anchors only the **root**; trust reaches the rest of the tree through the existing reference-digest walk. The resource-content leg must separately bind downloaded bytes to the **signed OCM resource digest** (not merely the access/content-address digest), tracked in the P1 implementation.
* **Out of scope / follow-up**: genuinely rootless non-root invocations (`get cv <sub>`, `download resource <sub>` with no root in hand) cannot be anchored by a root pin and require per-node signatures — a separate ADR.
