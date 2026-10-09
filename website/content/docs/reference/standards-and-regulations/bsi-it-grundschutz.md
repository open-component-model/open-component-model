---
title: "BSI IT-Grundschutz"
description: "Reference mapping of OCM CLI and controller images, Helm chart, and signing to the BSI IT-Grundschutz Compendium modules SYS.1.6 and APP.4.4."
weight: 4
toc: true
---

This page maps the hardening measures of the OCM CLI image, the OCM controller
image, and the controller Helm chart to the BSI
[IT-Grundschutz Compendium](https://www.bsi.bund.de/DE/Themen/Unternehmen-und-Organisationen/Standards-und-Zertifizierung/IT-Grundschutz/IT-Grundschutz-Kompendium/it-grundschutz-kompendium_node.html)
(German: `IT-Grundschutz-Kompendium`, Edition 2023, February 2023). The English
requirement titles below are taken from the
[IT-Grundschutz Compendium Edition 2022](https://www.bsi.bund.de/SharedDocs/Downloads/EN/BSI/Grundschutz/International/bsi_it_gs_comp_2022.html),
the latest English edition published by BSI. Two modules are relevant:

- **SYS.1.6** `Containerisierung` (Containerization, Edition 2022) —
  requirements for container images, runtimes, and host systems.
- **APP.4.4** Kubernetes (Edition 2023) — requirements for Kubernetes clusters,
  pods, and their infrastructure.

**Most IT-Grundschutz requirements address the platform operator**, not the
software vendor. OCM can influence a subset of them — image contents, pod
security settings, RBAC scope, and build-time signing. The tables below note
what OCM provides out of the box, what the operator must do, and where gaps
exist. OCM does not give compliance guarantees; whether a deployment satisfies
IT-Grundschutz depends on the cluster, the node operating system, the network
infrastructure, and the operator's own security concept.

For how the images are built and hardened, and how they are scanned against the
DISA GPOS SRG, see
[DISA STIG]({{< relref "docs/reference/standards-and-regulations/disa-stig.md" >}}).
For the FIPS 140-3 cryptographic module, see
[FIPS 140-3]({{< relref "docs/reference/standards-and-regulations/fips.md" >}}).

## SYS.1.6 Containerization

SYS.1.6 covers container images, the container runtime, and the host system. It
applies whenever containerized services are operated. The requirement IDs and
English titles below are taken from the
[SYS.1.6 PDF](https://www.bsi.bund.de/SharedDocs/Downloads/DE/BSI/Grundschutz/IT-GS-Kompendium_Einzel_PDFs_2022/07_SYS_IT_Systeme/SYS_1_6_Containerisierung_Edition_2022.pdf)
(Edition 2022, February 2022). **(B)** = basic, **(S)** = standard,
**(H)** = increased protection needs.

| Requirement | OCM provides | Operator responsibility / gap |
| --- | --- | --- |
| **SYS.1.6.A1** Planning the use of containers (B) | — | Entirely the operator's planning task. |
| **SYS.1.6.A2** Planning the management of containers (B) | — | Operator plans lifecycle, management tooling, and admin processes. |
| **SYS.1.6.A3** Secure deployment of containerized IT systems (B) | The chart sets liveness and readiness probes with configurable intervals (`values.yaml`). | Operator verifies isolation, virtual networking, and monitors performance. |
| **SYS.1.6.A4** Planning the deployment and distribution of images (B) | OCM images are published to `ghcr.io` with digest-pinned tags. The release pipeline builds, scans, and signs every image before publishing. | Operator documents their image distribution process and controls access to the registry. |
| **SYS.1.6.A5** Separation of the administration and access networks for containers (B) | The chart includes an opt-in `NetworkPolicy` (`manager.networkPolicy.enabled`, default `false`). When enabled, it allows ingress to the health-probe port (and the metrics port when metrics are enabled), and defaults egress to DNS (UDP/TCP 53) and HTTPS/Kubernetes API (TCP 443, 6443). Egress rules are replaceable via `manager.networkPolicy.egress`; extra ingress rules can be added via `manager.networkPolicy.ingress`. | Operator must enable the policy (`manager.networkPolicy.enabled: true`), tighten the rules for their environment, and use a CNI plugin that enforces `NetworkPolicy`. |
| **SYS.1.6.A6** Use of secure images (B) | The controller and slim CLI images are built `FROM scratch` with only a static binary and a CA bundle from a digest-pinned Garden Linux FIPS base image. The default CLI image adds `cosign` and GnuPG on the digest-pinned Garden Linux `bare-libc` image. No shell, no package manager, no setuid binaries. Image tags include digests. | Operator must track OCM releases and apply updates. |
| **SYS.1.6.A7** Persistence of container logging data (B) | The controller logs to stdout/stderr (JSON by default). No log files are written inside the container. | Operator collects logs externally (e.g. a cluster-level log aggregator). |
| **SYS.1.6.A8** Secure storage of access data for containers (B) | The controller reads registry credentials and signing keys from Kubernetes Secrets referenced in custom resources. No credentials are baked into images. | Operator manages Secret lifecycle, encryption at rest (etcd encryption), and RBAC. The controller's `ClusterRole` has cluster-wide `get`/`list`/`watch` on Secrets — see [RBAC scope](#rbac-scope). |
| **SYS.1.6.A9** Suitability for container operation (S) | Both binaries are statically linked, require no shared libraries, and handle `SIGTERM` gracefully. The chart sets `terminationGracePeriodSeconds: 10`. | Operator documents suitability assessment. |
| **SYS.1.6.A10** Policy for images and container operations (S) | — | Operator creates and enforces the image policy. |
| **SYS.1.6.A11** Only one service per container (S) | Each image has one entrypoint (`/ocm` or `/manager`). In the default CLI image, `ocm` starts `cosign` and GnuPG only as subprocesses for signing and verification. | — |
| **SYS.1.6.A12** Distribution of secure images (S) | OCM images are signed (Sigstore cosign keyless) and carry SLSA provenance. The Helm chart pins the image by digest in `values.yaml`. | Operator verifies signatures before deployment and documents trusted sources. |
| **SYS.1.6.A13** Release of images (S) | The release pipeline runs unit tests, integration tests, the STIG scan, and signing before publishing. | Operator integrates OCM image releases into their own approval workflow. |
| **SYS.1.6.A14** Updating images (S) | The Garden Linux base image is digest-pinned; updates are tracked by Renovate and produce a new release. | Operator plans patch and change management for OCM updates. |
| **SYS.1.6.A15** Limitation of resources per container (S) | The chart sets default CPU and memory requests and limits (100 m / 256 Mi request, 500 m / 512 Mi limit). | Operator tunes limits and defines behavior on exceeded quotas. |
| **SYS.1.6.A16** Remote administrative access to containers (S) | No shell, no SSH, no remote admin port in either image. | Operator ensures no `kubectl exec` access beyond break-glass. |
| **SYS.1.6.A17** Execution of containers without privileges (S) | Both images run as non-root user `65532`. The chart sets `runAsNonRoot: true`, `allowPrivilegeEscalation: false`, and drops all capabilities. | Operator secures the container runtime and node. |
| **SYS.1.6.A18** Application services accounts (S) | User `65532` has no host-level privileges. | Operator maps UIDs if host integration is needed. |
| **SYS.1.6.A19** Integrating data stores into containers (S) | The chart mounts two `emptyDir` volumes (`/data`, `/tmp`) and, when the webhook is enabled, a read-only Secret volume for TLS certificates. The root filesystem is read-only. | Operator controls persistent volumes and network storage. |
| **SYS.1.6.A20** Securing configuration data (S) | The Helm chart and all Kubernetes manifests are version-controlled in the OCM repository. | Operator versions and audits their own values overrides. |
| **SYS.1.6.A21** Advanced security policies (H) | The chart sets `seccompProfile: RuntimeDefault`. | Operator adds AppArmor/SELinux profiles and network-level MAC if required. |
| **SYS.1.6.A23** Container immutability (H) | Root filesystem is read-only; only `emptyDir` mounts are writable. | — |

Requirements SYS.1.6.A22 (provision for examinations), SYS.1.6.A24 (host-based
intrusion detection), SYS.1.6.A25 (high availability of containerized
applications), and SYS.1.6.A26 (further isolation and encapsulation of
containers) are entirely the operator's responsibility and not influenced by
OCM's artifacts.

## APP.4.4 Kubernetes

APP.4.4 covers Kubernetes cluster operation, pod orchestration, and supporting
infrastructure. It must always be applied together with SYS.1.6. The
requirement IDs and English titles below are taken from the
[APP.4.4 PDF](https://www.bsi.bund.de/SharedDocs/Downloads/DE/BSI/Grundschutz/IT-GS-Kompendium_Einzel_PDFs_2023/06_APP_Anwendungen/APP_4_4_Kubernetes_Edition_2023.pdf)
(Edition 2023, February 2023).

| Requirement | OCM provides | Operator responsibility / gap |
| --- | --- | --- |
| **APP.4.4.A1** Planning the separation of applications (B) | — | Operator plans namespace layout, network zones, and cluster separation. |
| **APP.4.4.A2** Planning automation with CI/CD (B) | — | Operator plans CI/CD lifecycle and secrets handling for their own pipelines. |
| **APP.4.4.A3** Identity and permission management in Kubernetes (B) | The chart creates a dedicated `ServiceAccount` for the controller and binds it to a `ClusterRole` with least-privilege verbs on OCM custom resources. See [RBAC scope](#rbac-scope). | Operator manages cluster-wide RBAC, authentication, and restricts who can modify the OCM `ClusterRole`. |
| **APP.4.4.A4** Separation of pods (B) | The pod spec meets the Kubernetes restricted Pod Security Standard (`runAsNonRoot`, all capabilities dropped, seccomp `RuntimeDefault`). | Operator enforces Pod Security Admission or an equivalent policy engine and ensures kernel namespace isolation on nodes. |
| **APP.4.4.A5** Data backup in the cluster (B) | — | Operator backs up etcd, persistent volumes, and cluster configuration. |
| **APP.4.4.A6** Initialization of pods (S) | The controller uses no init containers; initialization happens inside the manager binary. | Operator ensures initialization of their own pods follows this requirement. |
| **APP.4.4.A7** Separation of networks in Kubernetes (S) | The chart includes an opt-in `NetworkPolicy` (`manager.networkPolicy.enabled`). When enabled, it restricts ingress to the health-probe port (and the metrics port when metrics are enabled) and limits egress to DNS and HTTPS/Kubernetes API by default. Rules are configurable via `manager.networkPolicy.egress` and `manager.networkPolicy.ingress`. | Operator must enable the policy, adjust rules as needed, and ensure the cluster's CNI enforces `NetworkPolicy`. |
| **APP.4.4.A8** Securing configuration files in Kubernetes (S) | Chart templates, CRDs, and RBAC manifests are version-controlled and annotated with Helm metadata. | Operator controls access to the Helm release secrets and the Git repository. |
| **APP.4.4.A9** Use of Kubernetes service accounts (S) | The chart creates a named `ServiceAccount` bound only to the controller's `ClusterRole`. The controller does not use the `default` Service Account. | Operator restricts token projection for pods that do not need API access. `automountServiceAccountToken` is not explicitly disabled in the chart for pods that need it — the controller requires API access. |
| **APP.4.4.A10** Securing automation processes (S) | — | Operator secures CI/CD tooling used to deploy OCM. |
| **APP.4.4.A11** Monitoring of containers (S) | The chart defines liveness (`/healthz`) and readiness (`/readyz`) probes with configurable timing. | Operator monitors pod health and integrates alerts. |
| **APP.4.4.A12** Securing infrastructure applications (S) | OCM images are stored in `ghcr.io` with signed, digest-pinned references. | Operator secures registry access, enables encrypted communication, and audits changes. |
| **APP.4.4.A13** Automated configuration auditing (H) | The release pipeline runs an OpenSCAP STIG scan against every image before publishing. | Operator runs CIS Kubernetes Benchmark, kube-bench, or equivalent tools against the cluster. |
| **APP.4.4.A18** Use of micro-segmentation (H) | The chart includes an opt-in `NetworkPolicy` that implements pod-level micro-segmentation when enabled (`manager.networkPolicy.enabled`). | Operator enables the policy and tightens the default rules for their namespace layout and registry topology. Requires a CNI that enforces `NetworkPolicy`. |

Requirements APP.4.4.A14 (use of dedicated nodes), APP.4.4.A15 (separation of
applications at node and cluster levels), APP.4.4.A16 (use of operators),
APP.4.4.A17 (attestation of nodes), APP.4.4.A19 (Kubernetes high availability),
APP.4.4.A20 (encrypted data storage for pods), and APP.4.4.A21 (regular restart
of pods) are entirely the operator's responsibility.

## RBAC Scope

The controller's `ClusterRole` grants cluster-wide `get`, `list`, and `watch`
on `secrets`, `configmaps`, and `serviceaccounts` in the core API group. It
also grants `create` on `serviceaccounts/token` and `events`. These permissions
are necessary because the controller resolves OCI registry credentials and
signing keys referenced by custom resources in any namespace.

Cluster-wide secret read access is a broad permission. SYS.1.6.A8 and
APP.4.4.A3 require that only authorized entities access credentials. Operators
should:

- Restrict who can edit the OCM `ClusterRole` and `ClusterRoleBinding`.
- Enable etcd encryption at rest so that Secrets stored in etcd are encrypted.
- Audit Secret access through the Kubernetes audit log.
- Consider namespace-scoped installations if the controller only needs to watch
  a subset of namespaces (not currently supported by the chart).

## BSI C5

The BSI
[Cloud Computing Compliance Criteria Catalogue (C5:2026)](https://www.bsi.bund.de/EN/Themen/Unternehmen-und-Organisationen/Informationen-und-Empfehlungen/Empfehlungen-nach-Angriffszielen/Cloud-Computing/Kriterienkatalog-C5/kriterienkatalog-c5.html)
defines 168 criteria for secure cloud services. C5 is an audit framework for
cloud service providers, not for individual workloads. If you operate OCM
inside a C5-audited cloud environment, the IT-Grundschutz measures described
above contribute to meeting several C5 criteria in the areas of asset
management, identity management, and operations security, but C5 compliance is
a property of the cloud service as a whole, not of a single component.
