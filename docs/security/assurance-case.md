# OCM Security Assurance Case

This document presents a structured assurance case for the Open Component Model (OCM), mapping identified threats to the mitigations implemented in the codebase. Each mitigation is linked to specific code or architecture decision records as evidence.

> **Audience**: This is a contributor and auditor document. It is not end-user documentation.

## Purpose and Scope

This assurance case demonstrates that OCM's design and implementation address the security threats relevant to a software supply chain management system. It covers:

- The OCM Go bindings (`bindings/go/`)
- The OCM CLI (`cli/`)
- The OCM Kubernetes controller (`kubernetes/controller/`)

It does not cover external plugin implementations, user-managed infrastructure, or the OCM specification itself.

## Threat Model

OCM operates in an environment where software artifacts are built, signed, stored in repositories, transferred across trust boundaries, and deployed. The following threat categories are relevant:

| ID | Threat | Description |
|----|--------|-------------|
| T1 | Supply chain tampering | An attacker modifies a component descriptor or its artifacts after signing, attempting to inject malicious content. |
| T2 | Credential theft | An attacker obtains signing keys, registry credentials, or Vault tokens from configuration files or environment. |
| T3 | Signature forgery | An attacker produces a valid-looking signature without possessing the legitimate signing key. |
| T4 | Transport interception | An attacker intercepts or modifies data in transit between the CLI/controller and OCI registries or other services. |
| T5 | Input injection | An attacker provides malicious input through CLI flags, configuration files, or component descriptors to cause unexpected behavior. |
| T6 | Container escape / privilege escalation | An attacker exploits the controller's runtime environment to gain elevated access to the cluster. |
| T7 | Trust anchor manipulation | An attacker embeds a self-signed root CA in a signature to bypass the verifier's trust store. |
| T8 | Plugin compromise | A malicious or vulnerable plugin is loaded and used by the OCM runtime. |

## Security Assumptions and Trust Boundaries

### Assumptions

- The operating system and Go runtime provide correct cryptographic primitives.
- System trust roots (CA certificates) are maintained by the platform operator.
- Private signing keys are stored securely by the user or organization (e.g., in a hardware security module or a secrets manager).
- The Kubernetes cluster's IAM and authentication are configured securely by the cluster administrator; OCM does not manage user identity.
- The Kubernetes cluster's RBAC and network policies are configured appropriately for the controller. The cluster administrator is responsible for granting least-privilege roles to the controller service account and to OCM Controller users.
- All controller pods run at minimum Kubernetes Pod Security Standard baseline.
- The artifact storage (OCI registry, Helm repository) is secured by the deploying organization with respect to authentication, authorization, and transport security.
- The Kubernetes API Server performs schema/admission validation before objects reach the controller, but controller-side validation of security-sensitive fields remains required.

### Threat Actors

| Actor | Trust Level | Description |
|-------|-------------|-------------|
| Cluster Administrator | Trusted | Configures RBAC, deploys the controller, responsible for cluster security posture. |
| OCM Controller User | Partially trusted | DevOps user creating or updating OCM API objects. Authorized via cluster RBAC; assumed not to be malicious but may be compromised. |
| OCM Controller Service Account | Minimally privileged | Service account under which the controller process runs. Should be granted least-privilege RBAC by the cluster administrator. |
| External Attacker | Untrusted | Attacker without cluster access attempting to tamper with artifacts, intercept traffic, or exploit the controller through external interfaces. |

### Trust Boundaries

1. **User to CLI**: The user provides configuration, credentials, and component references to the CLI. The CLI validates inputs before processing.
2. **CLI/Controller to OCI Registry**: Communication crosses a network boundary. TLS protects data in transit.
3. **Core to Plugin**: Plugins run as separate processes. The core controls connection parameters and validates plugin capabilities.
4. **Signer to Verifier**: The signed descriptor crosses a trust boundary. The verifier independently recomputes digests and validates signatures against its own trust material.
5. **Controller to Kubernetes API Server**: The controller watches and reads API objects over HTTPS. The cluster's RBAC controls what the service account may access. The API Server is treated as a trusted intermediary; a compromised API Server is out of scope.
6. **Controller to External Artifact Storage**: Crosses the Kubernetes cluster boundary. Production deployments use HTTPS. Plain HTTP is technically possible in development configurations; this is a residual risk (see Residual Risks).

