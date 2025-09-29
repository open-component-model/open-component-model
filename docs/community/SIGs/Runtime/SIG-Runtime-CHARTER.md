# SIG Runtime — Charter

**Status:** Proposed  
**Last updated:** 2025-09-29

## Mission & Scope

### Mission

The SIG Runtime steers, develops, and maintains the **OCM Kubernetes Runtime Toolkit** aka OCM controller and its integrations so that OCM components can be deployed, orchestrated, verified, and operated reliably on Kubernetes clusters.

### Scope

- Design, implementation, and maintenance of the OCM Runtime toolkit.
- Integrations with **Kro** (Kubernetes resource orchestration) and **Flux** (GitOps workflows).
- Authoring and curating **patterns** and **best practices** for runtime deployments using OCM.
- Community enablement: docs, examples, troubleshooting guides, support channels, and triage for runtime-related topics.

## Deliverables

- Production-grade **Runtime Toolkit** releases with versioned artifacts and upgrade notes.
- **Integrations** with Kro and Flux, including examples and tests. Actively participate in upstream discussions, issues, and PRs.
- **Best-practice patterns**: reference docs, blueprints, Helm/Kustomize samples.
- **Operational docs**: install, day-2 ops, migration handling, security hardening.
- **Conformance & e2e tests** for runtime scenarios and supported OCM CLI and Kubernetes versions.
- **Public roadmap** and release notes.

## Responsibilities

1. **Development & Maintenance (Runtime Toolkit)**
   - Own controller APIs/CRDs and backward-compatibility guarantees.
   - Define support matrix and SLAs for support and deprecation policy.
   - Provide performance benchmarks and SLOs, e.g., for reconciliation latency and resource footprint.

2. **Integration with Kro (Kubernetes Resource Orchestration)**
   - ...

3. **Integration with Flux (GitOps)**
   - ...

4. **Patterns & Best Practices**
   - ...

5. **Community Support**
   - Run triage for runtime-related issues and questions.
   - Provide documentation, define and keep up support channels, and maintain a troubleshooting playbook.
   - Labeling/taxonomy for issues and PRs; SLA targets for response and review.

## Areas of Ownership (Code & Tests)

- **Primary:** OCM Runtime Toolkit GitHub code repositories and folders:
  - [open-component-model](https://github.com/morri-son/open-component-model) /kubernetes/controller
  - ...
- **Test ownership:** component-level tests and runtime e2e/conformance tests for owned code.

> Code/test ownership declarations will be reflected in `CODEOWNERS` and repository READMEs.

## Interfaces & Dependencies

- **External projects:** [Kro](https://kro.run), [Flux](https://fluxcd.io).
- **OCM internal** CLI, OCM library, OCM specification, website/docs (for docs publishing).
- **Contract management:** publish versioned compatibility matrix (e.g. for OCM CLI and Kubernetes versions) and breaking-change notices.

## Operating Model

> Where not specified, the OCM SIG Handbook governs process details (decision-making, conflict resolution, escalation).

### Roles

- **Chair(s):** _TBD_  
  Administrative lead(s); schedule meetings, ensure process adherence, represent SIG to the TSC.
- **Tech Lead(s):** _TBD (can potentially be the same as Chair)_
  Technical direction; approves designs/roadmaps; final reviewer on architectural changes.
- **Maintainers:** listed in repo `CODEOWNERS`.
- **Contributors:** anyone adhering to the CoC and contribution guidelines.

### Membership & Voting

- **Voting members:** Chair(s), Tech Lead(s), and designated maintainers of SIG-owned code.
- **Becoming a voting member:** nomination by an existing voting member and confirmation at a public SIG meeting.

### Meetings

- **Regular meeting:** bi-weekly, 30–60 min, recorded with public notes.
- ...

### Communication

- **GitHub:** issues/PRs labeled `sig/runtime`.
- **Slack Channel:** ?
- **Mailing list:** ?
- **Docs & notes:** under `docs/community/SIGs/SIG-Runtime/` and meeting notes folder.
