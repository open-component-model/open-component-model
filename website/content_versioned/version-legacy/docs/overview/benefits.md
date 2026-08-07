---
title: "Benefits of OCM"
description: "Security for your software supply chain"
icon: "✨"
weight: 12
toc: true
---

The Open Component Model (OCM) is an open-source toolset for secure software delivery.

- OCM gives you full visibility and control across the supply chain, streamlining compliance checks, security scans, and deployments.
- OCM works everywhere — cloud, on-premises, hybrid, and air-gapped environments.
- OCM integrates seamlessly with your existing tools and is easy to extend.

With OCM, you gain control, reduce risk, and keep your delivery approach adaptable.

## Why choose OCM

### Create a Software Bill of Delivery

With OCM, you can describe everything you deliver in a unified, machine-readable format. This enables you to create a *Software Bill of Delivery (SBoD)*. Unlike a Software Bill of Materials (SBOM), which lists all components inside an application, a **Software Bill of Delivery** focuses on **everything you need for a successful deployment** — including container images, Helm charts, configuration files, and binaries. It is a complete, verifiable record of **all deliverables and how to access them**.

### Protect Your Supply Chain

Security is built into OCM. You can **cryptographically sign and verify** every component in your supply chain to ensure its integrity or confirm its provenance.

Beyond signatures, OCM uses [**immutable, globally unique component identities**]({{<relref "coordinates.md">}}). These act like tracking IDs, linking all lifecycle phases. They make compliance checks, audits, and vulnerability scans easier and more reliable. With OCM, your software is fully traceable from build to deployment.

### Deploy Anywhere, Even Air-Gapped

You can **deliver across boundaries** and **deploy anywhere — public cloud, on-premises, or air-gapped environments**. OCM separates the identity of software artifacts from their location. Identities remain stable while locations can change as needed. You can store software artifacts in local registries, move them between systems, and work in environments with limited or no internet access — all without losing integrity or traceability.

### Works with Your Existing Tools

OCM seamlessly integrates with your current ecosystem. It is **compatible with any implementation technology**, whether container images, NPM packages, or binaries. You can manage both cloud-native and legacy software without rewriting existing tools or processes.

### Adapts to Your Needs

OCM is built for flexibility. Its **plugin system** lets you extend functionality without changing the core. You can integrate new technologies, customize workflows, and scale from small teams to enterprise environments. OCM ensures that your supply chain remains agile and future-proof.

### Committed to Open Source

OCM has open development and transparent governance. We welcome contributions of any kind. The design of OCM makes it easy to add new features, so anyone can suggest, review, and merge improvements in a transparent way. 

Our commitment to open source goes beyond OCM. We are active members of the open-source community and have maintainers in projects such as [kro](https://kro.run/), [Flux](https://fluxcd.io/), and [External Secrets Operator](https://external-secrets.io/latest/). We believe in open source and work to shape its future.

## Try OCM Out

Does OCM sound like the right fit for your project? Check out our [Getting Started]({{< relref "docs/getting-started/_index.md" >}}) guides to see how easy a secure delivery can be.