## Mitigations

### T1: Supply Chain Tampering

**Goal**: Detect any modification to a component descriptor or its artifacts after signing.

| Mitigation | Evidence |
|------------|----------|
| Component descriptors are normalized using a versioned algorithm (`jsonNormalisation/v4alpha1`) before hashing, ensuring a canonical representation that is tamper-evident. | `bindings/go/signing/digest.go` (`GenerateDigest`, `ensureNormalisationAlgo`), [ADR-0008](../adr/0008_signing_verification.md) |
| Verification recomputes the digest from the descriptor and compares it against the signed digest using `bytes.Equal`, rejecting any mismatch. | `bindings/go/signing/digest.go` (`VerifyDigestMatchesDescriptor`) |
| `IsSafelyDigestible` enforces that all component references and accessible resources carry complete digests (hash algorithm, normalization algorithm, value). Resources without access must not have digests. | `bindings/go/signing/digest.go` (`IsSafelyDigestible`) |
| Two-stage signing allows digest computation in CI and signing in a separate step, with the signing step pinning against the precomputed digest value to prevent substitution. | [ADR-0008](../adr/0008_signing_verification.md) |

### T2: Credential Theft

**Goal**: Prevent exposure of sensitive credentials through configuration files or runtime state.

| Mitigation | Evidence |
|------------|----------|
| The `ComponentSignatureSigner` contract mandates: "Configurations MUST NOT contain any private key or otherwise sensitive material." | [ADR-0008](../adr/0008_signing_verification.md) (`ComponentSignatureSigner` interface documentation) |
| The `ComponentSignatureVerifier` contract mandates: "Configurations MUST NOT contain any key or otherwise sensitive material." | [ADR-0008](../adr/0008_signing_verification.md) (`ComponentSignatureVerifier` interface documentation) |
| Credentials are resolved through the credential graph at runtime; direct credential entries (key-value pairs) are supported in `.ocmconfig`, while plugin-mediated resolution enables external secret stores and keeps sensitive material out of local configuration. | [ADR-0002](../adr/0002_credentials.md) |
| The credential graph uses limited recursion (credential-to-credential only, no repository recursion) to prevent unbounded resolution chains that could leak credentials through unexpected paths. | [ADR-0002](../adr/0002_credentials.md) (Decision Outcome) |

### T3: Signature Forgery

**Goal**: Ensure only holders of legitimate signing keys can produce valid signatures.

| Mitigation | Evidence |
|------------|----------|
| RSA signature verification uses Go's standard `crypto/rsa` library with RSASSA-PSS or RSASSA-PKCS1-v1\_5. | `bindings/go/rsa/signing/handler/rsa.go` (`verifyRSA`, definition); called from `bindings/go/rsa/signing/handler/handler.go` (lines 164, 219) |
| Hash algorithms for descriptor digest computation are restricted to SHA-256 and SHA-512 via an explicit allowlist; unsupported algorithms are rejected. RSA signature verification additionally accepts SHA-384 (via `hashFromString` in the handler) because signature hash selection is controlled by the signer's certificate, not by OCM configuration. | `bindings/go/signing/digest.go` (`supportedHashes`, `getSupportedHash`), `bindings/go/rsa/signing/handler/hash.go` (`hashFromString`) |
| Signature algorithms are restricted to RSASSA-PSS and RSASSA-PKCS1-V1\_5 via the `SignatureAlgorithm` type with JSON schema enforcement. | `bindings/go/rsa/signing/v1alpha1/algorithm.go` |
| Unknown media types are rejected during verification. | `bindings/go/rsa/signing/handler/handler.go` (`Verify` method, default case) |
| For PEM signatures, X.509 certificate chains are validated against system roots or a verifier-supplied anchor, with `ExtKeyUsageCodeSigning` enforcement. | `bindings/go/rsa/signing/handler/handler.go` (`verifyChainWithOptionalAnchor`) |

### T4: Transport Interception

**Goal**: Protect data in transit between OCM components and external services.

