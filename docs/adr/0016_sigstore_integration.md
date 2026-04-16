# Sigstore Integration for OCM

* Status: approved
* Deciders: OCM Maintainer Team
* Date: 2026-04-15

Technical Story: This ADR outlines the decision on how to integrate Sigstore/Cosign keyless signing
into the OCM CLI, comparing a CLI wrapper approach with a direct library integration.

## Context and Problem Statement

Traditional code signing requires managing long-lived keys, which is complex and lacks a public record of signing events. Sigstore offers a solution by binding signatures to identities (email, CI workload) using short-lived certificates and an immutable transparency log, eliminating the need for long-lived keys.

OCM's current signing system is based on a `signing.Handler` interface, which is agnostic to the underlying signing algorithm. To integrate Sigstore, a new implementation of this interface is required. This ADR evaluates two primary approaches for this integration.

## Decision Drivers

- **User Experience:** The end user should only need to install and interact with the `ocm` CLI. Any additional tools should be managed transparently.
- **Dependency Management:** The number of third-party dependencies should be minimized to reduce the supply chain attack surface and maintenance overhead.
- **Maturity and Stability:** The chosen solution should be based on a mature, battle-tested implementation of the Sigstore protocol.
- **Maintainability:** The integration should be easy to maintain and upgrade.

## Considered Options

- **Option A: Cosign CLI Wrapper:** Delegate all Sigstore operations to the `cosign` binary as an external process. The handler would manage the `cosign` binary transparently (auto-download, caching).
- **Option B: sigstore-go Library:** Use the official `sigstore-go` library to perform signing and verification operations entirely in-process.

## Decision Outcome

Chosen [Option A](#option-a-cosign-cli-wrapper): "Cosign CLI Wrapper".

Justification:

- **Minimal Dependency Risk:** This option adds zero new Go dependencies to the OCM module, leveraging the most mature and widely-used Sigstore client (`cosign`).
- **Clean Architecture:** The proposed `Executor` interface provides a clean abstraction that is easily testable.
- **Seamless User Experience:** The `cosign` binary is managed automatically by the OCM CLI, making it invisible to the end-user. The user experience is a true single-tool experience.
- **Familiarity:** The configuration model mirrors `cosign` conventions, which is beneficial for users already familiar with it.

The main trade-off (dependency on an external binary) is fully mitigated by the transparent auto-download and
caching mechanism with SHA256 verification of the downloaded binary.

## Pros and Cons of the Options

### Option A: Cosign CLI Wrapper

Pros:

- Zero sigstore Go dependencies.
- `cosign` is the battle-tested reference implementation.
- Clean `Executor` abstraction for testing.
- Automatic feature inheritance by updating the `cosign` binary.
- No manual tool installation for the user.
- Familiar UX for `cosign` users.

Cons:

- Text-based error handling from `stderr`.
- Implicit version coupling with the `cosign` binary.
- Network access required for the initial download of the `cosign` binary.

### Option B: sigstore-go Library

Pros:

- Self-contained with no external binary.
- Typed Go error handling.
- Fine-grained control over the Sigstore process.

Cons:

- Heavy dependency tree with over 15 transitive modules.
- Larger supply chain attack surface.
- Tighter coupling with the `sigstore-go` API, requiring recompilation for updates.
- `sigstore-go` is less mature than the `cosign` CLI.
- A panic in the library could crash the OCM CLI.

## Discovery and Distribution

The Sigstore handler will be implemented as an internal OCM plugin.
The `cosign` binary will be downloaded on-demand and cached in the user's home directory (`~/.cache/ocm/cosign/...`).
The version of `cosign` will be pinned and managed by Renovate. The user will interact with the feature through the standard `ocm sign` and `ocm verify` commands,
with `sigstore` as the algorithm name.

## Conclusion

Option A provides a pragmatic and robust solution for integrating Sigstore signing into OCM. It minimizes dependencies,
leverages a mature toolchain, and provides a seamless experience for the end-user.
The architecture is clean, maintainable, and flexible for future evolution.
