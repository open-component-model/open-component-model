# Verify-Before-Use Enforcement for Component Operations

* **Status**: proposed
* **Deciders**: OCM Maintainer Team
* **Date**: 2026-10-09

## Context and Problem Statement

`ocm verify` establishes trust at a point in time (T0). A subsequent command that acts on the same component — `ocm transfer`, `ocm get componentversion`, `ocm download resource` — runs at a later point (T1) and re-fetches content from the repository. Nothing binds the content seen at T1 to what was verified at T0: an attacker with write access to the repository can tamper with descriptors or blobs in the window between the two commands. This is a time-of-check/time-of-use (TOCTOU) gap in the `verify → act` pipeline.

OCM signing is already a Merkle tree. `Reference.Digest` and resource `Digest` (`bindings/go/descriptor/runtime/descriptor.go`) are embedded in each component descriptor, and the root's normalized signed digest transitively commits to them. The transfer walk already exercises this: `bindings/go/transfer/internal/discovery.go` records each reference's expected digest in `discoveredDigests` and verifies every fetched child via `signing.VerifyDigestMatchesDescriptor` (`bindings/go/signing/digest.go`). The only node with no parent reference to anchor it is the **root**.

All of these commands resolve and load a component version through the same path — `ocm.NewComponentVersionRepositoryForComponentProvider` → `GetComponentVersion` — which is also what `verify` itself uses.

This ADR decides **where verification happens and how it is triggered** so that a consuming command cannot act on content that was not verified. It covers only commands invoked **on a root** (the root anchors the existing downward walk). Making genuinely rootless non-root invocations verifiable (per-node signatures) is a separate, larger decision with its own ADR and is out of scope here.

## Decision Drivers

* **Close the TOCTOU window** for `verify → act` without weakening the existing signature model.
* **No flag sprawl.** Verification is a cross-cutting concern over *loading a component version*; it should not be re-implemented or re-flagged on every command.
* **Reuse existing machinery** — the shared CV-load path and the anchored reference-digest walk already exist.
* **Do not require trust material where it cannot exist.** Some pipelines verify in a gated step that holds keys and act in a later step that does not.
* **Fail closed.** A missing or invalid verification result must stop the action, never silently proceed.

## Considered Options

### Option 1: Per-command `--verify` flag (inline)

#### High-Level Design

Each acting command grows a `--verify` flag; when set, the command verifies the root signature itself and then performs the anchored walk, all in one process against the same fetched bytes. There is no T0/T1 window.

#### Structural / API Changes

* A `--verify` flag plus verifier wiring duplicated on `transfer`, `get cv`, `download resource`, and every future CV-loading command.

#### Pros and Cons

* **Pros**: eliminates the window; trust material lives with the actor (the thing acting is the thing that verified).
* **Cons**: flag sprawl — the same concern re-implemented on every command; verification is a property of *loading a CV*, not of each command, so this puts it at the wrong layer; easy to forget to pass on one command and silently act unverified.

### Option 2: Pipe a pin between invocations (`verify | transfer --config -`)

#### High-Level Design

`verify` emits a pin (the verified root digest) to stdout; the acting command reads it via the existing stdin-config mechanism and verifies the re-fetched root against it before acting.

#### Structural / API Changes

* A pin output on `verify` and a pin-consuming config on the actor.

#### Pros and Cons

* **Pros**: composable in the shell; keeps trust material in `verify`.
* **Cons**: a shell pipe starts both processes concurrently, so the actor never sees `verify`'s exit code; `pipefail` only reports failure *after* the action's side effects. Making it safe requires the actor to **fail closed on a missing pin**, which in turn needs either an explicit `--require-pin` opt-in or an always-emitted status document (`ok`/`failed`) to disambiguate "verify failed" from "no pin supplied" — and even then a crashed `verify` yields empty stdin that is indistinguishable from a legitimate unpinned run. The safety machinery is entirely an artifact of using a pipe.

### Option 3: Verification policy enforced at the shared CV-load layer (config-driven)

#### High-Level Design

Verification becomes a policy attached to *loading a component version*, configured in `.ocmconfig`, and enforced in the shared resolve/load path that every command already uses. When the policy requires verification, loading a root CV verifies its signature inline and then the existing walk checks children against reference digests and resources against their signed digests. Every CV-loading command inherits this with no per-command surface, and because check-and-use happen in one process on the same bytes, there is no window.

#### Structural / API Changes

