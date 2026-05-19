---
title: "Sign Component Versions"
description: "Cryptographically sign a component version using key-based or keyless signing algorithms."
icon: "🔏"
weight: 6
toc: true
---

## Goal

Sign a component version to certify its authenticity and enable downstream verification. OCM supports multiple signing algorithms.
Pick the tab that matches the algorithm you want to use — each tab is a self-contained walkthrough.

{{< tabs "sign-algorithm" >}}
{{< tab "RSA (key-based)" >}}

Sign with a long-lived RSA key pair. The private key produces the signature; verifiers use the matching public key to validate it.

## You'll end up with

- A component version with an RSA signature attached

**Estimated time:** ~3 minutes

## Prerequisites

- [OCM CLI installed]({{< relref "ocm-cli-installation.md" >}})
- [Signing credentials configured]({{< relref "configure-signing-credentials.md" >}})
- A component version in a CTF archive or OCI registry (we'll use `github.com/acme.org/helloworld:1.0.0` from the [getting started guide]({{< relref "create-component-version.md" >}})) in this guide, but you can use any component version you have.

## Steps

{{< steps >}}

{{< step >}}

### Sign the component version

Run the sign command against your component:

{{< tabs "sign-rsa-target" >}}
{{< tab "Local CTF Archive" >}}

```bash
ocm sign cv /tmp/helloworld/transport-archive//github.com/acme.org/helloworld:1.0.0
```
{{< /tab >}}
{{< tab "Remote OCI Registry" >}}

```bash
ocm sign cv ghcr.io/<your-namespace>//github.com/acme.org/helloworld:1.0.0
```
{{< /tab >}}
{{< /tabs >}}

{{< details "Expected output from signing" >}}

```text
time=2026-03-12T21:45:16.517+01:00 level=INFO msg="no signer spec file provided, using default" algorithm=RSASSA-PSS encodingPolicy=Plain
digest:
  hashAlgorithm: SHA-256
  normalisationAlgorithm: jsonNormalisation/v4alpha1
  value: 91dd197868907487e62872695db1fa7b397fde300bcbae23e24abc188fb147ad
name: default
signature:
  algorithm: RSASSA-PSS
  mediaType: application/vnd.ocm.signature.rsa.pss
  value: d1ea6e0cd850c8dbd0d20cd39b9c79547005e56a3df08974543e8a8b2f4ce17784d473a9397928432dfac1cefbf9c74087d3f0432275d692025b65d4feca6acabd6ed2cb495f77026a699f3e5009515d6b845cd698c210718a0788dbb08c4a345dae6c64a39c652edc1ede71ff2c7b0d4315351abede51c136d680b478a0ae9ae1a88916b289c59a8d263a8ad2223386f76104356b060caf8643405646bf106811cddbf7df1cdc7ba2a8323a1803d76238a9cd5bf700752ce4d9666acdb361f55d4fbda99ff794cf6d743f56a3d974441e708a4455686d5aefe1d22bc068c2e91acd18492af8624c0e6ef62afac0e176abd1db581ec12871281ad26f996c64d8ec164e9b9100f19d37491d2c13464f40c51ca8e0521e17578df4d8a89deb141c0fe7f4833ddee19ebffe292a065cf1a428860280905826469f1d44fed54c8654b94b32d19a798d6e40518fb53988f23a6266c968706d5276c1dc2664085337d169d1375413b75b86fc379bda3c1abab27c646502850eb27d88bdad4400d08ec1ca8dfe98806dff2bfd24cc1f50bd74fd632a881b99f72cf5ef7b20df910da663410b7021afffd5bd983805d461d27585225c933d52a2bea3a438c65a494b03d17fc9421fc02dff7d5bc36782fa5e9d1314bd5bfc291fed341fad084e3a5bb5da895fdaa00d6947c66e8cf0ed671ec44591c5fb84898e3263190c13d511380ad5

time=2026-03-12T21:45:16.532+01:00 level=INFO msg="signed successfully" name=default digest=91dd197868907487e62872695db1fa7b397fde300bcbae23e24abc188fb147ad hashAlgorithm=SHA-256 normalisationAlgorithm=jsonNormalisation/v4alpha1
```
{{< /details >}}
{{< /step >}}

{{< step >}}

### Use a named signature (optional)

If you have multiple signing configurations in your `.ocmconfig`,
use `--signature` flag to specify which one to use.
Without the flag, OCM uses the configuration named `default`. In this example, we'll use a configuration named `prod`:

```bash
ocm sign cv --signature prod /tmp/helloworld/transport-archive//github.com/acme.org/helloworld:1.0.0
```

{{< /step >}}

{{< step >}}

### Verify the signature was added

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
      algorithm: RSASSA-PSS
      value: d1ea6e0...
```

{{< /step >}}
{{< /steps >}}

## Advanced: Choosing an Encoding Policy

By default, OCM uses **Plain encoding** -- the raw signature bytes are hex-encoded and stored directly. No extra configuration is needed.

To use **PEM encoding** (embeds the signer's X.509 certificate chain in the signature), create a signer spec file and pass it with `--signer-spec`:

```yaml
# pem-signer.yaml
type: RSASigningConfiguration/v1alpha1
signatureAlgorithm: RSASSA-PSS
signatureEncodingPolicy: PEM
```

```bash
ocm sign cv \
  --signer-spec pem-signer.yaml \
  /tmp/helloworld/transport-archive//github.com/acme.org/helloworld:1.0.0
```

PEM encoding requires that `public_key_pem_file` in `.ocmconfig` points to an X.509 certificate chain (leaf + any intermediates), not a bare public key. Verifiers only need the root CA. See [Tutorial: Certificate Chains (PEM)]({{< relref "docs/tutorials/signing/pem.md" >}}) for the full workflow.

## Troubleshooting (RSA)

### Symptom: "no private key found"

**Cause:** OCM cannot find a matching signing configuration in `.ocmconfig`.

**Fix:** Ensure your `.ocmconfig` has a consumer entry with matching `signature` name:

- Without `--signature` flag: must have `signature: default`
- With `--signature prod`: must have `signature: prod`

See [Configure Signing Credentials]({{< relref "configure-signing-credentials.md" >}}) guide.

### Symptom: "signature already exists"

**Cause:** The component version already has a signature with this name.

**Fix:** Use a different signature name with `--signature newname`.

### Symptom: Permission denied on registry

**Cause:** Missing write access to the OCI registry.

**Fix:** Ensure you're `.ocmconfig` file is configured with credentials for the registry.
See [How-To: Configure Credentials for Multiple Registries]({{< relref "configure-multiple-credentials.md" >}}) for details.

{{< /tab >}}
{{< tab "Sigstore (keyless)" >}}

Sign with [Sigstore](https://www.sigstore.dev/) keyless signing.
Instead of a long-lived RSA key, the signature is bound to your OIDC identity (e.g. your corporate email)
via a short-lived certificate issued by Fulcio and recorded in the Rekor transparency log.

This walkthrough uses the public-good Sigstore infrastructure: Dex federates to Google/GitHub/Microsoft for OIDC, Fulcio issues the certificate, and the entry is recorded in the public Rekor transparency log.

## You'll end up with (Sigstore)

- A component version signed with a Sigstore keyless signature
- A signature that ties the component to your OIDC identity, recorded in a Rekor transparency log

**Estimated time:** ~5 minutes

## Prerequisites (Sigstore)

- [OCM CLI installed]({{< relref "ocm-cli-installation.md" >}})
- A browser available on the machine (the OIDC flow opens a browser to authenticate you)
- A component version in a CTF archive or OCI registry (we'll use `github.com/acme.org/helloworld:1.0.0` from the [getting started guide]({{< relref "create-component-version.md" >}}); any component you can write to works)

{{< callout context="tip" >}}
You do **not** need to generate or distribute keys for this flow. Identity is the signer.
{{< /callout >}}

## Steps (Sigstore)

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

### Create a Sigstore signer spec

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

This is what the verifier's `certificateOIDCIssuer` constraint must match (see [How-to: Verify Component Versions]({{< relref "verify-component-version.md" >}})).
{{< /callout >}}

{{< /step >}}

{{< step >}}

### Sign the component version

Run the sign command with the signer spec:

{{< tabs "sign-sigstore-target" >}}
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

{{< step >}}

### Verify the signature was added

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

{{< /step >}}
{{< /steps >}}

## Troubleshooting (Sigstore)

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

{{< /tab >}}
{{< /tabs >}}

## Next Steps

- [How-to: Verify a Component Version]({{< relref "verify-component-version.md" >}}) — Verify signatures (RSA or Sigstore)

## Related Documentation

- [Concept: Signing and Verification]({{< relref "signing-and-verification-concept.md" >}}) — Understand how OCM signing works
- [Tutorial: Sign and Verify Components]({{< relref "docs/tutorials/signing/plain.md" >}}) — End-to-end signing workflow
- [ADR 0017: Sigstore Integration](https://github.com/open-component-model/open-component-model/blob/main/docs/adr/0017_sigstore_integration.md) — Sigstore design and OIDC flow details
