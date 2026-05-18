---
title: "Verify Component Versions (Keyless via Sigstore)"
description: "Verify a Sigstore-signed component version against identity constraints — no public key distribution required."
icon: "🛡️"
weight: 9
toc: true
---

## You'll end up with

- Confidence that a component version was signed by an identity you trust (verified via Sigstore + Fulcio + Rekor)

**Estimated time:** ~3 minutes

## Prerequisites

- [OCM CLI installed]({{< relref "ocm-cli-installation.md" >}})
- A component version signed with Sigstore (see [How-To: Sign Component Versions (Keyless)]({{< relref "sign-component-version-keyless.md" >}}))
- The expected signer identity (OIDC issuer + email or workload identity URI)

{{< callout context="tip" >}}
No public key configuration is needed. Verification is bound to the **identity** recorded in the signing certificate, not to a key you have to distribute.
{{< /callout >}}

## Steps

Pick the tab that matches the Sigstore stack the signature was made against. Each tab is a self-contained walkthrough.

{{< tabs >}}
{{< tab "Public Sigstore (sigstore.dev)" >}}

The signature was made against the public-good Sigstore infrastructure (`fulcio.sigstore.dev`, `rekor.sigstore.dev`). The trusted root is fetched from the embedded TUF root — no credentials needed.

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

{{< tabs >}}
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

### Verify a specific signature (optional)

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

{{< /tab >}}
{{< tab "Private/Enterprise Sigstore" >}}

The signature was made against a self-hosted Sigstore stack (own OIDC provider + Fulcio + Rekor + TSA). The verifier does **not** consult the public Rekor; instead it relies on a trusted root you configure as a credential.

{{< steps >}}

{{< step >}}

### Configure the trusted root via credentials

The trusted root is the verifier's anchor for your private Fulcio CA and Rekor public key. It does **not** live on the verifier spec — it goes into `.ocmconfig` as a credential, the same place RSA public keys go. Add a consumer entry under the `SigstoreVerifier/v1alpha1` identity:

```yaml
type: generic.config.ocm.software/v1
configurations:
- type: credentials.config.ocm.software
  consumers:
  - identity:
      type: SigstoreVerifier/v1alpha1
      signature: default
    credentials:
    - type: Credentials/v1
      properties:
        trusted_root_json_file: /path/to/trusted_root.json
```

Use `trusted_root_json` (inline JSON) if you'd rather not point at a file. The `signature` field must match the `--signature` flag passed to `ocm verify` (defaults to `default`).

{{< /step >}}

{{< step >}}

### Create a Sigstore verifier spec

Set `privateInfrastructure: true` and the identity constraints the signature must satisfy:

```yaml
# sigstore-verify.yaml
type: SigstoreVerificationConfiguration/v1alpha1
privateInfrastructure: true
certificateOIDCIssuer: https://login.example.com/realms/ocm
certificateIdentity: ci@example.com
```

{{< callout context="note" title="What privateInfrastructure does" >}}
`privateInfrastructure: true` tells the verifier the signature was made against a privately-deployed Sigstore stack — don't try to look the certificate up in the public Rekor transparency log. Signature, certificate chain, identity (SAN + issuer), and SCT (Signed Certificate Timestamp) checks are **unchanged**. The flag must be paired with the trusted-root credential from the previous step; without it, the verifier errors out.
{{< /callout >}}

{{< callout context="caution" >}}
Without identity constraints, verification cannot assert **who** signed the component. The verifier rejects specs that omit them.
{{< /callout >}}

{{< /step >}}

{{< step >}}

### Verify the component version

Run the verify command with the verifier spec:

{{< tabs >}}
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

### Verify a specific signature (optional)

If the component carries multiple signatures (e.g. an RSA signature and a Sigstore signature), select one with `--signature`:

```bash
ocm verify cv \
  --verifier-spec ./sigstore-verify.yaml \
  --signature sigstore \
  ghcr.io/myorg/components//github.com/acme.org/helloworld:1.0.0
```

{{< /step >}}

{{< /steps >}}

{{< /tab >}}
{{< /tabs >}}

## Inspect the recorded identity (optional)

The signature value is a Sigstore bundle that embeds the Fulcio certificate. To see the identity that was bound at signing time:

```bash
ocm get cv /tmp/helloworld/transport-archive//github.com/acme.org/helloworld:1.0.0 -o yaml \
  | yq '.[0].signatures[] | select(.signature.algorithm == "sigstore")'
```

The bundle includes the certificate's `Subject Alternative Name` (your OIDC identity) and the `Issuer` extension (your OIDC provider). These are exactly what `certificateIdentity` and `certificateOIDCIssuer` are matched against during verification.

## Troubleshooting

### Symptom: "no matching identity in signing certificate"

**Cause:** The certificate's `SAN` or `OIDC Issuer` extension doesn't match the spec's identity constraints.

**Fix:** Inspect the embedded certificate (see above) and align `certificateIdentity` / `certificateOIDCIssuer` with what's actually there. Watch for trailing slashes, capitalization, and exact-vs-regexp mismatches.

### Symptom: "transparency log entry not found"

**Cause:** The signature was made against a different Rekor instance than the one being queried, or the entry was pruned.

**Fix:** For private/enterprise infrastructure, ensure a trusted-root credential is configured for the `SigstoreVerifier/v1alpha1` consumer in `.ocmconfig` and that your verifier spec sets `privateInfrastructure: true`. For the public log, verify the signature is recent and that you're not behind a network filter blocking `rekor.sigstore.dev`.

### Symptom: "certificate expired"

**Cause:** Sigstore certificates are short-lived (~10 minutes). The verifier checks that the certificate was valid **at the time** the Rekor inclusion proof was issued — not at verification time. This error means the inclusion proof itself is missing or invalid.

**Fix:** Re-fetch the component version. If the bundle is intact, the original signing flow likely failed to record the entry — re-sign the component.

### Symptom: "identity constraints required"

**Cause:** Your verifier spec is missing both an issuer **and** an identity constraint.

**Fix:** Set `certificateOIDCIssuer` (or `…Regexp`) **and** `certificateIdentity` (or `…Regexp`). Both are mandatory.

## CLI Reference

| Command | Description |
| --- | --- |
| [`ocm verify component-version`]({{< relref "docs/reference/ocm-cli/ocm_verify_component-version.md" >}}) | Verify a component version signature |
| [`ocm get component-version`]({{< relref "docs/reference/ocm-cli/ocm_get_component-version.md" >}}) | View component with signatures |

## Next Steps

- [How-to: Sign Component Versions (Keyless)]({{< relref "sign-component-version-keyless.md" >}}) — Produce Sigstore signatures
- [How-to: Verify Component Versions (RSA)]({{< relref "verify-component-version.md" >}}) — Traditional key-based verification

## Related Documentation

- [Concept: Signing and Verification]({{< relref "docs/concepts/signing-and-verification-concept.md" >}}) — How OCM signing works
- [ADR 0017: Sigstore Integration](https://github.com/open-component-model/open-component-model/blob/main/docs/adr/0017_sigstore_integration.md) — Design and OIDC flow details
