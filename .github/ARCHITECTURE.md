# CI & Release Infrastructure

Reference document for the OCM monorepo's GitHub Actions workflows, scripts, and configuration.
27 workflows organized into 7 categories: CI, Component Build, Release, Component Publishing, Website, Website Testing, and Maintenance.

## Key Architectural Principles

- **Dynamic module discovery**: `ci.yml` discovers Go modules via `task go_modules`, then filters by changed files on PRs (all modules on push to main or when the CI workflow itself changed).
- **ARM runners**: Most CI jobs run on `ubuntu-24.04-arm` for performance. Exception: `analyze-go` (CodeQL) stays on `ubuntu-latest` (no ARM64 binary). E2E and conformance jobs use a matrix strategy (`matrix.arch: [arm64]`) with dynamic runner selection.
- **Sparse checkouts**: Most workflows check out only the paths they need for efficiency.
- **RC-to-final promotion**: Releases go through Release Candidate, environment gate (manual approval), then final promotion. No rebuild for the final release -- same binaries, same image digests.
- **Reusable build workflows**: `cli.yml` and `kubernetes-controller.yml` serve dual roles as both CI (PR/push) and release build steps via `workflow_call`.
- **Environment gates**: Release promotions require manual approval via GitHub Environments (`release` for CLI/Controller, `go-modules` for Go submodules).
- **OCMBot**: GitHub App token (`OCMBOT_APP_ID` + `OCMBOT_PRIV_KEY`) used for automated commits, tags, and PRs that must trigger other workflows. The default `GITHUB_TOKEN` cannot trigger workflows on tags/dispatches it creates.
- **Version computation**: All version logic lives in `.github/scripts/` as testable JavaScript ES modules with companion `.test.js` files.
- **Attestation**: Build provenance attestations are created for binaries, OCI images, and Helm charts. Verified before final release promotion.
- **OCI registry**: All images and charts publish to GHCR (`ghcr.io/<owner>/...`). Tags are promoted via `oras tag` (no re-push needed for images).

---

## 1. CI Pipeline

**Workflows**: `ci.yml`, `pull-request.yaml`

### ci.yml

Triggers: `pull_request` (all) + `push` (main only).

**Orchestrator**: `discover_modules` job:

1. Discovers modules via `task go_modules`
2. Generates `dorny/paths-filter` config per module
3. Integration modules (path contains `/integration`) are linked to parent module paths
4. Detects `.env` changes (lint version bump) and `ci.yml` changes (full rebuild trigger)
5. **Module filtering** — three branches in priority order:
   - CI workflow (`ci.yml`) changed → build, lint, and test ALL modules
   - PR (`check_only_changed = true`) → build and test only changed modules; if `.env` also changed, lint all modules
   - Push to main → ALL modules
6. Testability filtering: checks each module's Taskfile for `test` and `test/integration` tasks via `task -d <module> -aj`

**Parallel jobs** (all depend on `discover_modules`):

| Job | Matrix source | What it does |
|-----|--------------|--------------|
| `golangci_lint_verify` | none | Verifies lint config consistency (runs once) |
| `golangci_lint` | `lint_modules_json` | Runs golangci-lint per module (sparse checkout) |
| `run_unit_tests` | `unit_test_modules_json` | Runs `task <module>:test` per module (sparse checkout) |
| `run_integration_tests` | `integration_test_modules_json` | Runs `task <module>:test/integration` with Docker network |
| `analyze-go` | `modules_json` | CodeQL security analysis per module (runs on `ubuntu-latest` — CodeQL has no ARM64 binary) |
| `generate` | none | Runs `task generate`, fails if working tree is dirty |
| `check-completion` | none | Aggregation gate — runs only on `failure()`, fails if any dependency failed |

The `check-completion` job exists because dynamic matrices cannot be used as required status checks in GitHub branch protection. It fires only when a dependency fails or is cancelled.

### pull-request.yaml

Triggers: `pull_request_target` (opened, edited, synchronize, reopened).

