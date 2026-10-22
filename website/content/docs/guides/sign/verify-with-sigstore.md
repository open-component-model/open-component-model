---
title: "Verify Sigstore (Keyless)"
description: "Verify a keyless Sigstore-signed component version by declaring the OIDC identity you trust."
weight: 120
toc: true
aliases:
  - /docs/guides/sign/verify-with-sigstore-keyless/
---

Once a component version is signed with Sigstore, consumers confirm its authenticity on their own machines. This is for the **consumer side**: you received a Sigstore-signed component version and want to confirm it was signed by an identity you trust. There's no public key to install, you declare the expected identity and OCM checks the signature was made by it.

The flow is the same whether the signature was made interactively (a person logging in via a browser) or in a CI pipeline (a workflow's workload identity). Only the identity you trust differs: a person's email and consumer OIDC issuer (Google, GitHub, Microsoft) for the interactive case, a workflow URL and CI-provider OIDC issuer for the CI case.

## Prerequisites

- The [`ocm` CLI]({{< relref "docs/getting-started/ocm-cli-installation.md" >}}) installed.
- The signed component version, reachable in a repository you can read (a local CTF archive or a remote OCI registry).
- The identity you expect the signature to carry: a person's email and consumer OIDC issuer for an interactive signature, or a workflow URL and CI-provider OIDC issuer for a CI signature. No public key is required.

If you ran [Sign with Sigstore]({{< relref "docs/guides/sign/sign-with-sigstore.md#prerequisites" >}}) yourself, the setup from that guide already satisfies these.

If you've done classical key-based verification, here's what changes:

| Aspect                  | RSA                                                    | Sigstore                                                        |
|-------------------------|--------------------------------------------------------|-----------------------------------------------------------------|
| Before you start        | Obtain the signer's public key, configure `.ocmconfig` | Nothing — declare expected identity in `.ocmconfig`             |
| What proves trust       | Signature decrypts with the public key you have        | Signature ties back to an OIDC identity you've decided to trust |
| What the verifier needs | The signer's public key (rotated and re-distributed)   | The expected OIDC identity and issuer — no long-lived key       |

For the **interactive** case, the verifier config is exactly the `verifier` entry from [Configure the verifier identity]({{< relref "docs/guides/sign/sign-with-sigstore.md#configure-the-verifier-identity" >}}) in the signing guide, matching the signer's email and consumer OIDC issuer. Run the same [`ocm verify cv`]({{< relref "docs/guides/sign/sign-with-sigstore.md#verify-right-after-signing" >}}) command. The rest of this section covers the CI case and reading the identity out of a signature someone else produced.

## Verify a CI (workload identity) signature

CI signatures are made by a workflow's workload identity, not a person. Trust them with a workflow URL and the CI provider's OIDC issuer:

- **`certificateOIDCIssuer`** — which IdP minted the workload-identity token
- **`certificateIdentity`** — the workflow URL the IdP stamps into the cert

For **GitHub Actions** the values are:

| Field                   | Value                                                                          |
| ----------------------- | ------------------------------------------------------------------------------ |
| `certificateOIDCIssuer` | `https://token.actions.githubusercontent.com`                                  |
| `certificateIdentity`   | `https://github.com/<org>/<repo>/.github/workflows/<file>@refs/heads/<branch>` |

Add the entry to the `configurations` list of your `.ocmconfig`:

```yaml
- type: signing.config.ocm.software/v1alpha1
  verifier:
    type: SigstoreVerificationConfiguration/v1alpha1
    certificateOIDCIssuer: https://token.actions.githubusercontent.com
    certificateIdentity: https://github.com/acme/helloworld/.github/workflows/release.yml@refs/heads/main
```

For GitHub Actions you often want to accept a release workflow on **any** ref (any branch, any tag). Use `certificateIdentityRegexp` instead of `certificateIdentity`:

```yaml
- type: signing.config.ocm.software/v1alpha1
  verifier:
    type: SigstoreVerificationConfiguration/v1alpha1
    certificateOIDCIssuer: https://token.actions.githubusercontent.com
    certificateIdentityRegexp: ^https://github\.com/acme/helloworld/\.github/workflows/release\.yml@refs/.*$
```

Exactly one of `certificateIdentity` or `certificateIdentityRegexp` is required (the same applies to the issuer's `*Regexp` form). Then run `ocm verify cv` exactly as in the interactive case.

{{< details "Other CI providers (GitLab, Buildkite, self-hosted, …)" >}}

The mechanism is the same; only the issuer URL and identity shape are provider-specific. Two ways to find them:

- Read your provider's OIDC discovery document at `https://<provider>/.well-known/openid-configuration` — the `issuer` field is what goes into `certificateOIDCIssuer`.
- Decode an existing signature to read the values directly (see the decode recipe below); the SAN identity is in the `URI:` line.

{{< /details >}}

## Read the identity from a received signature

Verifying a signature **someone else** produced? Decode the Fulcio certificate to read the exact identity to paste into your spec. The OIDC issuer is recorded under the Sigstore-defined OID `1.3.6.1.4.1.57264.1.1`; the identity is the `email:` entry in the Subject Alternative Name for interactive logins, or the `URI:` entry for CI/workload signatures. The cert is buried two base64 layers deep:

```bash
REF=ctf::./ctf//github.com/acme.org/helloworld:1.0.0

ocm get cv "$REF" -o yaml \
  | yq '.[].signatures[]? | select(.signature.algorithm == "Sigstore/v1alpha1") | .signature.value' \
  | head -1 | base64 -d | jq -r '.verificationMaterial.certificate.rawBytes' \
  | base64 -d | openssl x509 -inform DER -noout -text \
  | awk '
    /1.3.6.1.4.1.57264.1.1:/ {flag="issuer"; next}
    /^[[:space:]]*X509v3 /    {san=0}
    /Subject Alternative Name/ {san=1}
    san && /^ *email:/ {sub(/^[ \t]*email:/,""); print "certificateIdentity:    " $0; next}
    san && /URI:/      {sub(/.*URI:[ \t]*/,""); print "certificateIdentity:    " $0; next}
    flag=="issuer" {sub(/^[ \t]+/,""); print "certificateOIDCIssuer: " $0; flag=""}
  '
```

Output is two lines you can paste straight under the `verifier` field.

Note: `ocm get cv -o yaml` also shows a `signature.issuer` field on the OCM signature object — don't rely on it for verification. The cert is the authoritative source.

## Verify a specific signature

If the component carries multiple signatures (e.g. an RSA signature and a Sigstore signature), select one by **name** with `--signature`. The name is whatever was set at sign time, `default` if no `--signature` flag was passed *when signing*. Omitting the flag here verifies every signature rather than picking one:

```bash
ocm verify cv \
  --signature default \
  ghcr.io/<your namespace>//github.com/acme.org/helloworld:1.0.0
```

{{< callout context="note" >}}
The verifier's `type` field decides **which algorithm/handler** verifies the signature. The `--signature` flag picks **which named signature** on the component to verify (matched by name, not by algorithm).

You can also scope a config entry itself to one signature by giving it a `signature` field. The verifier is then resolved per signature, so a run without `--signature` verifies every signature, each with the handler its own name resolves to.
{{< /callout >}}

## Troubleshooting

Verification errors (`no matching CertificateIdentity found`, identity mismatches with long CI workflow URLs, missing issuer or identity constraints) and their fixes are covered in the [Troubleshooting section of Sign with Sigstore]({{< relref "docs/guides/sign/sign-with-sigstore.md#troubleshooting" >}}).
