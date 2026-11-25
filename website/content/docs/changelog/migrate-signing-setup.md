---
title: "Migrate Signing Setup"
description: "Move OCM v1 signing and verification to OCM v2: keys in the credential configuration, new defaults, and which v1 signatures still verify."
icon: "arrow-autofit-right"
slug: "migrate-signing-setup"
weight: 90
toc: true
---

## Goal

Move the signing and verification of your component versions from the OCM v1 CLI to OCM v2. Keys move from
command-line flags into the credential configuration, new signatures use different defaults, and some v1 signatures
have to be re-signed.

{{< callout context="caution" >}}
OCM v1 cannot verify signatures created by OCM v2. OCM v2 signs only with `jsonNormalisation/v4alpha1` to
`v4alpha3`, and OCM v1 knows none of them. This holds for every signature algorithm, including
`RSASSA-PKCS1-V1_5`. Upgrade your verifiers first, or keep a v1 signature next to the v2 one while you migrate
(see step 7).
{{< /callout >}}

## Why Migrate?

- OCM v2 has no `--private-key`, `--public-key`, `--algorithm`, `--issuer`, `--ca-cert` or `--keyless` flags. Scripts
  that pass them fail with `unknown flag`.
- OCM v2 ignores the v1 `keys.config.ocm.software` configuration type without a warning.
- New signatures use `RSASSA-PSS` and `jsonNormalisation/v4alpha3`. OCM v1 used `RSASSA-PKCS1-V1_5` and
  `jsonNormalisation/v3`.
- OCM v2 rejects some v1 signatures, see step 6 and
  [Key Differences Between OCM v1 and v2]({{< docref "docs/changelog/differences-v1-v2.md#data-compatibility" >}}).

## Prerequisites

- The [OCM CLI]({{< docref "docs/getting-started/ocm-cli-installation.md" >}}) (OCM v2)
- Your v1 key files and certificates, and the `ocm sign` and `ocm verify` calls of your pipelines
- `openssl`, to create keys and to edit certificate chains

## Steps

### 1. Map the v1 options

| OCM v1 | OCM v2 |
| ------ | ------ |
| `--private-key <file>`, `privateKeys` in `keys.config.ocm.software` | `privateKeyPEMFile` in an `RSACredentials/v1` credential |
| `--public-key <file>`, `publicKeys` | `publicKeyPEMFile` in the same credential |
| `--signature <name>` | `--signature <name>`, plus `signature: <name>` in the consumer identity |
| `--algorithm` | `signatureAlgorithm` of the signer in `signing.config.ocm.software/v1alpha1`, plus `algorithm` in the consumer identity |
| `--ca-cert`, `rootCertificates` (verify) | The root certificate as `publicKeyPEMFile` of the verifier |
| `--public-key <certificate>` (sign) | The chain file as `publicKeyPEMFile` and `signatureEncodingPolicy: PEM` in the signer |
| `--issuer`, `issuers` | No counterpart, see step 5 |
| `--keyless`, `--algorithm sigstore` | The [Sigstore signer]({{< docref "docs/guides/sign/sign-with-sigstore.md" >}}) |
| `-N`, `--normalization` | `--normalisation`. Keep the default `jsonNormalisation/v4alpha3`. |
| `-s`, `--signature` repeated | `--signature` once. Repeating it keeps only the last name. |
| `--latest`, `-c`, `--constraints`, a component without version | No counterpart. Pass one reference with a version. |
| `--repo` | No counterpart. Pass the full reference. |
| `-V`, `--verify` (verify resource digests) | No counterpart. `ocm verify cv` checks the descriptor digest only. Resource digests are checked on download and when a transfer copies the resource. |
| `--verified`, `--update`, `--local` | No counterpart |
| `-H`, `--hash` | `--hash` |
| `--tsa`, `--tsa-url` | Same flags |
| `-R`, `--recursive` | No counterpart. Sign each component version that needs its own signature. |

### 2. Move the keys into the credential configuration

A v1 pipeline passes the keys on every call:

```bash
ocm sign componentversions --signature acme --private-key rsa.priv ./transport-archive//acme.org/helloworld:1.0.0
ocm verify componentversions --signature acme --public-key rsa.pub ./transport-archive//acme.org/helloworld:1.0.0
```

