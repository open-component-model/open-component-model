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

See the [SIG Handbook](https://github.com/open-component-model/open-component-model/blob/main/docs/community/SIGs/SIG-Handbook.md) for goals, roles, lifecycle, and how to propose a new SIG.

### SIG Runtime

<div class="person-section-grid my-3">
  {{< person-card name="Gergely Brautigam" role="Chair" github="Skarlso" company="Kubermatic" profile="Skarlso" >}}
  <div>

SIG Runtime owns the core runtime layer of OCM -- the Go language bindings, the unified CLI, and the Kubernetes controller. Its mission is to ensure that OCM components can be created, signed, transported, verified, and deployed reliably across any environment.

Concretely, SIG Runtime maintains the modular Go runtime library, a production-grade Kubernetes controller with native Flux and Kro integration, and the OCM CLI as the primary developer toolchain. It also curates reference patterns and documentation for authoring, curation, and dissemination of OCM components.

Channels, meeting time, and full scope are in the [SIG Runtime charter](https://github.com/open-component-model/open-component-model/tree/main/docs/community/SIGs/Runtime).

  </div>
</div>

### How to get involved

- **Join a SIG:** see [Contributing to an OCM SIG](https://github.com/open-component-model/open-component-model/blob/main/docs/community/SIGs/CONTRIBUTING.md).
- **Propose a new SIG:** follow [Section 2.3 of the Handbook](https://github.com/open-component-model/open-component-model/blob/main/docs/community/SIGs/SIG-Handbook.md#23-sig-creation--charter-requirements).
