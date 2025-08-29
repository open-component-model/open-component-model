# ADR-0008: Digest Calculation & Signing/Pinning (Signing-Only, Pluggable Signers incl. OCI Cosign)

- **Status:** Proposed
- **Deciders:** OCM Maintainers
- **Date:** 2025-08-27
- **Technical Story:** Provide a consistent, **pluggable** way to sign component descriptors (CD) over a normalized representation, while keeping digest calculation separate.
- **Related:** OCM signing specification and examples

---

## Context and Problem Statement

To sign a Component Version (CV), users need a stable canonicalization, precomputed digests inside the descriptor, and a way to attach one or more signatures. We must support multiple signing backends (X.509 certificate/private key, Sigstore/OCI Cosign, etc.) **without** changing core CLI logic whenever a new backend appears. Therefore the signing **CLI surface** must be clean and stable, while **signer plugins** supply backend-specific flags and behavior.

> Scope: **signing and signature publishing**. Uploading/pushing OCI images or other artifacts is **out of scope** for this ADR.

---

## Decision Drivers

- **Extensibility:** New key types and ecosystems should be added via plugins with minimal/no changes to the core.
- **Simplicity:** Users see a single signing command; digesting remains a separate step.
- **Determinism:** Fixed canonicalization (OCM normalization) and explicit pinning avoid ambiguity.
- **Security:** Clear handling of key material, passphrases, and OIDC tokens (for Cosign).

---

## Outcome (High Level)

- Keep a **two-step flow**:
  1) **Digest calculation** (separate command; not defined here) embeds all required digests into the descriptor.
  2) **Signing** computes the component-version digest over canonical bytes and appends a signature envelope.
- Introduce a **Signer Plugin SPI** used by the CLI to execute one concrete signing backend per invocation.
- Provide two initial signer plugins:
  - **Certificate Signer** (X.509 + private key from file/bundle).
  - **Cosign Signer** (Sigstore keyless via OIDC; stores **bundle** in the envelope and publishes signatures).

---

## Non‑Plugin CLI 

```bash
# Step 1 — Digest calculation (not part of this ADR)
# Must be done prior to signing; ensures descriptor contains resource/reference digests.

# Step 2 — Signing (specification-based approach)
ocm sign cv <ref> \
  --sig <slot> \
  --pin <sha256:...> \
  --kind <signing-algorithm-spec> \
  [<signer‑specific flags>]

# Alternative: Direct specification approach
ocm sign cv <ref> \
  --sig <slot> \
  --pin <sha256:...> \
  --sign-spec <path-to-spec-file>
```

- `<ref>`: descriptor reference (file path or repository reference).
- `--sig <slot>`: **named signature slot**, e.g., `release@2025-08-27`.
- `--pin <sha256:...>`: expected **component-version** digest; fail on mismatch.
- `--kind <signing-algorithm-spec>`: OCM signing algorithm specification (e.g., `signing/RSA-PSS/v1`, `signing/COSIGN/v1`, `signing/x509.acme.com/v1`).
- `--sign-spec <path>`: path to a YAML specification file containing signer configuration.
- `<signer‑specific flags>`: plugin-specific flags when using `--kind` approach.

### Signing Algorithm Specification Format

