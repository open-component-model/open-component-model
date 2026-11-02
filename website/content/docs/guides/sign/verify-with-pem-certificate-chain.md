---
title: "Verify a PEM Certificate Chain"
description: "Verify a PEM-signed component version by pinning the signer's root CA as the trust anchor and validating the full certificate chain."
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

## Verifying signatures with an RFC 3161 timestamp

{{< callout context="note" title="Early access" >}}
RFC 3161 timestamping is currently being rolled out and we are awaiting feedback. The interface may evolve.
{{< /callout >}}

When a signature carries a timestamp (added at signing time with `--tsa` / `--tsa-url`), `ocm verify cv` checks it
automatically — no extra flag. To fully verify the timestamp, provide the TSA's **root certificates** through the
credential graph with a [`TSA/v1alpha1`]({{< relref "docs/reference/configuration/credential-consumer-identities.md#tsav1alpha1" >}})
consumer entry:

```yaml
type: generic.config.ocm.software/v1
configurations:
  - type: credentials.config.ocm.software
    consumers:
      - identity:
          type: TSA/v1alpha1
          hostname: timestamp.digicert.com
          scheme: http
        credentials:
          - type: TSACredentials/v1alpha1
            rootCertsPEMFile: /path/to/digicert-tsa-root.pem
```

With the root certificates present, the timestamp token's chain is verified at its generation time and — for RSA/PEM
certificate-chain signatures — the signing certificate is validated as of the timestamped time, so such a signature
stays verifiable after its certificate expires. Without the root certificates, verification falls back to
structural-only mode (the token is parsed and its imprint checked, but the chain is not verified): the attested time
is **not** used to relax certificate validity, a warning is logged, and the signature itself still verifies normally.
Errors from the credential graph other than "not found" fail the verification.

Timestamps created by the OCM v1 CLI are verified as well, using the same TSA root configuration. They cover only the
descriptor digest, which is known before signing, so a signature made after its certificate expired could reuse one.
Their time is therefore not used to validate an expired certificate unless you pass
`--insecure-trust-legacy-timestamps`; only do so for component versions you know were signed with OCM v1. See
[Concept: RFC 3161 Timestamping]({{< relref "docs/concepts/signing-and-verification-concept.md#rfc-3161-timestamping" >}})
for the trust model and the difference between these TSA *token* roots and the HTTP client's TLS trust.

## Verification troubleshooting

If verification fails with `certificate signed by unknown authority`, the root CA in your verifier config does not match the root that signed the embedded chain. See [the Troubleshooting section]({{< relref "docs/guides/sign/sign-with-pem-certificate-chain.md#certificate-signed-by-unknown-authority" >}}).