| Mitigation | Evidence |
|------------|----------|
| HTTP/2 is disabled by default in the Kubernetes controller to mitigate the HTTP/2 Stream Cancellation and Rapid Reset CVEs (GHSA-qppj-fm5r-hxr3, GHSA-4374-p667-p6c8). When disabled, `disableHTTP2` sets `TLSConfig.NextProtos` to `[]string{"http/1.1"}`, ensuring only HTTP/1.1 over TLS is negotiated. | `kubernetes/controller/cmd/main.go` (`disableHTTP2` function) |
| No `InsecureSkipVerify` usage exists in the codebase; TLS certificate verification is always enforced using Go's standard defaults. | `rg --type=go 'InsecureSkipVerify\s*:\s*true'` returns zero matches across the entire monorepo |
| Go's `crypto/tls` defaults enforce TLS 1.2 as the minimum version when no explicit `MinVersion` is set. | `rg --type=go 'MinVersion\s*[=:]'` returns zero matches; no `tls.Config` construction overrides `MinVersion` anywhere in the codebase |
| Plain HTTP connections to artifact storage are technically possible for development/test scenarios. Production deployments must use HTTPS; enforcement is an operator responsibility. This is acknowledged as a residual risk (see Residual Risks). | Trust boundary 6; operator deployment configuration |

### T5: Input Injection

**Goal**: Prevent malicious input from causing unexpected behavior.

| Mitigation | Evidence |
|------------|----------|
| CLI enum flags use an allowlist pattern that rejects any value not in a predefined set of options. | `cli/internal/flags/enum/flag.go` (`Set` method) |
| Hash algorithm selection for descriptor digests is constrained to an explicit allowlist (SHA-256, SHA-512). RSA signature verification additionally accepts SHA-384 for interoperability with signing certificates that use it. | `bindings/go/signing/digest.go` (`supportedHashes`), `bindings/go/rsa/signing/handler/hash.go` (`hashFromString`) |
| Signature algorithm selection is constrained to RSASSA-PSS and RSASSA-PKCS1-V1\_5. | `bindings/go/rsa/signing/v1alpha1/algorithm.go` |
| The signing contract requires implementations to reject signature specifications without a precalculated digest, preventing operations on unvalidated input. | [ADR-0008](../adr/0008_signing_verification.md) (`ComponentSignatureSigner` contract) |
| RFC 2253 parsing is used for issuer DN comparison, preventing string-based bypass of issuer constraints. | `bindings/go/rsa/signing/handler/handler.go` (`verifyIssuerForLeafCert`) |

### T6: Container Escape / Privilege Escalation

**Goal**: Minimize the attack surface and blast radius of the controller's runtime environment.

| Mitigation | Evidence |
|------------|----------|
| The controller runs on a `gcr.io/distroless/static:nonroot` base image with no shell, package manager, or system utilities. | `kubernetes/controller/Dockerfile` (`FROM gcr.io/distroless/static:nonroot` stage) |
| The controller runs as non-root user `65532:65532`. | `kubernetes/controller/Dockerfile` (`USER` directive) |
| The controller binary is statically compiled with `CGO_ENABLED=0`, eliminating C library dependencies. | `kubernetes/controller/Dockerfile` (`RUN CGO_ENABLED=0` build step) |
| Base images are pinned by SHA-256 digest to prevent tag mutation attacks. | `kubernetes/controller/Dockerfile` (both `FROM` directives use `@sha256:` pinning) |
| CLI binaries are built with `-ldflags "-s -w"` to strip symbols and debug information. | `cli/Taskfile.yml` (`build:target` task) |
| The in-memory Component Descriptor Cache (resolved digests and signatures) is process-local and not externally accessible, limiting exposure if the process is compromised. | `kubernetes/controller/` architecture; threat model asset analysis |
| IAM and RBAC for the controller service account are delegated to the Kubernetes cluster. The cluster administrator is responsible for granting least-privilege access; the controller does not manage user identity or authorization. | Assumption: IAM delegated to Kubernetes (see Security Assumptions) |

### T7: Trust Anchor Manipulation

**Goal**: Prevent signers from embedding root CAs that bypass the verifier's trust store.

