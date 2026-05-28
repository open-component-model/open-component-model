---
title: "GPG Signatures"
description: "Learn to cryptographically sign a component version with a GPG key and verify its authenticity."
icon: "✍️"
weight: 30
toc: true
---

In this tutorial, you'll sign a component version with a GPG private key and verify it with the corresponding public key.
By the end, you'll understand how to use OpenPGP (GPG) signatures with OCM for component authenticity and integrity.

## What You'll Learn

- Create a GPG key pair for signing and verification
- Export ASCII-armored public and private keys
- Configure OCM credentials for GPG signing
- Sign a component version in a CTF archive
- Verify the GPG signature

**Estimated time:** ~15 minutes

## Scenario

You're a software engineer who manages components and uses GPG keys (the same keys used for signing Git commits or releases) to sign OCM component versions.
This lets consumers verify that:

1. **The component is authentic** — it comes from you, not an imposter
2. **The component has integrity** — it hasn't been tampered with since signing

## How It Works

```mermaid
flowchart LR
    subgraph sign ["Sign (You)"]
        direction TB
        A[Component Version] --> C[Sign with GPG Private Key]
        C --> D[Signed Component Version]
    end
    
    D --> T["Share Component"]
    
    T --> verify
    
    subgraph verify ["Verify (Consumer)"]
        direction TB
        E[Signed Component Version] --> H[Verify with GPG Public Key]
        H --> I{Valid?}
        I -->|Yes| VALID["✓ Trusted"]
        I -->|No| INVALID["✗ Rejected"]
    end
    
    style VALID fill:#dcfce7,color:#166534
    style INVALID fill:#fee2e2,color:#991b1b
```

The producer signs the component version with a GPG private key, creating an ASCII-armored OpenPGP detached signature.
Consumers verify using the corresponding public key to confirm authenticity and integrity.

## Prerequisites

