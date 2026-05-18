---
title: "Sign Component Versions (Keyless via Sigstore)"
description: "Cryptographically sign a component version with Sigstore keyless signing — no long-lived keys, identity bound via OIDC."
icon: "🪪"
weight: 7
toc: true
---

## Goal

Sign a component version using Sigstore keyless signing.
Instead of a long-lived RSA key, the signature is bound to your OIDC identity (e.g. your corporate email)
via a short-lived certificate issued by Fulcio and recorded in the Rekor transparency log.

## You'll end up with

- A component version signed with a Sigstore keyless signature
- A signature that ties the component to your OIDC identity, recorded in a Rekor transparency log

**Estimated time:** ~5 minutes

## Prerequisites

- [OCM CLI installed]({{< relref "ocm-cli-installation.md" >}})
- A browser available on the machine (the OIDC flow opens a browser to authenticate you)
- A component version in a CTF archive or OCI registry (we'll use `github.com/acme.org/helloworld:1.0.0` from the [getting started guide]({{< relref "create-component-version.md" >}}); any component you can write to works)

{{< callout context="tip" >}}
You do **not** need to generate or distribute keys for this flow. Identity is the signer.
{{< /callout >}}

## Steps

Pick the tab that matches your Sigstore stack. Each tab is a self-contained walkthrough.

{{< tabs >}}
{{< tab "Public Sigstore (sigstore.dev)" >}}

Sign through the public-good Sigstore infrastructure: Dex federates to Google/GitHub/Microsoft for OIDC, Fulcio issues the certificate, and the entry is recorded in the public Rekor transparency log.

{{< steps >}}

{{< step >}}

### Configure your OCM credentials for OIDC

Add a consumer entry to your `.ocmconfig` so the Sigstore signing handler can acquire an OIDC identity token via the public Sigstore Dex instance:

```yaml
type: generic.config.ocm.software/v1
configurations:
- type: credentials.config.ocm.software
  consumers:
  - identity:
      type: SigstoreSigner/v1alpha1
      signature: default
    credentials:
    - type: OIDCIdentityTokenProvider/v1alpha1
```

{{< callout context="note" >}}
`signature` must match the `--signature` flag passed to `ocm sign` (defaults to `default`); add more consumer entries with distinct `signature` names for multi-signature setups.
{{< /callout >}}

{{< /step >}}

{{< step >}}

### Create a Sigstore signer spec (Public Sigstore)

Create `sigstore-sign.yaml`. With no extra fields, the handler uses public-good Sigstore TUF discovery (`sigstore.dev`) and the public Sigstore Dex OIDC issuer:

```yaml
# sigstore-sign.yaml
type: SigstoreSigningConfiguration/v1alpha1
```

{{< callout context="caution" title="The OIDC issuer in the Fulcio certificate is the upstream IdP, not the Dex endpoint" >}}
On public Sigstore (`oauth2.sigstore.dev`), Dex federates to upstream identity providers (Google, GitHub, Microsoft) but **passes through the upstream `iss` claim** to Fulcio. Fulcio then writes that **upstream issuer** into the signing certificate (OID `1.3.6.1.4.1.57264.1.8`) — *not* the Dex URL.

Concretely, depending on which provider you logged in with:

| Login provider | Issuer recorded in the certificate |
| --- | --- |
| Google | `https://accounts.google.com` |
| GitHub | `https://github.com/login/oauth` |
| Microsoft | `https://login.microsoftonline.com` |

This is what the verifier's `certificateOIDCIssuer` constraint must match (see [How-to: Verify Component Versions (Keyless)]({{< relref "verify-component-version-keyless.md" >}})).
{{< /callout >}}

{{< /step >}}

{{< step >}}

### Sign the component version (Public Sigstore)

Run the sign command with the signer spec:

{{< tabs >}}
{{< tab "Local CTF Archive" >}}

```bash
ocm sign cv \
  --signer-spec ./sigstore-sign.yaml \
  /tmp/helloworld/transport-archive//github.com/acme.org/helloworld:1.0.0
```
{{< /tab >}}
{{< tab "Remote OCI Registry" >}}

```bash
ocm sign cv \
  --signer-spec ./sigstore-sign.yaml \
  ghcr.io/<your-namespace>//github.com/acme.org/helloworld:1.0.0
```
{{< /tab >}}
{{< /tabs >}}

A browser window opens against your OIDC provider's login page. Authenticate, and you'll see the OCM "Signing identity verified!" page. Return to the terminal — signing continues automatically.

{{< details "Expected output from signing" >}}

```text
time=2026-05-18T10:12:03.118+02:00 level=INFO msg="acquiring OIDC identity token" issuer=https://oauth2.sigstore.dev/auth
time=2026-05-18T10:12:08.402+02:00 level=INFO msg="OIDC identity token acquired"
time=2026-05-18T10:12:08.601+02:00 level=INFO msg="signing via Sigstore" fulcio=https://fulcio.sigstore.dev rekor=https://rekor.sigstore.dev
time=2026-05-18T10:12:11.972+02:00 level=INFO msg="signed successfully" name=default digest=91dd197868907487e62872695db1fa7b397fde300bcbae23e24abc188fb147ad hashAlgorithm=SHA-256 normalisationAlgorithm=jsonNormalisation/v4alpha1
```
{{< /details >}}

{{< callout context="tip" >}}
The first run downloads and caches the `cosign` binary into `~/.cache/ocm/cosign/...`. Subsequent runs skip the download.
{{< /callout >}}

{{< /step >}}

{{< /steps >}}

{{< /tab >}}
{{< tab "Private/Enterprise Sigstore" >}}

Sign through a self-hosted Sigstore stack: your own OIDC provider (Keycloak/Okta/Azure AD/...), your own Fulcio CA, your own Rekor instance, and optionally your own TSA. Endpoint discovery uses a `signing_config.json` produced by `cosign signing-config create`.

{{< steps >}}

{{< step >}}

### Configure your OCM credentials for enterprise OIDC

Add a consumer entry to your `.ocmconfig`. For an enterprise OIDC provider, `issuer` and `clientID` must appear on the consumer **identity** so the credential graph routes to the correct entry. The same values must appear on the signer spec (next step) — the handler emits them into the lookup identity at sign time, and the graph match only succeeds when both sides agree:

```yaml
type: generic.config.ocm.software/v1
configurations:
- type: credentials.config.ocm.software
  consumers:
  - identity:
      type: SigstoreSigner/v1alpha1
      signature: default
      issuer: https://login.example.com/realms/ocm
      clientID: ocm-cli
    credentials:
    - type: OIDCIdentityTokenProvider/v1alpha1
```

{{< /step >}}

{{< step >}}

### Create a Sigstore signer spec (Private/Enterprise)

Point at a local `signing_config.json` that lists your Fulcio/Rekor/TSA endpoints (create one with `cosign signing-config create`). Set `issuer` and `clientID` to the same values used on the consumer identity above:

```yaml
# sigstore-sign.yaml
type: SigstoreSigningConfiguration/v1alpha1
signingConfig: /path/to/signing_config.json
issuer: https://login.example.com/realms/ocm
clientID: ocm-cli
```

{{< callout context="note" >}}
`signingConfig` replaces the public-good TUF auto-discovery with your enterprise endpoints. `issuer`/`clientID` are not used by the handler directly — they are emitted into the credential consumer identity so `.ocmconfig` can route to the enterprise OIDC credential plugin instead of the public-good default.
{{< /callout >}}

{{< /step >}}

{{< step >}}

### Sign the component version (Private/Enterprise)

Run the sign command with the signer spec:

{{< tabs >}}
{{< tab "Local CTF Archive" >}}

```bash
ocm sign cv \
  --signer-spec ./sigstore-sign.yaml \
  /tmp/helloworld/transport-archive//github.com/acme.org/helloworld:1.0.0
```
{{< /tab >}}
{{< tab "Remote OCI Registry" >}}

```bash
ocm sign cv \
  --signer-spec ./sigstore-sign.yaml \
  ghcr.io/<your-namespace>//github.com/acme.org/helloworld:1.0.0
```
{{< /tab >}}
{{< /tabs >}}

A browser opens against your enterprise OIDC provider. Authenticate, and signing continues automatically.

{{< callout context="tip" >}}
The first run downloads and caches the `cosign` binary into `~/.cache/ocm/cosign/...`. Subsequent runs skip the download.
{{< /callout >}}

{{< /step >}}

{{< /steps >}}

{{< /tab >}}
{{< /tabs >}}

## Verify the signature was added

Check that the signature is present in the component descriptor:

```bash
ocm get cv /tmp/helloworld/transport-archive//github.com/acme.org/helloworld:1.0.0 -o yaml
```

Look for the `signatures` section in the output:

```yaml
signatures:
  - name: default
    digest:
      hashAlgorithm: SHA-256
      normalisationAlgorithm: jsonNormalisation/v4alpha1
      value: 91dd197...
    signature:
      algorithm: sigstore
      mediaType: application/vnd.dev.sigstore.bundle.v0.3+json
      value: <base64-bundle>
```

The `value` field contains the full Sigstore bundle (signature + Fulcio certificate + Rekor inclusion proof).

## Troubleshooting

### Symptom: "browser did not open" or "timed out waiting for authentication callback"

**Cause:** The OIDC flow needs a browser on the machine running `ocm sign`, plus a free loopback port (`127.0.0.1`) for the OAuth callback.

**Fix:** Run on a workstation with a graphical browser. Headless / CI environments need a different identity flow (e.g. workload identity tokens supplied via the credentials config) — the interactive flow is not designed for unattended use.

### Symptom: "OIDC provider does not support PKCE S256"

**Cause:** The configured `issuer` doesn't advertise `S256` in `code_challenge_methods_supported`.

**Fix:** Use a provider that supports PKCE S256 (sigstore.dev does; most modern enterprise OPs do). PKCE S256 is required for the OCM CLI's public client flow.

### Symptom: "issuer mismatch in callback"

**Cause:** Your provider sent an `iss` parameter that doesn't match the configured `issuer` (RFC 9207).

**Fix:** Make sure the `issuer` in `.ocmconfig` matches your provider's canonical issuer URL exactly (scheme, host, path). Trailing slashes matter.

### Symptom: Permission denied on registry

**Cause:** Missing write access to the OCI registry.

**Fix:** Configure registry credentials in `.ocmconfig`. See [How-To: Configure Credentials for Multiple Registries]({{< relref "configure-multiple-credentials.md" >}}).

## Next Steps

- [How-to: Verify a Component Version (Keyless)]({{< relref "verify-component-version-keyless.md" >}}) — Verify a Sigstore signature using identity constraints

## Related Documentation

- [How-to: Sign Component Versions (RSA)]({{< relref "sign-component-version.md" >}}) — Traditional key-based signing
- [Concept: Signing and Verification]({{< relref "signing-and-verification-concept.md" >}}) — How OCM signing works
- [ADR 0017: Sigstore Integration](https://github.com/open-component-model/open-component-model/blob/main/docs/adr/0017_sigstore_integration.md) — Design and OIDC flow details
