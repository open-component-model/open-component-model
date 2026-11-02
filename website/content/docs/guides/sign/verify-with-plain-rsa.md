---
title: "Verify Plain RSA"
description: "Verify an RSA-signed OCM component version using the signer's public key: run the verify command and understand the output and exit codes."
weight: 90
toc: true
aliases:
  - /docs/guides/sign/verify-with-plain-rsa/
---

The [Sign with Plain RSA]({{< relref "docs/guides/sign/sign-with-plain-rsa.md" >}}) tutorial verifies right after signing, on the same machine, with the same `.ocmconfig`. This guide is for the **consumer side**: you received a signed component version and the signer's public key, and you want to confirm authenticity before trusting it. You need the signer's public key on disk and pointed at by `.ocmconfig` (see [Configure Signing Credentials]({{< relref "docs/guides/sign/configure-signing-credentials.md" >}})). With Sigstore (see [Sign with Sigstore]({{< relref "docs/guides/sign/sign-with-sigstore.md" >}})) you don't install a public key at all, you just declare which identity you trust.

## Prerequisites

- The [`ocm` CLI]({{< relref "docs/getting-started/ocm-cli-installation.md" >}}) installed.
- The signed component version, reachable in a repository you can read (a local CTF archive or a remote OCI registry).
- The signer's RSA **public** key on disk, pointed at by your `.ocmconfig` (see [Configure Signing Credentials]({{< relref "docs/guides/sign/configure-signing-credentials.md" >}})). The private key is never needed for verification.

If you ran [Sign with Plain RSA]({{< relref "docs/guides/sign/sign-with-plain-rsa.md#prerequisites" >}}) yourself, the setup from that guide already satisfies these.

## Verify the signature

Run the verify command against the signed component:

```bash
ocm verify cv <repository>//<component>:<version>
```

**Local CTF Archive:**

```bash
ocm verify cv /tmp/helloworld/transport-archive//github.com/acme.org/helloworld:1.0.0
```

**Remote OCI Registry:**

```bash
ocm verify cv ghcr.io/<your-namespace>//github.com/acme.org/helloworld:1.0.0
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

## Verify a specific signature

If the component has multiple signatures, specify which one to verify by name:

```bash
ocm verify cv --signature prod ghcr.io/<your-namespace>//github.com/acme.org/helloworld:1.0.0
```

> 👉 Without the `--signature` flag, **every** signature on the descriptor is verified, not just the one named `default`. Each is verified under its own name, so each needs a matching consumer entry.

## List available signatures

View all signatures on a component version:

```bash
ocm get cv /tmp/helloworld/transport-archive//github.com/acme.org/helloworld:1.0.0 -o yaml | grep -A 10 signatures:
```

## RFC 3161 timestamps

If a signature carries an RFC 3161 timestamp, `ocm verify cv` checks it automatically with no extra flag. Plain RSA
signatures carry no certificate, so a timestamp does not extend their validity past any expiry. For the full trust
model and the `TSA/v1alpha1` credential needed to verify timestamp token chains, see [Verify a PEM Certificate
Chain]({{< relref "docs/guides/sign/verify-with-pem-certificate-chain.md#verifying-signatures-with-an-rfc-3161-timestamp" >}})
and [Concept: RFC 3161 Timestamping]({{< relref "docs/concepts/signing-and-verification-concept.md#rfc-3161-timestamping" >}}).

## Troubleshooting

Verification errors (`signature verification failed`, `no public key found`, `invalid key format`) and their fixes are covered in the [Troubleshooting section of Sign with Plain RSA]({{< relref "docs/guides/sign/sign-with-plain-rsa.md#troubleshooting" >}}).
