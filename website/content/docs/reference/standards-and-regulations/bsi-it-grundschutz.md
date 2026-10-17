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

## Automated Checks

Like the [ComplianceAsCode](https://github.com/ComplianceAsCode/content) BSI
profiles, OCM maps each requirement to the checks that cover it. The mapping
lives in `.github/benchmarks/bsi/` and the tables below are rendered from it.
After every build, the `Image scan / BSI IT-Grundschutz` job evaluates the
mapping against the reports of the image and chart scans (OCM's DISA GPOS SRG
supplement, the CIS Docker Benchmark with dockle, the OCI metadata and
provenance of each image, Trivy's Kubernetes and Dockerfile checks plus OCM's
own Trivy checks, and Kubescape's NSA/CISA and MITRE controls; see
[DISA STIG]({{< relref "docs/reference/standards-and-regulations/disa-stig.md" >}})
and [Security Benchmarks]({{< relref "docs/reference/standards-and-regulations/benchmarks.md" >}})).
It writes the result per requirement to the job summary and to the
`bsi-it-grundschutz` workflow artifact. It fails when a mapped check fails, or
when no scan evaluated it, so the mapping cannot drift from what the scanners
check. Accepted findings are listed on the Security Benchmarks page.

The status in the **Checked by** column describes the part OCM can influence:

- **automated**: every measure OCM provides for the requirement is checked.
- **partial**: some of OCM's measures are checked; the rest is described in
  the table.
- **manual**: OCM provides a measure that no image or chart scan can observe,
  such as a property of the release process or the repository; the table says
  why.
- **operator**: entirely the operator's responsibility; OCM's artifacts do not
  influence it.

Where the BSI maps a requirement to [Grundschutz++](https://github.com/BSI-Bund/Stand-der-Technik-Bibliothek),
the successor of the IT-Grundschutz Compendium, the Grundschutz++ controls are
listed with it (mapping `ITGS-to-GS++` of the BSI Stand-der-Technik-Bibliothek).
The BSI has not yet published machine-readable assessment content for
Grundschutz++, so OCM checks against the Compendium requirements.

## SYS.1.6 Containerization

SYS.1.6 covers container images, the container runtime, and the host system. It
applies whenever containerized services are operated. The requirement IDs and
English titles below are taken from the
[SYS.1.6 PDF](https://www.bsi.bund.de/SharedDocs/Downloads/DE/BSI/Grundschutz/IT-GS-Kompendium_Einzel_PDFs_2022/07_SYS_IT_Systeme/SYS_1_6_Containerisierung_Edition_2022.pdf)
(Edition 2022, February 2022). **(B)** = basic, **(S)** = standard,
**(H)** = increased protection needs.

{{< bsi-controls "sys-1-6" >}}

## APP.4.4 Kubernetes

APP.4.4 covers Kubernetes cluster operation, pod orchestration, and supporting
infrastructure. It must always be applied together with SYS.1.6. The
requirement IDs and English titles below are taken from the
[APP.4.4 PDF](https://www.bsi.bund.de/SharedDocs/Downloads/DE/BSI/Grundschutz/IT-GS-Kompendium_Einzel_PDFs_2023/06_APP_Anwendungen/APP_4_4_Kubernetes_Edition_2023.pdf)
(Edition 2023, February 2023).

{{< bsi-controls "app-4-4" >}}

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
