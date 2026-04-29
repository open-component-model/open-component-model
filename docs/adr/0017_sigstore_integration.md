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

## OIDC Authentication Model

When the `.ocmconfig` does not contain a pre-provisioned OIDC token, the CLI performs an interactive
OIDC Authorization Code flow with PKCE (RFC 7636, S256) to acquire an identity token from the configured
issuer. This flow is implemented in `cli/internal/oidcflow` without depending on `github.com/sigstore/sigstore`.

### Why PKCE Only

The OCM CLI is a **public OAuth client** — it cannot hold a client secret. The protection mechanisms considered:

| Mechanism | Applicability | Decision |
|-----------|--------------|----------|
| PKCE S256 (RFC 7636) | Prevents authorization code interception | **Implemented** |
| Client authentication (client_secret) | Not applicable — public client cannot hold secrets | N/A |
| DPoP (RFC 9449) | Proof-of-possession for tokens | Not needed — token is used once immediately, never stored |
| PAR (RFC 9126) | Pushed Authorization Requests | Not needed — no confidential request parameters beyond PKCE |
| JARM | JWT-secured authorization response | Not needed — code exchange over TLS is sufficient |

PKCE S256 is the RFC-recommended protection for native public clients (RFC 8252 §7.1, OAuth 2.1 draft §4.1.2).
The loopback redirect URI (`http://127.0.0.1:<random-port>`) limits code interception to same-machine processes,
which PKCE fully mitigates.

### Enterprise Provider Compatibility

The implementation supports arbitrary OIDC providers via the `issuer` and `clientID` fields in `.ocmconfig`.
Enterprise deployments (Keycloak, Azure AD, Okta, etc.) are supported with the following considerations:

- **RFC 9207 (Issuer Identification):** The callback validates the `iss` parameter when present (permissive mode).
  Providers that omit `iss` are accepted without issuer verification. This protects against mix-up attacks
  in multi-provider enterprise environments.
- **PKCE S256 required:** The provider must advertise `S256` in `code_challenge_methods_supported`.
  The flow fails with a clear error if PKCE S256 is not supported.
- **Session behavior:** The flow does not send `prompt=consent` or `prompt=login`, allowing the provider's
  default session policy to apply. Enterprise providers typically control this server-side.

## Conclusion

Option A provides a pragmatic and robust solution for integrating Sigstore signing into OCM. It minimizes dependencies,
leverages a mature toolchain, and provides a seamless experience for the end-user.
The architecture is clean, maintainable, and flexible for future evolution.
