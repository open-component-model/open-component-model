# Release Process

For OCM release managers. CLI and Kubernetes Controller ship together in lockstep from a single workflow.

## How it works

* Development happens on `main`. Releases are cut from `releases/vX.Y` branches.
* Minor and patch releases only (no majors).
* One workflow run produces three tags on the same commit:
  * `v0.X.Y` (canonical, the GitHub release)
  * `cli/v0.X.Y` (website install scripts)
  * `kubernetes/controller/v0.X.Y` (Go module side tag)
* Cadence: one release per sprint (two weeks). RC at sprint start, promote previous RC at the next sprint start.

## Release Workflow Diagram

```mermaid
flowchart TD
    Start([workflow_dispatch<br/>release.yml on releases/vX.Y])
    Start --> Prepare[prepare<br/>compute RC version<br/>generate changelog<br/>decide set_latest]
    Prepare --> TagRC[tag_rc<br/>GPG-signed tags pushed:<br/>v0.X.Y-rc.N<br/>cli/v0.X.Y-rc.N<br/>kubernetes/controller/v0.X.Y-rc.N]
    TagRC --> Pipeline

    subgraph Pipeline["build_and_test - calls monorepo shared build & publish"]
        direction TB
        BuildCLI["build_cli<br/>cli build & test"]
        BuildCtl["build_controller<br/>controller build & test<br/>verify-chart, build, E2E"]
        Conf[conformance<br/>conformance.yml]
        PubCLI[publish_cli<br/>OCI image to GHCR]
        PubCtl[publish_controller<br/>image + Helm chart to GHCR]

        BuildCLI --> Conf
        BuildCtl --> Conf
        Conf --> PubCLI
        Conf --> PubCtl
    end

    Pipeline --> ReleaseRC[release_rc<br/>GitHub pre-release<br/>binaries + OCI tarballs + chart + changelog]
    ReleaseRC --> Gate{{release environment<br/>manual approval}}
    Gate --> Verify[verify_attestations<br/>CLI binaries, CLI OCI,<br/>controller image, chart]
    Verify --> Promote[promote_and_release_final<br/>tag v0.X.Y + cli/v0.X.Y + kubernetes/controller/v0.X.Y<br/>oras retag images, set :latest if applicable<br/>repackage chart, diff vs RC, push + attest<br/>publish final GitHub release]
    Promote --> End([Final release live])

    classDef rc fill:#e0f2fe,stroke:#0284c7,color:#0c4a6e
    classDef final fill:#dcfce7,stroke:#16a34a,color:#14532d
    classDef gate fill:#fef3c7,stroke:#d97706,color:#78350f

    class Prepare,TagRC,Pipeline,ReleaseRC,BuildCLI,BuildCtl,Conf,PubCLI,PubCtl rc
    class Verify,Promote final
    class Gate gate
```

Phase 1 (RC, blue) runs end-to-end without human intervention once you trigger the workflow with `dry_run=false`. The run then parks at the `release` environment gate. Approving it kicks off Phase 2 (final, green): attestation verification, tag promotion, chart repack with byte-equivalence check against the RC, and the final GitHub release. Anything failing in Phase 1 aborts before any user-facing tags or releases are visible; anything failing in Phase 2 leaves the RC intact so you can re-approve after fixing the underlying issue.

## Release responsible

Rotates each sprint. Decides if a release is needed, runs the workflows, approves the gate, handles patches.

## Sprint checklist

```markdown
* [ ] Release branch created (e.g. `releases/v0.7`)
* [ ] RC released and verified (`v0.7.0-rc.1`)
* [ ] Previous RC promoted to final (`v0.6.0-rc.N` -> `v0.6.0`)
* [ ] Final release visible on GitHub Releases (`v0.6.0`)
```

## 1. Create the release branch

Run [Create OCM Release Branch](https://github.com/open-component-model/open-component-model/actions/workflows/release-branch.yml) with `target_branch=releases/vX.Y`.

> [!CAUTION]
> Once the branch exists, only bugfixes with docs should land on it. Needs release-responsible approval.
> Fix the issue on `main`, then once merged, `cherry-pick` the commit to the release branch.

## 2. Create a release candidate

Run [Release](https://github.com/open-component-model/open-component-model/actions/workflows/release.yml):

1. Set **"Use workflow from"** to the release branch.
2. `dry_run=true` first. Check the prepare summary: next RC version, next final version, set-latest decision.
3. `dry_run=false` to actually cut the RC.

The workflow builds CLI + controller, runs conformance, then publishes the RC as a GitHub pre-release with all assets attached.

> [!IMPORTANT]
> Always dry-run first. When patching an older minor (e.g. `v0.6.1` while `v0.7.0` exists), `set_latest=false` is required. That prevents `latest` from rolling backwards.

## 3. Promote RC to final

Same workflow, same run. After conformance passes, the run pauses on the `release` environment gate.

1. Wait for the RC
2. Open the workflow run, approve the environment gate. _Approval should only happen for RCs that were created and tested
   during the previous sprint._
3. The workflow verifies all attestations, re-packages the chart with final version strings, pushes everything, and publishes the GitHub release.

If attestation verification fails the run aborts. This shouldn't happen, but if it does, investigate why the attestation
failed and fix the root cause before creating a new release!

## 4. Patch release

Critical fix needs to land on an older minor:

```bash
git checkout releases/vX.Y
git cherry-pick <commit-from-main>
gh pr create --base releases/vX.Y \
  --title "chore(releases-041): <original PR>"
```

After merge, run the Release workflow on that branch. On hot-fixes we do not wait for the entire promotion process.

## Release notes

Generated by [git-cliff](https://git-cliff.org) from conventional commits. No manual editing for normal runs.

### Retract a release

1. Mark the GitHub release as pre-release.
2. Prepend a retraction notice to the body:
   ```markdown
   > **RETRACTED**: superseded by vX.Y.Z due to <reason>.
   ```
3. [Retract](https://go.dev/ref/mod#go-mod-file-retract) the release from the go.mod file as well.
4. Ship a patch release with the fix. File a security advisory if relevant.

OCI tags stay live in GHCR for users already pinned to the version.

## Verifying release tags

All release tags are GPG-signed.

```bash
curl -sSL https://raw.githubusercontent.com/open-component-model/open-component-model/main/website/static/gpg/OCM-RELEASES-PUBLIC-CURRENT.gpg | gpg --import
git tag -v v0.7.0
```

Key fingerprint: `36BDEEDF40C1A3077DE4A9D9F11241A047C49B13`.