* A new typed verification-policy config (extending the verifier configuration ADR 0008 already places in `.ocmconfig`), e.g. a `require` posture.
* An enforcement hook in `ocm.NewComponentVersionRepositoryForComponentProvider` / `GetComponentVersion` (reusing `signing.VerifyDigestMatchesDescriptor` and the resolver's handler lookup).
* Resource download binds bytes to the **signed OCM resource digest**, not merely the access/content-address digest (close the gap in the spirit of #3641).

#### Pros and Cons

* **Pros**: no flag sprawl and no handoff — the concern lives in one place; applies uniformly to current and future commands; reuses the anchored walk; no TOCTOU window; policy is explicit and auditable in config.
* **Cons**: the actor must hold trust material (verifier config/keys) wherever a verifying command runs — unsuitable on its own for pipelines that deliberately separate key-holding from acting; verification cost is paid on every policy-enabled load.

### Option 4: File-based pin handoff gated by `&&`

#### High-Level Design

For pipelines that must separate verification (key-holding, gated) from the action (no keys), `verify` writes the verified root digest to a file and the actor consumes it:

```shell
ocm verify … --output-pin pin.json && ocm transfer … --pin pin.json
```

`&&` gates on `verify`'s exit code — the actor does not run if verification failed. `--pin pin.json` is explicit, so there is no "is this pinned mode?" ambiguity and no "absent" case. The actor seeds the root of the anchored walk from the pin instead of re-verifying a signature, so it needs no trust material.

Because the actor does **not** re-verify the signature, the pin must bind everything the check depends on: **normalisation algorithm + hash algorithm + value**. Binding only the value lets an attacker-controlled re-fetched descriptor downgrade the declared hash; binding only `algo:hex` lets them select a weaker normalisation whose coarser equivalence class permits tampering a covered field while the hash still matches (a coverage downgrade, not a preimage — a strong hash does not prevent it).

#### Structural / API Changes

* `verify --output-pin <file>` writing a typed pin document (per-component `descruntime.Digest`).
* `transfer --pin <file>` (and peers) seeding `discoveredDigests[root]`; invalid/empty file is fatal.

#### Pros and Cons

* **Pros**: works when the actor has no trust material; `&&` gives exit-code gating with no concurrency; explicit `--pin` removes absence ambiguity; no status-doc or `--require-pin` gymnastics.
* **Cons**: a pin flag on the acting commands (narrower than Option 1 — only where handoff is wanted); a transient file; the actor trusts the pin producer rather than re-verifying.

### Option 5: Persisted `checksum.db` for the whole tree

#### High-Level Design

`verify` eagerly walks the whole tree and writes a digest lookup table to disk; consuming commands read it.

#### Pros and Cons

* **Pros**: whole tree covered in one pass.
* **Cons**: an on-disk artifact that is itself a tamper target; forces an eager full-tree walk even when the consumer touches few nodes; the pinned-root + lazy-walk approach gives the same guarantee without persistence.

## Decision Outcome

Chosen Option: **Option 3 (verification policy at the shared CV-load layer) as the default mechanism, complemented by Option 4 (file-based pin handoff) for pipelines that separate key-holding from acting.**

### Justification

Verification is a cross-cutting property of *loading a component version*, so it belongs in the shared load path, configured once, rather than re-implemented as a flag on every command (Option 1) or smuggled through a fragile stream (Option 2). Enforcing it at the loader closes the TOCTOU window outright — check and use occur in one process on the same bytes — and extends to every current and future CV-loading command for free. The pipe (Option 2) is rejected because shell concurrency denies the actor `verify`'s exit code, and all the machinery needed to make it safe (status document, `--require-pin`, crash-ambiguity handling) exists only to work around the pipe itself; a file handoff gated by `&&` achieves the same decoupling with none of it.

Option 3 cannot serve pipelines that intentionally keep trust material out of the acting step, so Option 4 is retained as a complementary opt-in for exactly that case. Both options converge on the same core — the acting command performs the anchored walk from a **trusted root digest** — differing only in where that trust originates: inline signature verification (Option 3) or a prior-verify pin file (Option 4). A persisted `checksum.db` (Option 5) adds a tamper target and an eager full-tree walk and is rejected.

### Consequences / Trade-offs

* A typed verification-policy config in `.ocmconfig` and an enforcement hook in the shared resolve/load path; verification is paid only when the policy is enabled.
* Resource download must bind bytes to the signed OCM resource digest (not just the access/content-address digest) for the leaf of the tree to be covered.
* Option 4 adds `--output-pin` on `verify` and `--pin` on the acting commands; the pin document binds normalisation + hash + value, consumption is explicit and fails closed on an invalid/empty file.
* **Out of scope / follow-up**: genuinely rootless non-root invocations (`get cv <sub>`, `download resource <sub>` with no root in hand) cannot be anchored by a load-layer policy or a root pin and require per-node signatures — a separate ADR.