- [OCM CLI installed]({{< relref "docs/getting-started/ocm-cli-installation.md" >}})
- [GnuPG installed](https://gnupg.org/download/) (`gpg` binary available in `$PATH`)
- A component version to sign (we'll create one if you don't have one)

## Steps

{{< steps >}}

{{< step >}}

### Create a sample component (if needed)

If you already have a component version in a CTF archive,
e.g. by following our [Create a Component Version]({{< relref "create-component-version.md" >}}) guide, skip to the next step.

```bash
mkdir -p /tmp/ocm-gpg-tutorial && cd /tmp/ocm-gpg-tutorial

cat > component-constructor.yaml << 'EOF'
components:
- name: github.com/acme.org/helloworld
  version: 1.0.0
  provider:
    name: acme.org
EOF

ocm add cv
```

{{< /step >}}

{{< step >}}

### Generate a GPG key pair

Create a dedicated signing key for OCM components:

```bash
mkdir -p /tmp/ocm-gpg-tutorial/keys

# Interactive — answer the prompts (RSA 4096, no expiry for this tutorial)
gpg --full-generate-key

# Or non-interactive batch generation:
gpg --batch --gen-key << 'EOF'
%no-protection
Key-Type: RSA
Key-Length: 4096
Subkey-Type: RSA
Subkey-Length: 4096
Name-Real: OCM Tutorial Key
Name-Email: ocm-tutorial@example.com
Expire-Date: 0
%commit
EOF
```

Verify the key was created:

```bash
gpg --list-secret-keys --keyid-format=long
```

<details>
<summary>Expected output</summary>

```text
sec   rsa4096/ABCDEF1234567890 2026-01-01 [SC]
      AABBCCDDEEFF00112233445566778899AABBCCDD
uid           [ultimate] OCM Tutorial Key <ocm-tutorial@example.com>
ssb   rsa4096/1122334455667788 2026-01-01 [E]
```

Note the key fingerprint (`AABBCCDDEEFF00112233445566778899AABBCCDD`) — you'll need it for pinning.

</details>

{{< callout context="caution" title="Keep your private key secure!" icon="outline/warning">}}
Never commit it to version control or share it.
{{< /callout >}}

{{< /step >}}

{{< step >}}

### Export the keys to files

Export the private and public keys as ASCII-armored files:

```bash
# Export private key (replace FINGERPRINT with your key fingerprint)
gpg --export-secret-keys --armor FINGERPRINT > /tmp/ocm-gpg-tutorial/keys/signing-key.asc

# Export public key
gpg --export --armor FINGERPRINT > /tmp/ocm-gpg-tutorial/keys/verify-key.asc

# Secure the private key file
chmod 600 /tmp/ocm-gpg-tutorial/keys/signing-key.asc
```

Verify both files:

```bash
ls -la /tmp/ocm-gpg-tutorial/keys/
```

{{< /step >}}

{{< step >}}

### Configure signing credentials

Create a `.ocmconfig` that tells OCM where to find your GPG keys.
If you already have a `$HOME/.ocmconfig`, add the consumer block to your existing file.

```bash
cat > /tmp/ocm-gpg-tutorial/.ocmconfig << 'EOF'
type: generic.config.ocm.software/v1
configurations:
  - type: credentials.config.ocm.software
    consumers:
      - identity:
          type: GPG/v1alpha1
          signature: default
        credentials:
          - type: Credentials/v1
            properties:
              privateKeyPGPFile: /tmp/ocm-gpg-tutorial/keys/signing-key.asc
              publicKeyPGPFile: /tmp/ocm-gpg-tutorial/keys/verify-key.asc
EOF
```

> 👉 The `signature: default` name is used when you don't specify `--signature` on the command line.

To pin a specific key when the keyring contains multiple keys, add `keyFingerprint` to a `GPGSigningConfiguration` signer spec:

```yaml
# signer-spec.yaml
type: GPGSigningConfiguration/v1alpha1
keyFingerprint: AABBCCDDEEFF00112233445566778899AABBCCDD
```

Then pass it with `--signer-spec signer-spec.yaml`.

For more details, see [How-to: Configure Signing Credentials]({{< relref "configure-signing-credentials.md" >}}).

{{< /step >}}

{{< step >}}

### Sign the component version

```bash
ocm sign cv \
  /tmp/ocm-gpg-tutorial/transport-archive//github.com/acme.org/helloworld:1.0.0 \
  --config /tmp/ocm-gpg-tutorial/.ocmconfig
```

<details>
<summary>Expected output</summary>

```text
digest:
  hashAlgorithm: SHA-256
  normalisationAlgorithm: jsonNormalisation/v4alpha1
  value: 4e376182b3d535143e8e009b1e467df3a5b0c1f912c71ae432200654c355606f
name: default
signature:
  algorithm: GPG
  mediaType: application/vnd.ocm.signature.gpg
  value: |
    -----BEGIN PGP SIGNATURE-----
    ...
    -----END PGP SIGNATURE-----

signed successfully name=default digest=4e376182b3d535143e8e009b1e467df3a5b0c1f912c71ae432200654c355606f
```

</details>

{{< /step >}}

{{< step >}}

### Verify the signature

```bash
ocm verify cv \
  /tmp/ocm-gpg-tutorial/transport-archive//github.com/acme.org/helloworld:1.0.0 \
  --config /tmp/ocm-gpg-tutorial/.ocmconfig
```

<details>
<summary>Expected output</summary>

```text
SIGNATURE VERIFICATION SUCCESSFUL
```

</details>

> ✅ **Success!** ✅  
> The component version is verified as authentic and unmodified.

{{< /step >}}
{{< /steps >}}

## What You've Learned

Congratulations! You've successfully:

- ✅ Generated a GPG key pair for signing and verification
- ✅ Configured OCM to use your keys via `.ocmconfig`
- ✅ Signed a component version with your GPG private key
- ✅ Verified the signature using the public key

## Best Practices for Production

- **Reuse existing GPG keys** — If you already sign Git tags or release artifacts with a GPG key, the same key works for OCM.
- **Protect private keys** — Use a hardware token (YubiKey, OpenPGP card) or a passphrase-protected key; OCM supports the `passphrase` credential property.
- **Rotate keys periodically** — OCM supports multiple signatures per component version to ease key transitions.
- **Distribute public keys securely** — Publish your public key to a key server (e.g. `keys.openpgp.org`) or share via a trusted channel.
- **Verify before deployment** — Make signature verification a mandatory step in your deployment pipeline.
- **Pin key fingerprints** — Use `keyFingerprint` in a signer spec to prevent accidentally signing or verifying with a different key.

## Check Your Understanding

{{< details "How is GPG signing different from RSA signing in OCM?" >}}
Both sign the component descriptor digest, but they differ in key format and signature encoding:

- **RSA** uses PEM-encoded keys (PKCS#1 / PKCS#8) and produces a raw hex or PEM-wrapped signature.
- **GPG** uses ASCII-armored OpenPGP keyring files and produces an ASCII-armored OpenPGP detached signature.

GPG is a natural fit if you already manage GPG keys for code signing or release workflows.
{{< /details >}}

{{< details "Can I use a passphrase-protected private key?" >}}
Yes. Add the `passphrase` property to your credentials block:

```yaml
credentials:
  - type: Credentials/v1
    properties:
      privateKeyPGPFile: /path/to/signing-key.asc
      publicKeyPGPFile: /path/to/verify-key.asc
      passphrase: my-secret-passphrase
```

OCM decrypts the key in-memory only; the passphrase is never written to disk.
{{< /details >}}

{{< details "Can a component have both RSA and GPG signatures?" >}}
Yes. Each signature has a distinct `name`. Use `--signature <name>` when signing to create named signatures, and OCM will store all of them on the component version.
{{< /details >}}

## Cleanup

```bash
rm -rf /tmp/ocm-gpg-tutorial
```

## Next Steps

- [Tutorial: Plain RSA Signatures]({{< relref "plain.md" >}}) — Sign with raw RSA keys instead of GPG.
- [Tutorial: PEM-encoded Signatures]({{< relref "pem.md" >}}) — Use X.509 certificate chains with RSA for enterprise PKI trust.
- [How-to: Configure Signing Credentials]({{< relref "configure-signing-credentials.md" >}}) — Full reference for credential configuration.

## Related Documentation

- [Concept: Signing and Verification]({{< relref "docs/concepts/signing-and-verification-concept.md" >}}) — Understand the theory behind OCM signing
