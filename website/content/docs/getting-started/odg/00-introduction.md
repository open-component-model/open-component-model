---
title: "From Zero to Hero"
description: "Curated onboarding journey for Open Delivery Gear."
weight: 1
toc: true
---

Welcome to your onboarding journey for Open Delivery Gear (ODG)!
This page contains a curated reading list that presents several sources in a meaningful order.
Feel free to skip sections you are already confident with!

At the end of this guide you will understand:

- How ODG uses the semantic model of Open Component Model (OCM) to correlate metadata with artefacts
- How ODG implements a Kubernetes-native architecture to enable an extension-based approach
- How to run ODG locally and start building your own extensions!

> **Note:** Is there something you wish to improve during your journey? We highly appreciate feedback, whether via [GitHub Issue](https://github.com/open-component-model/open-delivery-gear/issues) or directly via [Pull Request](https://github.com/open-component-model/open-delivery-gear/tree/main/docs/website/contents).

---

## Motivation

Security and compliance are fundamental aspects of modern software development. Cloud-native methodology emphasises a shift-left approach for the entire software lifecycle, bringing security and compliance considerations earlier into the development process. With OCM serving as the foundation of the software lifecycle, a robust technical platform exists upon which to build powerful automations.

ODG builds upon this foundation by integrating security and compliance automations directly into the software lifecycle. This approach enables software teams to remain agile in an environment of constantly evolving requirements whilst maintaining a strong security posture and providing traceable, auditable assessment information to auditors and customers.

Let's start by elaborating on the fundamentals and core principles of OCM.

---

## About modelling software

> **Note:** Already familiar with OCM?
> Please skip to [Security and Compliance Automation with ODG](#security-and-compliance-automation-with-odg)

ODG is built upon the semantic model of OCM.
The following sections elaborate on the semantic model of OCM, and provide motivation and explanation for the rationale behind it.

### Why it matters

Start with the [Benefits of OCM](https://ocm.software/docs/overview/benefits-of-ocm/) to understand the rationale behind OCM and the problems it addresses. You'll discover the motivation for OCM's creation, its scope within the software delivery landscape, and how it enables better governance of software components across organisational boundaries.

### Understanding the Open Component Model

Explore the [OCM Core Model](https://ocm.software/docs/overview/the-ocm-core-model/) to grasp the fundamental concepts. This section covers the problem statement OCM addresses, defines software identity in the context of component management, and explains why decoupling software artefacts from their storage locations is essential for flexible and secure software delivery.

### How it works

Review [How OCM Works](https://ocm.software/docs/overview/how-ocm-works/) to understand the mechanics. You'll learn how OCM packages and transports software components, and discover the mechanisms that ensure supply-chain security throughout the delivery process.

### Working with OCM

The following sections provide guidance on how to install OCM in a local environment and how to create your first OCM component version.

#### Installing the CLI

Follow the [OCM CLI Installation Guide](https://ocm.software/docs/getting-started/install-the-ocm-cli/) to set up the OCM command-line interface on your system. This practical guide walks you through the installation process and verifies your environment is ready to work with OCM components.

#### Creating Component Versions

Work through the [Creating Component Versions Guide](https://ocm.software/docs/getting-started/create-component-versions/) to gain hands-on experience with the OCM CLI. You'll learn how to create your first component version, add resources and sources, and understand the practical workflow for managing components with OCM.

---

## Security and Compliance Automation with ODG

ODG allows subscription to OCM components, which upon new version release trigger ODG scans (e.g., vulnerability scans). ODG tracks the findings according to specified SLAs and provides a processing interface with assisted rescorings.
As the assessment information is correlated using OCM coordinates, the same benefits (especially transportability) are inherited.
Thus, ODG implements an end-to-end trust-but-verify scenario.

### Architecture

ODG employs an asynchronous architecture with eventual consistency, implemented as Kubernetes-native deployments. It leverages core Kubernetes concepts such as desired state, reconciliation loops, and custom resources, whilst providing multiple interaction points including custom HTTP APIs, a web UI, and native Kubernetes operations.

Read more: [ODG's System Architecture](/docs/concepts/odg/00-odg-architecture/)

### Extending ODG

ODG offers multiple extension points. As ODG provides an automation scheduling framework with correlated persistence layers, you can either integrate new data sources (such as additional security scanners) or process data extracted from ODG to implement custom audit automations.

For now, we'll focus on integrating a new data source.
Follow the guide linked below to learn how to set up a local development environment so you can start working on your first ODG extension.

Read more: [Contribute a new Extension](/docs/tutorials/odg/00-contributing-extension/)

---

## Final Words

Congratulations, you've successfully finished the ODG learning journey, you rock!
To jump directly into ODG development, we recommend looking at our [GitHub Issues](https://github.com/open-component-model/open-delivery-gear/issues?q=is%3Aissue%20state%3Aopen%20label%3Akind%2Fgood-first-issue).
We explicitly track beginner-friendly topics.
To get involved with the [Community](https://ocm.software/community/), feel free to reach out via our open-source channels or join our regular open community calls.
