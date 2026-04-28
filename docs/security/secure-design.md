# OCM Secure Design

This document consolidates OCM's security design principles and mechanisms, providing a single reference for how the project addresses software supply chain security. All claims are traceable to architecture decision records (ADRs) and source code.

> **Audience**: This is a contributor and auditor document. It is not end-user documentation.

## Security Design Principles

OCM's security design is guided by the following principles:

- **Separation of concerns**: Signing, verification, credential resolution, and transport are decoupled into independent subsystems with well-defined contracts.
- **No sensitive material in signing/verifying configs**: Signer and verifier configurations must not contain private keys or sensitive key material. Credentials are resolved at runtime through the credential graph; direct credential entries (key-value pairs) may exist in credential configuration, while plugin-backed stores (e.g., HashiCorp Vault, Sigstore OIDC) are preferred for stronger secret isolation ([ADR-0002](../adr/0002_credentials.md), [ADR-0008](../adr/0008_signing_verification.md)).
- **Algorithm agility**: Signing and verification handlers are pluggable, allowing new cryptographic algorithms without changes to core logic ([ADR-0008](../adr/0008_signing_verification.md)).
- **Least privilege in deployment**: Container images run as non-root on distroless base images, and binaries are statically compiled with no CGO dependencies.
- **Defense in depth**: Multiple layers of security controls are applied, from input validation through transport security to cryptographic verification.

## Trust Model

OCM supports two trust models for signature verification, both implemented in the RSA signing handler (`bindings/go/rsa/signing/handler/handler.go`):

### Key Pinning (Plain Signatures)

In plain signature mode, the verifier supplies a public key via the credential graph. The signature is verified directly against this key. This is a key-pinning model where the verifier explicitly trusts a specific key pair.

### Certificate Chains (PEM Signatures)

In PEM signature mode, the signing handler embeds the signer's X.509 certificate chain in the signature. During verification:

1. The leaf certificate's chain is validated against system trust roots or an optional verifier-supplied root anchor.
2. Self-signed certificates embedded in the signature are rejected to prevent signers from asserting their own trust anchors (`classifyEmbeddedChain` in `handler.go`).
3. If the verifier supplies a self-signed root CA via credentials, system roots are ignored and the chain must terminate at that exact anchor.
4. An optional issuer constraint validates the signature's declared issuer against the leaf certificate's X.509 Issuer DN using RFC 2253 parsing.
5. Certificate verification enforces `ExtKeyUsageCodeSigning` key usage.

## Signing and Verification

Reference: [ADR-0008](../adr/0008_signing_verification.md)

### Architecture

Signing and verification are built on the `ComponentSignatureHandler` contract, which separates the concerns of normalization, digest computation, and cryptographic operations:

1. **Normalization**: The component descriptor is normalized using a versioned algorithm (currently `jsonNormalisation/v4alpha1`, with transparent legacy v3 fallback in `bindings/go/signing/digest.go`).
2. **Digest computation**: The normalized bytes are hashed with a supported algorithm.
3. **Signing**: The handler produces a signature envelope using credentials resolved through the credential graph.
4. **Verification**: The handler recomputes the digest from the descriptor, then verifies the signature against the recomputed value.

### Supported Algorithms

Defined in `bindings/go/rsa/signing/v1alpha1/`:

- **Signature algorithms**: RSASSA-PSS (default), RSASSA-PKCS1-V1\_5
- **Hash algorithms**: SHA-256, SHA-512 (enforced in `bindings/go/signing/digest.go` via an allowlist in `supportedHashes`)

### Handler Contract Enforcement

The `ComponentSignatureSigner` contract specifies that:

- Implementations must reject signature specifications without a precalculated digest.
- Implementations must not modify the digest specification when signing.
- Configurations must not contain any private key or sensitive material.

The `ComponentSignatureVerifier` contract specifies that:

- If the media type cannot be verified, signature verification must fail.
- Configurations must not contain any key or sensitive material.

### Two-Stage Signing

OCM supports splitting digest computation and signing into separate steps, enabling CI/CD workflows where the build environment computes digests and a separate step with signing credentials produces the signature. The second step pins against the precomputed digest value ([ADR-0008](../adr/0008_signing_verification.md)).

### Digest Integrity Checks

The `IsSafelyDigestible` function (`bindings/go/signing/digest.go`) enforces that:

- All component references carry complete digests (hash algorithm, normalization algorithm, and value).
- Resources with access must have complete digests.
- Resources without access must not carry a digest, preventing meaningless digest claims.

## Credential Management

Reference: [ADR-0002](../adr/0002_credentials.md)

### Graph-Based Resolution

Credentials are resolved through a directed acyclic graph (DAG) built from `.ocmconfig`. The system supports:

- **Direct credentials**: Key-value pairs configured inline.
- **Plugin-based credentials**: Resolved through `CredentialPlugin` implementations (e.g., HashiCorp Vault).
- **Repository-based credentials**: Dynamic lookup through `RepositoryPlugin` implementations (e.g., Docker config files).

### Isolation Properties

