---
title: "How We Work"
description: >-
  Meetings, rituals, project boards, and decision-making processes
  in the Open Component Model project.
slug: "how-we-work"
toc: true
weight: 4
---

This page describes how the OCM team organizes its day-to-day development work. Most meetings listed here are open to
the public - anyone is welcome to join, listen, or participate. The Retrospective is invite-only to maintain a safe
space for feedback.

## How We Structure Work

All work items are GitHub issues tracked on the
[OCM Backlog Board](https://github.com/orgs/open-component-model/projects/10). Work is organized in a hierarchy:

| Type | Prefix / Label | Scope | Purpose | Template |
| --- | --- | --- | --- | --- |
| Initiative | `Initiative:` | Multiple quarters | Communicates vision and progress to stakeholders by aggregating <u>**epics**</u> | - |
| Epic | `EPIC:` (`epic` type) | ~1 quarter | Organizes <u>**tasks**</u> into a manageable chunk that tracks progress towards an <u>**initiative**</u> | [epic](https://github.com/open-component-model/ocm-project/blob/main/.github/ISSUE_TEMPLATE/epic.md) |
| Task | `task` type | 1 sprint | Documents a small unit of work within an <u>**epic**</u> so progress is traceable and hand-off is possible | [task](https://github.com/open-component-model/ocm-project/blob/main/.github/ISSUE_TEMPLATE/task.md) |
| Spike | `spike` type | Time-boxed | Explores a question or proof-of-concept within an <u>**epic**</u>; result is open-ended | [spike](https://github.com/open-component-model/ocm-project/blob/main/.github/ISSUE_TEMPLATE/spike.md) |

## Project Board and Issue Tracking

All work is tracked on the
[OCM Backlog Board](https://github.com/orgs/open-component-model/projects/10/views/1) in the
[ocm-project](https://github.com/open-component-model/ocm-project) repository. The board has several views:

| View | Purpose |
| --- | --- |
| [Current Sprint](https://github.com/orgs/open-component-model/projects/10/views/20) | Active sprint work |
| [Next Sprint](https://github.com/orgs/open-component-model/projects/10/views/21) | Upcoming sprint queue |
| [Backlog](https://github.com/orgs/open-component-model/projects/10/views/1) | All open issues, prioritized |
| [Roadmap](https://github.com/orgs/open-component-model/projects/10/views/5) | Timeline view of planned work |
| [Epics](https://github.com/orgs/open-component-model/projects/10/views/16) | High-level feature tracks |

## How Issues Enter the Sprint

Anyone can propose work for an upcoming sprint. The process is:

1. **Create an issue** in the relevant repository (or in
   [ocm-project](https://github.com/open-component-model/ocm-project/issues) for cross-cutting work). Write a clear
   description following the appropriate issue template.
2. **Self-refine the issue** - ensure it has enough context for others to understand scope and intent.
3. **Add it to the project board** with status "Needs Refinement", assign an initial priority, and place it in the
   next sprint.
4. **Refinement discussion** - during the weekly refinement meeting the team discusses the issue. If everyone
   understands it, it is story-pointed and its priority is evaluated.
5. **Ready for sprint** - once refined, the issue moves to the "Next-Up" column and is available for sprint planning.

## Meetings

{{<callout context="note" title="Joining meetings" icon="outline/info-circle">}}
We are working on making all meeting links publicly discoverable. In the meantime, reach out on
[Zulip](https://linuxfoundation.zulipchat.com/#narrow/channel/532975-neonephos-ocm-support) or
[Slack](https://kubernetes.slack.com/archives/C05UWBE8R1D) to receive a calendar invite.
{{</callout>}}

| Meeting | Cadence | Purpose |
| --- | --- | --- |
| Daily Standup | Every workday | Casual sync - not mandatory, not necessarily work-related |
| Planning | Biweekly (Monday) | Review the [Next Sprint](https://github.com/orgs/open-component-model/projects/10/views/21) view, agree on sprint goals |
| Retrospective | Biweekly (Monday) | Reflect on what went well and what to improve (invited members only to maintain a safe space for feedback) |
| Refinement | Weekly (Thursday) | Discuss items in "Needs Refinement" on the [Next Sprint](https://github.com/orgs/open-component-model/projects/10/views/21) view, clarify scope, and story-point |
| Warroom | Every workday | Synchronous coordination on tasks or open topics |
| [Community Call]({{< relref "_index.md" >}}) | First Wednesday of the month | Project updates, demos, and open Q&A with the broader community |
| TSC Meeting | First Monday of the month | Governance decisions, SIG approvals ([meeting notes](https://github.com/open-component-model/open-component-model/tree/main/docs/steering/meeting-notes)) |

## Decision-Making

Day-to-day technical decisions are made by the contributors doing the work, in pull requests and issues. For
decisions that affect multiple areas or establish a precedent, the team uses
[Architecture Decision Records (ADRs)](https://github.com/open-component-model/open-component-model/tree/main/docs/adr).

Larger decisions that affect project direction are escalated to the TSC. The process is:

1. Discuss in a GitHub issue, SIG meeting, or on Slack/Zulip
2. If consensus is not reached, bring it to the TSC agenda
3. The TSC decides by majority vote (quorum: 50% of voting members)

For full governance details, see the [Governance]({{< relref "governance/_index.md" >}}) page and the
[Project Charter](https://github.com/open-component-model/open-component-model/blob/main/docs/steering/CHARTER.md).

## Communication Channels

For communication channels and how to reach the team, see the
[Community Engagement]({{< relref "_index.md" >}}) page.
