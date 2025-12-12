# SIG Runtime — Charter

**Status:** Proposed  
**Last updated:** 2025-10-16

## Mission & Scope

### Mission

The SIG Runtime maintains and shapes:

- **OCM Language Bindings** ([bindings](https://github.com/open-component-model/open-component-model/tree/main/bindings))
- **OCM CLI** ([cli](https://github.com/open-component-model/open-component-model/tree/main/cli))
- **OCM Kubernetes Controller** ([kubernetes/controller](https://github.com/open-component-model/open-component-model/tree/main/kubernetes/controller))
- **Legacy version of OCM Library and CLI (only maintenance)** ([ocm](https://github.com/open-component-model/ocm/tree/main))

The mission of SIG Runtime is to allow OCM components to be:

- creatable
- signable
- transportable
- verifiable
- deployable

### Scope

- **OCM Language Bindings**
  - Modular, extensible runtime library for components and versions
  - Support for dynamic creation of component versions
  - Cryptographic standard-compliant signing and verification per the [OCM Specification](https://github.com/open-component-model/ocm-spec/tree/main)
  - Introspection, transport, and transformation of components and versions

- **OCM CLI**
  - Unified toolchain to create, inspect, sign, verify, and transport components versions
  - CLI commands and flags should mirror the capabilities and behavior of the language bindings

- **OCM Kubernetes Controller**
  - Native Kubernetes API for OCM repositories, components, and artifacts
  - Seamless integration with mainstream deployment frameworks (e.g., Flux, Kro)
  - Controllers to synchronize, verify, and deploy OCM component versions within clusters

- **Patterns and Best Practices**
  - Authoring, curation, and dissemination of runtime deployment patterns
  - Reference implementations, blueprints, and operational guidance for the community

## Deliverables

- **OCM Language Bindings** - Go libraries, maintained as a first-class module with regular, versioned releases

- **OCM Kubernetes Controller** - Production-grade controller with predictable release cadence

- **Best-Practice Patterns** - Reference documentation, blueprints, and deployment samples (Helm, Kustomize)

- **Operational Documentation** - Installation, upgrades, Day 2 operations, migration strategies, and security hardening

- **Public Roadmap and Release Notes** - Transparent planning and lifecycle communication for the community

## Responsibilities

- **Development & Maintenance (Bindings and Controller)**
  - Own and evolve controller APIs/CRDs with backward-compatibility guarantees
  - Define support matrix, SLAs, and deprecation policy
  - Deliver performance benchmarks and SLOs (e.g., reconciliation latency, resource footprint)
  - Maintain security posture: dependency scanning, CVE triage, patch workflow
  - Manage releases: versioning, changelogs, upgrade guidance

- **Patterns & Best Practices**
  - Curate and update reference deployment patterns (Helm and Kustomize)
  - Provide tested blueprints for multi-environment delivery, signing, and verification pipelines
  - Publish operational hardening guides (RBAC, audit logging, secret management)
  - Incorporate adoption feedback into evolving best practices

- **Community Support**
  - Run triage for runtime issues and provide timely support
  - Maintain documentation, troubleshooting guides, and support channels
  - Define issue/PR taxonomy and enforce SLA targets for response and review
  - Onboard contributors with clear guides, examples, and “good first issue” tracking
  - Facilitate public demos, office hours, and knowledge-sharing sessions

## Areas of Ownership (Code & Tests)

- **Primary Code Repositories:**
  - **OCM Language Bindings** — [bindings](https://github.com/open-component-model/open-component-model/tree/main/bindings)
  - **OCM Kubernetes Controller** — [kubernetes/controller](https://github.com/open-component-model/open-component-model/tree/main/kubernetes/controller)
  - **OCM CLI** — [cli](https://github.com/open-component-model/open-component-model/tree/main/cli)

- **Test Strategy**
  - Component-level unit and integration tests for owned code
  - End-to-end and conformance test suites for the runtime

> Ownership will be explicitly declared in `CODEOWNERS` files and documented in repository `README`s.

## Interfaces & Dependencies

- **External Integrations that are regularly tested:**
  - [Kro](https://kro.run) for dependency orchestration
  - [Flux](https://fluxcd.io) for delivery and deployment

- **OCM Internal** — Controllers and CLI consume the [bindings](https://github.com/open-component-model/open-component-model/tree/main/bindings)

- **Specification Compliance** — All runtime libraries must implement the latest [OCM Specification](https://github.com/open-component-model/ocm-spec/) to the fullest extent possible

---

## Operating Model

> Unless otherwise specified here, processes follow the **OCM SIG Handbook** (decision-making, conflict resolution, escalation).

### Roles

- **Chair** — Jakob Möller (SAP) - @jakobmoellerdev
  Provide administrative leadership, schedule and run meetings, ensure process adherence, and represent the SIG to the TSC.

- **Tech Lead** — Fabian Burth (SAP) - @fabianburth
  Define technical direction, approve designs and roadmaps, and act as final reviewers for architectural changes.

- **Maintainers**  
  Defined in repository [`CODEOWNERS`](https://github.com/open-component-model/open-component-model/blob/main/.github/CODEOWNERS); responsible for code quality, reviews, and releases.

- **Contributors**  
  Any community member following the Code of Conduct and contribution guidelines.

### Membership & Voting

- **Voting members:** Chair, Tech Lead, and designated maintainers of SIG-owned code.
- **Becoming an additional voting member:** Nomination by an existing voting member and confirmation at a public SIG meeting.

### Meetings

**Participation in [OCM Community Call](https://ocm.software/community/engagement/#community-calls):** Regular updates and discussions held within the OCM shared community call.

### Communication

- **Zulip Channel:** [neonephos-ocm-support](https://linuxfoundation.zulipchat.com/#narrow/channel/532975-neonephos-ocm-support)
- **Slack Channel in Kubernetes Slack (_deprecated_):** [#open-component-model-sig-runtime](https://kubernetes.slack.com/archives/C05UWBE8R1D)
- **Mailing list:** `open-component-model-sig-runtime@lists.neonephos.org`
- **Docs & notes:** under [docs/community/SIGs/Runtime/](.) and meeting notes folder. Technical decisions are centrally tracked and aligned with TSC via [ADRs](./../../../adr)
