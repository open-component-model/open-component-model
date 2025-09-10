# SIG Lifecycle Guide

This guide describes the lifecycle management for Special Interest Groups (SIGs) in the OCM project, including creation, operation, and archiving/dissolution.

## 1. Creation

- Read the SIG Governance Model and Lifecycle Guide to understand the process and requirements.

Draft your SIG charter, outlining:
  - Scope and mission of the SIG
  - Responsibilities and deliverables
  - Leadership roles (Chair, Tech Lead; both roles may be held by the same person)
  - Meeting cadence and communication channels
  - How the SIG interacts with other groups and the community

The decision-making and conflict resolution processes are defined in the SIG Governance and Lifecycle documents and do not need to be included in the charter.
  
- Fill out the SIG Submission Template with all required info (purpose, scope, initial leadership, meeting cadence, communication channels, repository needs, and code/test ownership statement).
- Submit your proposal and charter as a new issue in the OCM repository ([create issue](https://github.com/open-component-model/open-component-model/issues)).
- The OCM Technical Steering Committee (TSC) reviews and approves proposals through a formal vote. The proposal must be added to the TSC meeting agenda. Chair and Tech Lead must be proposed by the SIG and approved by the TSC.
- Once approved, announce your SIG in the community (mailing list, Slack, etc.) and update documentation.
- Add your SIG to `sigs.yaml` (see example/template below) and update documentation as needed.

## SIG Charter Approval Process

After submitting a SIG proposal and charter (as a GitHub issue), the following approval process applies:

1. **Announcement & Public Review:** Announce the proposal and charter in the community (e.g., mailing list, Slack, repo) and invite feedback for a defined period (e.g., 2 weeks).
2. **Feedback Collection:** Community members can provide comments and suggestions on the issue or via designated channels.
3. **TSC Review & Discussion:** The OCM Technical Steering Committee (TSC) reviews all feedback, discusses the proposal, and may request changes or clarifications.
4. **Formal Approval:** The TSC formally approves the charter and leadership (Chair/Tech Lead) through a vote, records the decision in meeting minutes, and links the approved charter in the community documentation.

Only changes to the SIG charter, leadership, or the dissolution of a SIG require TSC approval.

## 2. Operation

- Schedule regular meetings (at least every 8 weeks; cadence may be adjusted for project needs).
- Use the Meeting Notes Template (see Operations Guide) and link notes from your SIG page in the community repo.
- Communicate openly in public forums and project-owned repositories.
- Maintain up-to-date documentation and a simple roadmap for SIG activities and enhancements.
- SIGs are responsible for code, tests, issue triage, PR reviews, and bug fixes in their area. SIGs must specify in their charter and submission where work will be performed (dedicated repository or OCM monorepo) and how code ownership is managed.
- Onboard new members using the docs and templates provided.
- SIGs may provide an annual summary of activities (optional).

## 3. Archiving/Dissolution

- Discuss retirement/archiving with SIG members and the TSC.
- Announce the retirement to the community (mailing list, Slack, etc.).
- Archive SIG documentation and repositories.
- Update `sigs.yaml` and the community documentation to reflect the change.
- Record the retirement in meeting minutes and the OCM repository.
- SIGs may be archived or dissolved by consensus of SIG voting members or by decision of the OCM TSC if inactive, obsolete, or no longer aligned with project needs. Dissolution requires TSC approval.
- If a SIG is unable to regularly establish consistent quorum or fulfill its Organizational Management responsibilities for 3 or more months, it SHOULD be considered for retirement.
- If inactivity persists for 6 or more months, the SIG MUST be retired.
- Archiving process includes: announcement to the community, archiving documentation and repositories, and updating the community repo.

For a machine-readable list of SIGs and their details, see the example/template in `sigs.yaml`.

## Voting Members

Voting Members are contributors who regularly participate in SIG meetings and activities. Admission as a Voting Member requires consensus of existing Voting Members and is documented in meeting notes. Voting rights may be lost after 3 months of inactivity or by consensus of the SIG. Voting Members participate in all formal votes and major decisions.

## Code of Conduct

All SIG members and activities are subject to the OCM Code of Conduct. See [CODE_OF_CONDUCT.md](../../CODE_OF_CONDUCT.md).

## Mailing Lists

SIG mailing lists should use the domain `lists.linuxfoundation.org` and be provisioned via LFX. Example: `sig-runtime@lists.linuxfoundation.org`.

---
For questions or feedback, [open an issue in the OCM repository](https://github.com/open-component-model/open-component-model/issues).
