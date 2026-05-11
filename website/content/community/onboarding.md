---
title: "Developer Onboarding"
description: >-
  New to the project? Understand what OCM is, how the repo is structured,
  and where to start.
slug: "onboarding"
toc: true
hasMermaid: true
weight: 2
---

Welcome to the Open Component Model (OCM). This page gives you everything you need to orient yourself in the project,
understand how it is built, and find the right starting point for your interests.

{{<callout context="note" title="Prerequisites" icon="outline/info-circle">}}

- **To use the OCM CLI**

  Basic command-line experience and familiarity with container images or software packaging concepts.

- **To use the Kubernetes controllers**

  Working knowledge of Kubernetes (clusters, manifests, custom resources).

- **To contribute code**

  The core libraries, CLI, and controllers are written in Go (1.26+) and use [Task](https://taskfile.dev/) as their build
runner. Contributions in other areas - such as language bindings, documentation, the website, or tooling - may use
different languages and are equally welcome.
{{</callout>}}

## What is OCM?

OCM is a standard and toolkit for describing, signing, transporting, and deploying software artifacts as a single,
versioned unit. These overview pages explain the model in depth:

{{< card-grid >}}
{{< link-card title="How OCM Works" href="/docs/overview/how-ocm-works/" description="The Pack-Sign-Transport-Deploy lifecycle." >}}
{{< link-card title="Benefits of OCM" href="/docs/overview/benefits-of-ocm/" description="Supply chain protection, traceability, and air-gap support." >}}
{{< link-card title="The OCM Core Model" href="/docs/overview/the-ocm-core-model/" description="Components, resources, sources, references, and identity." >}}
{{< /card-grid >}}

## The Mono-Repository

All active OCM development happens in a single repository:
[open-component-model](https://github.com/open-component-model/open-component-model). The mono-repo contains the Go
library, the CLI, the Kubernetes controllers, conformance tests, and this website - all sharing one dependency tree and
one test suite.

```mermaid
graph TD
    Root["open-component-model/"]
    Bindings["bindings/\nLanguage bindings (currently Go)"]
    CLI["cli/\nOCM CLI"]
    K8s["kubernetes/controller/\nOCM K8s Toolkit"]
    Website["website/\nProject website"]
    Conformance["conformance/\nConformance tests"]
    Docs["docs/\nCommunity docs & governance"]

    Root --> Bindings
    Root --> CLI
    Root --> K8s
    Root --> Website
    Root --> Conformance
    Root --> Docs
```

{{<callout context="caution" title="Legacy repositories" icon="outline/alert-triangle">}}
The [ocm](https://github.com/open-component-model/ocm) and
[ocm-controller](https://github.com/open-component-model/ocm-controller) repositories are the previous generation of
OCM tooling. They are maintained but no longer receive new features. All new development targets the mono-repo above.
Read the [OCM v2 announcement]({{< relref "blog/ocm_v2_announcement.md" >}}) for background on the rewrite.
{{</callout>}}

## Technical Layers

OCM is built as a stack of layers. Each layer builds on the one below it:

```mermaid
flowchart TD
    Spec["OCM Specification\n(defines the model)"]
    Go["Go Bindings\n(bindings/go/)"]
    CLI["OCM CLI\n(cli/)"]
    Controllers["OCM K8s Toolkit\n(kubernetes/controller/)"]
    ODG["Open Delivery Gear\n(separate repository, Python)"]

    Spec --> Go
    Spec -.->|"implements spec\n(via Python/cc-utils)"| ODG
    Go --> CLI
    Go --> Controllers
```

**OCM Specification** - The formal standard that defines how components, resources, and signatures are represented. It
is technology-agnostic and lives in its own repository:
[ocm-spec](https://github.com/open-component-model/ocm-spec).

**Go Bindings** (`bindings/go/`) - The reference implementation of the specification in Go. The `bindings/` directory is
structured to welcome implementations in other languages in the future. This library provides the core types and
operations (creating, signing, resolving, transferring component versions) that the CLI and controllers build on.

**OCM CLI** (`cli/`) - A command-line tool for the Pack-Sign-Transport workflow. Built on the Go bindings,
it is designed for interactive use and CI/CD pipelines. Start with
[Install the OCM CLI]({{< relref "docs/getting-started/ocm-cli-installation.md" >}}).

**OCM K8s toolkit** (`kubernetes/controller/`) - A set of controllers that handle deployment and verification of
OCM component versions in Kubernetes clusters. They use a dependency chain of custom resources: Repository, Component,
Resource, and Deployer. Read more in the [Kubernetes Controllers]({{< relref "docs/concepts/ocm-controllers.md" >}})
concept page.

**Open Delivery Gear (ODG)** - A compliance automation engine that subscribes to OCM component versions and
continuously scans delivery artifacts for security and compliance issues. ODG tracks findings against configurable SLAs,
supports assisted rescoring, and provides a Delivery Dashboard UI for both platform operators and application teams. It
is designed for public and sovereign cloud scenarios where trust-but-verify assurance is required. ODG lives in its own
repository: [open-delivery-gear](https://github.com/open-component-model/open-delivery-gear).

## Getting Started

The getting-started guides walk you through the full workflow - from installing the CLI to deploying with Kubernetes
controllers. The first two guides (CLI installation and creating a component version) require no Kubernetes knowledge.

{{< card-grid >}}
{{< link-card title="Getting Started" href="/docs/getting-started/" description="Install the CLI, create your first component version, set up controllers, and deploy." >}}
{{< /card-grid >}}

## Advanced Topics

Once you are comfortable with the basics, explore these concept pages for a deeper technical understanding:

{{< card-grid >}}
{{< link-card title="Component Identity" href="/docs/concepts/component-identity/" description="Naming, versioning, and coordinate notation." >}}
{{< link-card title="Signing and Verification" href="/docs/concepts/signing-and-verification/" description="Digest calculation, normalization, and trust models." >}}
{{< link-card title="Transfer and Transport" href="/docs/concepts/transfer-and-transport/" description="Moving components between registries and air-gapped environments." >}}
{{< link-card title="Kubernetes Controllers" href="/docs/concepts/kubernetes-controllers/" description="Reconciliation chain and controller architecture." >}}
{{< link-card title="Plugin System" href="/docs/concepts/plugin-system/" description="Extending OCM with custom repository types, credentials, and signing handlers." >}}
{{< /card-grid >}}

## Project Organization

OCM is an open standard contributed to the [Linux Foundation](https://www.linuxfoundation.org/) under the
[NeoNephos Foundation](https://neonephos.org/). A Technical Steering Committee (TSC) provides technical oversight,
sets project direction, and coordinates across working groups. Specific technical areas are owned by Special Interest
Groups (SIGs). Currently, **SIG Runtime** maintains the Go bindings, CLI, and Kubernetes controllers.

{{< card-grid >}}
{{< link-card title="Governance" href="/governance/" description="TSC membership, project charter, and SIG framework." >}}
{{< link-card title="How We Work" href="/community/how-we-work/" description="Meetings, planning rituals, project boards, and decision-making." >}}
<!-- markdownlint-disable MD034 -- bare URLs are expected in shortcode href attributes -->
{{< link-card title="SIG Handbook" href="https://github.com/open-component-model/open-component-model/blob/main/docs/community/SIGs/SIG-Handbook.md" description="How to join or propose a new Special Interest Group." >}}
{{< link-card title="TSC Meeting Notes" href="https://github.com/open-component-model/open-component-model/tree/main/docs/steering/meeting-notes" description="Public monthly meeting notes from the Technical Steering Committee." >}}
<!-- markdownlint-enable MD034 -->
{{< /card-grid >}}

## Contributing and Engaging

Ready to contribute or connect with the community? If you are looking for something to work on, check the
[`kind/good-first-issue`](https://github.com/search?q=org%3Aopen-component-model+label%3A%22kind%2Fgood-first-issue%22+state%3Aopen&type=issues)
label across our repositories.

{{< card-grid >}}
{{< link-card title="Contributing Guide" href="/community/contributing/" description="Fork-and-pull workflow, pull request process, DCO sign-off, and AI-generated code guidelines." >}}
{{< link-card title="Community Engagement" href="/community/" description="Communication channels (Slack, Zulip), community calls, and how to reach the team." >}}
{{< /card-grid >}}
