---
title: "FIPS 140-3"
description: "Reference for FIPS 140-3 support in the OCM CLI and OCM controller: support policy, Go FIPS module, runtime modes, artifacts, and known limitations."
weight: 1
toc: true
---

This page describes how the OCM CLI and the OCM controller support FIPS 140-3.
Since OCM 0.20.0, the binaries and container images are built in FIPS mode. OCM
does not ship a separate FIPS variant: the regular binaries and images are built
for FIPS, so the same artifact runs in both normal and FIPS-restricted
environments.

## Support Policy

**What OCM provides.** OCM binaries and images are built against a frozen
version of the Go Cryptographic Module and run in FIPS 140-3 mode by default.
OCM is not itself a cryptographic module and is not FIPS certified or
validated; FIPS 140-3 validation applies to the cryptographic module it uses.
See [Validation Status](#validation-status) for that module's current CMVP
status.

**What you are responsible for.** Whether a deployment meets your regulatory
requirements depends on more than OCM: the node operating system and kernel,
the external binaries OCM runs (`cosign`), your configuration, and your
compliance regime. Check these with your compliance owner. OCM does not give
compliance guarantees.

**Default and opt-out.** FIPS mode is on by default (`fips140=on`), so there is
no separate FIPS build. Outside regulated environments you can turn it off with
`GODEBUG=fips140=off`. `GODEBUG=fips140=only` additionally rejects external
binaries that run outside the FIPS boundary. It is meant for testing and
assessment, not for production, see [Runtime Modes](#runtime-modes).

**What changes in FIPS mode.** Besides the cryptography itself running in the
Go Cryptographic Module, FIPS mode changes OCM's behavior in these places:

| Area | `fips140=on` (default) | `fips140=only` | Details |
| --- | --- | --- | --- |
| TLS connections | Only FIPS-approved TLS versions, cipher suites and key exchanges | Same | [Effects of FIPS Mode](#effects-of-fips-mode) |
| Signing and verification | Resource and component reference digests must use SHA-256 or SHA-512 | Same | [Digest Algorithms](#digest-algorithms) |
| Sigstore signing | A `cosign` that is not a FIPS build, or a downloaded one, is used and logged at debug level | Rejected; `cosign` must be on `PATH` and a FIPS build | [Sigstore and cosign](#sigstore-and-cosign) |
| GPG signing | GPG keys other than unprotected v6 RSA or ECDSA P keys are used and logged at debug level | Rejected; see the four strict-mode errors | [GPG](#gpg) |

**Module version.** OCM builds with `GOFIPS140=certified`, set once in the
repository's root `.env`. `certified` selects the newest Go Cryptographic Module
version that has a CMVP validation certificate, as recorded by the Go
toolchain the release is built with. When a newer module version is certified,
OCM releases pick it up with the next Go toolchain update; no pin needs to
change. The resolved version is part of every binary's build information, see
[Verifying a Binary](#verifying-a-binary).

**Out of scope.** Code outside the Go Cryptographic Module, such as
`golang.org/x/crypto`. See [Known Limitations](#known-limitations).

**Reporting gaps.** Report FIPS-related problems or gaps as
[GitHub issues](https://github.com/open-component-model/open-component-model/issues).
Open work on GPG and cosign is tracked in
[ocm-project#1327](https://github.com/open-component-model/ocm-project/issues/1327).

## Cryptographic Module

OCM uses the [Go Cryptographic Module](https://go.dev/doc/security/fips140),
which is part of the Go standard library. The OCM CLI and the OCM controller
are built against a frozen version of the module instead of the module version
that ships with the current Go toolchain.

| Setting | Value |
| --- | --- |
| Build setting | `GOFIPS140=certified` |
| Module version in the binary | `v1.0.0` (build information: `GOFIPS140=v1.0.0-c2097c7c`) |
| Default runtime setting | `GODEBUG=fips140=on` |

Setting `GOFIPS140` at build time does two things:

- It compiles the binary against the frozen module source.
- It sets the default `GODEBUG` value to `fips140=on`, so FIPS mode is active
  without any runtime configuration.

### Validation Status

- **Go Cryptographic Module v1.0.0**, used by current OCM releases, is validated:
  [CMVP Certificate #5247](https://csrc.nist.gov/projects/cryptographic-module-validation-program/certificate/5247).
  Its [Security Policy](https://csrc.nist.gov/CSRC/media/projects/cryptographic-module-validation-program/documents/security-policies/140sp5247.pdf)
  lists the tested operating environments, and section 11.1 names the build
  information that marks a correctly configured binary:
  `build GOFIPS140=v1.0.0-c2097c7c`.
- **Go Cryptographic Module v1.26.0** is on the
  [CMVP Modules In Process List](https://csrc.nist.gov/Projects/cryptographic-module-validation-program/modules-in-process/modules-in-process-list)
  (status as of 2026-10-02: *Comment Resolution - CMVP*). OCM switches to it
  automatically through `GOFIPS140=certified` once it is certified and a Go
  release marks it as such.

v1.0.0 was frozen from Go 1.24. Standard library features added to the module
later, such as `crypto/mldsa`, are not available in OCM until the module
version changes.

## Artifacts

| Artifact | Build | Image contents |
| --- | --- | --- |
| `ocm` CLI binaries (all OS/architectures) | `GOFIPS140=certified`, `CGO_ENABLED=0` | — |
| OCM CLI image (`cli:<version>`, default) | `GOFIPS140=certified`, `CGO_ENABLED=0` | `scratch` with `ocm`, a FIPS build of `cosign`, and the CA bundle |
| OCM CLI slim image (`cli:<version>-slim`) | `GOFIPS140=certified`, `CGO_ENABLED=0` | `scratch` with `ocm` and the CA bundle |
| OCM controller image | `GOFIPS140=certified`, `CGO_ENABLED=0` | `scratch` with `manager`, a FIPS build of `cosign`, and the CA bundle |

The default CLI image is built `FROM scratch`. It contains the static `/ocm`
binary (the entrypoint), `cosign` at `/usr/local/bin/cosign` built with the
same `GOFIPS140` value as OCM, and a CA bundle at
`/etc/ssl/certs/ca-certificates.crt`. No GnuPG is included: OCM performs all
GPG cryptography in-process with the Go OpenPGP library. Sigstore signing
works in the image because `cosign` is on `PATH`. The slim image is built
`FROM scratch` and contains only `/ocm` and the CA bundle; use it when you
don't sign with Sigstore, or bring your own `cosign`.
The controller image is built `FROM scratch` with the static `/manager` binary
(the entrypoint), the FIPS `cosign`, the CA bundle, and a world-writable `/tmp`.
No GnuPG is included.

The images have no shell and no package manager. See
[Sigstore and cosign](#sigstore-and-cosign) and [GPG](#gpg) for how OCM uses
`cosign` and handles GPG signing.

The binaries are statically linked and do not use any system cryptographic
library. All cryptography in OCM itself goes through the Go Cryptographic
Module.

## Runtime Modes

You control the runtime mode with the `GODEBUG` environment variable.

| `GODEBUG` | Behavior |
| --- | --- |
| `fips140=on` (default) | FIPS mode is active. Approved algorithms run in their FIPS-compliant form, and non-approved algorithms such as MD5 and SHA-1 stay available. |
| `fips140=only` | Like `on`, but non-approved algorithms return an error or panic. OCM also rejects GPG keys that are not unprotected v6 RSA or ECDSA P keys, a `cosign` that runs outside the FIPS boundary, Helm chart provenance verification (see [Known Limitations](#known-limitations)), and resource or reference digests other than SHA-256/SHA-512 (see [Digest Algorithms](#digest-algorithms)). Go documents this as a best-effort mode for testing and assessment, not for production. |
| `fips140=off` | FIPS mode is disabled. |

OCM runs in `fips140=on` mode by default, which allows non-approved algorithms
instead of rejecting them. Parts of the dependency tree use non-approved
algorithms for non-security purposes, such as content addressing and legacy
digests. The Security Policy of the Go Cryptographic Module does not require
`fips140=only`: the module enters and leaves its approved mode per service and
reports the state through a service indicator (Security Policy sections 2.4
and 4.4), and Go
[documents](https://go.dev/doc/security/fips140#the-fips140-godebug-option)
`only` as "not intended to be used in production".

With `fips140=only`, OCM rejects GPG keys outside the strict-mode set and
refuses to run a non-FIPS `cosign`. Go's own enforcement, however, applies to every Go program OCM starts,
including programs OCM does not control. For example, the Docker Desktop
credential helper (`docker-credential-desktop`) panics on an internal MD5 use, so
OCM cannot resolve registry credentials. Use `fips140=only` where all such
programs are known to work, or as a one-off audit to find non-approved
algorithms in a workload's call path.

Two OCM features use non-approved hashes by design and run them outside strict
enforcement (`crypto/fips140.WithoutEnforcement`), so they keep working with
`fips140=only`. FIPS mode itself stays on, so TLS and SSH still negotiate
approved algorithms only:

- **Git** identifies objects by SHA-1. Cloning, fetching and archiving a
  repository runs outside strict enforcement; the archive OCM records is
  digested with SHA-256.
- **wget checksum verification** accepts MD5 and SHA-1 checksums that a server
  publishes. Only these hashes run outside strict enforcement; the digest OCM
  records and signs is always SHA-256.

Test packages named `fips140` (`bindings/go/cli/cmd/fips140`,
`bindings/go/gpg/fips140`, `bindings/go/git/fips140`,
`bindings/go/helm/fips140`, `bindings/go/wget/fips140`) set
`//go:debug fips140=only` and run in every unit test run, so CI exercises
signing, verification, Git downloads, the Helm provenance rejection and
legacy checksum verification in strict mode. All other tests run in the
default `fips140=on` mode.

### Effects of FIPS Mode

In FIPS mode, the Go Cryptographic Module:

- Runs an integrity self-check and known-answer self-tests at startup or on
  first use of an algorithm.
- Runs pairwise consistency tests on generated keys, which can make key
  generation up to 2x slower.
- Implements `crypto/rand` with a NIST SP 800-90A DRBG.
- Restricts `crypto/tls` to FIPS-approved protocol versions, cipher suites,
  signature algorithms, and key exchanges: TLS 1.2 and 1.3 only, AES-GCM (and
  ECDHE AES-CBC-SHA256 on TLS 1.2) cipher suites, no plain X25519 key exchange,
  and RSA certificates of at least 2048 bits. A registry or HTTP endpoint that
  offers only non-approved options fails the TLS handshake.
- Draws entropy for its DRBG from the operating system. Module v1.0.0 uses the
  kernel (`getrandom`) as a passive entropy source outside the module boundary,
  so the quality of OCM's random numbers rests on the operating system. Run OCM
  on an operating system with a FIPS-compliant entropy source, for example a
  distribution with FIPS-validated kernel crypto. Module v1.26.0 adds a CPU
  jitter entropy source with ESV certificate
  [#E318](https://go.dev/doc/security/fips140); OCM moves to it once that module
  is certified.

FIPS mode does not make TLS compliant with
[CNSA 1.0 or CNSA 2.0](https://media.defense.gov/2025/May/30/2003728741/-1/-1/0/CSA_CNSA_2.0_ALGORITHMS.PDF),
which National Security Systems and DoD IL5 require: CNSA asks for AES-256,
P-384 or `ML-KEM-1024`, and SHA-384, while Go also offers AES-128 and P-256, and
does not let applications restrict TLS 1.3 cipher suites. OCM does not
configure CNSA TLS. Deployments that need it have to build OCM with a
toolchain that enforces it, such as the `go-fips` toolchain from Chainguard.

To set the mode explicitly for the CLI:

```shell
GODEBUG=fips140=on ocm version
```

For the controller, set the variable in the Deployment:

```yaml
env:
  - name: GODEBUG
    value: fips140=on
```

## Verifying a Binary

The OCM CLI prints the Go build settings embedded in it, including the FIPS
ones:

```shell
ocm version -o gobuildinfo | grep -E 'GOFIPS140|DefaultGODEBUG'
```

Expected output:

```text
build DefaultGODEBUG=fips140=on
build GOFIPS140=v1.0.0-c2097c7c
```

`-o gobuildinfojson` prints the same information as JSON. For the CLI image,
run `docker run --rm ghcr.io/open-component-model/cli:<version> version -o gobuildinfo`.

For any Go binary, including the controller's `/manager`, `go version -m`
shows the same settings (plain `go version` prints only the Go version):

```shell
go version -m ./manager | grep -E 'GOFIPS140|DefaultGODEBUG'
```

For a container image, copy the binary out first.

### Startup Log

Both binaries log the FIPS state at startup, as reported by
`crypto/fips140.Enabled()` and `crypto/fips140.Version()`.

The controller logs it at `info` level on every start:

```text
INFO setup FIPS 140-3 mode {"enabled": true, "module": "v1.0.0"}
```

The CLI logs it at `debug` level for every command, so it does not add noise to
regular output. Pass `--loglevel debug` to see it:

```shell
ocm --loglevel debug get cv <reference>
```

```text
level=DEBUG msg="FIPS 140-3 mode" enabled=true module=v1.0.0
```

`enabled=false` means the binary was built without `GOFIPS140` or was started
with `GODEBUG=fips140=off`.

## Digest Algorithms

A component signature covers the normalized component descriptor, which is
hashed with SHA-256 or SHA-512. Resources and component references are
covered only through the digests recorded in the descriptor, so those digests
are security-relevant: with a weak hash such as MD5 or SHA-1, content could be
swapped under a valid signature.

With `GODEBUG=fips140=only`, OCM therefore requires every resource and
component reference digest to use SHA-256 or SHA-512. In the default
`fips140=on` mode and outside FIPS mode, it logs a warning instead, so component
versions with existing MD5 or SHA-1 digests keep working:

| Operation | `fips140=only` | `fips140=on` (default) and `off` |
| --- | --- | --- |
| `ocm sign cv` | Fails with `refusing to sign component version with GODEBUG=fips140=only: unsupported digest hash algorithm` | Signs, logs a warning |
| `ocm verify cv` | Fails with `refusing to verify component version with GODEBUG=fips140=only: unsupported digest hash algorithm` | Verifies, logs a warning |
| Controller signature verification | Fails the resolution | Verifies, logs the weak digest |

Resources excluded from the signature (`NO-DIGEST` / `EXCLUDE-FROM-SIGNATURE`)
are exempt. Digests that OCM computes itself, for example when adding a
component version from a constructor, already use SHA-256, and downloading a
resource accepts only SHA-256 digests in any mode.

MD5 and SHA-1 remain usable for purposes that are not security-relevant, such
as Git object IDs or caching.

## Known Limitations

FIPS mode only covers cryptography that runs through the Go Cryptographic
Module. The following signing mechanism runs its cryptography in an external
binary:

| Feature | Where the cryptography runs |
| --- | --- |
| Sigstore/cosign signing and verification | OCM runs the `cosign` binary from `PATH`, or downloads the upstream release, which is not a FIPS build. OCM checks whether `cosign` is a FIPS build, see [Sigstore and cosign](#sigstore-and-cosign). |

GPG signing and verification run in-process with the Go OpenPGP library
(`github.com/ProtonMail/go-crypto`), whose RSA, NIST ECDSA, SHA-2 and RNG run
through the Go Cryptographic Module. Algorithms and operations outside the
module (EdDSA/Ed25519/Ed448 via circl, v4 key fingerprints via SHA-1,
passphrase-protected key unlocking via CFB, secret-key export from the GnuPG
keyring) run with a debug log by default and are rejected in strict mode. See
[GPG](#gpg).

In a FIPS-restricted environment, use RSA signing, Sigstore with a FIPS build
of `cosign`, or GPG with v6 RSA or ECDSA P keys, see [GPG](#gpg).

Some dependencies bring cryptography that does not run through the Go
Cryptographic Module. `github.com/ProtonMail/go-crypto` (OpenPGP) is also
imported by go-git (commit and tag signature verification) and Helm (chart
provenance). OCM does not call go-git's verification. It does call Helm's
provenance verification when downloading a chart from a Helm repository with
Helm credentials that include a `keyring`:

| Mode | Helm chart download with a `keyring` |
| --- | --- |
| `fips140=on` (default) | Provenance verified with OpenPGP outside the module; logged at debug level |
| `fips140=only` | Rejected: `Helm chart provenance verification is not available`; remove the `keyring` to download without verification |
| `fips140=off` | Provenance verified |

Without a `keyring`, OCM only passes Helm provenance files through. In OCM's
own code, `golangci-lint` (`depguard`) rejects imports of non-approved
algorithms (DES, RC4, DSA, secp256k1, `golang.org/x/crypto` outside reviewed
exceptions) and limits MD5 and SHA-1 to wget checksum verification. The GPG
signing handler (`bindings/go/gpg/signing/handler`) is exempted because it
enforces the FIPS 140-3 mode rules itself.

### Sigstore and cosign

OCM does not implement Sigstore itself. Its Sigstore signing handler runs the
external `cosign` binary, so all Sigstore cryptography runs in `cosign`. The CLI
and controller images include a FIPS build of `cosign`; the OCM CLI binaries do not.

#### How OCM uses cosign

| Step | What OCM does |
| --- | --- |
| Locate | Uses `cosign` from `PATH` (v3.0.4 or later). If none is found, OCM downloads the cosign release pinned in `bindings/go/sigstore/signing/handler/internal/.env` from GitHub, verifies it against the release's `cosign_checksums.txt`, and caches it under the user cache directory (`~/.cache/ocm/cosign/...` on Linux). |
| FIPS check | In FIPS mode, reads the Go build information of `cosign`, the same data that `go version -m` shows, and looks for `GOFIPS140=v<version>`. See the table below. |
| Sign | `ocm sign cv` runs `cosign sign-blob <digest file> --bundle <file> --yes` for keyless signing. The OIDC token is passed in `SIGSTORE_ID_TOKEN` (or GitHub Actions OIDC), never on the command line. OCM stores the resulting Sigstore bundle as the signature. |
| Verify | `ocm verify cv` and the controller write the bundle to a file and run `cosign verify-blob <digest file> --bundle <file>` with the configured `--certificate-identity[-regexp]`, `--certificate-oidc-issuer[-regexp]`, and optionally `--trusted-root` or `--insecure-ignore-tlog`. |

`cosign` inherits OCM's environment, including `GODEBUG`, so a FIPS build of
`cosign` runs in the same FIPS mode as OCM.

In this keyless flow, `cosign` only uses the Go standard library for
cryptography: an ephemeral ECDSA P-256 key, SHA-256, X.509 certificate chains,
and TLS to Fulcio, Rekor, and the TUF repository. In a `GOFIPS140` build, all of
it runs in the Go Cryptographic Module. `cosign` does not need cgo or a system
crypto library: only hardware-token support (`pkcs11key`, `pivkey` build tags)
uses cgo, and the default build leaves it out.

The following `cosign` features use `golang.org/x/crypto`, which is outside the
Go Cryptographic Module. OCM's keyless flow uses none of them:

- Encrypted private key files (`cosign generate-key-pair`): scrypt and NaCl
  secretbox.
- Rekor entries signed with PGP keys: `x/crypto/openpgp`.
- SSH-format keys: `x/crypto/ssh`.
- Cloud KMS providers: the signing runs in the KMS; the Azure and GCP SDKs use
  `x/crypto` for credentials and transport options.

#### FIPS Check

The upstream cosign releases are not FIPS builds.

| Mode | `cosign` that is not a FIPS build | No `cosign` on `PATH` |
| --- | --- | --- |
| `fips140=on` (default) | Used; logged at debug level | Downloaded; logged at debug level |
| `fips140=only` | Rejected: `Sigstore signing and verification require a cosign built against a frozen Go Cryptographic Module` | Rejected: `downloading cosign is disabled`; a previously downloaded one is not used either |
| `fips140=off` | Used | Downloaded |

To keep Sigstore signing inside the FIPS boundary, put a FIPS build of `cosign`
on `PATH`. The CLI image already does.

#### Build a Static FIPS cosign

cosign builds unmodified against the Go Cryptographic Module. Build it with the
same `GOFIPS140` value as OCM, and with cgo disabled so that the binary is
statically linked and runs on any Linux distribution. This is how the CLI image
builds it. The cosign version OCM is tested with is pinned in
`bindings/go/sigstore/signing/handler/internal/.env`:

```shell
CGO_ENABLED=0 GOFIPS140=certified \
  go install -trimpath -ldflags="-s -w" github.com/sigstore/cosign/v3/cmd/cosign@v3.1.3
```

Check the build information. The output must contain `GOFIPS140=v<version>`,
`CGO_ENABLED=0`, and `fips140=on` in `DefaultGODEBUG`:

```shell
$ go version -m "$(go env GOPATH)/bin/cosign" | grep -E 'GOFIPS140|CGO_ENABLED|DefaultGODEBUG'
        build   DefaultGODEBUG=fips140=on,tracebacklabels=0,x509sslcertoverrideplatform=0
        build   CGO_ENABLED=0
        build   GOFIPS140=v1.0.0-c2097c7c
```

To build for another platform, set `GOOS` and `GOARCH`. `go install` then writes
the binary to `$(go env GOPATH)/bin/<os>_<arch>/cosign`, for example
`GOOS=linux GOARCH=amd64` writes `bin/linux_amd64/cosign`.

- **Local `ocm` binary:** put that `cosign` on `PATH` before running `ocm`.
- **Slim CLI image:** build cosign for the image's platform and mount it at
  `/usr/local/bin/cosign`, which is on the default `PATH`:

  ```shell
  docker run --rm \
    -v "$PWD/cosign:/usr/local/bin/cosign:ro" \
    -v "$PWD/.ocmconfig:/.ocmconfig:ro" \
    ghcr.io/open-component-model/cli:latest-slim \
    verify cv --config /.ocmconfig ghcr.io/<namespace>//<component>:<version>
  ```

#### Commercial cosign Images

Commercial FIPS images of cosign are also available, such as Chainguard's
[`cosign-fips`](https://images.chainguard.dev/directory/image/cosign-fips/overview)
and the FIPS variant of the Docker Hardened Image
[`dhi.io/cosign`](https://hub.docker.com/hardened-images/catalog/dhi/cosign).
These images use the OpenSSL FIPS provider instead of the Go Cryptographic
Module, so their `cosign` only works inside the image. Copy the static `ocm`
binary into such an image instead of copying `cosign` out:

```dockerfile
FROM <registry>/cosign-fips:<tag>
COPY --from=ghcr.io/open-component-model/cli:<version> /ocm /usr/local/bin/ocm
ENTRYPOINT ["/usr/local/bin/ocm"]
```

OCM's check only recognizes `GOFIPS140` builds, so with `fips140=only` it
rejects these images' `cosign`; with the default `fips140=on` it uses them.

### GPG

OCM signs and verifies GPG signatures in-process with the Go OpenPGP library
(`github.com/ProtonMail/go-crypto`). No external `gpg` binary is needed for
signing or verification. The `gpg` binary is used only when
`keyringFingerprint` is set in the GPG credentials, to export key material
from the user's GnuPG keyring (`gpg --export`, `gpg --export-secret-keys`).

OCM reads v4 keys, LibrePGP v5 keys (GnuPG, RNP) and RFC 9580 v6 keys.
Encryption-only subkeys are ignored, so GnuPG's PQC keys work: their primary
key signs, and their Kyber encryption subkey is skipped. GnuPG's Ed448 keys
are not supported, because the Go OpenPGP library cannot read their key
encoding. Keys on hardware tokens are not supported either: GnuPG exports only
a stub of such a secret key.

The following table shows what happens in each mode for GPG operations that
fall outside the Go Cryptographic Module:

| Operation | `fips140=on` (default) | `fips140=only` | `fips140=off` |
| --- | --- | --- | --- |
| v4 keys (SHA-1 fingerprints) | Used; logged at debug level | `with GODEBUG=fips140=only, GPG signing and verification require OpenPGP v6 (RFC 9580) or v5 (LibrePGP) keys: v4 and older keys are identified by SHA-1 fingerprints` | Used |
| EdDSA, Ed25519, Ed448, DSA, non-P curves | Used; logged at debug level | `with GODEBUG=fips140=only, GPG signing and verification require an RSA or ECDSA (P-256, P-384, P-521) key` | Used |
| Passphrase-protected keys | Used; logged at debug level | `with GODEBUG=fips140=only, passphrase-protected GPG keys cannot be unlocked because OpenPGP key protection is not FIPS-approved; provide the private key without passphrase protection, for example from a secret store` | Used |
| Exporting a secret key from the GnuPG keyring (`keyringFingerprint`) | Used; logged at debug level | `with GODEBUG=fips140=only, secret keys cannot be exported from the GnuPG keyring because gpg-agent processes them outside the Go Cryptographic Module; provide the private key in the GPG credentials` | Used |

To use GPG in strict mode (`fips140=only`), provide:

- An OpenPGP **v6** key (RFC 9580) or **v5** key (LibrePGP); both use SHA-256
  fingerprints. GnuPG creates v4 keys for RSA and ECDSA (v5 only for Ed448 and
  PQC, which strict mode rejects for their algorithm), and it can neither
  create nor verify v6 keys. Use [Sequoia](https://sequoia-pgp.org/) (`sq`) to
  generate a v6 key:

  ```shell
  sq key generate --profile rfc9580 --signing-algorithm rsa3k \
    --cannot-encrypt --cannot-authenticate --without-password \
    --own-key --name "OCM Signing" --email "ocm@example.com" \
    --output signing-key.pgp --rev-cert signing-key.rev

  # Export the secret key (ASCII-armored):
  sq key export --cert "OCM Signing" > signing-key.asc

  # Export the public certificate (ASCII-armored):
  sq cert export --cert-email "ocm@example.com" > verify-key.asc
  ```

- An RSA or ECDSA (P-256, P-384, P-521) key. This applies to the primary key
  and every signing subkey, not only the key that signs, because OCM verifies
  their self-signatures when it reads the key.
- An unprotected secret key (no passphrase). Use a secret store instead.
- Key material in the GPG credentials (`privateKeyPGPFile`/`publicKeyPGPFile`
  or inline `privateKeyPGP`/`publicKeyPGP`), not `keyringFingerprint`.

## Building from Source

To build FIPS binaries yourself, use the same settings as the release build
(`build:target` in `bindings/go/cli/Taskfile.yml`). Run this from `bindings/go`:

```shell
CGO_ENABLED=0 GOTOOLCHAIN=local GOFIPS140=certified go build \
  -ldflags "-s -w -X ocm.software/open-component-model/bindings/go/cli/cmd/version.BuildVersion=<version>" \
  -o ocm ./cli
```

`GOTOOLCHAIN=local` makes the build fail if the installed Go does not match the
`toolchain` in `go.mod`, instead of downloading another toolchain.

`-s -w` strips the symbol table and DWARF debug information, as the release
binaries do. The `GOFIPS140` build information that `go version -m` reads is
kept.

The module setting lives once, as `GOFIPS140` in the repository's root
`.env`. `task bindings/go/cli:build`, the controller image build
(`task docker-build/multi-arch`), and the `bindings/go` test tasks all read it
from there. The controller `Dockerfile` refuses to build without the
`GOFIPS140` build argument.
