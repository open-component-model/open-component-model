---
title: "Verify a PEM Certificate Chain"
description: "Verify a PEM-signed component version by pinning the signer's root CA as the trust anchor."
weight: 100
toc: true
aliases:
  - /docs/guides/sign/verify-with-pem/
---

The tutorial in [Sign with a PEM Certificate Chain]({{< relref "docs/guides/sign/sign-with-pem-certificate-chain.md" >}}) verifies right after signing, on the same machine. This guide is for the **consumer side**: you received a PEM-signed component version and the signer's root CA, and you want to confirm authenticity before trusting it. You need the root CA on disk and pointed at by `publicKeyPEMFile` in a verifier `.ocmconfig` (the `~/.ocmconfig-pem-verify` file from [Configure `.ocmconfig`]({{< relref "docs/guides/sign/sign-with-pem-certificate-chain.md#configure-ocmconfig" >}})). The verifier trusts the root CA; the leaf certificate and any intermediates travel embedded in the signature, so you never install them yourself.

## Prerequisites

- The [`ocm` CLI]({{< relref "docs/getting-started/ocm-cli-installation.md" >}}) installed.
- The signed component version, reachable in a repository you can read (a local CTF archive or a remote OCI registry).
- The signer's **root CA** certificate on disk, pointed at by `publicKeyPEMFile` in a verifier `.ocmconfig`. The leaf and intermediate certificates travel embedded in the signature, so you never install them yourself.

If you ran [Sign with a PEM Certificate Chain]({{< relref "docs/guides/sign/sign-with-pem-certificate-chain.md#prerequisites" >}}) yourself, the setup from that guide already satisfies these.

## Verify the signature

Run the verify command against the signed component:

```bash
ocm verify cv \
  --config ~/.ocmconfig-pem-verify \
  /tmp/helloworld/transport-archive//github.com/acme.org/helloworld:1.0.0
```

No `verifier` entry is needed in `.ocmconfig` -- OCM infers the PEM encoding from the
`application/x-pem-file` media type stored alongside the signature and selects the correct
handler automatically.

<details>
<summary>Expected output</summary>

```text
time=2026-04-01T10:00:00.000+02:00 level=INFO msg="verifying signature" name=default
time=2026-04-01T10:00:00.001+02:00 level=INFO msg="signature verification completed" name=default duration=1.2ms
time=2026-04-01T10:00:00.001+02:00 level=INFO msg="SIGNATURE VERIFICATION SUCCESSFUL"
```

</details>

The command exits with status code `0` on success.

## Verify a specific signature

If the component carries multiple signatures, select the one to verify by name:

```bash
ocm verify cv \
  --config ~/.ocmconfig-pem-verify \
  --signature prod \
  /tmp/helloworld/transport-archive//github.com/acme.org/helloworld:1.0.0
```

Without `--signature`, **every** signature on the descriptor is verified, each under its own name, so each needs a matching verifier entry.

## Verification troubleshooting

If verification fails with `certificate signed by unknown authority`, the root CA in your verifier config does not match the root that signed the embedded chain. See [the Troubleshooting section]({{< relref "docs/guides/sign/sign-with-pem-certificate-chain.md#certificate-signed-by-unknown-authority" >}}).
