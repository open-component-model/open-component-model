---
title: "Verify Component Versions"
description: "Validate component version signatures to ensure authenticity and integrity."
icon: "🔍"
weight: 7
toc: true
---

## You'll end up with

- Confidence that a component version is authentic and hasn't been tampered with

**Estimated time:** ~3 minutes

## Prerequisites

- [OCM CLI installed]({{< relref "ocm-cli-installation.md" >}})
- [Verification credentials configured]({{< relref "docs/how-to/configure-signing-credentials.md" >}}) with the public key
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

## Troubleshooting

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

See [Configure Signing Credentials]({{< relref "docs/how-to/configure-signing-credentials.md" >}}).

### Symptom: "invalid key format"

**Cause:** The public key file is not in PEM format.

**Fix:** Verify the key starts with `-----BEGIN PUBLIC KEY-----`:

```bash
head -n 1 /tmp/keys/public-key.pem
```

## CLI Reference

| Command | Description |
|---------|-------------|
| [`ocm verify componentversions`]({{< relref "docs/reference/ocm-cli/ocm_verify_component-version.md" >}}) | Verify a component version signature |
| [`ocm get componentversions`]({{< relref "docs/reference/ocm-cli/ocm_get_component-version.md" >}}) | View component with signatures |

## Next Steps

- [How-to: Sign Component Versions]({{< relref "sign-component-version.md" >}}) - Add signatures to your components
- [Tutorial: Signing and Verification]({{< relref "signing-and-verification.md" >}}) - Learn how to sign and verify components in a complete tutorial

## Related Documentation

- [Concept: Signing and Verification]({{< relref "docs/concepts/signing-and-verification-concept.md" >}}) - Understand how OCM signing works
