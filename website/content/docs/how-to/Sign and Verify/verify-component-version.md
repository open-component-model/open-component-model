---
title: "Verify Component Versions"
description: "Validate component version signatures using key-based or keyless verification methods."
icon: "🔍"
weight: 8
toc: true
---

## Goal

Validate a component version signature to ensure it is authentic and has not been tampered with. OCM supports multiple verification algorithms.
Pick the tab that matches the algorithm the signature was made with — each tab is a self-contained walkthrough.

{{< tabs "verify-algorithm" >}}
{{< tab "RSA (key-based)" >}}

Verify an RSA signature using the matching public key configured in `.ocmconfig`.

## You'll end up with

- Confidence that a component version is authentic and hasn't been tampered with

**Estimated time:** ~3 minutes

## Prerequisites

- [OCM CLI installed]({{< relref "ocm-cli-installation.md" >}})
- [Verification credentials configured]({{< relref "configure-signing-credentials.md" >}}) with the public key
- A signed component version to verify in your current directory, here we use the `helloworld` component version from the [getting started guide]({{< relref "create-component-version.md" >}}) that you've signed in the [How-To: Sign Component Versions]({{< relref "sign-component-version.md" >}}) guide.

## Steps

{{< steps >}}

{{< step >}}

### Verify the component version

Run the verify command against your signed component:

```bash
ocm verify cv <repository>//<component>:<version>
```

**Local CTF Archive:**

```bash
ocm verify cv /tmp/helloworld/transport-archive//github.com/acme.org/helloworld:1.0.0
```

**Remote OCI Registry:**

```bash
ocm verify cv ghcr.io/myorg/components//github.com/acme.org/helloworld:1.0.0
```

<details>
<summary>Expected output</summary>

```text
time=2025-11-19T15:58:22.431+01:00 level=INFO msg="verifying signature" name=default
time=2025-11-19T15:58:22.435+01:00 level=INFO msg="signature verification completed" name=default duration=4.287541ms
time=2025-11-19T15:58:22.435+01:00 level=INFO msg="SIGNATURE VERIFICATION SUCCESSFUL"
```

</details>

The command exits with status code `0` on success.

{{< /step >}}

{{< step >}}

### Verify a specific signature (optional)

If the component has multiple signatures, specify which one to verify:

```bash
ocm verify cv --signature prod ghcr.io/myorg/components//github.com/acme.org/helloworld:1.0.0
```

> 👉 Without the `--signature` flag, OCM uses the configuration named `default`.

{{< /step >}}

{{< step >}}

### List available signatures (optional)

View all signatures in a component version:

```bash
ocm get cv ./tmp/helloworld/transport-archive//github.com/acme.org/helloworld:1.0.0 -o yaml | grep -A 10 signatures:
```

{{< /step >}}

{{< /steps >}}

## Troubleshooting (RSA)

### Symptom: "signature verification failed"

**Cause:** Public key doesn't match the signing private key, or the component was modified after signing.

**Fix:** Ensure you're using the correct public key that corresponds to the private key used for signing:

```bash
# Check which signature names exist
ocm get cv /tmp/helloworld/transport-archive//github.com/acme.org/helloworld:1.0.0 -o yaml | grep -A 3 "signatures:"

# Verify with the correct signature name
ocm verify cv --signature <name> /tmp/helloworld/transport-archive//github.com/acme.org/helloworld:1.0.0
```

### Symptom: "no public key found"

**Cause:** OCM cannot find a matching verification configuration in `.ocmconfig`.

**Fix:** Ensure your `.ocmconfig` has a consumer entry with the matching `signature` name and `public_key_pem_file` path.

See [Configure Signing Credentials]({{< relref "configure-signing-credentials.md" >}}).

### Symptom: "invalid key format"

**Cause:** The public key file is not in PEM format.

**Fix:** Verify the key starts with `-----BEGIN PUBLIC KEY-----`:

```bash
head -n 1 /tmp/keys/public-key.pem
```

{{< /tab >}}
{{< tab "Sigstore (keyless)" >}}

