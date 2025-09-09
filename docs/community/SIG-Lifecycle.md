# SIG Lifecycle Guide

This guide describes the lifecycle management for Special Interest Groups (SIGs) in the OCM project, including creation, operation, and archiving/dissolution.

## 1. Creation

- Read the SIG Governance Model and Lifecycle Guide to understand the process and requirements.
- Draft your SIG charter (see example [Kubernetes SIG Charter](https://github.com/kubernetes/community/blob/master/committee-steering/governance/README.md)), outlining:

  - Scope and mission of the SIG
  - Responsibilities and deliverables
  - Leadership roles (Chair, Tech Lead, etc.)
  - Membership criteria (if any)
  - Decision-making and voting process
  - Meeting cadence and communication channels
  - Conflict resolution and escalation process
  - How the SIG interacts with other groups and the community
  - Any additional processes or policies relevant to the SIG
  
- Fill out the SIG Submission Template with all required info (purpose, scope, initial leadership, meeting cadence, communication channels, repository needs).
- Submit your proposal and charter as a new issue in the OCM repository ([create issue](https://github.com/open-component-model/open-component-model/issues)).
- The OCM Technical Steering Committee (TSC) reviews and approves proposals.
- Once approved, announce your SIG in the community (mailing list, Slack, etc.) and update documentation.
- Add your SIG to `sigs.yaml` and update documentation as needed.

## SIG Charter Approval Process

After submitting a SIG proposal and charter (as a GitHub issue), the following approval process applies:

1. **Announcement & Public Review:** Announce the proposal and charter in the community (e.g., mailing list, Slack, repo) and invite feedback for a defined period (e.g., 2 weeks).
2. **Feedback Collection:** Community members can provide comments and suggestions on the issue or via designated channels.
3. **TSC Review & Discussion:** The OCM Technical Steering Committee (TSC) reviews all feedback, discusses the proposal, and may request changes or clarifications.
4. **Formal Approval:** The TSC formally approves the charter, records the decision in meeting minutes, and links the approved charter in the community documentation.

This process ensures transparency, community involvement, and clear documentation for all new SIGs.

## 2. Operation

- Schedule regular meetings (at least every 4-6 weeks; cadence may be adjusted for project needs).
- Use the Meeting Notes Template (see Operations Guide) and link notes from your SIG page in the community repo.
- Communicate openly in public forums and project-owned repositories.
- Maintain up-to-date documentation and a simple roadmap for SIG activities and enhancements.
- SIGs are responsible for code, tests, issue triage, PR reviews, and bug fixes in their area.
- Onboard new members using the docs and templates provided.
- SIGs may sponsor working groups for focused efforts.
- SIGs may provide an annual summary of activities (optional).

## 3. Archiving/Dissolution

- Discuss retirement/archiving with SIG members and the TSC.
- Announce the retirement to the community (mailing list, Slack, etc.).
- Archive SIG documentation and repositories.
- Update `sigs.yaml` and the community documentation to reflect the change.
- Record the retirement in meeting minutes and the OCM repository.
- SIGs may be archived or dissolved by consensus of SIG members or by decision of the OCM TSC if inactive, obsolete, or no longer aligned with project needs.
- If a SIG is unable to regularly establish consistent quorum or fulfill its Organizational Management responsibilities for 3 or more months, it SHOULD be considered for retirement.
- If inactivity persists for 6 or more months, the SIG MUST be retired.
- Archiving process includes: announcement to the community, archiving documentation and repositories, and updating the community repo.

For a machine-readable list of SIGs and their details, see [sigs.yaml](./sigs.yaml).

---
For questions or feedback, [open an issue in the OCM repository](https://github.com/open-component-model/open-component-model/issues).
