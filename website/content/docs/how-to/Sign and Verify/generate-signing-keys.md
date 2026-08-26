---
title: "Generate Signing Keys"
description: "Create an RSA or GPG key pair for signing and verifying OCM component versions."
weight: 4
toc: true
---

## Goal

Generate a key pair that can be used to sign and verify OCM component versions. Pick the tab that matches the algorithm you want to use.

## You'll end up with

- A private key file for signing component versions
- A public key file for sharing with consumers who need to verify signatures

**Estimated time:** ~2 minutes

{{< tabs "key-type" >}}
{{< tab "RSA" >}}

<!-- markdownlint-disable-next-line MD024 -->
## Prerequisites

- [OpenSSL](https://openssl-library.org) installed on your system (typically pre-installed on Linux/macOS)

## Generate an RSA key pair

To be able to use the keys across all How-to guides, we'll create them in `/tmp/keys`.
You can choose a different location if you prefer, just make sure to update the file paths in your `.ocmconfig` accordingly.

{{< steps >}}

{{< step >}}

### Generate the private key

Create a folder `/tmp/keys` and create a 4096-bit RSA private key in it:

```bash
mkdir /tmp/keys && cd /tmp/keys
openssl genpkey -algorithm RSA -out private-key.pem -pkeyopt rsa_keygen_bits:4096
```

Verify the private key file was created:

```bash
ls -la /tmp/keys
```

> ⚠️ **Keep your private key secure!** ⚠️  
> Anyone with access to this file can sign components as you.
> Store it in a secure location and never commit it to version control.

{{< /step >}}

{{< step >}}

### Extract the public key

Derive the public key from your private key:

```bash
openssl rsa -in private-key.pem -pubout -out public-key.pem
```

This creates `public-key.pem` which you can safely share with others.

{{< /step >}}

{{< step >}}

### Verify the keys were created

```bash
ls -la *.pem
```

You should see both files:

```text
-rw-------  1 user  group  3272 Jan 15 10:00 private-key.pem
-rw-r--r--  1 user  group   800 Jan 15 10:00 public-key.pem
```

{{< /step >}}

{{< /steps >}}

{{< /tab >}}
{{< tab "GPG" >}}

<!-- markdownlint-disable-next-line MD024 -->
## Prerequisites

- [GnuPG](https://gnupg.org/download/) installed (`gpg` binary available in `$PATH`)

## Generate a GPG key pair

If you already use a GPG key for signing Git tags or release artifacts, that key works as-is for OCM — skip ahead and just export it (Step 2). Otherwise, generate a new one in `/tmp/keys` so the file paths line up with the rest of the how-tos.

{{< steps >}}

{{< step >}}

### Generate the key pair

Create a folder `/tmp/keys` and generate an RSA 4096 GPG key non-interactively (no expiry, no passphrase — fine for tutorials; for production, drop `%no-protection` to be prompted for one):

```bash
mkdir -p /tmp/keys && cd /tmp/keys

gpg --batch --gen-key << 'EOF'
%no-protection
Key-Type: RSA
Key-Length: 4096
Subkey-Type: RSA
Subkey-Length: 4096
Name-Real: OCM Signing Key
Name-Email: ocm-signer@example.com
Expire-Date: 0
%commit
EOF
```

List the key and note its **fingerprint** (the 40-character hex string under `sec`):

```bash
gpg --list-secret-keys --keyid-format=long
```

{{< details "Expected output" >}}

```text
sec   rsa4096/167C7102F8AC81E4 2026-06-15 [SCEAR]
      B118BE3A32BE4AF28E37E881167C7102F8AC81E4
uid           [ ultimate ] OCM Signing Key <ocm-signer@example.com>
ssb   rsa4096/23F18B76957B0A91 2026-06-15 [SEA]
      26C11231549F81AF2AAC4F7723F18B76957B0A91
```

{{< /details >}}

> ⚠️ **Keep your private key secure!** ⚠️  
> Never commit it to version control or share it. For production use, prefer a hardware token (YubiKey, OpenPGP card) or a passphrase-protected key.

{{< /step >}}

{{< step >}}

### Export the keys to ASCII-armored files

OCM loads GPG keys from ASCII-armored files (`.asc`). Export both — replace `<key fingerprint>` with the value from the previous step:

```bash
gpg --export-secret-keys --armor <key fingerprint> > /tmp/keys/signing-key.asc
gpg --export --armor <key fingerprint> > /tmp/keys/verify-key.asc

chmod 600 /tmp/keys/signing-key.asc
```

{{< /step >}}

{{< step >}}

### Verify the keys were created

```bash
ls -la /tmp/keys/*.asc
```

You should see both files:

```text
-rw-------  1 user  group  7487 Jun 15 12:00 signing-key.asc
-rw-r--r--  1 user  group  3988 Jun 15 12:00 verify-key.asc
```

{{< /step >}}

{{< /steps >}}

{{< /tab >}}
{{< /tabs >}}

## Key management tips

| Key             | Who has it                     | Purpose                  |
|-----------------|--------------------------------|--------------------------|
| **Private key** | Only you (the signer)          | Sign component versions  |
| **Public key**  | Anyone who needs to verify     | Verify signatures        |

- Use different key pairs for different environments (dev, staging, production)
- Document which public key corresponds to which signing identity
- Consider key rotation policies for long-lived projects

## Troubleshooting

### Symptom: "command not found: openssl"

**Fix:** Install OpenSSL:

- macOS: `brew install openssl`
- Ubuntu/Debian: `sudo apt-get install openssl`
- RHEL/CentOS: `sudo dnf install openssl`

### Symptom: "command not found: gpg"

**Fix:** Install GnuPG:

- macOS: `brew install gnupg`
- Ubuntu/Debian: `sudo apt-get install gnupg`
- RHEL/CentOS: `sudo dnf install gnupg2`

### Symptom: Permission denied when creating files

**Fix:** Ensure you have write permissions in the current directory, or specify a full path where you have access.

## Next Steps

- [How-to: Configure Signing Credentials]({{< relref "configure-signing-credentials.md" >}}) - Set up OCM to use your keys for signing and verification
- [How-to: Sign a Component Version]({{< relref "sign-component-version.md" >}}) - Use your private key to sign components
- [How-to: Verify a Component Version]({{< relref "verify-component-version.md" >}}) - Share your public key and verify signatures

## Related documentation

- [Concept: Signing and Verification]({{< relref "signing-and-verification-concept.md" >}}) - Understand how OCM signing and verification works
- [Tutorial: Sign Your First Component]({{< relref "docs/tutorials/signing/plain.md" >}}) - A hands-on tutorial for signing components end-to-end
- [Tutorial: GPG Signatures]({{< relref "docs/tutorials/signing/gpg.md" >}}) - End-to-end GPG signing tutorial