The `--kind` parameter follows the OCM specification format for [Signing Algorithms](https://github.com/open-component-model/ocm-spec/blob/main/doc/01-model/07-extensions.md#signing-algorithms). The format consists of:

- **Algorithm Name**: Following the pattern `[A-Z][A-Z0-9-_]*` for centrally defined algorithms
- **Version**: Following the pattern `v[1-9][0-9]*` 
- **Full Format**: `<algorithm-name>/<version>` or `<algorithm-name>` (defaults to `/v1`)

**Centrally Defined Algorithms Examples:**
- `signing/RSA-PSS/v1` - RSA-PSS signature with SHA-256
- `signing/ECDSA-P256/v1` - ECDSA with P-256 curve and SHA-256
- `signing/COSIGN/v1` - Sigstore/Cosign keyless signing

**Vendor-Specific Algorithms:**
- Must use DNS domain-based naming: `signing/<algorithm>.<domain>/<version>`
- Example: `signing/ENTERPRISE-HSM.acme.com/v2`

---

## Signer Plugins (CLI Flags & Behavior)

Exactly **one** signer plugin must be selected via `--kind` or `--sign-spec`:

### 1) Certificate Signer (X.509) - `--kind signing/RSA-PSS/v1`

**CLI flags** (plugin-specific):

```bash
--kind signing/RSA-PSS/v1 \
--cert <path> \
--password <pw> \
--password-prompt
```

**Specification file format** (when using `--sign-spec`):

```json
{
  "type": "signing/RSA-PSS/v1",
  "certPath": "/path/to/cert.p12",
  "passwordPrompt": true
}
```

**Behavior**

- Loads X.509 certificate & private key material from `<path>` (PEM/PKCS#8/PKCS#12).
- If encrypted, uses `--password` or `--password-prompt`.
- Signs the **canonical bytes** of the descriptor and emits a signature envelope compliant with OCM specification:
  - `algorithm`: one of `rsa-pss-sha256`, `ecdsa-p256-sha256`, `ed25519` (implementation-defined detection).
  - `mediaType`: `application/vnd.ocm.signature.x509+json`.
  - `value`: base64-encoded signature bytes.
  - `issuer`: certificate subject information.

**CLI example**

```bash
ocm sign cv ghcr.io/org/app:1.2.3 \
  --sig release@2025-08-27 \
  --kind signing/RSA-PSS/v1 \
  --cert ~/.keys/release.p12 \
  --password-prompt
```

---

### 2) Cosign Signer (OCI Cosign, keyless) - `--kind signing/COSIGN/v1`

**CLI flags** (plugin-specific, minimal set):

```bash
--kind signing/COSIGN/v1 \
--cosign-identity-token <token-or-@path> \
--cosign-annotation <key=value>   # repeatable
```

**Specification file format** (when using `--sign-spec`):

```json
{
  "type": "signing/COSIGN/v1",
  "identityToken": "@/path/to/token",
  "annotations": {
    "some": "thing"
  }
}
```

**Behavior**

- Resolves an **OIDC ID token** in this order:
  1. `--cosign-identity-token` (string or `@/path/to/token`),
  2. `SIGSTORE_ID_TOKEN` environment variable,
  3. interactive **loopback browser** flow (if TTY),
  4. **device flow** fallback.
- Produces a Cosign signature and records a **bundle** (signature/certs/optional info) into the envelope compliant with OCM specification:
  - `algorithm`: `cosign`.
  - `mediaType`: `application/vnd.dev.sigstore.bundle+json`.
  - `value`: base64-encoded Sigstore bundle JSON.
- Publishes signatures to the component descriptor but does not upload OCI artifacts.

**CLI examples**

```bash
# Interactive (browser/device) token
ocm sign cv ghcr.io/org/app:1.2.3 \
  --sig release@2025-08-27 \
  --kind signing/COSIGN/v1

# Using specification file
ocm sign cv ghcr.io/org/app:1.2.3 \
  --sig release@2025-08-27 \
  --sign-spec cosign-config.json

# Vendor-specific signing algorithm
ocm sign cv ghcr.io/org/app:1.2.3 \
  --sig release@2025-08-27 \
  --kind signing/ENTERPRISE-HSM.acme.com/v2 \
  --hsm-slot 1 \
  --key-id production-key
```

---

## Processing Architecture

1. **Input**
   - `<ref>`, base flags (`--sig`, `--pin`), plugin flags.
2. **Load & Canonicalize**
   - Load descriptor; canonicalize via **OCM normalization**.
3. **Compute Digest**
   - Compute component-version digest from canonical bytes.
   - If `--pin` provided, fail when mismatch.
4. **Select Signer Plugin**
   - Exactly one plugin must be active based on `--kind` signing algorithm specification or `--sign-spec` file.
5. **Sign**
   - Delegate to the selected plugin to produce an envelope.
6. **Append Envelope**
   - Append to `.signatures[]` in the descriptor.
7. **Output**
   - Updated descriptor (signed).

---

## SPI: Signer Plugin Interface (Go, pseudo‑code)

Following the plugin architecture established in [ADR-0001](0001_plugins.md), signer plugins are separate binaries that communicate via HTTP/Unix domain sockets:

```go
// Package: ocm.software/open-component-model/bindings/go/descriptor/signature

// Plugin capabilities for signing
const (
    SignComponentVersionCapability = "sign.componentversion"
    VerifyComponentVersionCapability = "verify.componentversion"
)

// SignerPlugin interface for direct library integration (when plugins are embedded)
type SignerPlugin interface {
    // Name of the plugin (e.g., "x509", "cosign").
    Name() string

    // Type returns the plugin type for --kind parameter
    Type() string

    // Sign receives the canonical bytes and returns an OCM-compliant signature envelope.
    Sign(ctx context.Context, canonical []byte, slot string, spec *SignerSpec) (*SignatureInfo, error)
    
    // Verify checks a signature against canonical bytes
    Verify(ctx context.Context, canonical []byte, signature *SignatureInfo) error
}

// SignerSpec represents the specification for a signer (from --sign-spec file or --kind flags)
type SignerSpec struct {
    Type    string                 `json:"type"`    // e.g., "signing/RSA-PSS/v1", "signing/COSIGN/v1", "signing/ENTERPRISE-HSM.acme.com/v2"
    Config  map[string]interface{} `json:"config"`  // plugin-specific configuration
}

// Plugin binary contract (following ADR-0001)
type PluginCapabilities struct {
    Type map[string][]string `json:"type"`
}

// Example capabilities response for a signing plugin:
// {
//   "type": {
//     "signing/RSA-PSS/v1": ["sign.componentversion", "verify.componentversion"],
//     "signing/COSIGN/v1": ["sign.componentversion", "verify.componentversion"],
//     "signing/ENTERPRISE-HSM.acme.com/v2": ["sign.componentversion", "verify.componentversion"]
//   }
// }
```

### Plugin Binary Interface

Each signer plugin binary must implement:

1. **`capabilities` command**: Returns supported types and capabilities
2. **`server` command**: Starts HTTP/Unix socket server with endpoints:
   - `POST /sign` - Sign canonical bytes
   - `POST /verify` - Verify signature
   - `GET /health` - Health check

### Signature Envelope (OCM Specification Compliant)

The signature envelope must comply with the OCM specification's [Signature Info](https://github.com/open-component-model/ocm-spec/blob/7bfbc171e814e73d6e95cfa07cc85813f89a1d44/doc/01-model/03-elements-sub.md#signature-info) structure:

```go
// OCM Specification compliant signature structure
type SignatureInfo struct {
    Algorithm string `json:"algorithm"`  // The used signing algorithm
    MediaType string `json:"mediaType"`  // The media type of the technical representation
    Value     string `json:"value"`      // The signature itself (base64 encoded)
    Issuer    string `json:"issuer,omitempty"` // The description of the issuer
}

// Extended envelope for plugin-specific data (internal use)
type SignatureEnvelope struct {
    // OCM specification fields
    Algorithm string `json:"algorithm"`              // e.g., rsa-pss-sha256 | ecdsa-p256-sha256 | ed25519 | cosign
    MediaType string `json:"mediaType"`              // e.g., application/vnd.ocm.signature.x509+json
    Value     string `json:"value"`                  // base64-encoded signature or bundle
    Issuer    string `json:"issuer,omitempty"`       // certificate subject or OIDC issuer info
    
    // Internal fields for processing (not part of final signature)
    ComponentDigest string `json:"componentDigest,omitempty"` // sha256:... (for validation)
}
```

**Media Type Examples:**
- X.509 Certificate: `application/vnd.ocm.signature.x509+json`
- Cosign/Sigstore: `application/vnd.dev.sigstore.bundle+json`
- Generic RSA: `application/vnd.ocm.signature.rsa+json`

### Canonicalization (fixed)

```go
// Using OCM component descriptor normalization; not configurable here. #PSEUDOCODE
canon, err := ocm.NormalizeComponentDescriptor(componentDescriptor)
if err != nil { /* ... */ }
```

---

## Cosign OIDC Token Resolution (pseudo‑code)

```go
func resolveSigstoreIDToken(ctx context.Context, flagToken string) (string, error) {
    if flagToken != "" {
        if strings.HasPrefix(flagToken, "@") {
            return readTokenFromFile(flagToken)
        }
        return flagToken, nil
    }
    if isInteractive() {
        return runLoopbackBrowserFlow(ctx)
    }
    return runDeviceFlow(ctx)
}
```

### Loopback browser flow (pseudo‑code)

```go
// Opens the browser for OIDC auth and captures the redirect locally.
func runLoopbackBrowserFlow(ctx context.Context) (string, error) {
    // 1. Start local HTTP server on random port
    server := startLocalServer("127.0.0.1:0")
    redirectURL := server.URL + "/callback"
    
    // 2. Build OIDC authorization URL with PKCE
    authURL := buildOIDCAuthURL(redirectURL, pkceChallenge, state, nonce)
    
    // 3. Open browser to authorization URL
    openBrowser(authURL)
    
    // 4. Wait for callback with authorization code
    code := waitForCallback(server, state)
    
    // 5. Exchange code for ID token
    idToken := exchangeCodeForToken(code, pkceVerifier)
    
    // 6. Verify and return ID token
    return verifyAndExtractIDToken(idToken, nonce)
}
```

---

## Sequence Diagrams

### Signing with Certificate Plugin (--kind approach)

```mermaid
sequenceDiagram
  autonumber
  actor U as User/CI
  participant C as ocm CLI
  participant PM as Plugin Manager
  participant P as X.509 Signer Plugin

  U->>C: ocm sign cv <ref> --sig <slot> --pin <digest> --kind signing/RSA-PSS/v1 --cert <path> [--password-prompt]
  C->>C: Load & canonicalize descriptor (OCM normalization)
  C->>C: Compute CV digest; check --pin
  C->>PM: Get plugin for type "signing/RSA-PSS/v1"
  PM->>P: Start plugin server (if not running)
  PM-->>C: Plugin endpoint
  C->>P: POST /sign {canonical, slot, spec}
  P-->>C: SignatureInfo{algorithm, mediaType, value, issuer}
  C->>C: Append signature to .signatures[]
  C-->>U: Signed descriptor
```

### Signing with Specification File (--sign-spec approach)

```mermaid
sequenceDiagram
  autonumber
  actor U as User/CI
  participant C as ocm CLI
  participant PM as Plugin Manager
  participant P as Cosign Signer Plugin

  U->>C: ocm sign cv <ref> --sig <slot> --pin <digest> --sign-spec cosign-config.json
  C->>C: Load & parse specification file
  C->>C: Load & canonicalize descriptor (OCM normalization)
  C->>C: Compute CV digest; check --pin
  C->>PM: Get plugin for type from spec
  PM->>P: Start plugin server (if not running)
  PM-->>C: Plugin endpoint
  C->>P: POST /sign {canonical, slot, spec}
  P-->>C: SignatureInfo{algorithm, mediaType, value, issuer}
  C->>C: Append signature to .signatures[]
  C-->>U: Signed descriptor
```

---

## Pros and Cons

**Pros**
- Stable CLI with a clear split: base flags vs. plugin flags.
- New signing schemes ship as plugins without core changes.
- Deterministic normalization and pinning.
- Works in CI (non-interactive) and locally (interactive).

**Cons**
- Plugin discovery/validation complexity.
- Need strict rules to avoid multiple plugins being activated at once.
- Responsibility to secure key/token handling lies partly with plugins.

---

## Security Considerations

- Prefer `--password-prompt` over `--password` to avoid secrets in process lists.
- Keep OIDC tokens short-lived; prefer passing by file descriptor or `@path` with restrictive permissions.
- Record the `ComponentDigest` and a `NormalizationID` inside the envelope for reproducibility.

---

## Out of Scope

- Digest calculation details (covered by separate command/implementation).
