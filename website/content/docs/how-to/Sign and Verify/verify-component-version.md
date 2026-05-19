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

To run this you need the signer's public key on disk and pointed at by `.ocmconfig` (see prerequisites). With Sigstore (other tab) you don't install a public key at all — you just declare which identity you trust.

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

Verify a [Sigstore](https://www.sigstore.dev/) keyless signature. There's no public key to install on this side either — you tell OCM **which identity you trust**, and it checks the signature was made by that identity.

If you've done classical key-based verification, here's what changes:

| Aspect | RSA | Sigstore |
| --- | --- | --- |
| Before you verify | Obtain the signer's public key, configure `.ocmconfig` | Nothing — declare expected identity in a small spec file |
| What proves trust | Signature decrypts with the public key you have | Signature ties back to an OIDC identity you've decided to trust |
| Key rotation problem | You re-distribute the new public key | Doesn't apply — there's no long-lived key |

**Mental model:** instead of asking "does this signature match the public key I was handed?" you ask "was this signed by `jane.doe@example.com` logging in via GitHub?" The verifier only needs to know **who** to trust.

## You'll end up with (Sigstore)

- Confidence that a component version was signed by an identity you trust

**Estimated time:** ~3 minutes

## Prerequisites (Sigstore)

- [OCM CLI installed]({{< relref "ocm-cli-installation.md" >}})
- A component version signed with Sigstore (see [How-To: Sign Component Versions]({{< relref "sign-component-version.md" >}}))
- The expected signer identity (their OIDC email and which provider they logged in with)

{{< callout context="note" >}}
Want the full picture of how Sigstore verification works behind the scenes (Fulcio certificate validation, Rekor inclusion proofs, TUF trust roots)? A dedicated Sigstore tutorial is in the works. For now, [ADR 0017: Sigstore Integration](https://github.com/open-component-model/open-component-model/blob/main/docs/adr/0017_sigstore_integration.md) covers the design.
{{< /callout >}}

## Steps (Sigstore)

{{< steps >}}

{{< step >}}

### Declare which identity you trust

In Sigstore, **the verifier's only job is to decide whose signatures to accept.** You do that with a small spec file.

Two values matter:

- **`certificateIdentity`** — the email or workload identity of whoever signed (e.g. `jane.doe@example.com`)
- **`certificateOIDCIssuer`** — *which* OIDC provider they logged in with (e.g. GitHub vs. Google — both could have a `jane.doe@example.com`)

Create `sigstore-verify.yaml`:

```yaml
# sigstore-verify.yaml
type: SigstoreVerificationConfiguration/v1alpha1
certificateOIDCIssuer: https://github.com/login/oauth
certificateIdentity: jane.doe@example.com
```

That's the entire trust configuration. No public key to fetch, no certificate to install.

{{< callout context="caution" title="Pick the right `certificateOIDCIssuer` value" >}}
On public Sigstore (`oauth2.sigstore.dev`), the issuer recorded in the signing certificate is the **upstream identity provider's** issuer URL — *not* the Sigstore Dex endpoint. Pick the one matching the provider the signer logged in with:

| Signer logged in via | `certificateOIDCIssuer` value |
| --- | --- |
| Google | `https://accounts.google.com` |
| GitHub | `https://github.com/login/oauth` |
| Microsoft | `https://login.microsoftonline.com` |

{{< /callout >}}

{{< callout context="note" >}}
Both `certificateOIDCIssuer` and `certificateIdentity` are required. They're what *makes* the signature meaningful — without them you'd be accepting any Sigstore signature from anyone.
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

If verification fails because of an identity mismatch, you can read the identity directly from the signature to see what to put in your spec:

```bash
ocm get cv /tmp/helloworld/transport-archive//github.com/acme.org/helloworld:1.0.0 -o yaml \
  | yq '.[0].signatures[] | select(.signature.algorithm == "sigstore")'
```

Look for the signer email and the OIDC issuer URL. Those are exactly what `certificateIdentity` and `certificateOIDCIssuer` are matched against.

## Troubleshooting (Sigstore)

### Symptom: "no matching identity in signing certificate"

**Cause:** The signature was made by a different identity than what your spec expects — or the same identity but via a different OIDC provider.

**Fix:** Inspect the signature (see above) to see the actual identity, and update `certificateIdentity` / `certificateOIDCIssuer` to match. Watch for trailing slashes and capitalization.

### Symptom: "transparency log entry not found"

**Cause:** OCM couldn't reach Rekor (the public log Sigstore uses to record signatures), or the signature wasn't recorded there.

**Fix:** Check your network can reach `rekor.sigstore.dev`. If the signature is recent and the network is fine, the signing flow likely failed to record the entry — ask the signer to re-sign.

### Symptom: "certificate expired"

**Cause:** Sigstore certificates are intentionally short-lived (~10 minutes) — they're meant to last only as long as the signing operation. This error means the proof that ties the signature back to the certificate's valid window is missing or broken in the bundle.

**Fix:** Re-fetch the component version (the bundle may be partial). If it's still failing, ask the signer to re-sign.

### Symptom: "identity constraints required"

**Cause:** Your verifier spec is missing `certificateIdentity`, `certificateOIDCIssuer`, or both.

**Fix:** Both are mandatory — they're how Sigstore knows whose signatures to accept. See Step 1 above.

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
