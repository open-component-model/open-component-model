---
title: "Configure Credentials for Signing"
description: "Configure OCM signing and verification keys using .ocmconfig or signer specification files."
icon: "🔑"
weight: 5
toc: true
---

Set up credential configuration so OCM can find your signing keys when signing or verifying component versions.

## You'll end up with

- A configured `.ocmconfig` file that OCM uses to locate your signing keys
- Ability to sign and verify component versions without specifying key paths manually

**Estimated time:** ~3 minutes

## Prerequisites

- [OCM CLI installed]({{< relref "docs/getting-started/ocm-cli-installation.md" >}})
- [RSA key pair generated]({{< relref "generate-signing-keys.md" >}})
- A component version to test your configuration in your current directory (we'll use `github.com/acme.org/helloworld:1.0.0` from the [getting started guide]({{< relref "create-component-version.md" >}})) in this guide, but you can use any component version you have.

## Steps

{{< steps >}}
{{< step >}}

## Create .ocmconfig file (optional)

Create `.ocmconfig` in your current directory. If you already have an `.ocmconfig` file, you can skip this step and add the credential configuration to your existing file.

```bash
touch .ocmconfig
```

{{< /step >}}

{{< step >}}

## Add the signing credential configuration to your .ocmconfig

Copy the following YAML into your `.ocmconfig` file.

We use the key pair you created in the [How-To: Generate Signing Keys]({{< relref "generate-signing-keys.md" >}}).
If you already have a key pair that is located in a different location, simply update the file paths accordingly.

All three identity attributes (`type`, `algorithm`, `signature`) are required for credential matching.
See the [Consumer Identities Reference]({{< relref "docs/reference/credential-consumer-identities.md" >}}) for details.

There are two ways to configure signing credentials, either using file paths that point to your key files,
or by including the key material directly in the config file.
Check out the [Credential Consumer Identities Reference]({{< relref "docs/reference/credential-consumer-identities.md" >}})
for more details on the supported attributes and configuration options.

The most convenient way to configure signing credentials is to add a consumer block to your `.ocmconfig` with the key file paths:

```yaml
type: generic.config.ocm.software/v1
configurations:
  - type: credentials.config.ocm.software
    consumers:
      - identity:
          type: RSA/v1alpha1
          algorithm: RSASSA-PSS
          signature: default
        credentials:
          - type: Credentials/v1
            properties:
              private_key_pem_file: /tmp/keys/private-key.pem
              public_key_pem_file: /tmp/keys/public-key.pem
```

**Key paths:**

- `private_key_pem_file` - Required for **signing** operations
- `public_key_pem_file` - Required for **verification** operations

<br>
It is also possible to configure the keys inline using `private_key_pem` and `public_key_pem` properties instead of file paths.

{{< details "Example .ocmconfig with inline keys" >}}
```yaml
type: generic.config.ocm.software/v1
configurations:
  - type: credentials.config.ocm.software
    consumers:
      - identity:
          type: RSA/v1alpha1
          algorithm: RSASSA-PSS
          signature: default
        credentials:
          - type: Credentials/v1
            properties:
              private_key_pem: |
                -----BEGIN RSA PRIVATE KEY-----
                MIIEpAIBAAKCAQEA...
                -----END RSA PRIVATE KEY-----
              public_key_pem: |
                -----BEGIN PUBLIC KEY-----
                MIIBIjANBgkqhkiG9w0BAQEFAAOCAQ8A...
                -----END PUBLIC KEY-----
```
{{< /details >}}
{{< /step >}}

{{< step >}}

## Test the configuration

```bash
ocm sign cv --dry-run /tmp/helloworld/transport-archive//github.com/acme.org/helloworld:1.0.0
```

If configured correctly, the dry run completes without "no private key found" errors.

{{< details "Expected output" >}}

```text
time=2026-03-12T17:05:46.428+01:00 level=INFO msg="no signer spec file provided, using default" algorithm=RSASSA-PSS encodingPolicy=Plain
digest:
  hashAlgorithm: SHA-256
  normalisationAlgorithm: jsonNormalisation/v4alpha1
  value: 91dd197868907487e62872695db1fa7b397fde300bcbae23e24abc188fb147ad
name: default
signature:
  algorithm: RSASSA-PSS
  mediaType: application/vnd.ocm.signature.rsa.pss
  value: 0cb48e5867575151fca94e995fc03c6df734163aed8fbee46231c9b36e59956d51df60263d8cd58e3de7662b2fbc3c4f800107d96b4fc27e7a16807388f7e5a73d2269290c0f367d0eb92d930b485054911c10e22ed1fe6c5bfab441f1af28d8deec4df8d67ca5a54fa4495510e2fff809fe8162f875d6b91a6bc1d29e7466f113a9d9d23f16956588a5792e4c7553a8ceb6f8c630aa6090aceb83e763734c33902d4697beadc65a6bc4761e6221ec49a6882bd46c87a14c5a5c24c70bf95880d0a43b176a5bf6200837ce344abff360e13f07db35290b3e1e3639a0fdc87252542965ea95231444807564c718734ccf10a5dbbb58a8b11f7df418002e6bebfa

time=2026-03-12T17:05:46.437+01:00 level=INFO msg="dry run: signature not persisted"
```

{{< /details >}}
{{< /step >}}

{{< /steps >}}

### Configure multiple signing identities

For different environments (e.g., dev and prod) you can create different key pairs and
add multiple consumer blocks to your `.ocmconfig` with different `signature` names:

```yaml
type: generic.config.ocm.software/v1
configurations:
  - type: credentials.config.ocm.software
    consumers:
      - identity:
          type: RSA/v1alpha1
          algorithm: RSASSA-PSS
          signature: dev
        credentials:
          - type: Credentials/v1
            properties:
              private_key_pem_file: /tmp/keys/dev/private-key.pem
              public_key_pem_file: /tmp/keys/dev/public-key.pem
      - identity:
          type: RSA/v1alpha1
          algorithm: RSASSA-PSS
          signature: prod
        credentials:
          - type: Credentials/v1
            properties:
              private_key_pem_file: /tmp/keys/prod/private-key.pem
              public_key_pem_file: /tmp/keys/prod/public-key.pem
```

Specify the signature name when signing:

```bash
ocm sign cv --dry-run --signature dev /tmp/helloworld/transport-archive//github.com/acme.org/helloworld:1.0.0
ocm sign cv --dry-run --signature prod /tmp/helloworld/transport-archive//github.com/acme.org/helloworld:1.0.0
```

## Identity Attributes Reference

The consumer identity for RSA signing/verification supports these attributes:

| Attribute   | Required | Description                                         |
|-------------|----------|-----------------------------------------------------|
| `type`      | Yes      | Must be `RSA/v1alpha1`                              |
| `algorithm` | Yes      | `RSASSA-PSS` (default) or `RSASSA-PKCS1-V1_5`. Required for credential matching — the lookup always includes this field. |
| `signature` | Yes      | Logical name for this key configuration (default: `default`). Must match the `--signature` CLI flag. |

## Troubleshooting

### Symptom: "no private key found"

**Cause:** OCM cannot find a matching consumer entry in `.ocmconfig`.

**Fix:** Ensure:

- The file path `private_key_pem_file` is correct and the file exists
- The `algorithm` attribute is present in the identity (e.g. `algorithm: RSASSA-PSS`). See [Consumer Identities Reference]({{< relref "docs/reference/credential-consumer-identities.md" >}}).
- The `signature` name matches what you're using (or is `default` if not specified)
- The file is valid YAML with correct indentation

### Symptom: "permission denied" reading key file

**Cause:** Key file has restrictive permissions.

**Fix:** Ensure your user can read the key file:

```bash
chmod 600 /tmp/keys/private-key.pem
ls -la /tmp/keys/private-key.pem
```

## CLI Reference

| Command                                                                  | Description |
|--------------------------------------------------------------------------|-------------|
| [`ocm sign cv --dry-run`]({{< relref "/docs/reference/ocm-cli/ocm_sign_component-version.md" >}}) | Test signing configuration |
| [`ocm verify cv`]({{< relref "/docs/reference/ocm-cli/ocm_verify_component-version.md" >}})       | Test verification configuration |

## Next Steps

- [How-to: Sign Component Versions]({{< relref "sign-component-version.md" >}}) - Sign components with your configured credentials
- [How-to: Verify Component Versions]({{< relref "verify-component-version.md" >}}) - Verify signatures using public keys

## Related Documentation

- [How-to: Generate Signing Keys]({{< relref "generate-signing-keys.md" >}}) - Create the key pair needed for this configuration
- [Concept: Signing and Verification]({{< relref "signing-and-verification-concept.md" >}}) - Understand how OCM signing works
