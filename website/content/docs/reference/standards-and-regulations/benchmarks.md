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

The results on this page are produced with three open-source scanners:

| Tool | What it checks |
| --- | --- |
| [dockle](https://github.com/goodwithtech/dockle) | CIS Docker Benchmark section 4 checks against a saved image tarball |
| [Trivy](https://github.com/aquasecurity/trivy) `config` | CIS Kubernetes Benchmark and built-in Kubernetes security checks against rendered Helm chart manifests |
| [Kubescape](https://github.com/kubescape/kubescape) | NSA and MITRE ATT&CK frameworks against rendered manifests |

## CIS Docker Benchmark (Section 4)

Both images are scanned with dockle. The `scratch`-based images have no shell,
no package manager, no setuid/setgid files, no secrets, no `ADD` instructions,
and run as user `65532`. dockle produces no WARN or FATAL findings:

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

Scan either image:

```shell
docker save local/ocm-cli:bench \
  | docker run --rm -i --entrypoint sh goodwithtech/dockle:latest \
      -c 'cat > /tmp/i.tar && dockle --exit-code 0 -af settings.py --input /tmp/i.tar'
```

Clean up:

```shell
docker rmi local/ocm-cli:bench local/ocm-controller:bench
```

## CIS Kubernetes Benchmark (Section 5)

The rendered Helm chart is scanned with Trivy `config`. Out of 939 checks, 936
pass and 3 report findings. All three are by design.

| Check | Severity | Title | Result | Rationale |
| --- | --- | --- | --- | --- |
| KSV-0041 | Critical | ClusterRole can manage secrets | Finding | The controller resolves OCI registry credentials from Kubernetes Secrets referenced in `Repository` and `Resource` custom resources. Read/list/watch on secrets is required for this. The ClusterRole does not grant create, update, or delete on secrets. |
| KSV-0049 | Medium | Role can manage configmaps | Finding | The leader-election Role grants create/update/patch/delete on ConfigMaps in the release namespace. `controller-runtime` uses a ConfigMap (or Lease) for leader election; write access is required. |
| KSV-0125 | Medium | Image not from a trusted registry | Finding | The default `values.yaml` references `ghcr.io/open-component-model/kubernetes/controller`. Trivy flags any registry not on its built-in allowlist. Override `manager.image.repository` if your policy requires a mirror. |

The remaining 936 checks pass. The pod spec sets `runAsNonRoot: true`,
`runAsUser`/`runAsGroup: 65532`, `allowPrivilegeEscalation: false`,
`readOnlyRootFilesystem: true`, drops all capabilities, and uses
`seccompProfile: RuntimeDefault`. These settings satisfy the CIS Kubernetes
Benchmark section 5.2 (Pod Security Standards) controls.

### Reproducing with Trivy

Because Docker Desktop on macOS does not share the worktree path, copy the chart
into a Docker volume first:

```shell
cd <repository-root>
tar cf - --no-mac-metadata -C bindings/go/kubernetes/controller chart \
  | docker run --rm -i -v trivy-scan:/data alpine:latest sh -c 'cd /data && tar xf -'

docker run --rm -v trivy-scan:/data \
  aquasec/trivy config --exit-code 0 \
  --misconfig-scanners helm /data/chart
```

Add `--include-non-failures` to see every passing check. Clean up:

```shell
docker volume rm trivy-scan
```

## NSA/CISA Kubernetes Hardening Guide

The rendered chart is scanned with Kubescape against the NSA and MITRE ATT&CK
frameworks. Kubescape reports an NSA compliance score of 92.50% and a MITRE
score of 88.24%, with 25 of 30 controls passing and 5 findings.

| Control | Severity | Title | Result | Rationale |
| --- | --- | --- | --- | --- |
| C-0015 | High | List Kubernetes secrets | Finding | Same as KSV-0041: the ClusterRole grants get/list/watch on Secrets for credential resolution. |
| C-0030 | Medium | Ingress and Egress blocked | Finding | The chart does not ship a NetworkPolicy. Network segmentation depends on the cluster's CNI and the operator's network policies. |
| C-0034 | Medium | Automatic mapping of service account | Finding | The Deployment does not set `automountServiceAccountToken: false`. The controller needs the projected service account token to authenticate to the Kubernetes API. |
| C-0037 | Medium | CoreDNS poisoning | Finding | The leader-election Role grants write access to ConfigMaps, which Kubescape flags as a vector for CoreDNS ConfigMap tampering. The Role is namespace-scoped to the release namespace and used only for leader election. |
| C-0053 | Medium | Access container service account | Finding | The ServiceAccount is mounted into the pod. As noted under C-0034, the controller requires API access and cannot opt out. |

All other controls pass, including: no privileged containers, no host PID/IPC/network, no
hostPath mounts, no insecure capabilities, non-root execution, read-only
filesystem, CPU and memory limits set, immutable container filesystem, and no
privilege escalation.

### Gaps

**NetworkPolicy (C-0030).** The chart does not include a NetworkPolicy because
the required rules depend on the cluster's CNI, the namespace layout, and which
OCI registries the controller must reach. To restrict traffic, create a
NetworkPolicy in the release namespace that allows egress to the Kubernetes API
server and to your OCI registries, and allows ingress on the health-probe and
metrics ports if exposed.

**automountServiceAccountToken (C-0034, C-0053).** The controller is a
Kubernetes operator that watches custom resources and reads Secrets. Disabling
the service account token mount would break it. This is an accepted trade-off
for any controller-runtime operator.

### Reproducing with Kubescape

```shell
cd <repository-root>
helm template ocm bindings/go/kubernetes/controller/chart > /tmp/ocm-rendered.yaml

docker run --rm -i -v trivy-scan:/data alpine:latest \
  sh -c 'cat > /data/rendered.yaml' < /tmp/ocm-rendered.yaml

docker run --rm -v trivy-scan:/data \
  quay.io/kubescape/kubescape-cli:latest \
  scan framework nsa,mitre /data/rendered.yaml
```

Add `-v` for per-resource details. Clean up:

```shell
docker volume rm trivy-scan
rm /tmp/ocm-rendered.yaml
```

## Summary

| Benchmark | Tool | Total checks | Pass | Findings | Failures |
| --- | --- | --- | --- | --- | --- |
| CIS Docker Benchmark v1.7.0 §4 | dockle | 9 | 7 | 2 INFO | 0 |
| CIS Kubernetes Benchmark v1.11.0 §5 | Trivy config | 939 | 936 | 3 | 0 |
| NSA/CISA Hardening Guide v1.2 | Kubescape | 30 | 25 | 5 | 0 |

Every finding has an explanation above. The three categories of accepted
findings are:

1. **Secrets access** (KSV-0041, C-0015): required for credential resolution.
2. **Service account and leader election** (KSV-0049, C-0034, C-0037, C-0053):
   required for controller-runtime operation.
3. **Image registry policy and network policy** (KSV-0125, C-0030):
   environment-specific; the operator configures these.

Image-level checks (CIS-DI-0005, CIS-DI-0006) are informational and not
applicable in a Kubernetes deployment.
