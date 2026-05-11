---
title: "Governance"
description: "OCM project governance, Technical Steering Committee, and Special Interest Groups"
toc: true
---

## Technical Steering Committee

<div class="person-section-grid my-3">
  {{< person-card name="Jakob Möller" role="Chair" github="jakobmoellerdev" company="SAP" profile="jakobmoellersap" >}}
  <div>

The Open Component Model project is governed by a Technical Steering Committee (TSC) operating under the [NeoNephos Foundation](https://neonephos.org). The TSC holds all technical oversight for the project -- it sets the direction, coordinates across working groups, and maintains the quality and integrity of the codebase and its processes.

Decisions are made by majority vote of the voting members, with a quorum of 50% required. Major changes -- such as charter amendments -- require a two-thirds supermajority and approval from LF Europe. The TSC meets monthly in public; meeting notes are published in the [steering directory](https://github.com/open-component-model/open-component-model/tree/main/docs/steering/meeting-notes) of the repository.

The full list of voting members, the project charter, and contribution guidelines can be found in the [open-component-model](https://github.com/open-component-model/open-component-model/tree/main/docs/steering) repository.

  </div>
</div>

## Special Interest Groups (SIGs)

SIGs are focused working groups that own specific areas of the project and move it forward. Following the example of CNCF projects, we defined a framework for SIGs to help contributors collaborate effectively.

The **[SIG Handbook](https://github.com/open-component-model/open-component-model/blob/main/docs/community/SIGs/SIG-Handbook.md)** covers:

- goals and motivation behind SIGs
- roles and responsibilities
- how to propose, establish, and run a SIG

### SIG Runtime

<div class="person-section-grid my-3">
  {{< person-card name="Gergely Brautigam" role="Chair" github="Skarlso" company="Kubermatic" profile="Skarlso" >}}
  <div>

SIG Runtime owns the core runtime layer of OCM -- the Go language bindings, the unified CLI, and the Kubernetes controller. Its mission is to ensure that OCM components can be created, signed, transported, verified, and deployed reliably across any environment.

Concretely, SIG Runtime maintains the modular Go runtime library, a production-grade Kubernetes controller with native Flux and Kro integration, and the OCM CLI as the primary developer toolchain. It also curates reference patterns and documentation for authoring, curation, and dissemination of OCM components.

The SIG participates in the monthly OCM community call and can be reached via the [mailing list](mailto:open-component-model-sig-runtime@lists.neonephos.org) or [Zulip chat](https://linuxfoundation.zulipchat.com/#narrow/channel/532975-neonephos-ocm-support). The full charter is in the [open-component-model](https://github.com/open-component-model/open-component-model/tree/main/docs/community/SIGs/Runtime) repository.

  </div>
</div>

### How to get involved

- **Curious?** Start with the [SIG Handbook](https://github.com/open-component-model/open-component-model/blob/main/docs/community/SIGs/SIG-Handbook.md).
- **Have a topic in mind?** Follow the handbook's steps to **propose a SIG** and gather interested contributors.
- **Prefer to join later?** Keep an eye on this page -- we'll announce and list new SIGs here.
- **Want to connect with others?** Visit the [Community]({{< relref "community/_index.md" >}}) page for community calls, and more.