- **No sensitive material in signing/verifying configs**: The signing and verification contracts explicitly state that configurations must not contain private keys or sensitive key material. Direct credential entries (key-value pairs) are permitted in credential configuration; plugin-mediated stores are preferred for stronger isolation. Credentials are resolved at runtime from the credential graph.
- **Limited recursion**: Credential resolution uses limited recursion (credentials can depend on other credentials, but repository recursion is not allowed), preventing unbounded resolution chains while supporting practical use cases like Vault tokens needed to access Vault-stored credentials.
- **Plugin-mediated access**: External credential stores (Vault, OIDC providers) are accessed through plugins with well-defined contracts, ensuring the core system never directly handles storage-specific authentication.

### Credential Flow

When a signing or verification operation requires credentials:

1. The handler calls `GetSigningCredentialConsumerIdentity` or `GetVerifyingCredentialConsumerIdentity` to produce a consumer identity.
2. The consumer identity is resolved against the credential graph.
3. Resolved credentials (key-value attributes) are passed to the handler's `Sign` or `Verify` method.
4. The handler interprets the credential attributes (e.g., `private_key_pem_file`, `public_key_pem_file`) according to its own logic.

## Transport Security

### TLS Configuration in the Kubernetes Controller

The Kubernetes controller (`kubernetes/controller/cmd/main.go`) disables HTTP/2 by default for both metrics and webhook servers to mitigate the HTTP/2 Stream Cancellation and Rapid Reset vulnerabilities:

- [GHSA-qppj-fm5r-hxr3](https://github.com/advisories/GHSA-qppj-fm5r-hxr3)
- [GHSA-4374-p667-p6c8](https://github.com/advisories/GHSA-4374-p667-p6c8)

When HTTP/2 is disabled, the TLS configuration is set to `NextProtos: []string{"http/1.1"}`, ensuring only HTTP/1.1 over TLS is used. HTTP/2 can be explicitly opted into via the `--enable-http2` flag.

### Certificate Verification

No `InsecureSkipVerify` usage exists in the codebase (verify with `git grep InsecureSkipVerify`). TLS certificate verification uses Go's standard library defaults, which require valid certificates.

## Input Validation

### Allowlist-Based Enum Flags

The CLI uses an allowlist pattern for enum-type flags (`cli/internal/flags/enum/flag.go`). The `Flag` type:

- Accepts a fixed set of valid options at construction time.
- Rejects any value not in the allowlist via `Set()`, returning an error with the valid options.
- Uses the first option as the default value.

### Hash Algorithm Allowlist

The `getSupportedHash` function in `bindings/go/signing/digest.go` maintains an explicit allowlist of supported hash algorithms (SHA-256, SHA-512). Unsupported algorithms are rejected with an error.

### Signature Algorithm Allowlist

Signature algorithms are constrained to `RSASSA-PSS` and `RSASSA-PKCS1-V1_5` via the `SignatureAlgorithm` type in `bindings/go/rsa/signing/v1alpha1/algorithm.go`, enforced through JSON schema generation tags.

### Media Type Validation

The RSA verification handler rejects unknown media types, returning an error for any `MediaType` not in the supported set (plain PSS, plain PKCS1v15, PEM).

## Build Hardening

### Static Compilation

CLI builds (`cli/Taskfile.yml`) use:

- `CGO_ENABLED=0`: Produces statically linked binaries with no C library dependencies, reducing the attack surface from native code vulnerabilities.
- `-ldflags "-s -w"`: Strips symbol tables and debug information from release binaries.

### Container Hardening

The Kubernetes controller Dockerfile (`kubernetes/controller/Dockerfile`):

- Uses `gcr.io/distroless/static:nonroot` as the runtime base image, which contains no shell, package manager, or unnecessary system utilities.
- Runs as user `65532:65532` (the distroless `nonroot` user), not as root.
- Pins base images by SHA-256 digest to prevent supply chain attacks through tag mutation.
- Uses multi-stage builds to exclude build tooling from the final image.
- Builds with `CGO_ENABLED=0` for a fully static binary.

## Plugin Isolation

Reference: [ADR-0001](../adr/0001_plugins.md)

### Process-Level Isolation

OCM plugins run as separate binaries communicating over Unix domain sockets or TCP connections. This provides:

- **Language independence**: Plugins can be written in any language that can listen on a socket and follow the contract.
- **Fault isolation**: A crashing or misbehaving plugin does not bring down the core process.
- **Lazy loading**: Plugins are started on demand and only when their capability is needed.

### Plugin Contracts

Each plugin exposes two commands:

- `capabilities`: Declares the types and capabilities the plugin supports.
- `server`: Starts the plugin with a configuration specifying connection type (socket or TCP) and location.

The plugin manager controls the connection location, preventing plugins from choosing arbitrary network endpoints.

### Built-in Plugin Registration

For environments that cannot fork processes, plugins can be registered directly as library implementations using `RegisterPluginImplementationForTypeAndCapabilities`. The OCI plugin uses this mechanism behind a build tag to provide core OCI functionality without external plugin dependencies.

### Distribution via Component Versions

Plugins can be distributed as OCM component versions, enabling verified plugin distribution through the same supply chain that OCM protects. The OCI capability is built into the client to avoid a bootstrap problem when fetching plugins from OCI registries.
