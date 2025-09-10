# OCM SIG Handbook

This handbook provides a comprehensive guide for Special Interest Groups (SIGs) in the Open Component Model (OCM) project. It covers both governance and operational aspects, ensuring clarity, transparency, and ease of use for all contributors.

## 1. Introduction & Purpose

Special Interest Groups (SIGs) are groups of contributors focused on specific technical or community topics within OCM. SIGs help drive collaboration, innovation, and community alignment in the project.

## 2. Governance

### 2.1 Roles & Responsibilities

**Members:** All participants in the SIG, including Chair and Tech Lead. Members may contribute to discussions, meetings, and SIG activities.

**Voting Members:** Members who have voting rights in SIG decisions. Chair and Tech Lead are always voting members. Initial voting members must be listed in the SIG submission/charter. Additional voting members may be nominated and confirmed by a majority vote of existing voting members in a SIG meeting. Removal of voting members also requires a majority vote. Voting rights may be lost after 3 months of inactivity or by consensus of the SIG. Voting Members participate in all formal votes and major decisions.

**Chair:** Facilitates meetings, represents the SIG, ensures process adherence. At least one Chair is required; the role may be held by the same person as Tech Lead. The Chair is always a voting member.

**Tech Lead:** Guides technical direction, reviews contributions, supports the Chair. At least one Tech Lead is required; may be the same person as Chair. The Tech Lead is always a voting member.

### 2.2 Code of Conduct

All SIG members and activities are subject to the OCM Code of Conduct. See [CODE_OF_CONDUCT.md](../../CODE_OF_CONDUCT.md).

### 2.3 SIG Creation & Charter Requirements

To create a SIG, draft a charter outlining:

- Scope and mission of the SIG
- Responsibilities and deliverables
- Leadership roles (Chair, Tech Lead; both roles may be held by the same person)
- Initial Voting Members (Chair and Tech Lead must be listed and are always voting members)
- Meeting cadence and communication channels
- How the SIG interacts with other groups and the community
- Repository needs and code/test ownership statement

Decision-making and conflict resolution processes are defined in this handbook and do not need to be included in the charter.

Fill out the [SIG Submission Template](./SIG-Submission-Template.md) with all required info (purpose, scope, initial leadership, initial voting members, meeting cadence, communication channels, repository needs, and code/test ownership statement).

Submit your proposal and charter as a new issue in the OCM repository ([create issue](https://github.com/open-component-model/open-component-model/issues)). The OCM Technical Steering Committee (TSC) reviews and approves proposals through a formal vote. The proposal must be added to the TSC meeting agenda. Chair and Tech Lead must be proposed by the SIG and approved by the TSC.

Once approved, announce your SIG in the community (mailing list, Slack, etc.) and update documentation. Add your SIG to `sigs.yaml` (see example/template below) and update documentation as needed.

### 2.4 Decision-Making & TSC Approval

- Prefer consensus; if consensus cannot be reached, a simple majority vote among Voting Members decides.
- Quorum for votes is at least 50% of Voting Members present.
- Changes to the SIG charter, leadership, or the dissolution of a SIG require TSC approval.
- All decisions must be documented in meeting notes and made public in the OCM community repository.

### 2.5 SIG Lifecycle

- **Creation**: As described above.
- **Operation**: Meet regularly (at least every 8 weeks; cadence may be adjusted for project needs), maintain documentation, communicate openly, and manage code/test ownership.
- **Archiving/Dissolution**: SIGs may be archived or dissolved by consensus of SIG Voting Members or by decision of the OCM TSC if inactive, obsolete, or no longer aligned with project needs. Dissolution requires TSC approval. If a SIG is unable to regularly establish consistent quorum or fulfill its responsibilities for 3 or more months, it SHOULD be considered for retirement. If inactivity persists for 6 or more months, the SIG MUST be retired.

## 3. Operations

### 3.1 Meetings & Communication

- Schedule regular meetings (at least every 8 weeks; cadence may be adjusted for project needs).
- Keep public meeting notes and publish them in the `SIGs` folder of the community repo.
- Optionally record meetings and make them available to the community.
- SIG mailing lists should use the domain `lists.linuxfoundation.org` and be provisioned via LFX. Example: `sig-placeholdere@lists.linuxfoundation.org`.

### 3.2 Code Ownership & Repository Structure

- SIGs must specify in their charter and submission where work will be performed (dedicated repository or monorepo) and how code ownership is managed.
- All related work must happen in a project-owned GitHub organization and repository.
- SIGs are responsible for code, tests, issue triage, PR reviews, test-failure response, bug fixes, and ongoing maintenance in their area.

### 3.3 Onboarding & Membership

- New members should read the SIG Handbook and understand the SIG's scope, responsibilities, and meeting cadence.
- Join the SIG's communication channels (mailing list, Slack, etc.).
- Get access to relevant repositories and documentation.
- Attend the next scheduled SIG meeting and introduce yourself.
- Review open issues, PRs, and the SIG roadmap to understand current priorities.
- Ask questions and connect with the Chair, Tech Lead, or other members for guidance.
- Voting status is granted after regular participation and consensus of existing Voting Members.
