---
title: "FIPS 140-3"
description: "Reference for FIPS 140-3 support in the OCM CLI and OCM controller: Go FIPS module, runtime modes, base images, and known limitations."
icon: "🛡️"
weight: 8
toc: true
---

This page describes how the OCM CLI and the OCM controller support FIPS 140-3.
OCM does not ship a separate FIPS variant. The regular binaries and container
images are built for FIPS, so the same artifact runs in both normal and
FIPS-restricted environments.

## Cryptographic Module

OCM uses the [Go Cryptographic Module](https://go.dev/doc/security/fips140),
which is part of the Go standard library. The OCM CLI and the OCM controller
are built against a frozen version of the module instead of the module version
that ships with the current Go toolchain.

| Setting | Value |
| --- | --- |
| Build setting | `GOFIPS140=v1.26.0` |
| Module version in the binary | `v1.26.0` |
| Default runtime setting | `GODEBUG=fips140=on` |

Setting `GOFIPS140` at build time does two things:

- It compiles the binary against the frozen module source.
- It sets the default `GODEBUG` value to `fips140=on`, so FIPS mode is active
  without any runtime configuration.

### Validation Status

{{< callout context="caution" >}}
The Go Cryptographic Module in the OCM binaries has no CMVP validation
certificate yet:

- **Go Cryptographic Module v1.26.0** is in the *Comment Resolution - CMVP*
  stage (since 2026-09-10) on the
  [CMVP Modules In Process List](https://csrc.nist.gov/Projects/cryptographic-module-validation-program/modules-in-process/modules-in-process-list).
  Its algorithms are covered by
  [CAVP Certificate A8028](https://csrc.nist.gov/projects/cryptographic-algorithm-validation-program/details?validation=40638).

Status as of 2026-10-02; check the list for the current state.

Until the module is validated, check with your compliance owner whether
modules on the Modules In Process List meet your requirements. The previous
Go Cryptographic Module v1.0.0 is validated
([CMVP Certificate #5247](https://csrc.nist.gov/projects/cryptographic-module-validation-program/certificate/5247)),
but it was frozen from Go 1.24, and Go removes it once v1.26.0 is validated.
{{< /callout >}}

## Artifacts

| Artifact | Build | Image contents |
| --- | --- | --- |
| `ocm` CLI binaries (all OS/architectures) | `GOFIPS140=v1.26.0`, `CGO_ENABLED=0` | — |
| OCM CLI image | `GOFIPS140=v1.26.0`, `CGO_ENABLED=0` | `scratch` with `ocm` and the CA bundle |
| OCM controller image | `GOFIPS140=v1.26.0`, `CGO_ENABLED=0` | `scratch` with `manager` and the CA bundle |

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
| Runs as non-root user `65532` | ✅ | ✅ (`runAsNonRoot: true`) |
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

Node operating system and cluster STIGs, such as the Kubernetes STIG, are the
platform operator's responsibility.

## Runtime Modes

You control the runtime mode with the `GODEBUG` environment variable.

| `GODEBUG` | Behavior |
| --- | --- |
| `fips140=on` (default) | FIPS mode is active. Approved algorithms run in their FIPS-compliant form, and non-approved algorithms such as MD5 and SHA-1 stay available. |
| `fips140=only` | Non-approved algorithms return an error or panic. Go documents this as a best-effort mode for testing and assessment, not for production. |
| `fips140=off` | FIPS mode is disabled. |

OCM runs in `fips140=on` mode by default, which allows non-approved algorithms
instead of rejecting them. Parts of the dependency tree use non-approved
algorithms for non-security purposes, such as content addressing and legacy
digests. The FIPS 140-3 Security Policy does not require `fips140=only`, and
OCM does not support it.

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
build GOFIPS140=v1.26.0
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
INFO setup FIPS 140-3 mode {"enabled": true, "module": "v1.26.0"}
```

The CLI logs it at `debug` level for every command, so it does not add noise to
regular output. Pass `--loglevel debug` to see it:

```shell
ocm --loglevel debug get cv <reference>
```

```text
level=DEBUG msg="FIPS 140-3 mode" enabled=true module=v1.26.0
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
| GPG signing and verification | OCM runs the GnuPG `gpg` binary (>= 2.2.0) from `PATH`, so all OpenPGP cryptography runs in its `libgcrypt`. It is FIPS-covered only with a FIPS 140-3 validated `libgcrypt` in FIPS mode, see [GPG](#gpg). |
| Sigstore/cosign signing and verification | OCM runs the `cosign` binary from `PATH`. In FIPS mode, OCM does not download cosign when none is found, because the upstream release is not a FIPS build. Provide your own FIPS build, see [Sigstore and cosign](#sigstore-and-cosign). |

In a FIPS-restricted environment, use RSA signing, or Sigstore with a FIPS
build of `cosign`. Progress on GPG is tracked in
[ocm-project#1327](https://github.com/open-component-model/ocm-project/issues/1327).

### Sigstore and cosign

OCM's Sigstore signing handler runs the external `cosign` binary from `PATH`.
Neither the OCM CLI binaries nor the CLI image include `cosign`. When none is
found, OCM fails in FIPS mode with `downloading cosign is disabled in FIPS 140-3
mode`, and does not use a previously downloaded one either, because the
upstream release is not a FIPS build. Only outside FIPS mode
(`GODEBUG=fips140=off`) does it download and cache the upstream release.

cosign builds unmodified against the Go Cryptographic Module. Build it with the
same `GOFIPS140` value as OCM; the cosign version OCM is tested with is pinned in
`bindings/go/sigstore/signing/handler/internal/.env`:

```shell
CGO_ENABLED=0 GOFIPS140=v1.26.0 go install -trimpath -ldflags="-s -w" github.com/sigstore/cosign/v3/cmd/cosign@v3.1.3
go version -m "$(go env GOPATH)/bin/cosign" | grep -E 'GOFIPS140|DefaultGODEBUG'
```

- **Local `ocm` binary:** put that `cosign` on `PATH` before running `ocm`.
- **OCM CLI image:** build cosign for the image's platform (for example
  `GOOS=linux GOARCH=amd64`) and mount it at `/usr/local/bin/cosign`, which is
  on the default `PATH`. cosign keeps its trust-root cache in the image's
  world-writable `/.sigstore`, so it works for any user ID:

  ```shell
  docker run --rm \
    -v "$PWD/cosign:/usr/local/bin/cosign:ro" \
    -v "$PWD/.ocmconfig:/.ocmconfig:ro" \
    ghcr.io/open-component-model/cli:latest \
    verify cv --config /.ocmconfig ghcr.io/<namespace>//<component>:<version>
  ```

cosign's encrypted private key files (`cosign generate-key-pair`) use scrypt and
NaCl secretbox from `golang.org/x/crypto`, which are outside the Go
Cryptographic Module. OCM's keyless Sigstore flow does not use these key files.

### GPG

OCM signs and verifies GPG signatures by running the `gpg` binary on `PATH`.
Neither the OCM CLI binaries nor the CLI image include it, and GPG signing does
not work in the CLI image as is.

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
- Garden Linux's `libgcrypt` is not a submitted Garden Linux module. FIPS mode
  restricts the algorithms but does not make GPG signing validated.

A statically linked `gpg` built from upstream sources is not a substitute:
`libgcrypt`'s FIPS integrity self-check works only on the shared library, and
the upstream build does not reject non-approved algorithms such as MD5 in FIPS
mode.

## Building from Source

To build FIPS binaries yourself, use the same settings as the release build
(`build:target` in `bindings/go/cli/Taskfile.yml`). Run this from `bindings/go`:

```shell
CGO_ENABLED=0 GOFIPS140=v1.26.0 go build \
  -ldflags "-s -w -X ocm.software/open-component-model/bindings/go/cli/cmd/version.BuildVersion=<version>" \
  -o ocm ./cli
```

`-s -w` strips the symbol table and DWARF debug information, as the release
binaries do. The `GOFIPS140` build information that `go version -m` reads is
kept.

The module version is pinned once, as `GOFIPS140` in the repository's root
`.env`. `task bindings/go/cli:build`, the controller image build
(`task docker-build/multi-arch`), and the `bindings/go` test tasks all read it
from there. The controller `Dockerfile` refuses to build without the
`GOFIPS140` build argument.