In OCM v2, the keys are credentials. Add a consumer to your `.ocmconfig` with the same signature name:

```yaml
type: generic.config.ocm.software/v1
configurations:
  - type: credentials.config.ocm.software
    consumers:
      - identities:
          - type: RSA/v1alpha1
            algorithm: RSASSA-PSS
            signature: acme
          - type: RSA/v1alpha1
            algorithm: RSASSA-PKCS1-V1_5
            signature: acme
        credentials:
          - type: RSACredentials/v1
            privateKeyPEMFile: /path/to/keys/rsa.priv
            publicKeyPEMFile: /path/to/keys/rsa.pub
```

The `algorithm` of the identity must match the algorithm of the signature. Signatures from OCM v1 use
`RSASSA-PKCS1-V1_5`, new ones `RSASSA-PSS`. With both identities, the key signs and verifies either kind. OCM v2 now
verifies the existing v1 signature and signs the next component version without key flags:

```bash
ocm verify cv --signature acme ./transport-archive//acme.org/helloworld:1.0.0
ocm sign cv --signature acme ./transport-archive//acme.org/helloworld:1.1.0
```

For key files in other places, inline keys and several signature names, see
[Configure Signing Credentials]({{< docref "docs/guides/sign/configure-signing-credentials.md" >}}).

### 3. Choose the signature algorithm

Without a signer configuration, OCM v2 signs with `RSASSA-PSS`. To keep `RSASSA-PKCS1-V1_5`, add a signer:

```yaml
type: generic.config.ocm.software/v1
configurations:
  - type: signing.config.ocm.software/v1alpha1
    signer:
      type: RSASigningConfiguration/v1alpha1
      signatureAlgorithm: RSASSA-PKCS1-V1_5
```

The signature then has `algorithm: RSASSA-PKCS1-V1_5` and `mediaType: application/vnd.ocm.signature.rsa`, as in
OCM v1. Its normalisation algorithm is still a `v4alpha` one, so OCM v1 cannot verify it either. For the normalisation
algorithm, keep the default `jsonNormalisation/v4alpha3`, see
[Migrate Normalisation Algorithms]({{< docref "docs/changelog/migrate-normalisation-algorithms.md" >}}).

### 4. Keep your key pair or create a new one

`ocm create rsakeypair` is gone. Key pairs it created work unchanged in OCM v2: `rsa.priv`
(`BEGIN RSA PRIVATE KEY`) for signing and `rsa.pub` (`BEGIN RSA PUBLIC KEY`) for verification. Private keys created
with `ocm create rsakeypair --encrypt` use an OCM v1 format that OCM v2 cannot read.

To create a new key pair, use `openssl`:

```bash
openssl genpkey -algorithm RSA -out private-key.pem -pkeyopt rsa_keygen_bits:4096
openssl rsa -in private-key.pem -pubout -out public-key.pem
```

OCM v1 accepts these files as well, so you can switch keys before you switch the CLI. For details, see
[Generate Signing Keys]({{< docref "docs/guides/sign/generate-signing-keys.md" >}}).

### 5. Migrate certificate chains

A leaf certificate created with
`ocm create rsakeypair --ca-cert ca.cert --ca-key ca.priv leaf.priv leaf.cert CN=acme-signer` contains the leaf and
the self-signed root. OCM v1 embeds the whole file in the signature, and OCM v2 rejects any self-signed
certificate in a signature. There is no verifier setting to accept these signatures, so re-sign them.

Remove the root from the chain file. This command keeps the first certificate, the leaf:

```bash
openssl x509 -in leaf.cert -out chain.pem
```

If your file also holds intermediate certificates, keep them in `chain.pem` and remove only the root. Then sign with
OCM v2:

```yaml
type: generic.config.ocm.software/v1
configurations:
  - type: credentials.config.ocm.software
    consumers:
      - identity:
          type: RSA/v1alpha1
          algorithm: RSASSA-PSS
          signature: acme-signer
        credentials:
          - type: RSACredentials/v1
            privateKeyPEMFile: /path/to/keys/leaf.priv
            publicKeyPEMFile: /path/to/keys/chain.pem
  - type: signing.config.ocm.software/v1alpha1
    signer:
      type: RSASigningConfiguration/v1alpha1
      signatureEncodingPolicy: PEM
```

