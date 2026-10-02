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
Neither cryptographic module in the OCM images has a CMVP validation certificate
yet. Both are still under review:

- **Go Cryptographic Module v1.26.0** is in the *Comment Resolution - CMVP*
  stage (since 2026-09-10) on the
  [CMVP Modules In Process List](https://csrc.nist.gov/Projects/cryptographic-module-validation-program/modules-in-process/modules-in-process-list).
  Its algorithms are covered by
  [CAVP Certificate A8028](https://csrc.nist.gov/projects/cryptographic-algorithm-validation-program/details?validation=40638).
- **The Garden Linux cryptographic modules** have received all Entropy Source
  Validation (ESV) certificates from NIST, were validated by an external
  auditor, and are submitted to NIST for final review. The *SAP SE Garden Linux
  1877 Kernel Cryptographic Module* (since 2026-09-18) and the *SAP SE Garden
  Linux OpenSSL Cryptographic Module* (since 2026-09-28) are listed as
  *Pending Review* on the same list.

Statuses as of 2026-10-02; check the list for the current state.

Until both modules are validated, check with your compliance owner whether
modules on the Modules In Process List meet your requirements. The previous
Go Cryptographic Module v1.0.0 is validated
([CMVP Certificate #5247](https://csrc.nist.gov/projects/cryptographic-module-validation-program/certificate/5247)),
but it was frozen from Go 1.24, and Go removes it once v1.26.0 is validated.
{{< /callout >}}

## Artifacts

| Artifact | Build | Image contents |
| --- | --- | --- |
| `ocm` CLI binaries (all OS/architectures) | `GOFIPS140=v1.26.0`, `CGO_ENABLED=0` | — |
| OCM CLI image | `GOFIPS140=v1.26.0`, `CGO_ENABLED=0` | `scratch` with `ocm`, `cosign`, `gpg` and the CA bundle |
| OCM controller image | `GOFIPS140=v1.26.0`, `CGO_ENABLED=0` | `ghcr.io/gardenlinux/gardenlinux/fips` |

The controller image is based on the
[Garden Linux FIPS image](https://github.com/gardenlinux/gardenlinux/pkgs/container/gardenlinux%2Ffips).
[Garden Linux](https://docs.gardenlinux.org/reference/glossary.html#fips), like
OCM, is an Apeiro project. The image is pinned by digest and kept up to date by
Renovate.

The CLI image is built `FROM scratch` and contains only:

- `/ocm` (the entrypoint) and `/usr/local/bin/cosign`, both static Go binaries
  built with the Go Cryptographic Module.
- `gpg`, `gpg-agent`, `gpgconf` and `gpg-connect-agent` with their shared
  libraries (glibc, `libgcrypt`, ...), taken from Garden Linux FIPS packages.
  The packages are recorded under `/var/lib/dpkg/status.d/` for image scanners.
- The CA bundle at `/etc/ssl/certs/ca-certificates.crt`.

It has no shell, no package manager and no `dirmngr`, so gpg keyserver
operations such as `--recv-keys` are not available.

`ocm` and `cosign` are statically linked and do not use any system cryptographic
library. All cryptography in OCM itself goes through the Go Cryptographic
Module.

### Container Hardening

Both images and the controller Helm chart follow the DISA
[Container Image Creation and Deployment Guide](https://dl.dod.cyber.mil/wp-content/uploads/devsecops/pdf/DevSecOps_Enterprise_Container_Image_Creation_and_Deployment_Guide_2.6-Public-Release.pdf)
for the parts OCM controls:

| Measure | CLI image | Controller image / chart |
| --- | --- | --- |
| Runs as non-root user `65532` | ✅ | ✅ (`runAsNonRoot: true`) |
| No setuid/setgid executables | ✅ (none shipped) | ✅ (bits removed from all base image executables) |
| No shell or package manager | ✅ | — |
| No privilege escalation, all capabilities dropped | — | ✅ |
| `seccompProfile: RuntimeDefault` | — | ✅ |
| Read-only root filesystem, writable `emptyDir` at `/tmp` | — | ✅ |
| Liveness and readiness probes, resource requests and limits | — | ✅ |

The controller's pod spec meets the Kubernetes
[restricted Pod Security Standard](https://kubernetes.io/docs/concepts/security/pod-security-standards/#restricted),
and the end-to-end tests install the chart into a namespace that enforces it.

The CLI image sets `HOME=/`, so configuration mounted at `/.ocmconfig` or
`/.docker/config.json` is still found. Caches and generated configuration go
to `/tmp`. To read files that only your user can read, or to write into a
mounted directory, run the container with `--user "$(id -u):$(id -g)"`.

Node operating system and cluster STIGs, such as the Garden Linux
`disaSTIGlow` feature and the Kubernetes STIG, are the platform operator's
responsibility. The Garden Linux FIPS container image is not built with
`disaSTIGlow`.

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

`go version -m` shows the FIPS build settings embedded in a binary:

```shell
go version -m ./ocm | grep -E 'GOFIPS140|DefaultGODEBUG'
```

Expected output:

```text
build DefaultGODEBUG=fips140=on
build GOFIPS140=v1.26.0
```

For a container image, copy the binary out first. The binary is `/ocm` in the
CLI image and `/manager` in the controller image.

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

## Known Limitations

FIPS mode only covers cryptography that runs through the Go Cryptographic
Module. The following signing mechanisms are fully or partly outside that
boundary:

| Feature | Reason |
| --- | --- |
| GPG signing and verification | Uses `github.com/ProtonMail/go-crypto/openpgp`, which ships its own cryptographic implementation. |
| Sigstore/cosign outside the CLI image | When no `cosign` is on `PATH`, OCM downloads the upstream release binary, which is not a FIPS build. |
| `gpg` command in the CLI image | GnuPG uses `libgcrypt`. The image forces its FIPS mode (`/etc/gcrypt/fips_enabled`), so only approved algorithms work: RSA, NIST P-curve and Ed25519 keys, SHA-2, AES. SHA-1 signatures, MD5, CAST5 and cv25519 (gpg's default encryption subkey, so pass an explicit algorithm such as `rsa3072` to `--quick-gen-key`) are rejected. FIPS mode is not validation: `libgcrypt` is not a submitted Garden Linux module. |

In a FIPS-restricted environment, use RSA signing, or Sigstore from the OCM CLI
image. Progress on GPG is tracked in
[ocm-project#1327](https://github.com/open-component-model/ocm-project/issues/1327).

### Sigstore and cosign

OCM's Sigstore signing handler runs the external `cosign` binary. It uses the
first `cosign` on `PATH` and only downloads the upstream release when none is
found.

The OCM CLI image ships `/usr/local/bin/cosign`, built from source with the
same `GOFIPS140` module version as `ocm`. The cosign version is pinned in
`bindings/go/sigstore/signing/handler/internal/.env`. Check it with:

```shell
go version -m cosign | grep -E 'GOFIPS140|DefaultGODEBUG'
```

To get the same outside the image, build cosign yourself and put it on `PATH`
before running `ocm`:

```shell
CGO_ENABLED=0 GOFIPS140=v1.26.0 go install github.com/sigstore/cosign/v3/cmd/cosign@v3.1.3
```

cosign's encrypted private key files (`cosign generate-key-pair`) use scrypt and
NaCl secretbox from `golang.org/x/crypto`, which are outside the Go
Cryptographic Module. OCM's keyless Sigstore flow does not use these key files.

## Building from Source

To build FIPS binaries yourself, set `GOFIPS140` the same way the release
build does. Run this from `bindings/go`:

```shell
CGO_ENABLED=0 GOFIPS140=v1.26.0 go build -o ocm ./cli
```

The module version is pinned once, as `GOFIPS140` in the repository's root
`.env`. `task bindings/go/cli:build`, the controller image build
(`task docker-build/multi-arch`), and the `bindings/go` test tasks all read it
from there. The controller `Dockerfile` refuses to build without the
`GOFIPS140` build argument.
