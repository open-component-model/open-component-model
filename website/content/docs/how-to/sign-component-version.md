---
title: "Sign Component Versions"
description: "Cryptographically sign a component version to establish authenticity and enable verification."
icon: "🔏"
weight: 6
toc: true
---

## Goal

Sign a component version to certify its authenticity and enable downstream verification.

## You'll end up with

- A component version with a cryptographic signature attached

**Estimated time:** ~3 minutes

## Prerequisites

- [OCM CLI installed]({{< relref "ocm-cli-installation.md" >}})
- [Signing credentials configured]({{< relref "docs/how-to/configure-signing-credentials.md" >}})
- A component version in a CTF archive or OCI registry (we'll use `github.com/acme.org/helloworld:1.0.0` from the [getting started guide]({{< relref "create-component-version.md" >}})) in this guide, but you can use any component version you have.

## Steps

{{< steps >}}

{{< step >}}

### Sign the component version

Run the sign command against your component:

{{< tabs >}}
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

## Troubleshooting

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

## Next Steps

- [How-to: Verify a Component Version]({{< relref "verify-component-version.md" >}}) — Verify signatures using public keys

## Related Documentation

- [Concept: Signing and Verification]({{< relref "signing-and-verification-concept.md" >}}) — Understand how OCM signing works
- [Tutorial: Sign and Verify Components]({{< relref "signing-and-verification.md" >}}) — End-to-end signing workflow