| Mitigation | Evidence |
|------------|----------|
| Self-signed certificates embedded in PEM signatures are rejected. The `classifyEmbeddedChain` function returns an error if any self-signed certificate is found in the embedded chain. | `bindings/go/rsa/signing/handler/handler.go` (`classifyEmbeddedChain`) |
| When a verifier supplies a self-signed root anchor via credentials, system roots are ignored and the chain must terminate at that exact anchor. | `bindings/go/rsa/signing/handler/handler.go` (`verifyChainWithOptionalAnchor`) |
| In the credential chain, a self-signed certificate is only accepted as the last entry. Self-signed certificates at any other position are rejected. | `bindings/go/rsa/signing/handler/handler.go` (`classifyCredentialChain`) |

### T8: Plugin Compromise

**Goal**: Limit the impact of a malicious or vulnerable plugin.

| Mitigation | Evidence |
|------------|----------|
| Plugins run as separate processes, communicating over Unix domain sockets or TCP. A compromised plugin cannot directly access the core process's memory. | [ADR-0001](../adr/0001_plugins.md) |
| The plugin manager controls the connection location (socket path or TCP port), preventing plugins from choosing arbitrary network endpoints. | [ADR-0001](../adr/0001_plugins.md) (Server contract, `Config.Location`) |
| Plugins are lazy-loaded and only started when their specific capability is needed. | [ADR-0001](../adr/0001_plugins.md) |
| Essential plugins (e.g., OCI) can be registered as library implementations via build tags, avoiding the need for external binaries in controlled environments. | [ADR-0001](../adr/0001_plugins.md) (`RegisterPluginImplementationForTypeAndCapabilities`) |
| Plugins can be distributed as OCM component versions, enabling the same signing and verification workflow to protect plugin integrity. | [ADR-0001](../adr/0001_plugins.md) (Discovery and Distribution section) |

## Residual Risks

The following risks are acknowledged and not fully mitigated by the current implementation:

| Risk | Description | Current Status |
|------|-------------|----------------|
| Plugin binary integrity at discovery | Filesystem-based plugin discovery does not verify plugin signatures before loading. Plugins distributed via component versions can be verified, but locally installed plugins rely on filesystem permissions. | Documented in [ADR-0001](../adr/0001_plugins.md); plugin registry ([ADR-0011](../adr/0011_plugin_registry.md)) may address this. |
| No explicit TLS `MinVersion` setting | The codebase relies on Go's default minimum TLS version (1.2). This is currently secure but depends on Go runtime defaults not changing to a weaker setting. | Go's defaults have been stable; an explicit `MinVersion: tls.VersionTLS12` would make the intent declarative. |
| Key management delegation | OCM does not manage key lifecycle (generation, rotation, revocation). These responsibilities are delegated to users and external systems (Vault, Sigstore, filesystem). | By design; OCM focuses on signing and verification, not key management. |
| Single hash algorithm family | Only SHA-2 family (SHA-256, SHA-512) is supported. Migration to SHA-3 or other post-quantum algorithms would require handler updates. | The pluggable handler architecture ([ADR-0008](../adr/0008_signing_verification.md)) supports adding new algorithms without core changes. |
| Plugin communication not encrypted | Plugin-to-core communication over Unix domain sockets or localhost TCP does not use TLS. This is standard for local IPC but could be a concern if the network boundary assumption is violated. | Communication is local-only; the plugin manager controls the connection location. |
| Plain HTTP to artifact storage in development | The controller does not enforce HTTPS for connections to OCI registries or artifact storage. Plain HTTP can be configured for development scenarios. In production, TLS enforcement is an operator responsibility. | By design for development flexibility; operators must ensure HTTPS in production environments. |
| No operator security hardening guide | Security-relevant configuration decisions—service account RBAC, TLS enforcement, pod security standards—are not centrally documented. Misconfiguration by cluster administrators can significantly weaken the security posture. | Acknowledged; a dedicated operator hardening guide is recommended for production deployments. |

## References

- [ADR-0001: Plugin System](../adr/0001_plugins.md)
- [ADR-0002: Credential System](../adr/0002_credentials.md)
- [ADR-0008: Signing and Verification](../adr/0008_signing_verification.md)
- [Secure Design Document](secure-design.md)
- [OpenSSF Best Practices: implement\_secure\_design](https://www.bestpractices.dev/en/criteria#1.implement_secure_design)
- [OpenSSF Best Practices: assurance\_case](https://www.bestpractices.dev/en/criteria#1.assurance_case)
