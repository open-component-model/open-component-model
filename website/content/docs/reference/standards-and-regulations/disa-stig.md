---
title: "DISA STIG"
description: "Reference for DISA STIG alignment of the OCM CLI and OCM controller images: container hardening, the Helm chart pod spec, and the GPOS SRG scan."
weight: 2
toc: true
---

This page describes how the OCM CLI and OCM controller images and the controller
Helm chart are hardened, and how the images are checked against DISA
requirements. Since OCM 0.20.0, the release pipeline scans every image against
the DISA GPOS SRG before publishing it. The controller image and the slim CLI
image contain only a static binary and a CA bundle; the default CLI image adds
`cosign` and GnuPG with its shared libraries on Garden Linux `bare-libc`. See
[FIPS 140-3: Artifacts]({{< relref "docs/reference/standards-and-regulations/fips.md#artifacts" >}})
for how they are built.

## Container Hardening

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

## STIG Scan

DISA publishes no STIG for container images. Like vendors of hardened images,
OCM scans its images against the DISA General Purpose Operating System Security
Requirements Guide (GPOS SRG) with OpenSCAP, using the open source
[Chainguard GPOS SRG profile](https://github.com/chainguard-dev/stigs). The
release pipeline scans the linux/arm64 and linux/amd64 variants of the CLI,
slim CLI and controller images it builds, and fails before publishing if any
rule fails.

The profile checks a Wolfi root filesystem in a few places.
`.github/stig/tailoring.xml` deselects those rules, and
`.github/stig/ocm-supplement-xccdf.xml` checks the same SRG requirements against
what the images contain, with OVAL definitions in
`.github/stig/ocm-supplement-oval.xml`:

| SRG rules | Chainguard profile checks | OCM check |
| --- | --- | --- |
| SV-203649, SV-203739, SV-203750, SV-203751, SV-203776 | OpenSSL FIPS provider | The entrypoint has `GOFIPS140=v<version>` and `fips140=on` in its build information |
| SV-263659 | Wolfi CA bundle digests | The only certificate file is the CA bundle of the digest-pinned Garden Linux base image |
| SV-203675 | Shared library permissions | Controller and slim CLI image: no shared libraries or dynamic loader. Default CLI image: every shared library and the dynamic loader are owned by root and not writable by group or others |
| SV-203716 | Shared library permissions | No package manager, setuid/setgid files, world-writable files, world-writable directories without the sticky bit, or executables other than the entrypoint and those declared for the image |
| SV-203616, SV-203617, SV-203664 | `/var/log` permissions | No on-disk log locations; logs go to stdout/stderr |

Each image declares the executables it may contain besides its entrypoint and
whether it ships shared libraries, in the matrix of
`.github/workflows/image-scan.yml`. Only the default CLI image declares any:
`/usr/local/bin/cosign` and the GnuPG binaries `gpg`, `gpg-agent`, `gpgconf`,
`gpg-connect-agent` and `dirmngr` in `/usr/bin`. Any other executable fails the
scan.

The scan runs offline on the exported root filesystem. To scan an image
locally, pass the declarations of its matrix entry, for example for the default
CLI image:

```shell
EXECUTABLES="/usr/local/bin/cosign /usr/bin/gpg /usr/bin/gpg-agent /usr/bin/gpgconf /usr/bin/gpg-connect-agent /usr/bin/dirmngr" \
SHARED_LIBRARIES=true \
node .github/scripts/stig-scan.js <image> bindings/go/cli/Containerfile tmp/stig
```

`tmp/stig` then contains the XCCDF results and HTML reports of both
evaluations. In the pipeline, the reusable `Image scan` workflow
(`.github/workflows/image-scan.yml`) runs this STIG scan and a vulnerability
scan (Trivy, with an OpenVEX document from `govulncheck`; see
[EU Cyber Resilience Act]({{< relref "docs/reference/standards-and-regulations/cra.md" >}}))
for each image and architecture. It writes both results to the job summary and
uploads the reports as `image-scan-<image>-<arch>` workflow artifacts, for
example `image-scan-cli-amd64`.

Node operating system and cluster STIGs, such as the Kubernetes STIG, are the
platform operator's responsibility.
