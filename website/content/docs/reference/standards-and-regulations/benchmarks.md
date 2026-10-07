---
title: "Security Benchmarks"
description: "How the OCM images and controller Helm chart perform against the CIS Docker Benchmark, CIS Kubernetes Benchmark, and NSA/CISA Hardening Guide."
weight: 5
toc: true
---

This page describes how the OCM container images and the controller Helm chart
perform against three widely used security benchmarks: the CIS Docker Benchmark,
the CIS Kubernetes Benchmark, and the NSA/CISA Kubernetes Hardening Guide. Since
OCM 0.20.0, both images are built `FROM scratch` with a static binary and a CA
bundle. See
[FIPS 140-3: Artifacts]({{< relref "docs/reference/standards-and-regulations/fips.md#artifacts" >}})
for build details and
[DISA STIG]({{< relref "docs/reference/standards-and-regulations/disa-stig.md" >}})
for the GPOS SRG scan and container hardening measures.

OCM does not give compliance guarantees. Whether a deployment meets your
requirements depends on your platform, configuration, and compliance regime.

## Benchmarks Covered

| Benchmark | Version | Scope checked |
| --- | --- | --- |
| [CIS Docker Benchmark](https://www.cisecurity.org/benchmark/docker) | v1.7.0 (July 2024, Docker 26.x) | Section 4 — Container images and build files |
| [CIS Kubernetes Benchmark](https://www.cisecurity.org/benchmark/kubernetes) | v1.11.0 (April 2025, Kubernetes 1.31/1.32) | Section 5 — Policies (the parts a workload or chart controls) |
| [NSA/CISA Kubernetes Hardening Guide](https://media.defense.gov/2022/Aug/29/2003066362/-1/-1/0/CTR_KUBERNETES_HARDENING_GUIDANCE_1.2_20220829.PDF) | v1.2 (August 2022) | Pod security, RBAC, network, and supply-chain recommendations |

The CIS Docker Benchmark section 4 covers image-level controls: non-root user,
trusted base images, no unnecessary packages, no secrets, HEALTHCHECK, and
content trust. The CIS Kubernetes Benchmark section 5 covers workload-level
policies: pod security, RBAC, network policies, and secrets. The NSA/CISA guide
covers similar areas from a threat-model perspective.

Sections 1–3 of the CIS Docker Benchmark (host, daemon, and daemon
configuration files) and sections 1–4 of the CIS Kubernetes Benchmark (control
plane, etcd, and worker nodes) are the platform operator's responsibility.

## Scan Tools

Every build and release pipeline run scans the images and the chart with three
open-source scanners, in the reusable `Image scan` workflow
(`.github/workflows/image-scan.yml`). The scans block publishing, like the
[DISA STIG]({{< relref "docs/reference/standards-and-regulations/disa-stig.md" >}})
scan:

| Tool | What it checks | Runs on | Fails on |
| --- | --- | --- | --- |
| [dockle](https://github.com/goodwithtech/dockle) | CIS Docker Benchmark section 4 | Each image and architecture | Any WARN or FATAL check |
| [Trivy](https://github.com/aquasecurity/trivy) `config` | CIS Kubernetes Benchmark and the built-in Kubernetes checks of Trivy | Rendered chart | Any finding not in `.github/benchmarks/trivyignore.yaml`, or no manifests checked |
| [Kubescape](https://github.com/kubescape/kubescape) | NSA and MITRE ATT&CK frameworks | Rendered chart | Any failed control on a resource without an exception in `.github/benchmarks/kubescape-exceptions.json`, or no resources scanned |

The chart is rendered with `helm template` and
`manager.networkPolicy.enabled=true`. That render is the default render plus the
`NetworkPolicy`, so it covers every template a default installation gets. Each
template is written to its own file, so each accepted Trivy finding applies only
to the template that needs it.
Every accepted finding is explained below. The dockle and Kubescape images are
pinned by digest in the root `.env` (`DOCKLE_IMAGE`, `KUBESCAPE_IMAGE`), and
Trivy by the `trivy-action` version. Kubescape downloads its framework
definitions at scan time, so a new upstream control can fail the scan without
any change in OCM.

Results are in the job summary and in the `image-scan-<image>-<arch>` and
`chart-scan` workflow artifacts.

## CIS Docker Benchmark (Section 4)

Both images are scanned with dockle. The `scratch`-based images have no shell,
no package manager, no setuid/setgid files, no secrets, no `ADD` instructions,
and run as user `65532`. dockle produces no WARN or FATAL findings. Its two
SKIP results (`DKL-LI-0001`, `DKL-LI-0002`) are checks specific to dockle of
`/etc/passwd` and `/etc/shadow`, which a `scratch` image does not contain:

| Check | Description | CLI image | Controller image | Notes |
| --- | --- | --- | --- | --- |
| CIS-DI-0001 | Non-root user | ✅ | ✅ | `USER 65532:65532` |
| CIS-DI-0002 | Trusted base images | ✅ | ✅ | Garden Linux FIPS (digest-pinned) and `scratch` |
| CIS-DI-0003 | No unnecessary packages | ✅ | ✅ | `scratch`: only the binary and CA bundle |
| CIS-DI-0005 | Content trust | ℹ️ INFO | ℹ️ INFO | `DOCKER_CONTENT_TRUST` is a client-side setting, not an image property |
| CIS-DI-0006 | HEALTHCHECK instruction | ℹ️ INFO | ℹ️ INFO | Not applicable: on Kubernetes the chart sets liveness and readiness probes |
| CIS-DI-0007 | No bare `update` instructions | ✅ | ✅ | No package manager |
| CIS-DI-0008 | No setuid/setgid files | ✅ | ✅ | `scratch`: no files with elevated permissions |
| CIS-DI-0009 | COPY instead of ADD | ✅ | ✅ | Only `COPY` is used |
| CIS-DI-0010 | No secrets in Dockerfile | ✅ | ✅ | No secrets or credentials |

CIS-DI-0005 and CIS-DI-0006 are INFO-level observations, not failures. Content
trust (CIS-DI-0005) is a Docker client environment variable that controls
whether pulls verify signatures; it cannot be set inside an image. HEALTHCHECK
(CIS-DI-0006) is superseded by Kubernetes probes when the image runs in a pod;
the controller chart configures both liveness and readiness probes.

### Reproducing with dockle

Build the CLI image (from `bindings/go/cli`):

```shell
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 GOFIPS140=certified \
  go build -trimpath -o tmp/bin/ocm-linux-arm64 .
docker buildx build --platform linux/arm64 --load \
  -t local/ocm-cli:bench -f Containerfile .
rm tmp/bin/ocm-linux-arm64
```

Build the controller image (from `bindings/go`):

```shell
docker buildx build --platform linux/arm64 --load \
  --build-arg GOFIPS140=certified \
  -t local/ocm-controller:bench \
  -f kubernetes/controller/Dockerfile .
```

Scan either image with the dockle version CI uses (from the root `.env`):

```shell
docker run --rm -v /var/run/docker.sock:/var/run/docker.sock \
  "$(sed -n 's/^DOCKLE_IMAGE=//p' .env)" \
  --exit-code 1 --exit-level warn local/ocm-cli:bench
```

dockle's CIS-DI-0001 check flags only images whose user is `root` by name. An
image built with a numeric `USER 0` passes it. The chart's `runAsNonRoot: true`
covers this on Kubernetes.

Clean up:

```shell
docker rmi local/ocm-cli:bench local/ocm-controller:bench
```

## CIS Kubernetes Benchmark (Section 5)

The rendered Helm chart is scanned with Trivy `config`. Every finding it reports
is accepted for the listed template only. The table below is rendered from
`.github/benchmarks/trivyignore.yaml`, the same file the CI scan uses:

{{< benchmark-exceptions "trivy" >}}

All other checks pass. The pod spec sets `runAsNonRoot: true`,
`runAsUser`/`runAsGroup: 65532`, `allowPrivilegeEscalation: false`,
`readOnlyRootFilesystem: true`, drops all capabilities, and uses
`seccompProfile: RuntimeDefault`. These settings satisfy the CIS Kubernetes
Benchmark section 5.2 (Pod Security Standards) controls.

### Reproducing with Trivy

The Helm renderer built into Trivy assumes Kubernetes 1.20, which the chart's
`kubeVersion: ">=1.26.0-0"` rejects, so Trivy would scan nothing. Render the
chart first, as CI does. Docker Desktop on macOS does not share the worktree
path, so copy the rendered templates into a Docker volume:

```shell
cd <repository-root>
helm template ocm-k8s-toolkit bindings/go/kubernetes/controller/chart \
  --namespace ocm-k8s-toolkit-system --kube-version 1.35.1 \
  --set manager.networkPolicy.enabled=true \
  --output-dir /tmp/ocm-bench/rendered
cp -R .github/benchmarks /tmp/ocm-bench/
tar cf - --no-mac-metadata -C /tmp/ocm-bench . \
  | docker run --rm -i -v ocm-bench:/data alpine:latest sh -c 'cd /data && tar xf - && chown -R 1001 /data'

docker run --rm -v ocm-bench:/data -w /data aquasec/trivy:0.70.0 \
  config --ignorefile benchmarks/trivyignore.yaml rendered
```

Omit `--ignorefile` to see the accepted findings, or add
`--include-non-failures` to see every passing check.

## NSA/CISA Kubernetes Hardening Guide

The chart, rendered with the `NetworkPolicy` enabled, is scanned with Kubescape
against the NSA and MITRE ATT&CK frameworks. Every control that still fails has
an exception. The table below is rendered from
`.github/benchmarks/kubescape-exceptions.json`, the same file the CI scan uses.
The compliance scores of each run are in the job summary of the
`Image scan / chart` job.

With default values (`manager.networkPolicy.enabled=false`), no `NetworkPolicy` is
rendered, so C-0030 (Ingress and Egress blocked) also fails. The policy is
opt-in because it needs a CNI that enforces `NetworkPolicy`. When enabled, it
allows ingress to the health-probe port (and the metrics port when metrics are
enabled) and egress to DNS (53) and HTTPS/Kubernetes API (443, 6443); replace
the egress rules with `manager.networkPolicy.egress`, add ingress rules with
`manager.networkPolicy.ingress`.

{{< benchmark-exceptions "kubescape" >}}

All other controls pass, including: no privileged containers, no host PID/IPC/network, no
hostPath mounts, no insecure capabilities, non-root execution, read-only
filesystem, CPU and memory limits set, immutable container filesystem, and no
privilege escalation.

### Reproducing with Kubescape

With the rendered templates in the `ocm-bench` volume (see
[Reproducing with Trivy](#reproducing-with-trivy)):

```shell
docker run --rm --user 1001 -e HOME=/tmp -v ocm-bench:/data -w /data \
  "$(sed -n 's/^KUBESCAPE_IMAGE=//p' .env)" \
  scan framework nsa,mitre rendered --exceptions benchmarks/kubescape-exceptions.json
```

Kubescape lists excepted controls as failed, marked `w/exceptions`. Omit
`--exceptions` to see them without the marker, or add `-v` for per-resource
details. Clean up:

```shell
docker volume rm ocm-bench
rm -r /tmp/ocm-bench
```

## Summary

No benchmark finding is open: all three scans block publishing, so every release
passes them. The accepted findings come from what the controller needs to work
(reading registry credentials from Secrets, leader election, Kubernetes API
access) and from a registry allowlist that depends on your environment. Each is
listed with its rationale above. Without the opt-in `NetworkPolicy`, C-0030 fails
as well.

Image-level checks (CIS-DI-0005, CIS-DI-0006) are informational and not
applicable in a Kubernetes deployment.