| Job | What it does |
|-----|--------------|
| `conventional-commit-labeler` | Validates PR title matches conventional commit format. Maps type to label and scope to label. |
| `labeler` | Auto-labels based on file paths (config: `.github/config/labeler.yml`) |
| `size-labeler` | Labels by diff size: xs <10, s <100, m <500, l <10000, xl >=10000 |
| `verify-labels` | Ensures PR has at least one of: `kind/chore`, `kind/bugfix`, `kind/feature`, `kind/dependency`, `kind/refactor` |

**Type-to-label mapping**: `feat`->`kind/feature`, `fix`->`kind/bugfix`, `chore`->`kind/chore`, `docs`->`area/documentation`, `test`->`area/testing`, `perf`->`area/performance`. **Scope-to-label**: `deps`->`kind/dependency`. Breaking changes get `!BREAKING-CHANGE!`.

---

## 2. Component Build Workflows

These serve dual purpose: CI (on PR/push) and release builds (via `workflow_call`).

### cli.yml

| Trigger | REF resolves to | Publishes? |
|---------|----------------|------------|
| PR to main | PR branch | No (build only) |
| Push to main | `main` | Yes |
| Push `releases/v*` | `releases/v0.4` | No (build-only) |
| Tag `cli/v*` | `cli/v0.4.0` | Yes |
| `workflow_call` (release) | `inputs.ref` (e.g. `cli/v0.4.0-rc.1`) | Yes |

**Jobs**: `build` -> `publish` -> `conformance` (+ `conformance-pr` for PRs)

- **build**: Sparse checkout of `cli/` + `.github/scripts`. Compute version via `compute-version.js` (`TAG_PREFIX=cli/v`). Generate CTF (`task cli:generate/ctf VERSION=... BUILD_VERSION=...`). The `BUILD_VERSION` can be overridden via `inputs.build_version` (used by release workflow to embed the base version, not the RC version, in ldflags). Attest binaries with `actions/attest-build-provenance` (non-PR only). Determine publish eligibility via `branch-check`. Upload binaries (`ocm-*`) and OCI layout (`cli.tar`) as artifacts.
- **publish**: Download artifacts, push OCI layout to GHCR via `oras cp --from-oci-layout`, resolve digest, attest image. Registry path: `ghcr.io/<owner>/cli:<version>`. Gated by `should_push_oci_image`.
- **conformance**: Calls `conformance.yml` with published image reference (image + digest).
- **conformance-pr**: For PRs only, calls `conformance.yml` with build artifacts (not published image).

**Version computation**: Tag `cli/v1.2.3` -> `1.2.3`. Non-tag ref -> `0.0.0-<sanitized-ref>` (slashes replaced with hyphens, lowercased).

**Publish eligibility logic** (`branch-check` step): Publishes when REF is the default branch (`main`) OR matches `cli/v\d+\.\d+(\.\d+)?(-.*)?`. The step uses `inputs.ref` when set (workflow_call from release), otherwise `GITHUB_REF_NAME`. This distinction matters because `workflow_call` inherits the caller's `github.ref_name` (the release branch), not the passed `inputs.ref`.

### kubernetes-controller.yml

Similar to `cli.yml` but adds Helm chart specifics and E2E testing:

- **`verify-chart`**: Runs before build (not gated by `workflow_call`). Generates values schema (`task helm/schema`), docs (`task helm/docs`), lints chart (`task helm/lint`), renders templates (`task helm/template`), verifies no uncommitted changes.
- **`build`**: Sparse checkout of `kubernetes/controller/` + scripts. Compute version via `compute-version.js` (`TAG_PREFIX=kubernetes/controller/v`). Build multi-arch Docker image to OCI layout. Package Helm chart WITHOUT image digest (for PR conformance testing). Upload OCI layout and chart as separate artifacts.
- **`E2E`**: End-to-end tests on kind cluster. Uses architecture matrix (`matrix.arch: [arm64]`) with dynamic runner selection. Skopeo extracts the matching platform image from multi-arch OCI layout into docker-archive format. Loads into kind, runs `task kubernetes/controller:test/e2e`. On failure, dumps controller pods, logs, events, and Helm release info.
- **`conformance`**: Runs against build artifacts (chart + image, before publish).
- **`publish`**: Pushes image via ORAS, packages Helm chart WITH image digest, pushes chart to GHCR, attests both image and chart. Registry paths: `ghcr.io/<owner>/kubernetes/controller:<version>` (image), `ghcr.io/<owner>/kubernetes/controller/chart:<version>` (chart). On push to main, also tags image and chart with floating `main` tag. Uploads chart artifact for downstream use.
- **`conformance-published`**: Runs conformance against published chart OCI reference.
- `MAX_VERSION_LENGTH=57` (Kubernetes label constraint: 63 chars max for `helm.sh/chart` label, minus 6 for `chart-` prefix).
- Concurrency: `cancel-in-progress: true` for PRs, `false` for other events (queue, don't cancel).

### conformance.yml

Reusable workflow (+ direct push/PR triggers for `conformance/` changes). Runs the sovereign cloud scenario on a kind cluster with Flux. Timeout: 30 minutes. Uses architecture matrix (`matrix.arch: [arm64]`) with dynamic runner selection (`ubuntu-24.04-arm` for arm64, `ubuntu-latest` otherwise).

Two input modes:

1. **Direct image reference** (main/release): `cli_image` and/or `toolkit_image` inputs point to published OCI refs. Used when testing against published artifacts.
2. **Artifact mode** (PRs): `cli_artifact`/`toolkit_image_artifact`/`toolkit_chart_artifact` inputs reference workflow artifacts. Images are extracted from OCI layout via skopeo (`--override-arch ${{ matrix.arch }} --override-os linux`) into docker-archive format, then loaded into Docker daemon or kind cluster.

Environment variables (`CLI_IMAGE`, `TOOLKIT_IMAGE`) set by artifact steps take precedence over input parameters. If neither CLI nor toolkit image is provided via `workflow_call`, the workflow fails with an error.

Steps: `check` (validate dependencies) -> `run` (deploy scenario) -> `upgrade` (test upgrade path) -> `verify:deployment` (validate post-upgrade state) -> `status` (always, show cluster state) -> `clean` (always, tear down).

---

## 3. Release Workflows

Three release tracks, all using the RC-to-final promotion pattern.

### CLI Release (cli-release.yml)

Triggered manually via `workflow_dispatch` on a `releases/v*` branch. The operator MUST select the release branch in the GitHub UI "Use workflow from" dropdown. Supports `dry_run` input (default: true). Concurrency: cancel-in-progress per release branch.

```
cli-release.yml
  |
  +-- prepare: release-candidate-version.yml (compute RC version, changelog)
  |
  +-- tag_rc: create + push annotated RC tag (skipped on dry_run)
  |
  +-- build: cli.yml (build, publish OCI, conformance)
  |
  +-- release_rc: create GitHub pre-release with binaries + OCI tarballs
  |
  +-- [ENVIRONMENT GATE: "release" -- requires manual approval]
  |
  +-- verify_attestations: verify binary + OCI image attestations via gh attestation verify
  |
  +-- promote_final: create final git tag (same commit as RC), promote OCI tags via oras tag
  |
  +-- release_final: create GitHub release with same assets (no rebuild)
```

**Phase 1 -- RC Creation:**

1. `prepare`: Calls `release-candidate-version.yml`. Computes next RC version (e.g. `cli/v0.4.3-rc.1`). Generates changelog via git-cliff. Determines `set_latest`.
2. `tag_rc`: Creates annotated git tag, pushes to origin. Skipped on `dry_run`.
3. `build`: Calls `cli.yml` with `ref=<RC tag>`, `build_version=<base version>` (so the binary embeds the final version, not the RC version). The `ref` input is critical: inside `cli.yml`, `github.ref_name` inherits the caller's ref (the release branch), NOT the passed `inputs.ref`. The `branch-check` step in `cli.yml` uses `inputs.ref` to correctly determine publish eligibility.
4. `release_rc`: Creates GitHub pre-release with binaries (`ocm-*`) and OCI tarballs (`*.tar`).

**Phase 2 -- Final Release (environment-gated):**

5. `verify_attestations`: Runs in `release` environment. Downloads RC binaries from GitHub release, verifies each via `gh attestation verify`. Verifies OCI image attestation.
6. `promote_final`: Creates final git tag pointing to same commit as RC. Promotes OCI tags (e.g. `:0.4.3-rc.1` -> `:0.4.3`, optionally `:latest`).
7. `release_final`: Downloads RC release notes and assets. Rewrites changelog header (RC -> final). Creates GitHub release with `make_latest` flag. No rebuild -- same binaries, same image digest.

### Controller Release (controller-release.yml)

Same pattern as CLI but with Helm chart specifics. Uses OCMBot token for tagging (required to trigger downstream workflows). Concurrency: never cancel-in-progress (queues instead).

```
controller-release.yml
  |
  +-- prepare: release-candidate-version.yml
  |
  +-- tag_rc: create RC tag (uses create-tag.js, OCMBot token)
  |
  +-- build: kubernetes-controller.yml
  |     +-- verify-chart
  |     +-- build (multi-arch image + chart without digest)
  |     +-- E2E tests on kind cluster
  |     +-- conformance (against build artifacts)
  |     +-- publish (image + chart with digest + attestations)
  |     +-- conformance-published (against published chart)
  |
  +-- release_rc: create GitHub pre-release with chart .tgz
  |
  +-- [ENVIRONMENT GATE: "release"]
  |
  +-- verify_attestations: verify image + chart attestations
  |
  +-- promote_and_release_final (combined job):
        +-- create final git tag (create-tag.js)
        +-- promote image tags via oras
        +-- re-package chart with final version
        +-- verify: diff RC vs final chart (after normalizing version fields)
        +-- push final chart, attest it
        +-- publish GitHub release (publish-final-release.js)
```

**Key difference -- chart re-packaging**: The final chart is NOT the same artifact as the RC chart. The `promote_and_release_final` job:

1. Downloads RC chart from GitHub release (not workflow artifact, to avoid retention expiry)
2. Re-packages with final version (removes `-rc.N` from Chart.yaml, values.yaml)
3. Embeds the same image digest
4. Normalizes version fields in both charts, runs `diff -r` to verify they are otherwise identical
5. Pushes final chart with new attestation

Result: Image tags are promoted (same digest), but chart gets a new digest (different version in metadata).

### Go Submodule Release (release-go-submodule.yaml)

Triggered manually via `workflow_dispatch`. Inputs: `path` (e.g. `bindings/go/helm`), `bump` (major/minor/patch/none), `suffix` (e.g. `alpha1`), `dry_run`.

| Job | What it does |
|-----|--------------|
| `version` | Validates `go.mod` exists at path. Finds latest tag matching `<path>/v*`. Computes bumped version based on `bump` input. Handles `vN` major version suffix in paths (e.g. `bindings/go/foo/v2` -> tag prefix `bindings/go/foo/v`). Generates changelog from `git log` between latest tag and HEAD (filtered to module path). Always runs (even on dry_run) to show what would happen. |
| `release` | Creates and pushes annotated tag with changelog as message. Gated by `dry_run == false` + `go-modules` environment (requires manual approval). Uses OCMBot token. |
| `build` | Conditional: only runs when `new_tag` contains `bindings/go/helm`. Calls `publish-helminput-plugin-component.yaml` which builds plugin binaries, publishes component, and triggers `update-plugin-registry.yaml`. |

**Tag format**: `<path>/v<major>.<minor>.<patch>[-<suffix>]`. Example: `bindings/go/runtime/v0.3.1`, `bindings/go/helm/v0.1.0-alpha1`.

### Release Branch Creation (release-branch.yml)

Triggered manually. Inputs: `target_branch` (must match `releases/v0.\d+`), `source_branch` (default: `main`). Creates the branch via GitHub API using OCMBot token. Idempotent: fails if branch already exists.

### Version Computation (release-candidate-version.yml)

Reusable workflow called by both CLI and Controller releases. Runs `release-versioning.js`.

**RC version scenarios** (handled by `computeNextVersions()`):

| Existing tags | Result |
|--------------|--------|
| No tags exist | `v0.X.0-rc.1` (from branch prefix) |
| Stable only (e.g. `v0.1.0`) | Bump patch -> `v0.1.1-rc.1` |
| RC only (e.g. `v0.1.1-rc.2`) | Increment RC -> `v0.1.1-rc.3` |
| Both, same base | Bump patch -> next `rc.1` |
| Stable newer than RC | Bump patch -> next `rc.1` |
| RC newer than stable | Continue RC sequence |

Also determines `set_latest` by comparing against highest previous stable release version via GitHub Releases API.

**Changelog generation**: Uses git-cliff with the component's `cliff.toml` config. The changelog range is computed explicitly (`<previous-tag>..HEAD`) rather than relying on git-cliff's `--latest`, because `--include-path` can cause git-cliff to miss tag boundaries when the tagged commit doesn't touch files in the component path. If no previous tag exists (first release), uses `--latest` with full history. RC tags are excluded from git-cliff's tag pattern so they don't create separate changelog sections.

---

## 4. Website Workflows

| Workflow | Trigger | Purpose |
|----------|---------|---------|
| `website-publish-site.yaml` | Push to main (`website/**`), dispatch | Builds Hugo site + JSON schemas, publishes to GitHub Pages via OCMBot |
| `website-update-cli-docs.yaml` | `repository_dispatch` (`ocm-cli-release`), dispatch | Downloads CLI at tag, generates docs into `version-legacy` folder, creates PR |
| `website-manual-update-cli-docs.yaml` | Dispatch (version input) | Validates version against ocm/ocm releases, triggers `repository_dispatch` for update workflow |
| `website-update-security-txt.yaml` | Schedule (daily in December), dispatch | Fetches `security.txt` from internal SAP repo, creates PR |
| `website-verify-scripts.yml` | PR (`website/assets/js/**`, eslint config) | ESLint on website JavaScript |
| `website-live-test-install-script.yml` | PR (`install-cli.sh`), release published, dispatch | Resolves latest stable CLI release, runs install script on ubuntu x64 + arm, verifies attestation and version |

---

## 5. Maintenance & Quality Workflows

| Workflow | Trigger | Purpose |
|----------|---------|---------|
| `markdown.yml` | PR (`**/*.md`) | Multiple parallel jobs (see below) |
| `jsonschema.yml` | Push/PR (`**/schemas/*.schema.json`) | Lints JSON schemas via sourcemeta |
| `renovate.yml` | Daily schedule, push (main), PR (renovate config) | Dependency updates (see below) |
| `openssf-scorecard.yml` | Weekly (Wed), push (main) | OpenSSF Scorecard supply-chain analysis, uploads SARIF |
| `stale.yaml` | Daily schedule | Marks stale issues/PRs (delegates to central `.github` repo workflow) |
| `auto-label-ipcei.yaml` | Issue opened | Adds `area/ipcei` label |
| `reuse_helper_tool.yaml` | Push, PR | FSFE REUSE compliance check |
| `trigger-blackduck-scan.yaml` | Push (main), PR target, weekly (Sun), dispatch | Black Duck security scan (delegates to central `.github` repo workflow) |

### markdown.yml detail

Runs 5 parallel jobs on PRs that touch `**/*.md`:

| Job | What it does |
|-----|--------------|
| `markdown-lint` | markdownlint-cli2 on `website/**/*.md` (excludes auto-generated CLI/controller reference docs), uses `website.markdownlint-cli2.yaml` config |
| `lint` | markdownlint-cli2 on all `**/*.md`, uses `.markdownlint-cli2.yaml` config. Tool version read from `.env`. |
| `spellcheck` | pyspelling with config `.github/config/spellcheck.yml`, custom dictionary `wordlist.txt` |
| `verify-links` | linkspector on repo markdown (modified files only), config: `.github/config/linkspector.yaml` |
| `verify-links-website` | PR-only. Checks for website changes, waits up to 10 min for Netlify deploy preview, generates dynamic linkspector config with preview URL as base, validates links against preview site |

### renovate.yml detail

- Runs daily at midnight UTC. OCMBot token for non-PR runs. Dry-run (`extract` mode) on PRs.
- Caches repository data between runs via artifact upload/download.
- Post-upgrade task: `renovate-post-upgrade.sh` runs `go mod tidy` in all `*/integration/*` directories.
- Config in `.github/renovate.json5` (see section 8).

---

## 6. Component Publishing

| Workflow | Purpose |
|----------|---------|
| `publish-ocm-component-version.yml` | Reusable: publishes OCM component via Docker-based OCM CLI. Inputs: constructor artifact, optional build artifact, target repository. |
| `publish-helminput-plugin-component.yaml` | Builds multi-arch helm input plugin binaries, generates OCM component constructor, publishes component to GHCR via Docker OCM CLI. Triggers `plugin-published` repository_dispatch. |
| `update-plugin-registry.yaml` | Updates OCM plugin registry. Triggered by `plugin-published` dispatch or manual dispatch. Uses `prepare-registry-constructor.js` to add plugin reference, bump registry version, publish via Docker OCM CLI. |

**Plugin publishing flow:**

```
release-go-submodule.yaml (bindings/go/helm tag)
  -> publish-helminput-plugin-component.yaml
       -> build multi-arch binaries
       -> generate component-constructor.yaml
       -> publish component to ghcr.io/<owner>
       -> repository_dispatch: plugin-published
            -> update-plugin-registry.yaml
                 -> fetch existing registry descriptor
                 -> add new plugin reference
                 -> bump version (minor for new plugin type, patch for existing)
                 -> publish updated registry to ghcr.io/<owner>/plugins
```

---

## 7. Scripts (.github/scripts/)

All scripts are ES modules with accompanying `.test.js` files.

| Script | Purpose | Called by |
|--------|---------|----------|
| `release-versioning.js` | Computes RC/release versions from existing tags. Determines `set_latest` by comparing against highest previous stable release. Finds changelog range for git-cliff. Exports: `computeNextVersions()`, `parseBranch()`, `findPreviousTag()`, `determineLatestRelease()`, `extractHighestPreviousReleaseVersion()`, `shouldSetLatest()`. | `release-candidate-version.yml`, `website-live-test-install-script.yml` |
| `compute-version.js` | Converts git refs to semver. Tag matching `<prefix>\d+.\d+...` -> extract version. Non-tag -> `0.0.0-<sanitized-ref>`. Supports `MAX_VERSION_LENGTH` for K8s label truncation. | `cli.yml`, `kubernetes-controller.yml`, `publish-helminput-plugin-component.yaml` |
| `create-tag.js` | Creates annotated git tags. Idempotent: skips if tag exists at same commit, fails if at different commit. Exports `createRcTag()` and `createNewReleaseTag()`. | `controller-release.yml` |
| `publish-final-release.js` | Promotes RC to final GitHub release. Rewrites changelog header. Creates or updates release (idempotent). Uploads assets (replaces duplicates). Writes job summary. | `controller-release.yml` |
| `prepare-registry-constructor.js` | Manages OCM plugin registry. Fetches existing descriptor via Docker OCM CLI, adds new plugin reference, computes next version (minor for new plugin type, patch for existing), publishes to OCI registry. Rejects duplicate versions except `0.0.0-main`. | `update-plugin-registry.yaml` |
| `renovate-post-upgrade.sh` | Finds all `go.mod` files under `*/integration/*` and runs `go mod tidy` in their directories. | `renovate.yml` (post-upgrade task) |

---

## 8. Configuration (.github/config/)

| File | Tool | Purpose |
|------|------|---------|
| `labeler.yml` | actions/labeler | PR auto-labeling rules (file path -> label mapping) |
| `spellcheck.yml` | pyspelling | Spell-check config for `**/*.md` (custom dictionary: `wordlist.txt`) |
| `wordlist.txt` | pyspelling | 800+ OCM-specific terms, acronyms, contributor names |
| `linkspector.yaml` | linkspector | Repo link validation (modified files only) |
| `website-linkspector.yaml` | linkspector | Website link validation (dynamically generated with Netlify preview URL) |
| `.markdownlint-cli2.yaml` | markdownlint-cli2 | Repo markdown rules |
| `website.markdownlint-cli2.yaml` | markdownlint-cli2 | Website markdown rules |
| `plugin-registry-constructor.yaml` | OCM constructor | Template for plugin registry component descriptor |

### Other root-level config

- **`.github/renovate.json5`**: Extends `config:recommended` + `config:best-practices`. Key settings:
  - `minimumReleaseAge`: 28 days (wait before proposing updates)
  - Automerge: minor/patch only
  - Grouped deps (all scheduled Sundays): k8s (`controller-runtime`, `controller-tools`, `k8s.io/*`), golang versions, golang-x, google-golang, sigstore, docker, spf13
  - OCM monorepo deps (`ocm.software/open-component-model/**`): overnight only (`22:00-06:00`), no `minimumReleaseAge`
  - Indirect major Go deps: disabled
  - Custom datasources: `envtest` (fetches releases YAML from `kubernetes-sigs/controller-tools`)
  - Custom managers: regex for `.env` version patterns (`# renovate: datasource=... depName=...`), pip install versions in workflow YAML files
  - Post-upgrade: runs `renovate-post-upgrade.sh` on any `**/go.mod` or `**/go.sum` change
  - Git submodules: enabled
  - `separateMinorPatch`: true, `separateMultipleMajor`: true
- **`cli/cliff.toml`**: git-cliff changelog config for CLI releases. Conventional commit groups: `feat` -> Features, `fix` -> Bug Fixes, `chore(deps)`/`fix(deps)` -> Dependencies, `docs`/`chore(docs)` -> Documentation, `chore`/`ci` -> Miscellaneous Tasks. Includes installation guide and contributor section in release body template.
- **`.env`**: Tool versions (golangci-lint, markdownlint-cli2, envtest K8s version). Renovate manages these via custom regex managers. Changes to `.env` trigger lint on ALL modules in `ci.yml`.

---

## 9. OCI Registry Paths & Tag Conventions

All artifacts are published to GitHub Container Registry (GHCR).

| Artifact | Registry Path | Example Tags |
|----------|--------------|-------------|
| CLI image | `ghcr.io/<owner>/cli` | `0.0.0-main`, `0.4.3-rc.1`, `0.4.3`, `latest` |
| Controller image | `ghcr.io/<owner>/kubernetes/controller` | `0.0.0-main`, `main`, `0.1.0-rc.1`, `0.1.0`, `latest` |
| Controller Helm chart | `ghcr.io/<owner>/kubernetes/controller/chart` | `0.0.0-main`, `main`, `0.1.0-rc.1`, `0.1.0` |
| Helm input plugin | `ghcr.io/<owner>//ocm.software/plugins/helminput` | `0.0.0-main`, `0.1.0` |
| Plugin registry | `ghcr.io/<owner>/plugins//ocm.software/plugin-registry` | `v0.0.1`, `v0.1.0` |

**Tag lifecycle for releases:**

1. RC build publishes: `<version>-rc.N` (e.g. `0.4.3-rc.1`)
2. Final promotion adds: `<version>` (e.g. `0.4.3`) via `oras tag` (same digest, no re-push)
3. If `set_latest=true`: also tags `latest`
4. Push to main: tags `0.0.0-main` (CLI) or `0.0.0-main` + floating `main` tag (controller)

---

## 10. Secrets & Tokens

| Secret | Purpose |
|--------|---------|
| `OCMBOT_APP_ID` + `OCMBOT_PRIV_KEY` | GitHub App token for OCMBot. Used where `GITHUB_TOKEN` cannot trigger downstream workflows (tags, dispatches). |
| `SECURITY_TXT_READ` | PAT for fetching `security.txt` from internal SAP repository. |
| `GITHUB_TOKEN` | Default token. Used for most operations. Auto-downgraded to read-only for fork PRs. |

---

## 11. Concurrency Controls

| Workflow | Concurrency group | Cancel in-progress? |
|----------|------------------|-------------------|
| `kubernetes-controller.yml` | `workflow-ref` | Yes for PRs, No for other events |
| `conformance.yml` | `conformance-workflow-ref` | Yes (always) |
| `cli-release.yml` | `cli-release-<branch>` | Yes |
| `controller-release.yml` | `controller-release-<branch>` | No (queue, never cancel a release) |
| `publish-helminput-plugin-component.yaml` (publish job) | `helm-plugin-publish-ref` | No |
| `update-plugin-registry.yaml` | `plugin-registry-publish` | No (queue updates) |
| `website-live-test-install-script.yml` | `live-test-install-ref` | Yes |

---

## 12. Release Runbook (Quick Reference)

### CLI Release

1. Ensure `releases/v0.X` branch exists (create via `release-branch.yml` if needed)
2. Go to Actions -> "CLI Release" -> "Run workflow"
3. Select branch: `releases/v0.X`
4. Set `dry_run: true` first to verify version computation and changelog
5. Re-run with `dry_run: false` to create RC
6. Wait for RC build, conformance, and pre-release creation
7. Test the RC (download binaries from pre-release, pull image from GHCR)
8. Approve the `release` environment gate in the workflow run
9. Phase 2 runs automatically: verify attestations, promote tags, create final release

### Controller Release

Same as CLI, but use "Controller Release" workflow. Additional considerations:
- Helm chart is re-packaged with final version (chart digest changes, image digest stays the same)
- E2E tests run as part of the build phase

### Go Submodule Release

1. Go to Actions -> "Release Go Submodule" -> "Run workflow"
2. Set `path` (e.g. `bindings/go/runtime`), `bump` (patch/minor/major), optional `suffix`
3. Set `dry_run: true` first to verify version and changelog
4. Re-run with `dry_run: false`
5. Approve the `go-modules` environment gate
6. For `bindings/go/helm` only: plugin build and registry update happen automatically

---

## 13. Workflow Count by Category

| Category | Count | Workflows |
|----------|-------|-----------|
| CI | 2 | `ci.yml`, `pull-request.yaml` |
| Component Build | 3 | `cli.yml`, `kubernetes-controller.yml`, `conformance.yml` |
| Release | 5 | `cli-release.yml`, `controller-release.yml`, `release-go-submodule.yaml`, `release-candidate-version.yml`, `release-branch.yml` |
| Component Publishing | 3 | `publish-ocm-component-version.yml`, `publish-helminput-plugin-component.yaml`, `update-plugin-registry.yaml` |
| Website | 5 | `website-publish-site.yaml`, `website-update-cli-docs.yaml`, `website-manual-update-cli-docs.yaml`, `website-update-security-txt.yaml`, `website-verify-scripts.yml` |
| Website (testing) | 1 | `website-live-test-install-script.yml` |
| Maintenance & Quality | 8 | `markdown.yml`, `jsonschema.yml`, `renovate.yml`, `openssf-scorecard.yml`, `stale.yaml`, `auto-label-ipcei.yaml`, `reuse_helper_tool.yaml`, `trigger-blackduck-scan.yaml` |
| **Total** | **27** | |
