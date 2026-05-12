---
title: "Contributing to OCM"
description: "How to contribute to the Open Component Model project"
slug: "contributing"
toc: true
weight: 3
---

## Welcome

Thank you for your interest in contributing to the Open Component Model! Whether you are fixing a typo, reporting a
bug, adding a feature, or improving documentation - every contribution matters and helps the project grow.

This guide gives you a general overview of how to contribute. For repository-specific instructions (coding style,
testing, build setup), refer to the `CONTRIBUTING.md` in the root of the repository you want to work on.

## Where to Contribute

Most of the active development happens in the
[open-component-model](https://github.com/open-component-model/open-component-model) mono-repo. It contains the Go
bindings, CLI, Kubernetes controllers, and this website.

{{<callout context="caution" title="Legacy repositories" icon="outline/alert-triangle">}}
The [ocm](https://github.com/open-component-model/ocm) and
[ocm-controller](https://github.com/open-component-model/ocm-controller) repositories are legacy and no longer
actively developed. Please direct new contributions to the mono-repo above.
{{</callout>}}

## Finding Things to Work On

Not sure where to start? Most issues are tracked centrally in the
[ocm-project](https://github.com/open-component-model/ocm-project/issues) repository. Each repository also contains
issues specific to that repository. Here are some ways to find work:

- **Good first issues** - Look for issues labeled
  [`kind/good-first-issue`](https://github.com/search?q=org%3Aopen-component-model+label%3A%22kind%2Fgood-first-issue%22+state%3Aopen&type=issues)
  across our repositories. These are specifically chosen to be approachable for newcomers.
- **Bug reports** - Browse open issues and help fix bugs.
- **Documentation** - Improvements to documentation are always welcome. If you want to contribute to this website, see
  the [website contribution guidelines](https://github.com/open-component-model/open-component-model/blob/main/website/CONTRIBUTING.md)
  for guidance on structure and style.
- **Feature ideas** - If you have an idea for a new feature, open an issue first to discuss it with the maintainers
  before investing time in an implementation.

### Writing a Good Issue

A well-written issue helps maintainers understand and act on your report quickly. When filing a bug report, include:

- A clear and descriptive title.
- Steps to reproduce the problem.
- What you expected to happen versus what actually happened.
- Your environment (OS, Go version, OCM CLI version, Kubernetes version if applicable).

For feature requests, describe the use case and the problem you are trying to solve rather than jumping straight to a
proposed solution. This gives maintainers the context they need to evaluate the idea.

## How to Contribute

We follow the standard GitHub fork-and-pull workflow. The steps below use the
[open-component-model](https://github.com/open-component-model/open-component-model) mono-repo as an example, but the
same process applies to all repositories.

{{< steps >}}
{{< step >}}

#### Fork and clone the repository

[Fork the repository](https://docs.github.com/en/pull-requests/collaborating-with-pull-requests/working-with-forks/fork-a-repo#forking-a-repository)
on GitHub, then clone your fork locally:

```bash
git clone https://github.com/<your-username>/open-component-model.git
cd open-component-model
git remote add upstream https://github.com/open-component-model/open-component-model.git
```
{{< /step >}}

{{< step >}}

#### Create a branch for your changes

Always branch off the latest `main`:

```bash
git fetch upstream
git checkout -b my-feature-branch upstream/main
```
{{< /step >}}

{{< step >}}

#### Make your changes and commit

{{<callout context="note" title="Sign-off and signed commits" icon="outline/signature">}}
All commits must meet two requirements:

1. **DCO sign-off** - Add `-s` to your `git commit` command. This appends a `Signed-off-by` line to your commit
   message, certifying that you have the right to submit the code under the project's license ([Developer Certificate of Origin](https://developercertificate.org/)).
2. **Cryptographic signature** - Commits must be signed with a GPG or SSH key so GitHub can verify authorship.
   See the [GitHub signing guide](https://docs.github.com/en/authentication/managing-commit-signature-verification/signing-commits)
   for setup instructions.
{{</callout>}}

```bash
git add <files>
git commit -s -S -m "Brief description of your changes"
```

The `-s` flag adds the DCO sign-off line; the `-S` flag creates the cryptographic signature. If you have not set up
commit signing yet, follow the [GitHub signing guide](https://docs.github.com/en/authentication/managing-commit-signature-verification/signing-commits) first.
{{< /step >}}

{{< step >}}

#### Before you push

- **Read the repository's `CONTRIBUTING.md`** - It contains project-specific requirements such as coding style,
  required tools, and testing instructions. For the mono-repo, see the
  [root CONTRIBUTING.md](https://github.com/open-component-model/open-component-model/blob/main/CONTRIBUTING.md)
  and the [website CONTRIBUTING.md](https://github.com/open-component-model/open-component-model/blob/main/website/CONTRIBUTING.md)
  for website changes.
- **Run tests and linters locally** - Most repositories enforce these in CI. Running them locally first saves you a
  round-trip. Every repository has its own setup and configuration in its `Taskfile` or `Makefile` - use those
  commands rather than running tools directly, as they incorporate the correct versions and settings.
- **Keep your branch up to date** - Merge the latest `main` into your branch before submitting to avoid merge
  conflicts. There is no need to rebase because we squash all commits when merging a pull request.
- **Discuss large changes** - If you are planning significant or potentially controversial changes, please discuss them
  with the maintainers first - either in a GitHub issue, on
  [Slack](https://kubernetes.slack.com/archives/C05UWBE8R1D), or in the
  [community call](/community/#community-calls).
{{< /step >}}

{{< step >}}

#### Push and open a pull request

```bash
git push origin my-feature-branch
```

Then [open a pull request](https://docs.github.com/en/pull-requests/collaborating-with-pull-requests/proposing-changes-to-your-work-with-pull-requests/creating-a-pull-request?tool=webui)
from your fork's branch to the upstream repository's `main` branch on GitHub.

- **Write a clear PR description** - Explain what you changed and why. If your PR fixes an issue, reference it
  (e.g., `Fixes #123`). We squash all commits when merging, so your PR title and description become the final
  commit message.
{{< /step >}}
{{< /steps >}}

Once you open a pull request, CI checks run automatically (linting, tests, CodeQL analysis, DCO verification).
Maintainers will review your changes and may ask for adjustments - this is normal and part of the collaborative
process. Once approved and all checks pass, a maintainer will merge your pull request.

### What Reviewers Look For

To help your pull request move through review smoothly, keep these points in mind:

- **Tests** - All code changes must include appropriate tests. New features must include unit tests covering expected
  behavior and bug fixes must include a regression test that reproduces the original bug. PRs must not decrease overall
  test coverage - if existing uncovered code makes this impractical, explain in the PR description. This policy is
  required for [OpenSSF Best Practices](https://www.bestpractices.dev/) compliance.
- **Focused changes** - Keep pull requests scoped to a single concern. Avoid mixing unrelated refactors or formatting
  changes with functional work.
- **Commit hygiene** - Write a clear PR title and description. There is no need to clean up individual commits because
  we squash on merge, but the PR description should explain what changed and why.
- **Backwards compatibility** - If your change affects a public API or CLI command, call it out in the PR description so
  reviewers can evaluate the impact.

## Guideline for AI-Generated Code Contributions

As artificial intelligence evolves, AI-generated code is becoming valuable for many software projects, including
open-source initiatives. While we recognize the potential benefits of incorporating AI-generated content into our
open-source projects, there are certain requirements that need to be reflected and adhered to when making contributions.

When using AI-generated code contributions in OSS Projects, their usage needs to align with Open-Source Software values
and legal requirements. We have established these essential guidelines to help contributors navigate the complexities of
using AI tools while maintaining compliance with open-source licenses and the broader Open-Source Definition.

AI-generated code or content can be contributed to this project if the following conditions are met:

1. **Compliance with AI Tool Terms and Conditions:** Contributors must ensure that the AI tool's terms and conditions
   do not impose any restrictions on the tool's output that conflict with the project's open-source license or
   intellectual property policies. This includes ensuring that the AI-generated content adheres to the Open Source
   Definition.

2. **Filtering Similar Suggestions:** Contributors must use features provided by AI tools to suppress responses that
   are similar to third-party materials or flag similarities. Only contributions from AI tools with such filtering
   options are accepted. If the AI tool flags any similarities, contributors must review and ensure compliance with the
   licensing terms of such materials before including them in the project.

3. **Management of Third-Party Materials:** If the AI tool's output includes pre-existing copyrighted materials,
   including open-source code authored or owned by third parties, contributors must verify that they have the necessary
   permissions from the original owners. This typically involves ensuring that there is an open-source license or public
   domain declaration that is compatible with the project's licensing policies. Contributors must also provide
   appropriate notice and attribution for these third-party materials, along with relevant information about the
   applicable license terms.

4. **Employer Policies Compliance:** If AI-generated content is contributed in the context of employment, contributors
   must also adhere to their employer's policies. This ensures that all contributions are made with proper authorization
   and respect for relevant corporate guidelines.

## Getting Help

See the [Community Engagement]({{< relref "_index.md" >}}) page for communication channels, community calls, and
how to reach the team.

## Reporting Security Vulnerabilities

{{<callout context="danger" title="Do not open a public issue for security vulnerabilities" icon="outline/alert-triangle">}}
If you discover a security vulnerability, please report it responsibly through
[GitHub's private vulnerability reporting](https://docs.github.com/en/code-security/security-advisories/guidance-on-reporting-and-writing-information-about-vulnerabilities/privately-reporting-a-security-vulnerability)
instead of opening a public issue. Each repository in the organization has this feature enabled - navigate to the
**Security** tab of the affected repository to submit a report. This allows maintainers to assess and address the issue
before it is disclosed publicly.
{{</callout>}}

## License

The Open Component Model is licensed under the [Apache License 2.0](https://www.apache.org/licenses/LICENSE-2.0). By
contributing to any repository in the organization, you agree that your contributions will be licensed under the same
license.

## Code of Conduct

We want OCM to be a welcoming and harassment-free experience for everyone. All participants are expected to follow the
[NeoNephos Code of Conduct](https://github.com/neonephos/.github/blob/main/CODE_OF_CONDUCT.md).
