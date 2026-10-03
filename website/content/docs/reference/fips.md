---
title: "FIPS 140-3"
description: "Reference for FIPS 140-3 support in the OCM CLI and OCM controller: support policy, Go FIPS module, runtime modes, images, and known limitations."
icon: "🛡️"
weight: 8
toc: true
---

This page describes how the OCM CLI and the OCM controller support FIPS 140-3.
OCM does not ship a separate FIPS variant. The regular binaries and container
images are built for FIPS, so the same artifact runs in both normal and
FIPS-restricted environments.

## Support Policy

**What OCM provides.** OCM binaries and images are built against a frozen
version of the Go Cryptographic Module and run in FIPS 140-3 mode by default.
OCM is not itself a cryptographic module and is not FIPS certified or
validated; FIPS 140-3 validation applies to the cryptographic module it uses.
See [Validation Status](#validation-status) for that module's current CMVP
status.

**What you are responsible for.** Whether a deployment meets your regulatory
requirements depends on more than OCM: the node operating system and kernel,
the external binaries OCM runs (`gpg`, `cosign`), your configuration, and your
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
| GPG signing | A `gpg` without a FIPS-mode `libgcrypt` is used and logged at debug level | Rejected | [GPG](#gpg) |

**Module version.** OCM builds with `GOFIPS140=certified`, set once in the
repository's root `.env`. `certified` selects the newest Go Cryptographic Module
version that has a CMVP validation certificate, as recorded by the Go
toolchain the release is built with. When a newer module version is certified,
OCM releases pick it up with the next Go toolchain update; no pin needs to
change. The resolved version is part of every binary's build information, see
[Verifying a Binary](#verifying-a-binary).

**Out of scope.** Whether the `libgcrypt` that `gpg` uses is FIPS validated
(OCM only checks that it runs in FIPS mode), and code outside the Go
Cryptographic Module, such as `golang.org/x/crypto`. See
[Known Limitations](#known-limitations).

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
| OCM CLI image | `GOFIPS140=certified`, `CGO_ENABLED=0` | `scratch` with `ocm` and the CA bundle |
| OCM controller image | `GOFIPS140=certified`, `CGO_ENABLED=0` | `scratch` with `manager` and the CA bundle |

Both images are built `FROM scratch`. They contain only the static binary
(`/ocm` or `/manager`, the entrypoint) and a CA bundle at
`/etc/ssl/certs/ca-certificates.crt`. The CLI image also has world-writable
`/tmp`, `/.cache` and `/.sigstore` directories; the controller chart mounts an
`emptyDir` at `/tmp`.

The images have no shell and no package manager, and the CLI image does not
include `cosign` or `gpg`. See [Sigstore and cosign](#sigstore-and-cosign) and
[GPG](#gpg) for how to use them.

The binaries are statically linked and do not use any system cryptographic
library. All cryptography in OCM itself goes through the Go Cryptographic
Module.

### Container Hardening

Both images and the controller Helm chart follow the DISA
[Container Image Creation and Deployment Guide](https://dl.dod.cyber.mil/wp-content/uploads/devsecops/pdf/DevSecOps_Enterprise_Container_Image_Creation_and_Deployment_Guide_2.6-Public-Release.pdf)
for the parts OCM controls:

| Measure | CLI image | Controller image / chart |
| --- | --- | --- |
| Runs as non-root user `65532` | ✅ | ✅ (`runAsNonRoot`, `runAsUser`/`runAsGroup: 65532`) |
| No setuid/setgid executables | ✅ (none shipped) | ✅ (none shipped) |
| No shell or package manager | ✅ | ✅ |
| No privilege escalation, all capabilities dropped | — | ✅ |
| `seccompProfile: RuntimeDefault` | — | ✅ |
| Read-only root filesystem, writable `emptyDir` at `/tmp` | — | ✅ |
| Liveness and readiness probes, resource requests and limits | — | ✅ |

The controller's pod spec meets the Kubernetes
[restricted Pod Security Standard](https://kubernetes.io/docs/concepts/security/pod-security-standards/#restricted),
and the end-to-end tests install the chart into a namespace that enforces it.

The CLI image runs with `HOME=/`, Docker's default for a user without a passwd
entry, so configuration mounted at `/.ocmconfig` or `/.docker/config.json` is
found. Caches go to the world-writable `/.cache`, for any user ID. To read files
that only your user can read, or to write into a mounted directory, run the
container with `--user "$(id -u):$(id -g)"`.

### STIG Scan

DISA publishes no STIG for container images. Like vendors of hardened images,
OCM scans its images against the DISA General Purpose Operating System Security
Requirements Guide (GPOS SRG) with OpenSCAP, using the open source
[Chainguard GPOS SRG profile](https://github.com/chainguard-dev/stigs). The
release pipeline scans the CLI and controller images it builds, and fails before
publishing if any rule fails.

The profile checks a Wolfi root filesystem in a few places. For OCM's `scratch`
images, `.github/stig/tailoring.xml` deselects those rules, and
`.github/stig/ocm-supplement-xccdf.xml` checks the same SRG requirements against
what the images contain:

| SRG rules | Chainguard profile checks | OCM check |
| --- | --- | --- |
| SV-203649, SV-203739, SV-203750, SV-203751, SV-203776 | OpenSSL FIPS provider | Every Go executable has `GOFIPS140=v<version>` and `fips140=on` in its build information |
| SV-263659 | Wolfi CA bundle digests | The only certificate file is the CA bundle of the digest-pinned Garden Linux base image |
| SV-203675, SV-203716 | Shared library permissions | No shared libraries, dynamic loader, package manager, setuid/setgid files, or world-writable paths without the sticky bit |
| SV-203616, SV-203617, SV-203664 | `/var/log` permissions | No on-disk log locations; logs go to stdout/stderr |

The scan runs offline on the exported root filesystem. To scan an image
locally:

```shell
.github/scripts/stig-scan.sh <image> bindings/go/cli/Containerfile tmp/stig
```

`tmp/stig` then contains the XCCDF results and HTML reports of both
evaluations. The pipeline uploads them as the `stig-cli` and `stig-controller`
workflow artifacts.

Node operating system and cluster STIGs, such as the Kubernetes STIG, are the
platform operator's responsibility.

## Runtime Modes

You control the runtime mode with the `GODEBUG` environment variable.

| `GODEBUG` | Behavior |
| --- | --- |
| `fips140=on` (default) | FIPS mode is active. Approved algorithms run in their FIPS-compliant form, and non-approved algorithms such as MD5 and SHA-1 stay available. |
| `fips140=only` | Like `on`, but non-approved algorithms return an error or panic, and OCM rejects a `cosign` or `gpg` that runs outside the FIPS boundary. Go documents this as a best-effort mode for testing and assessment, not for production. |
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

With `fips140=only`, OCM refuses to run `cosign` or `gpg` outside the FIPS
boundary. Go's own enforcement, however, applies to every Go program OCM starts,
including programs OCM does not control. For example, the Docker Desktop
credential helper (`docker-credential-desktop`) panics on an internal MD5 use, so
OCM cannot resolve registry credentials. Use `fips140=only` where all such
programs are known to work, or as a one-off audit to find non-approved
algorithms in a workload's call path.

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

In FIPS 140-3 mode, OCM therefore requires every resource and component
reference digest to use SHA-256 or SHA-512:

| Operation | FIPS mode | Outside FIPS mode |
| --- | --- | --- |
| `ocm sign cv` | Fails with `refusing to sign component version in FIPS 140-3 mode: unsupported digest hash algorithm` | Signs, logs a warning |
| `ocm verify cv` | Fails with `refusing to verify component version in FIPS 140-3 mode: unsupported digest hash algorithm` | Verifies, logs a warning |
| Controller signature verification | Fails the resolution | Not affected |

Resources excluded from the signature (`NO-DIGEST` / `EXCLUDE-FROM-SIGNATURE`)
are exempt. Digests that OCM computes itself, for example when adding a
component version from a constructor, already use SHA-256, and downloading a
resource accepts only SHA-256 digests in any mode.

MD5 and SHA-1 remain usable for purposes that are not security-relevant, such
as Git object IDs or caching.

## Known Limitations

FIPS mode only covers cryptography that runs through the Go Cryptographic
Module. The following signing mechanisms run their cryptography in an external
binary:

| Feature | Where the cryptography runs |
| --- | --- |
| GPG signing and verification | OCM runs the GnuPG `gpg` binary (>= 2.2.0) from `PATH`, so all OpenPGP cryptography runs in its `libgcrypt`. OCM checks that `libgcrypt` runs in FIPS mode, but cannot check that it is FIPS validated, see [GPG](#gpg). |
| Sigstore/cosign signing and verification | OCM runs the `cosign` binary from `PATH`, or downloads the upstream release, which is not a FIPS build. OCM checks whether `cosign` is a FIPS build, see [Sigstore and cosign](#sigstore-and-cosign). |

In a FIPS-restricted environment, use RSA signing, Sigstore with a FIPS build
of `cosign`, or GPG with a `gpg` whose `libgcrypt` runs in FIPS mode on an
operating system with FIPS-validated cryptographic modules, see [GPG](#gpg).

### Sigstore and cosign

OCM does not implement Sigstore itself. Its Sigstore signing handler runs the
external `cosign` binary, so all Sigstore cryptography runs in `cosign`. Neither
the OCM CLI binaries nor the CLI image include `cosign`.

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
| `fips140=only` | Rejected: `Sigstore signing requires a cosign built against a frozen Go Cryptographic Module` | Rejected: `downloading cosign is disabled`; a previously downloaded one is not used either |
| `fips140=off` | Used | Downloaded |

To keep Sigstore signing inside the FIPS boundary, put a FIPS build of `cosign`
on `PATH`.

#### Build a Static FIPS cosign

cosign builds unmodified against the Go Cryptographic Module. Build it with the
same `GOFIPS140` value as OCM, and with cgo disabled so that the binary is
statically linked and runs on any Linux distribution and in the scratch-based OCM
CLI image. The cosign version OCM is tested with is pinned in
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
- **OCM CLI image:** build cosign for the image's platform and mount it at
  `/usr/local/bin/cosign`, which is on the default `PATH`. cosign keeps its
  trust-root cache in the image's world-writable `/.sigstore`, so it works for
  any user ID:

  ```shell
  docker run --rm \
    -v "$PWD/cosign:/usr/local/bin/cosign:ro" \
    -v "$PWD/.ocmconfig:/.ocmconfig:ro" \
    ghcr.io/open-component-model/cli:latest \
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

OCM signs and verifies GPG signatures by running the `gpg` binary on `PATH`.
Neither the OCM CLI binaries nor the CLI image include it, and GPG signing does
not work in the CLI image as is.

In FIPS mode, OCM checks whether the `libgcrypt` of `gpg` runs in FIPS mode by
asking `gpgconf --show-versions` for `fips-mode:y`. With `fips140=only`, a
missing `gpgconf` also fails the check.

| Mode | `gpg` without a FIPS-mode `libgcrypt` |
| --- | --- |
| `fips140=on` (default) | Used; logged at debug level |
| `fips140=only` | Rejected: `GPG signing requires a gpg whose libgcrypt runs in FIPS mode` |
| `fips140=off` | Used |

GnuPG does its cryptography in `libgcrypt`. For an approved-algorithms-only
`gpg`, use the
[Garden Linux FIPS image](https://github.com/gardenlinux/gardenlinux/pkgs/container/gardenlinux%2Ffips)
([Garden Linux](https://docs.gardenlinux.org/reference/glossary.html#fips) is,
like OCM, an Apeiro project) and force `libgcrypt` into FIPS mode. To use it
with OCM in a container, add `ocm` from the CLI image:

```dockerfile
FROM ghcr.io/gardenlinux/gardenlinux/fips:<version>
RUN apt-get update \
 && apt-get install -y --no-install-recommends gnupg \
 && rm -rf /var/lib/apt/lists/* \
 && mkdir -p /etc/gcrypt && echo 1 > /etc/gcrypt/fips_enabled
COPY --from=ghcr.io/open-component-model/cli:<version> /ocm /usr/local/bin/ocm
ENTRYPOINT ["/usr/local/bin/ocm"]
```

On a host, install GnuPG from your distribution. `libgcrypt` also enters FIPS
mode automatically when the kernel runs in FIPS mode
(`/proc/sys/crypto/fips_enabled` is `1`).

In FIPS mode:

- RSA, NIST P-curve and Ed25519 keys, SHA-2 and AES work.
- SHA-1 signatures, MD5, CAST5 and cv25519 are rejected. cv25519 is gpg's
  default encryption subkey, so pass an explicit algorithm such as `rsa3072` to
  `gpg --quick-gen-key`.

A statically linked `gpg` built from upstream sources is not a substitute:
`libgcrypt`'s FIPS integrity self-check works only on the shared library, and
the upstream build does not reject non-approved algorithms such as MD5 in FIPS
mode.

## Building from Source

To build FIPS binaries yourself, use the same settings as the release build
(`build:target` in `bindings/go/cli/Taskfile.yml`). Run this from `bindings/go`:

```shell
CGO_ENABLED=0 GOFIPS140=certified go build \
  -ldflags "-s -w -X ocm.software/open-component-model/bindings/go/cli/cmd/version.BuildVersion=<version>" \
  -o ocm ./cli
```

`-s -w` strips the symbol table and DWARF debug information, as the release
binaries do. The `GOFIPS140` build information that `go version -m` reads is
kept.

The module setting lives once, as `GOFIPS140` in the repository's root
`.env`. `task bindings/go/cli:build`, the controller image build
(`task docker-build/multi-arch`), and the `bindings/go` test tasks all read it
from there. The controller `Dockerfile` refuses to build without the
`GOFIPS140` build argument.