Verify a [Sigstore](https://www.sigstore.dev/) keyless signature against identity constraints. No public key configuration is needed — verification is bound to the **identity** recorded in the signing certificate, not to a key you have to distribute.

This walkthrough targets signatures made against the public-good Sigstore infrastructure (`fulcio.sigstore.dev`, `rekor.sigstore.dev`). The trusted root is fetched from the embedded TUF root — no credentials needed.

## You'll end up with

- Confidence that a component version was signed by an identity you trust (verified via Sigstore + Fulcio + Rekor)

**Estimated time:** ~3 minutes

## Prerequisites

- [OCM CLI installed]({{< relref "ocm-cli-installation.md" >}})
- A component version signed with Sigstore (see [How-To: Sign Component Versions]({{< relref "sign-component-version.md" >}}))
- The expected signer identity (OIDC issuer + email or workload identity URI)

## Steps

{{< steps >}}

{{< step >}}

### Create a Sigstore verifier spec

Create `sigstore-verify.yaml` with the identity constraints the signature must satisfy. Both an **issuer** and an **identity** constraint are required:

```yaml
# sigstore-verify.yaml
type: SigstoreVerificationConfiguration/v1alpha1
certificateOIDCIssuer: https://github.com/login/oauth
certificateIdentity: jane.doe@example.com
```

{{< callout context="caution" title="certificateOIDCIssuer is the upstream IdP, not the Dex endpoint" >}}
On public Sigstore (`oauth2.sigstore.dev`), the value Fulcio writes into the certificate's OIDC Issuer extension is the **upstream identity provider**'s issuer URL, not the Dex federation endpoint. Use the value that matches the provider the signer logged in with:

| Signer logged in via | `certificateOIDCIssuer` value |
| --- | --- |
| Google | `https://accounts.google.com` |
| GitHub | `https://github.com/login/oauth` |
| Microsoft | `https://login.microsoftonline.com` |

{{< /callout >}}

{{< callout context="caution" >}}
Without identity constraints, verification cannot assert **who** signed the component. The verifier rejects specs that omit them.
{{< /callout >}}

{{< /step >}}

{{< step >}}

### Verify the component version

Run the verify command with the verifier spec:

{{< tabs "verify-sigstore-target" >}}
{{< tab "Local CTF Archive" >}}

```bash
ocm verify cv \
  --verifier-spec ./sigstore-verify.yaml \
  /tmp/helloworld/transport-archive//github.com/acme.org/helloworld:1.0.0
```
{{< /tab >}}
{{< tab "Remote OCI Registry" >}}

```bash
ocm verify cv \
  --verifier-spec ./sigstore-verify.yaml \
  ghcr.io/myorg/components//github.com/acme.org/helloworld:1.0.0
```
{{< /tab >}}
{{< /tabs >}}

<details>
<summary>Expected output</summary>

```text
time=2026-05-18T10:18:42.114+02:00 level=INFO msg="verifying signature" name=default
time=2026-05-18T10:18:42.612+02:00 level=INFO msg="signature verification completed" name=default duration=498.214ms
time=2026-05-18T10:18:42.612+02:00 level=INFO msg="SIGNATURE VERIFICATION SUCCESSFUL"
```

</details>

{{< /step >}}

{{< step >}}

### Verify a specific signature

If the component carries multiple signatures (e.g. an RSA signature and a Sigstore signature), select one with `--signature`:

```bash
ocm verify cv \
  --verifier-spec ./sigstore-verify.yaml \
  --signature sigstore \
  ghcr.io/myorg/components//github.com/acme.org/helloworld:1.0.0
```

{{< callout context="note" >}}
The verifier spec's `type` field decides **which** verifier handles the signature. The `--signature` flag picks **which** signature on the component to verify.
{{< /callout >}}

{{< /step >}}

{{< /steps >}}

## Inspect the recorded identity (optional)

The signature value is a Sigstore bundle that embeds the Fulcio certificate. To see the identity that was bound at signing time:

```bash
ocm get cv /tmp/helloworld/transport-archive//github.com/acme.org/helloworld:1.0.0 -o yaml \
  | yq '.[0].signatures[] | select(.signature.algorithm == "sigstore")'
```

The bundle includes the certificate's `Subject Alternative Name` (your OIDC identity) and the `Issuer` extension (your OIDC provider). These are exactly what `certificateIdentity` and `certificateOIDCIssuer` are matched against during verification.

## Troubleshooting (Sigstore)

### Symptom: "no matching identity in signing certificate"

**Cause:** The certificate's `SAN` or `OIDC Issuer` extension doesn't match the spec's identity constraints.

**Fix:** Inspect the embedded certificate (see above) and align `certificateIdentity` / `certificateOIDCIssuer` with what's actually there. Watch for trailing slashes, capitalization, and exact-vs-regexp mismatches.

### Symptom: "transparency log entry not found"

**Cause:** The signature was made against a different Rekor instance than the one being queried, or the entry was pruned.

**Fix:** Verify the signature is recent and that you're not behind a network filter blocking `rekor.sigstore.dev`.

### Symptom: "certificate expired"

**Cause:** Sigstore certificates are short-lived (~10 minutes). The verifier checks that the certificate was valid **at the time** the Rekor inclusion proof was issued — not at verification time. This error means the inclusion proof itself is missing or invalid.

**Fix:** Re-fetch the component version. If the bundle is intact, the original signing flow likely failed to record the entry — re-sign the component.

### Symptom: "identity constraints required"

**Cause:** Your verifier spec is missing both an issuer **and** an identity constraint.

**Fix:** Set `certificateOIDCIssuer` (or `…Regexp`) **and** `certificateIdentity` (or `…Regexp`). Both are mandatory.

{{< /tab >}}
{{< /tabs >}}

## CLI Reference

| Command | Description |
| --- | --- |
| [`ocm verify component-version`]({{< relref "docs/reference/ocm-cli/ocm_verify_component-version.md" >}}) | Verify a component version signature |
| [`ocm get component-version`]({{< relref "docs/reference/ocm-cli/ocm_get_component-version.md" >}}) | View component with signatures |

## Next Steps

- [How-to: Sign Component Versions]({{< relref "sign-component-version.md" >}}) — Add signatures to your components (RSA or Sigstore)
- [Tutorial: Signing and Verification]({{< relref "docs/tutorials/signing/plain.md" >}}) — Learn how to sign and verify components in a complete tutorial

## Related Documentation

- [Concept: Signing and Verification]({{< relref "docs/concepts/signing-and-verification-concept.md" >}}) — Understand how OCM signing works
- [ADR 0017: Sigstore Integration](https://github.com/open-component-model/open-component-model/blob/main/docs/adr/0017_sigstore_integration.md) — Sigstore design and OIDC flow details
