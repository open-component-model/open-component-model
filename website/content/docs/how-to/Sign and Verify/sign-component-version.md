---
title: "Sign Component Versions"
description: "Cryptographically sign a component version using key-based or keyless signing algorithms."
icon: "🔏"
weight: 6
toc: true
---

## Goal

Sign a component version to certify its authenticity and enable downstream verification. OCM supports multiple signing algorithms.
Pick the tab that matches the algorithm you want to use — each tab is a self-contained walkthrough.

{{< tabs "sign-algorithm" >}}
{{< tab "RSA" >}}

Sign with a long-lived RSA key pair. The private key produces the signature; verifiers use the matching public key to validate it.

Before you can run `ocm sign`, you'll need to generate an RSA key pair and point `.ocmconfig` at it (see prerequisites).
With Sigstore (other tabs) you skip that setup entirely.

## You'll end up with

- A component version with an RSA signature attached

**Estimated time:** ~3 minutes

## Prerequisites

- [OCM CLI installed]({{< relref "ocm-cli-installation.md" >}})
- [Signing credentials configured]({{< relref "configure-signing-credentials.md" >}})
- A component version in a CTF archive or OCI registry (we'll use `github.com/acme.org/helloworld:1.0.0` from the [getting started guide]({{< relref "create-component-version.md" >}})) in this guide, but you can use any component version you have.

## Steps

{{< steps >}}

{{< step >}}

### Sign the component version

Run the sign command against your component:

{{< tabs "sign-rsa-target" >}}
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

## Advanced: Choosing an Encoding Policy

By default, OCM uses **Plain encoding** -- the raw signature bytes are hex-encoded and stored directly. No extra configuration is needed.

To use **PEM encoding** (embeds the signer's X.509 certificate chain in the signature), create a signer spec file and pass it with `--signer-spec`:

```yaml
# pem-signer.yaml
type: RSASigningConfiguration/v1alpha1
signatureAlgorithm: RSASSA-PSS
signatureEncodingPolicy: PEM
```

```bash
ocm sign cv \
  --signer-spec pem-signer.yaml \
  /tmp/helloworld/transport-archive//github.com/acme.org/helloworld:1.0.0
```

PEM encoding requires that `public_key_pem_file` in `.ocmconfig` points to an X.509 certificate chain (leaf + any intermediates), not a bare public key. Verifiers only need the root CA. See [Tutorial: Certificate Chains (PEM)]({{< relref "docs/tutorials/signing/pem.md" >}}) for the full workflow.

## Troubleshooting (RSA)

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

**Fix:** Ensure your `.ocmconfig` file is configured with credentials for the registry.
See [How-To: Configure Credentials for Multiple Registries]({{< relref "configure-multiple-credentials.md" >}}) for details.

{{< /tab >}}
{{< tab "GPG" >}}

Sign with an OpenPGP (GPG) key pair. The same key you already use to sign Git tags or release artifacts works for OCM — there's no separate keyring to maintain.

Unlike RSA (which is the default handler when no signer spec is given), GPG requires an explicit signer spec.
With Sigstore (other tabs) you skip the key-pair setup entirely.

<!-- markdownlint-disable-next-line MD024 -->
## You'll end up with

- A component version with a GPG signature (ASCII-armored OpenPGP detached signature) attached

**Estimated time:** ~3 minutes

<!-- markdownlint-disable-next-line MD024 -->
## Prerequisites

- [OCM CLI installed]({{< relref "ocm-cli-installation.md" >}})
- [Signing credentials configured]({{< relref "configure-signing-credentials.md" >}})
- A component version in a CTF archive or OCI registry (we'll use `github.com/acme.org/helloworld:1.0.0` from the [getting started guide]({{< relref "create-component-version.md" >}}); any component you can write to works)

{{< callout context="note" >}}
For the end-to-end learning walkthrough including key rotation and best practices, see [Tutorial: GPG Signatures]({{< relref "docs/tutorials/signing/gpg.md" >}}).
{{< /callout >}}

<!-- markdownlint-disable-next-line MD024 -->
## Steps

{{< steps >}}

{{< step >}}

### Create the GPG signer spec

Create a `signer-spec.yaml` that selects the GPG signing handler. RSA is the default when no signer spec is given, so this one-line file is what tells `ocm sign` to use GPG:

```yaml
# signer-spec.yaml
type: GPGSigningConfiguration/v1alpha1
```

If your keyring contains multiple keys, pin the one to use by adding `keyFingerprint`:

```yaml
# signer-spec.yaml
type: GPGSigningConfiguration/v1alpha1
keyFingerprint: B118BE3A32BE4AF28E37E881167C7102F8AC81E4
```

Keep this file around — the [verify how-to → GPG tab]({{< relref "verify-component-version.md" >}}) reuses it as a verifier spec.

{{< /step >}}

{{< step >}}

### Sign the component version

Run the sign command with the signer spec.

**Local CTF archive:**

```bash
ocm sign cv \
  --signer-spec ./signer-spec.yaml \
  /tmp/helloworld/transport-archive//github.com/acme.org/helloworld:1.0.0
```

**Remote OCI registry:**

```bash
ocm sign cv \
  --signer-spec ./signer-spec.yaml \
  ghcr.io/<your-namespace>//github.com/acme.org/helloworld:1.0.0
```

{{< details "Expected output from signing" >}}

```text
digest:
  hashAlgorithm: SHA-256
  normalisationAlgorithm: jsonNormalisation/v4alpha1
  value: 4e376182b3d535143e8e009b1e467df3a5b0c1f912c71ae432200654c355606f
name: default
signature:
  algorithm: GPG
  mediaType: application/vnd.ocm.signature.gpg
  value: |-
    -----BEGIN PGP SIGNATURE-----

    wsGpBAABCABdBYJqL83zCRAj8Yt2lXsKkTUUAAAAAAAcABBzYWx0QG5vdGF0aW9u
    ...
    =3vok
    -----END PGP SIGNATURE-----

time=2026-06-15T12:03:31.435+02:00 level=INFO msg="signed successfully" name=default digest=4e376182b3d535143e8e009b1e467df3a5b0c1f912c71ae432200654c355606f hashAlgorithm=SHA-256 normalisationAlgorithm=jsonNormalisation/v4alpha1
```
{{< /details >}}

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
      value: 4e37618...
    signature:
      algorithm: GPG
      mediaType: application/vnd.ocm.signature.gpg
      value: |-
        -----BEGIN PGP SIGNATURE-----
        ...
        -----END PGP SIGNATURE-----
```

{{< /step >}}
{{< /steps >}}

<!-- markdownlint-disable-next-line MD024 -->
## Troubleshooting

### Symptom: `Error: signing failed: private key not found in credentials`

**Cause:** No matching `GPG/v1alpha1` consumer entry in `.ocmconfig` — either the consumer block is missing, the `signature:` name doesn't match `--signature`, or `privateKeyPGPFile` isn't set.

**Fix:** Confirm the consumer block exists and the `signature:` value matches. Without `--signature`, OCM looks for `signature: default`. See [How-To: Configure Signing Credentials → GPG]({{< relref "configure-signing-credentials.md" >}}).

### Symptom: `Error: signing failed: private key not found` (preceded by `no signer spec file provided, using default`)

**Cause:** `--signer-spec` was forgotten. OCM defaulted to RSA, then couldn't find an RSA private key.

**Fix:** Pass `--signer-spec /path/to/signer-spec.yaml` so the GPG handler is selected.

### Symptom: `Error: signature "default" already exists`

**Cause:** The component version already carries a signature with that name.

**Fix:** Pass `--force` to overwrite, or pick a different `--signature <name>` to add a second signature alongside the first.

### Symptom: `Error: reading signer spec ...: no such file or directory`

**Cause:** The path passed to `--signer-spec` is wrong.

**Fix:** Double-check the path is reachable from your current working directory. Absolute paths are easiest.

{{< /tab >}}
{{< tab "Sigstore (interactive)" >}}

Sign with [Sigstore](https://www.sigstore.dev/) by logging in via your browser — no key pair to generate, no public key to distribute. (For CI/CD pipelines without a browser, see the next tab.)

If you've done classical key-based signing, here's what changes:

| Aspect | RSA | Sigstore |
| --- | --- | --- |
| Before you start | Generate key pair, configure `.ocmconfig` with file paths | Nothing — just log in when prompted |
| What proves trust | Possession of the private key | Your OIDC login (e.g. corporate email) |
| What the verifier needs | Your public key, distributed somehow | Your expected identity (email + provider) |

Sigstore also gives you a public, automatic audit trail (Rekor transparency log); RSA gives you none unless you build one.

For the conceptual picture (how Fulcio, Rekor, and OIDC fit together), see [Identity-Based Trust (Sigstore)]({{< relref "docs/concepts/signing-and-verification-concept.md#identity-based-trust-sigstore" >}}).

<!-- markdownlint-disable-next-line MD024 -->
## You'll end up with

- A component version signed with a Sigstore keyless signature, tied to your OIDC identity

**Estimated time:** ~5 minutes

<!-- markdownlint-disable-next-line MD024 -->
## Prerequisites

- [OCM CLI installed]({{< relref "ocm-cli-installation.md" >}})
- A browser on the same machine (signing opens a browser window to log you in)
- An OIDC identity with a provider supported by your Sigstore stack — on public Sigstore that's Google, GitHub, or Microsoft
- Network access to `*.sigstore.dev` (in particular `fulcio.sigstore.dev`, `oauth2.sigstore.dev`, `rekor.sigstore.dev`, `tuf-repo-cdn.sigstore.dev`) — corporate networks often block these
- If `cosign` isn't on your PATH or its version is too low, OCM downloads and caches it under `~/.cache/ocm/cosign/...` automatically; subsequent runs skip the download
- A component version in a CTF archive or OCI registry (we'll use `github.com/acme.org/helloworld:1.0.0` from the [getting started guide]({{< relref "create-component-version.md" >}}); any component you can write to works)

{{< callout context="note" >}}
<!-- TODO(#2588): once the Sigstore tutorial lands on main, restore the relref to docs/tutorials/signing/sigstore.md around "Tutorial: Sigstore (Keyless)" -->
For the end-to-end walkthrough including how Fulcio certificates, Rekor transparency log, and the OIDC token flow fit together, see Tutorial: Sigstore (Keyless). Design background lives in [ADR 0017: Sigstore Integration](https://github.com/open-component-model/open-component-model/blob/main/docs/adr/0017_sigstore_integration.md).
{{< /callout >}}

<!-- markdownlint-disable-next-line MD024 -->
## Steps

{{< steps >}}

{{< step >}}

### Tell OCM to use Sigstore for signing

Add this entry to your `.ocmconfig`. It says "for the signature named `default`, get an OIDC token instead of using a private key":

```yaml
type: generic.config.ocm.software/v1
configurations:
- type: credentials.config.ocm.software
  consumers:
  - identity:
      type: SigstoreSigner/v1alpha1
      signature: default
    credentials:
    - type: OIDCIdentityTokenProvider/v1alpha1
```

That's all the credential configuration you need. No keys, no paths.

{{< callout context="note" >}}
`signature: default` matches the default name `ocm sign` uses. Add more entries with different `signature` names if you want multiple Sigstore signing identities.
{{< /callout >}}

{{< /step >}}

{{< step >}}

### Create a minimal signer spec

Until Sigstore is the default signing handler, create `sigstore-sign.yaml` with one line — this picks the Sigstore signing handler and uses public Sigstore defaults:

```yaml
# sigstore-sign.yaml
type: SigstoreSigningConfiguration/v1alpha1
```

That's it. No URLs, no keys, no certificates. The handler uses public Sigstore (`fulcio.sigstore.dev`, `rekor.sigstore.dev`) automatically.

{{< /step >}}

{{< step >}}

### Sign the component version

Run the sign command with the signer spec:

{{< tabs "sign-sigstore-target" >}}
{{< tab "Local CTF Archive" >}}

```bash
ocm sign cv \
  --signer-spec ./sigstore-sign.yaml \
  /tmp/helloworld/transport-archive//github.com/acme.org/helloworld:1.0.0
```
{{< /tab >}}
{{< tab "Remote OCI Registry" >}}

```bash
ocm sign cv \
  --signer-spec ./sigstore-sign.yaml \
  ghcr.io/<your-namespace>//github.com/acme.org/helloworld:1.0.0
```
{{< /tab >}}
{{< /tabs >}}

A browser window opens against your OIDC provider's login page. Authenticate, and you'll see the OCM "Signing identity verified!" page. Return to the terminal — signing continues automatically.

That's the whole signing flow. No private key was generated, none was loaded from disk, and nothing needs to be distributed to verifiers. Your OIDC identity is what the verifier will check.

{{< details "Expected output from signing" >}}

```text
digest:
  hashAlgorithm: SHA-256
  normalisationAlgorithm: jsonNormalisation/v4alpha1
  value: 91dd197868907487e62872695db1fa7b397fde300bcbae23e24abc188fb147ad
name: default
signature:
  algorithm: sigstore
  mediaType: application/vnd.dev.sigstore.bundle.v0.3+json
  value: ...

time=2026-05-19T17:43:28.524+02:00 level=INFO msg="signed successfully" name=default digest=91dd197868907487e62872695db1fa7b397fde300bcbae23e24abc188fb147ad hashAlgorithm=SHA-256 normalisationAlgorithm=jsonNormalisation/v4alpha1
```
{{< /details >}}

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
      algorithm: sigstore
      mediaType: application/vnd.dev.sigstore.bundle.v0.3+json
      value: <base64-bundle>
```

The `value` field contains the full Sigstore bundle (signature + Fulcio certificate + Rekor inclusion proof).

{{< /step >}}
{{< /steps >}}

<!-- markdownlint-disable-next-line MD024 -->
## Troubleshooting

### Symptom: "browser did not open" or "timed out waiting for authentication callback"

**Cause:** The OIDC flow needs a browser on the machine running `ocm sign`, plus a free loopback port (`127.0.0.1`) for the OAuth callback.

**Fix:** Run on a workstation with a graphical browser. Headless / CI environments need a different identity flow (e.g. workload identity tokens supplied via the credentials config) — the interactive flow is not designed for unattended use.

### Symptom: "OIDC provider does not support PKCE S256"

**Cause:** The OIDC provider you're using doesn't support a security feature the OCM CLI requires for browser-based login.

**Fix:** Use a provider that supports it — public Sigstore (Google/GitHub/Microsoft via `sigstore.dev`) does, and most modern enterprise IdPs do too. If you're pointing at a custom enterprise provider, check with your platform team.

### Symptom: "issuer mismatch in callback"

**Cause:** Your OIDC provider returned a different issuer URL than the one configured.

**Fix:** Make sure the `issuer` in `.ocmconfig` matches your provider's canonical issuer URL exactly (scheme, host, path — trailing slashes matter). If you're using public Sigstore defaults, you don't need to set `issuer` at all.

### Symptom: Permission denied on registry

**Cause:** Missing write access to the OCI registry.

**Fix:** Configure registry credentials in `.ocmconfig`. See [How-To: Configure Credentials for Multiple Registries]({{< relref "configure-multiple-credentials.md" >}}).

{{< /tab >}}
{{< tab "Sigstore (CI)" >}}

Sign with [Sigstore](https://www.sigstore.dev/) from a CI/CD pipeline — no browser needed. Your CI runner's workload identity becomes the signing identity; the resulting signature, certificate, and Rekor entry are produced exactly as in the interactive case. Verifiers don't need to know it came from CI.

This walk-through uses **GitHub Actions** as the CI provider; other providers work the same way (see the box at the end).

<!-- markdownlint-disable-next-line MD024 -->
## You'll end up with

- A component version signed by your GitHub Actions workflow's identity, ready to be verified by anyone who trusts that workflow

**Estimated time:** ~5 minutes (excludes existing CI workflow plumbing)

<!-- markdownlint-disable-next-line MD024 -->
## Prerequisites

- The OCM CLI available in your CI image — see [How-To: Install the OCM CLI]({{< relref "ocm-cli-installation.md" >}}) or use the [pre-built OCM CLI container image]({{< relref "container-image-usage.md" >}}) directly as your job's runtime
- A GitHub Actions workflow with permission to mint OIDC tokens (one line in the workflow file, shown below)
- A component version reachable from the runner — either built in the same job, restored from a job artefact, or pushed to an OCI registry the runner can read/write

<!-- markdownlint-disable-next-line MD024 -->
## Steps

{{< steps >}}

{{< step >}}

### Grant the workflow permission to mint OIDC tokens

GitHub Actions only emits a workload-identity token when the workflow declares `id-token: write`. Add it at workflow level (or per-job):

```yaml
permissions:
  contents: read
  id-token: write   # required for Sigstore signing
```

Without this, the next step fails with `unable to mint OIDC token`.

{{< /step >}}

{{< step >}}

### Use the same `.ocmconfig` and signer spec as the interactive flow

Nothing changes here — the credential consumer and signer spec from the interactive tab work as-is in CI:

```yaml
# .ocmconfig (excerpt)
type: generic.config.ocm.software/v1
configurations:
- type: credentials.config.ocm.software
  consumers:
  - identity:
      type: SigstoreSigner/v1alpha1
      signature: default
    credentials:
    - type: OIDCIdentityTokenProvider/v1alpha1
```

```yaml
# sigstore-sign.yaml
type: SigstoreSigningConfiguration/v1alpha1
```

OCM auto-detects GitHub Actions' `ACTIONS_ID_TOKEN_REQUEST_TOKEN` environment variable and uses it instead of opening a browser. No code change between local interactive runs and CI runs.

{{< /step >}}

{{< step >}}

### Sign the component version in a workflow step

Drop a sign step into your workflow:

```yaml
jobs:
  sign:
    runs-on: ubuntu-latest
    permissions:
      contents: read
      id-token: write
    steps:
      - uses: actions/checkout@v4
      - name: Install OCM CLI
        run: |
          curl -sfL https://ocm.software/install-cli.sh | bash
      - name: Sign component version
        run: |
          ocm sign cv \
            --signer-spec ./sigstore-sign.yaml \
            ghcr.io/${{ github.repository_owner }}//github.com/acme.org/helloworld:1.0.0
```

A successful run logs `signed successfully` and embeds the Sigstore bundle into the component descriptor — same shape as in the interactive tab.

{{< details "Alternative: run `ocm` from the OCM CLI container image" >}}

Skip the install step by invoking `ocm` from the official container image (`ghcr.io/open-component-model/cli`) directly with `docker run`. The image is built `FROM scratch` for minimal attack surface — only the `ocm` binary plus CA certs, no shell — so it cannot be used as a GitHub Actions [`container:`]({{< relref "container-image-usage.md" >}}) job runtime. The `docker run` pattern below is the supported way:

```yaml
jobs:
  sign:
    runs-on: ubuntu-latest
    permissions:
      contents: read
      id-token: write
    steps:
      - uses: actions/checkout@v4
      - name: Sign component version
        run: |
          docker run --rm \
            -v "$PWD":/work -w /work \
            -e ACTIONS_ID_TOKEN_REQUEST_TOKEN \
            -e ACTIONS_ID_TOKEN_REQUEST_URL \
            ghcr.io/open-component-model/cli:0.6.0 \
            sign cv --signer-spec ./sigstore-sign.yaml \
              ghcr.io/${{ github.repository_owner }}//github.com/acme.org/helloworld:1.0.0
```

The OIDC environment variables are forwarded explicitly so OCM can mint a workload-identity token from inside the container. Pin a specific tag (e.g. `:0.x.y`) instead of `:latest` for reproducible builds. See [How-to: Use the OCM CLI container image]({{< relref "container-image-usage.md" >}}) for full image documentation.

The OCM release workflow publishes a [GitHub artifact attestation](https://docs.github.com/en/actions/security-for-github-actions/using-artifact-attestations) for each CLI image. The attestation is a Sigstore bundle under the hood — same Fulcio cert and Rekor log entry mechanics as the component-version signing this how-to is about; only the publication path is different (GitHub's attestation API instead of the OCM signature itself). Verify the image before pulling:

```bash
gh attestation verify oci://ghcr.io/open-component-model/cli:0.6.0 \
  --owner open-component-model
```

{{< /details >}}

{{< /step >}}

{{< step >}}

### Verify the signature was added

Identical to the interactive flow:

```bash
ocm get cv ghcr.io/<your-namespace>//github.com/acme.org/helloworld:1.0.0 -o yaml
```

The `signatures` section carries `algorithm: sigstore` and the bundle in the `value` field. The bundle embeds the Fulcio cert with the workflow's identity (`https://github.com/<org>/<repo>/.github/workflows/<file>@refs/heads/<branch>`) and the GitHub Actions OIDC issuer — those are what the verify how-to uses to check the signature.

{{< /step >}}

{{< /steps >}}

{{< callout context="caution" title="Mind the OIDC token's lifetime" >}}
GitHub Actions OIDC tokens expire quickly — often within minutes. The `ocm sign` step must complete before the token expires. For long pipelines, request the token (i.e. run the sign step) just before you need it, not at the start of the workflow.
{{< /callout >}}

{{< details "Other CI providers (GitLab, Buildkite, self-hosted, …)" >}}

The mechanism is the same; only how the runner exposes the OIDC token differs. Two patterns:

1. **The runner exports a known env var** that OCM auto-detects (like `ACTIONS_ID_TOKEN_REQUEST_TOKEN` for GitHub Actions). Check your provider's docs.
2. **The runner gives you a token via an API or file** — set `SIGSTORE_ID_TOKEN` from it before calling `ocm sign cv`:

   ```bash
   SIGSTORE_ID_TOKEN=$(your-runner-fetches-OIDC-token) \
     ocm sign cv --signer-spec ./sigstore-sign.yaml ...
   ```

OCM checks `SIGSTORE_ID_TOKEN` first, then `ACTIONS_ID_TOKEN_REQUEST_TOKEN`, then falls back to the credential provider configured in `.ocmconfig`. Whichever route the token takes, the signature, certificate, and Rekor entry are identical.

{{< /details >}}

<!-- markdownlint-disable-next-line MD024 -->
## Troubleshooting

### Symptom: "unable to mint OIDC token" in GitHub Actions

**Cause:** The workflow (or job) is missing `permissions: id-token: write`.

**Fix:** Add it as shown in Step 1. Workflow-level scope works for all jobs; per-job scope works for that job only.

### Symptom: "Fulcio returned 400: error processing the identity token"

**Cause:** The OIDC token expired between minting and the actual sign call. Common in long pipelines that fetch the token early.

**Fix:** Move the sign step closer to where the token is minted. If you absolutely need an earlier token, your CI may support refreshing it explicitly.

### Symptom: OCM still asks for browser auth in CI

**Cause:** Neither `SIGSTORE_ID_TOKEN` nor `ACTIONS_ID_TOKEN_REQUEST_TOKEN` is set in the env that OCM sees. Often a step-scoping or shell-quoting issue.

**Fix:** From the same step, run `env | grep -E 'SIGSTORE_ID_TOKEN|ACTIONS_ID_TOKEN_REQUEST_TOKEN'` right before `ocm sign cv` to confirm what's actually exported. For GitHub Actions, also confirm `permissions: id-token: write` is in scope.

{{< /tab >}}
{{< /tabs >}}

## Next Steps

- [How-to: Verify a Component Version]({{< relref "verify-component-version.md" >}}) — Verify signatures (RSA or Sigstore)

## Related Documentation

- [Concept: Signing and Verification]({{< relref "signing-and-verification-concept.md" >}}) — Understand how OCM signing works
- [Tutorial: Sign and Verify Components]({{< relref "docs/tutorials/signing/plain.md" >}}) — End-to-end signing workflow
- [ADR 0017: Sigstore Integration](https://github.com/open-component-model/open-component-model/blob/main/docs/adr/0017_sigstore_integration.md) — Sigstore design and OIDC flow details