```bash
ocm sign cv --signature acme-signer --force ./transport-archive//acme.org/helloworld:1.0.0
```

Verifiers configure the root certificate, `ca.cert`, as `publicKeyPEMFile` for the same signature name. This replaces
v1's `--ca-cert`. See [Verify a PEM Certificate Chain]({{< docref "docs/guides/sign/verify-with-pem-certificate-chain.md" >}}).

OCM v1 also accepts `chain.pem`. Signatures that OCM v1 creates with
`--private-key leaf.priv --public-key chain.pem --ca-cert ca.cert` verify in OCM v1 and OCM v2. In OCM v2 the
verifier's identity needs `algorithm: RSASSA-PKCS1-V1_5`, the v1 default. With `RSASSA-PSS` the verification fails
with an x509 error.

v1's `--issuer` and `issuers` have no counterpart. OCM v2 trusts the root certificate you configure and checks that
the `issuer` recorded in the signature names exactly the subject or the issuer of the leaf certificate. OCM v1 also
accepted an `issuer` that matched only part of the subject
([ocm-project#1418](https://github.com/open-component-model/ocm-project/issues/1418)). Signatures created by the v1 CLI
record the exact subject and are not affected.

### 6. Check your existing v1 signatures

| v1 signature | In OCM v2 |
| ------------ | --------- |
| RSA with `jsonNormalisation/v3`, the v1 default | Verifies, with a warning about the older normalisation algorithm. Exceptions: a signed label with `value: null` ([ocm-project#1402](https://github.com/open-component-model/ocm-project/issues/1402)) or reference labels with `merge` give `digest mismatch`. Re-sign. |
| RSA with `jsonNormalisation/v1` | OCM v2 computes this algorithm as cc-utils did. A v1 CLI signature gives `digest mismatch` if a resource has the type `blob`, `helmChart`, `filesystem` or `ociArtifact`, or `directoryTree/v1`, `npmPackage/v1` or `ociImage/v1`, if the provider has labels, or if a signed label value contains `<`, `>`, `&`, an exponent number or an integer above 2^53 ([#1434](https://github.com/open-component-model/ocm-project/issues/1434)). Re-sign. |
| RSA with `jsonNormalisation/v2` | `unknown normalisation algorithm` ([ocm-project#1403](https://github.com/open-component-model/ocm-project/issues/1403)). Re-sign. |
| Several signatures with different hash or normalisation algorithms on a component version with references | Only the signature created last verifies ([#1436](https://github.com/open-component-model/ocm-project/issues/1436)). Re-sign. |
| Certificate chain with an embedded root | Rejected, see step 5 |
| Sigstore (`sigstore`, `sigstore-v2`) | Rejected. `ocm verify cv` without `--signature` then fails even if another signature is valid, so pass `--signature` for the others. Re-sign with the [Sigstore signer]({{< docref "docs/guides/sign/sign-with-sigstore.md" >}}). |
| With an RFC 3161 timestamp | Verifies. See below. |

OCM v2 checks timestamps created by OCM v1 without extra flags. Without TSA root certificates (`TSA/v1alpha1`
credentials) it checks only their structure. They cover only the descriptor digest, so their time does not prove when
the signature was made, and OCM v2 does not use it to accept an expired signing certificate. With TSA roots
configured, `ocm verify cv --insecure-trust-legacy-timestamps` turns that on. It is insecure: a signature created after the
certificate expired can reuse the timestamp and pass. Use the flag only for component versions you know OCM v1 signed
while the certificate was valid. See
[Verify a PEM Certificate Chain]({{< docref "docs/guides/sign/verify-with-pem-certificate-chain.md#verifying-signatures-with-an-rfc-3161-timestamp" >}}).

### 7. Run OCM v1 and v2 verifiers side by side

While some verifiers still run OCM v1, give the component version two signatures: one created by OCM v1 for the v1
verifiers, one created by OCM v2. Sign with OCM v1 first. OCM v1 cannot update a CTF archive that OCM v2 has written.

```bash
ocm-v1 sign componentversions --signature acme-v1 --private-key rsa.priv ./transport-archive//acme.org/helloworld:1.0.0
ocm sign cv --signature acme ./transport-archive//acme.org/helloworld:1.0.0
```

Here `ocm-v1` is the OCM v1 CLI. Add an identity with `algorithm: RSASSA-PKCS1-V1_5` and `signature: acme-v1` to the
consumer from step 2. OCM v1 verifiers check `--signature acme-v1`. `ocm verify cv` without `--signature` checks both
signatures. Drop the v1 signature once all verifiers run OCM v2.

OCM v1 reads the same `.ocmconfig` and accepts the v2 credentials in it, but it stops at a
`signing.config.ocm.software/v1alpha1` entry. Keep the signer from step 3 or 5 in a separate file and pass it to
OCM v2 only, for example `ocm sign cv --config ~/.ocmconfig --config signer.yaml ...`.

## Troubleshooting

### Symptom: `missing public key, required for plain RSA signatures`

**Cause:** No consumer matches the signature name and algorithm. A v1 signature needs an identity with
`algorithm: RSASSA-PKCS1-V1_5`. Keys in `keys.config.ocm.software` are ignored.

**Fix:** Add a matching identity, see step 2.

### Symptom: `normalization algorithm "jsonNormalisation/v4alpha3" is unknown`

**Cause:** OCM v1 verifies a signature that OCM v2 created.

**Fix:** Upgrade the verifier, or add a v1 signature (step 7).

### Symptom: An existing v1 signature fails in OCM v2

**Cause:** OCM v2 does not accept this kind of v1 signature (step 6). The message tells which kind:

- `self-signed certificate "CN=..." must not be embedded in the signature`: the chain embeds its root.
- `unsupported media type "application/vnd.ocm.signature.sigstore"`: a v1 Sigstore signature.
- `unknown normalisation algorithm: jsonNormalisation/v2`, or `digest mismatch` for `jsonNormalisation/v1`.
- `issuer mismatch: ... matches neither the subject ... nor the issuer ...`: the `issuer` names only part of the
  subject of the leaf certificate.

**Fix:** Re-sign with OCM v2. For certificate chains, remove the root first (step 5). For Sigstore, use the
[Sigstore signer]({{< docref "docs/guides/sign/sign-with-sigstore.md" >}}).

### Symptom: `no signatures found to verify` after a transfer or `ocm add cv`

**Cause:** `ocm transfer cv` and `ocm add cv` replace a component version that already exists, and drop the
signatures stored with it. OCM v1 kept them.

**Fix:** Sign after the last transfer or build, and protect published versions with immutable tags in the registry.

### Symptom: `signing failed: private key not found`

**Cause:** No credential identity matches the signature name and the signer's algorithm, for example an identity
with `algorithm: RSASSA-PKCS1-V1_5` while the signer uses the default `RSASSA-PSS`. A private key encrypted with
`ocm create rsakeypair --encrypt` gives the same error. A missing key file gives `failed loading private key PEM`
instead.

**Fix:** Match `signature` and `algorithm` in the identity to the signer (step 2), or create a new key pair (step 4).

### Symptom: `config type "signing.config.ocm.software/v1alpha1" is unknown`

**Cause:** OCM v1 reads a configuration file that contains a v2 signer.

**Fix:** Move the signer into a separate file for OCM v2 (step 7).

### Symptom: `unable to write file "artifact-index.json": ... permission denied`

**Cause:** OCM v1 writes to a CTF archive that OCM v2 has already written.

**Fix:** Sign with OCM v1 before OCM v2 (step 7).

## Related Documentation

- [Key Differences Between OCM v1 and v2]({{< docref "docs/changelog/differences-v1-v2.md" >}})
- [Migrate Normalisation Algorithms]({{< docref "docs/changelog/migrate-normalisation-algorithms.md" >}})
- [Configure Signing Credentials]({{< docref "docs/guides/sign/configure-signing-credentials.md" >}})
- [Sign with a PEM Certificate Chain]({{< docref "docs/guides/sign/sign-with-pem-certificate-chain.md" >}})
- [Reference: ocm sign component-version]({{< docref "docs/reference/ocm-cli/ocm_sign_component-version.md" >}})
- [Reference: ocm verify component-version]({{< docref "docs/reference/ocm-cli/ocm_verify_component-version.md" >}})
