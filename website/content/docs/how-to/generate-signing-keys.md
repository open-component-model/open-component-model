---
title: "Generate Signing Keys"
description: "Create an RSA key pair for signing and verifying OCM component versions."
weight: 4
toc: true
---

## Goal

Generate an RSA key pair that can be used to sign and verify OCM component versions.

## You'll end up with

- A private key file for signing component versions
- A public key file for sharing with consumers who need to verify signatures

**Estimated time:** ~2 minutes

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

## Key management tips

| Key | Who has it | Purpose |
|-----|------------|---------|
| **Private key** | Only you (the signer) | Sign component versions |
| **Public key** | Anyone who needs to verify | Verify signatures |

- Use different key pairs for different environments (dev, staging, production)
- Document which public key corresponds to which signing identity
- Consider key rotation policies for long-lived projects

## Troubleshooting

### Symptom: "command not found: openssl"

**Fix:** Install OpenSSL:

- macOS: `brew install openssl`
- Ubuntu/Debian: `sudo apt-get install openssl`
- RHEL/CentOS: `sudo dnf install openssl`

### Symptom: Permission denied when creating files

**Fix:** Ensure you have write permissions in the current directory, or specify a full path where you have access.

## Next Steps

- [How-to: Configure Signing Credentials]({{< relref "configure-signing-credentials.md" >}}) - Set up OCM to use your keys for signing and verification
- [How-to: Sign a Component Version]({{< relref "sign-component-version.md" >}}) - Use your private key to sign components
- [How-to: Verify a Component Version]({{< relref "verify-component-version.md" >}}) - Share your public key and verify signatures

## Related documentation

- [Concept: Signing and Verification]({{< relref "signing-and-verification-concept.md" >}}) - Understand how OCM signing and verification works
- [Tutorial: Sign Your First Component]({{< relref "signing-and-verification.md" >}}) - A hands-on tutorial for signing components end-to-end
