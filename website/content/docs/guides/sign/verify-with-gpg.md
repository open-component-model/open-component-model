---
title: "Verify GPG"
description: "Verify a GPG-signed OCM component version using the signer's exported public key or a keyring, and understand what gets checked."
weight: 110
toc: true
aliases:
  - /docs/guides/sign/verify-with-gpg/
---

The signing tutorial verifies right after signing, with the same `.ocmconfig`. This guide is for the **consumer side**: you received a GPG-signed component version and the signer's public key, and you want to confirm authenticity. You need the signer's public key on disk and pointed at by `publicKeyPGPFile` in `.ocmconfig`, or present in your GnuPG keyring. With Sigstore (see [Sign with Sigstore]({{< relref "docs/guides/sign/sign-with-sigstore.md" >}})) you don't install a public key at all, you just declare which identity you trust.

## Prerequisites

- The [`ocm` CLI]({{< relref "docs/getting-started/ocm-cli-installation.md" >}}) installed.
- The GnuPG `gpg` binary (>= 2.2.0) on your `PATH`, only if you verify against your keyring via `keyringFingerprint`. OCM performs all GPG verification in-process; with a public key file no `gpg` is needed.
- The signed component version, reachable in a repository you can read (a local CTF archive or a remote OCI registry).
- The signer's GPG **public** key, either exported to a file (`publicKeyPGPFile`) or already imported into your GnuPG keyring.

If you ran [Sign with GPG]({{< relref "docs/guides/sign/sign-with-gpg.md#prerequisites" >}}) yourself, the setup from that guide already satisfies these.

## Point `.ocmconfig` at the signer's public key

If you signed locally, the same `.ocmconfig` you wrote while signing already works, skip to the verify command.

If you're verifying a signature **someone else** produced, follow [How-To: Configure Signing Credentials → GPG]({{< relref "docs/guides/sign/configure-signing-credentials.md" >}}) using only the signer's public key (`publicKeyPGPFile`); the `privateKeyPGPFile` entry is not needed for verification.

Verification takes the handler from `.ocmconfig`, so put the `type: GPGSigningConfiguration/v1alpha1` under the `verifier` field. RSA is the default when no verifier is configured, so this is what tells `ocm verify` to use GPG:

```yaml
- type: signing.config.ocm.software/v1alpha1
  signer:
    type: GPGSigningConfiguration/v1alpha1
  verifier:
    type: GPGSigningConfiguration/v1alpha1                     # added
```

If you are only verifying, the `signer` field can be left out entirely:

```yaml
- type: signing.config.ocm.software/v1alpha1
  verifier:
    type: GPGSigningConfiguration/v1alpha1
```

If the signer's public key is already in your GnuPG keyring (`$GNUPGHOME`, or `~/.gnupg`), verify against the keyring instead of a key file by setting `keyringFingerprint` in the GPG credentials to the **full** fingerprint (40 or 64 hex characters) of the key you trust. OCM runs `gpg --export` to export the public key, then verifies in-process. A signature by any other key fails. Keys revoked or expired in your keyring are rejected.

Key material in the GPG credentials is rejected with `keyringFingerprint`, and verification needs no passphrase. Replace `publicKeyPGPFile` (or `privateKeyPGPFile`) in the GPG consumer entry of `.ocmconfig` with `keyringFingerprint`:

```yaml
type: generic.config.ocm.software/v1
configurations:
- type: credentials.config.ocm.software
  consumers:
  - identity:
      type: GPG/v1alpha1
      signature: default
    credentials:
    - type: GPGCredentials/v1alpha1
      keyringFingerprint: B118BE3A32BE4AF28E37E881167C7102F8AC81E4
- type: signing.config.ocm.software/v1alpha1
  verifier:
    type: GPGSigningConfiguration/v1alpha1
```

{{< callout context="note" >}}
Give the entry a `signature` field to scope it to a single signature; without one it applies to every signature. Because the verifier is resolved per signature, a component carrying a GPG signature next to an RSA one can be verified in a single run, each with its own handler.
{{< /callout >}}

{{< callout context="note" >}}
`signature: default` applies to a signature _named_ `default`; it is not a catch-all for an omitted `--signature` flag. If the signature was created with `--signature <name>`, set the same value in the consumer identity.
{{< /callout >}}

{{< callout context="caution" >}}
Consumer identities are matched **exactly**. Credentials are looked up under the name of the signature being verified, so a `signature: default` entry does not serve a signature named `prod`, and an entry with no `signature` field at all matches nothing. Give every signature its own consumer entry, otherwise a run without `--signature` fails on the signatures that have no matching credential.
{{< /callout >}}

## Run the verify command

The GPG handler comes from the `verifier` field of the config entry.

```bash
ocm verify cv \
  /tmp/helloworld/transport-archive//github.com/acme.org/helloworld:1.0.0
```

<details>
<summary>Expected output</summary>

```text
time=2026-06-15T12:03:39.929+02:00 level=INFO msg="verifying signature" name=default
time=2026-06-15T12:03:39.930+02:00 level=INFO msg="signature verification completed" name=default duration=894.458µs
time=2026-06-15T12:03:39.930+02:00 level=INFO msg="SIGNATURE VERIFICATION SUCCESSFUL"
```

</details>

The command exits with status code `0` on success.

## Verify a specific signature

If the component carries multiple signatures (e.g. a GPG signature alongside an RSA one), select the one to verify by name:

```bash
ocm verify cv \
  --signature prod \
  /tmp/helloworld/transport-archive//github.com/acme.org/helloworld:1.0.0
```

Without `--signature`, **every** signature on the descriptor is verified. Configuration and credentials are resolved separately for each one, under that signature's own name.

## Troubleshooting

Verification errors (`No public key`, missing key file, fingerprint mismatch, keyring fingerprint requirements) and their fixes are covered in the [Troubleshooting section of Sign with GPG]({{< relref "docs/guides/sign/sign-with-gpg.md#troubleshooting" >}}).
