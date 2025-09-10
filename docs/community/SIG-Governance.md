# OCM SIG Governance Model

This document defines the governance framework for Special Interest Groups (SIGs) in the Open Component Model (OCM) project. It is designed to be lightweight and scalable for a small open source project, while ensuring transparency, consistency, and community alignment.

## 1. Purpose

SIGs are groups of contributors focused on specific technical or community topics within OCM. The governance model ensures clear roles, decision-making, and lifecycle management for all SIGs.

## 2. Roles & Responsibilities

- **Chair**: Facilitates meetings, represents the SIG, ensures process adherence.
- **Tech Lead**: Guides technical direction, reviews contributions, supports the Chair.
- **Members**: Regular contributors with voting rights.

At least one Chair and one Tech Lead is required. Due to the small size of the project, these roles can be held by the same person. Chairs and Tech Leads are appointed by consensus among Members and approved by the OCM Technical Steering Committee (TSC).

## 3. Decision-Making

- Prefer consensus; if consensus cannot be reached, a simple majority vote among Members decides.
- Major decisions (scope, leadership changes) require approval by the OCM Technical Steering Committee (TSC).
- All decisions must be documented in meeting notes in the `SIGs` folder of the open-component-model repository.
- Decisions requiring TSC approval need to be communicated to the TSC Chair for inclusion in the next TSC meeting agenda.

## 4. Conflict Resolution

- Attempt resolution within the SIG.
- If unresolved, escalate to the OCM TSC for mediation and final decision. The TSC will review the issue, facilitate discussion, and record the resolution in the community repository and meeting minutes.

## 5. SIG Lifecycle

Lifecycle management covers creation, operation, and archiving/dissolution of SIGs. For detailed steps and requirements, see the [SIG Lifecycle Guide](./SIG-Lifecycle.md).

- **Creation**: Submit a SIG proposal using the official template ([SIG Submission Template](./SIG-Submission-Template.md)). Requires TSC approval.
- **Operation**: Meet regularly, maintain documentation, communicate openly. See the [SIG Operations Guide](./SIG-Operations.md) for best practices.
- **Archiving/Dissolution**: SIGs may be archived by consensus or TSC decision if inactive or obsolete. See the lifecycle guide for process details.

## 6. Transparency

- All SIG documentation, meeting notes, and decisions must be public in the OCM community repository.

## References

- [Kubernetes SIG Governance](https://github.com/kubernetes/community/blob/master/committee-steering/governance/sig-governance.md)
- [CentOS SIG Governance](https://www.centos.org/about/governance/sigs/)
- [OCM SIGs List (sigs.yaml)](./sigs.yaml)

---
For questions or feedback, [open an issue in the OCM repository](https://github.com/open-component-model/open-component-model/issues).
