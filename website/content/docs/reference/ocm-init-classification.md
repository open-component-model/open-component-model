---
title: "ocm init Repository Scaffolding"
description: "Reference for ocm init: how it scaffolds a component-constructor from a local repository, deterministically by default, with optional AI assist."
icon: "🔎"
weight: 7
toc: true
slug: ocm-init-classification
---

This page is the technical reference for `ocm init`, which inspects a local
repository and writes a `component-constructor.yaml` scaffold you can build with
`ocm add cv`.

`ocm init` only **writes** the scaffold — it does not build a component. It is a
starting point that lowers the barrier to a first OCM component version; always
review the emitted file before building.

## Zero-configuration by default

The default classifier is fully **deterministic**: it needs no configuration, no
API key, and no network beyond an optional GitHub Releases lookup. It reads the
evidence already in your checkout and produces the same result every time:

```console
ocm init ./my-service
```

Signals used:

- **Ecosystem scanners** — one detector per ecosystem inspects the walked file
  set and proposes candidate deliverables. Each recognises its own manifests and
  runs its own content probes (see the table below).
- **Code shape** — a Go `func main` in the module root, `main/`, or `cmd/**`
  marks a runnable binary and distinguishes it from a source library.
- **Git signals** — origin (for the component name and provider), HEAD commit
  (for a `GitHub/v1` source), branch, and tags (for versions).
- **Repository metadata** — a root `LICENSE`/`COPYING` file is mapped to an SPDX
  identifier and emitted as a `classification.ocm.software/license` label.

### Ecosystem scanners

The classifier is a **scanner registry**: the repository is walked once into a
shared file set, and every registered scanner reads it and emits candidate
deliverables with a kind, a priority, and any evidence it found. Adding an
ecosystem is a new scanner plus one registration line — no change to the core.

| Ecosystem | Trigger | OCM kind | Evidence also extracted |
| --- | --- | --- | --- |
| Helm | `Chart.yaml` | `helmChart` | chart `name:` (the dash-tag namespace), `appVersion:`, image `repository:` from a sibling `values.yaml` |
| Docker | `Dockerfile` | `ociImage` | image reference from a sibling `Makefile` `IMG ?=` assignment |
| Go | `go.mod` | `blob` (with a `func main`) or `goModule` (library) | main-entrypoint probe |
| Node | `package.json` | `npmPackage` | — |
| Python | `pyproject.toml` / `setup.py` | `pythonPackage` | — |
| Rust | `Cargo.toml` | `blob` | — |
| Java | `pom.xml` / `build.gradle[.kts]` | `blob` | names Maven vs Gradle explicitly |

Only registry-qualified (dotted-host) image references are accepted from
`values.yaml`/`Makefile`, so a bare Helm subchart `repository:` value is not
mistaken for a published image.

### Kind selection

When several scanners fire, the primary OCM resource kind is the highest-priority
candidate **across the whole repository**:

1. `helmChart` — priority 100. A chart in `deploy/` outranks a root `Dockerfile`.
2. `ociImage` — priority 90.
3. `blob` (runnable binary) — priority 70. A Go module with a `main` entrypoint.
4. `npmPackage` / `pythonPackage` / Rust / Java package — priority 50.
5. `goModule` (source library) — priority 30.
6. `blob` — priority 10. Anything else (a packaged directory).

Priorities are distinct per kind, so this ranking is stable; scanner registration
order only breaks a genuine equal-priority tie.

### Monorepo detection is conservative

A repository fans out into multiple components **only** when there are at least
two genuine sibling sub-deliverables *and* the root itself is not the primary
deliverable. A single component that keeps build scaffolding or tooling in
subdirectories (for example a controller with `component/` and `deploy/`, or a
binary with `internal/tools`) stays **one** component. A chart collection with
many sibling `charts/*/Chart.yaml` and no root deliverable fans out into one
component per chart plus a root aggregate.

A root `go.work` file also suppresses fan-out: workspace members are treated as
helpers of a single component, not independent deliverables.

### Version discovery

Versions come from the repository, resolved per component with precedence
GitHub release > namespaced git tag > root tag > `0.0.0+<commit>` pseudo-version.
Tags are parsed in three real-world shapes:

- bare — `v1.4.2`
- path-namespaced — `services/api/v0.21.1`
- Helm-style name-delimited — `grafana-10.4.0`, `agent-operator-0.10.0`

The chosen source is recorded as a `classification.ocm.software/version-source`
label. Pass `--offline` to skip the GitHub Releases call and use git tags only.

### CI and release outputs

OCM ships build artifacts — images, binaries, and other CI outputs — so beyond
the primary deliverable, `ocm init` reads the repository's **release and CI
configuration** to discover the artifacts it actually publishes and emits each as
an additional resource on the component. These sources are read from the local
checkout (no network), so `--offline` does not disable them.

| Source | Read from | Discovers |
| --- | --- | --- |
| goreleaser | `.goreleaser.yaml` / `.yml` | container images from `dockers[].image_templates` and `docker_manifests[].name_template`; released binaries from `builds[]` with their `goos`×`goarch` platform matrix |
| GitHub Actions | `.github/workflows/*.yml` | images pushed by `docker/build-push-action` (combined with `docker/metadata-action` `images` when the tag is bare); released binaries uploaded by `softprops/action-gh-release` or `gh release` |
| Make / Task | `Makefile`, `Taskfile.yml` | images from `IMG ?=`, `docker push <ref>`, and Taskfile image vars; `ko` usage |
| ko | `.ko.yaml` / `ko.yaml`, `ko build` | a ko-built container image |

How the discovered artifacts are modelled:

- **Images** become `ociImage` resources with `external` relation. The first is
  named `image`; additional images get a registry-derived suffix
  (`image-<repo>`). An image already emitted as the primary deliverable is not
  duplicated.
- **Released binaries** become `blob` resources carrying a
  `classification.ocm.software/platforms` label (the discovered platform matrix).
  Because the built binary is not in the source checkout, each is flagged as a
  **TODO**: point its input at the real built artifact.

**Image tag resolution is conservative.** Template variables `{{ .Version }}` and
`{{ .Tag }}` are resolved against the resolved component version. Any other
template construct — an unknown variable, a template function, a computed tag
(for example a `ko` image whose reference derives from `KO_DOCKER_REPO` at build
time) — leaves the reference unresolved, and it is emitted with a **TODO to
verify** rather than a fabricated value. When several sources name the same
image, the more authoritative source (goreleaser or CI over a `Makefile` var)
wins and the reference appears once.

## Worked examples

All outputs below are produced by the deterministic classifier with
`--offline --dry-run`; only the YAML relevant to the behavior is shown.

### Single Helm chart (with a discovered image)

A repo with `deploy/Chart.yaml` and a `deploy/values.yaml` referencing
`ghcr.io/acme/my-service`:

```yaml
components:
  - name: github.com/acme/my-service
    version: 0.33.0
    labels:
      - name: classification.ocm.software/maturity
        value: active
      - name: classification.ocm.software/versioning-scheme
        value: semver
    resources:
      - name: chart
        type: helmChart
        relation: local
        input:
          type: Helm/v1
          path: deploy
    sources:
      - name: source
        type: git
        access:
          type: GitHub/v1
          repoUrl: https://github.com/acme/my-service
          commit: a5f62fb97f14d53bb060c7a6d27e2ac2a3470176
```

```text
Discovered a single component using deterministic rules (no model):
  • github.com/acme/my-service@0.33.0
      kind: helmChart · version from git-tag · provider github.com/acme
      – detected a Chart.yaml (appVersion 1.4.2)
      – discovered image reference "ghcr.io/acme/my-service" from the repository
```

### Go source library vs runnable binary

A `go.mod` with no `func main` is a source library (`goModule`); the module is
bundled as a `blob` with a `srcRefs` back-reference to the git source:

```yaml
resources:
  - name: module
    type: blob
    relation: local
    srcRefs:
      - identitySelector:
          name: source
    input:
      type: Dir/v1
      path: .
      excludeFiles: [.git]
```

```text
  • github.com/acme/lib@1.0.0
      kind: goModule · version from git-tag · provider github.com/acme
      – Go module with no main entrypoint (a source library)
```

A `go.mod` with a `func main` in `cmd/` instead reports
`kind: blob · Go module with a main entrypoint (a runnable binary)`.

### Monorepo fan-out

A chart collection with sibling `charts/grafana/Chart.yaml` and
`charts/loki/Chart.yaml`, tagged `grafana-10.4.0` and `loki-2.9.1`, fans out into
one component per chart plus a root aggregate that references them:

```yaml
components:
  - name: github.com/acme/helm-charts       # root aggregate
    version: 0.0.0+c6ea1548e18a
    labels:
      - name: classification.ocm.software/monorepo
        value: true
    componentReferences:
      - name: charts-grafana
        componentName: github.com/acme/helm-charts/charts/grafana
        version: 10.4.0
      - name: charts-loki
        componentName: github.com/acme/helm-charts/charts/loki
        version: 2.9.1
    resources: []
  - name: github.com/acme/helm-charts/charts/grafana
    version: 10.4.0                          # from the grafana-10.4.0 dash-tag
    resources:
      - name: chart
        type: helmChart
        input: {type: Helm/v1, path: charts/grafana}
  # ... charts/loki likewise at 2.9.1
```

### CI and release outputs (multi-artifact)

A Go binary whose `.goreleaser.yaml` builds an image and a binary matrix, and
whose workflow pushes a second image, yields one component with five resources:

```yaml
resources:
  - {name: artifact,      type: blob}                                   # the module
  - {name: image-tool,    type: ociImage}   # ghcr.io/acme/tool:2.3.1   (goreleaser)
  - name: binary-tool                        # goreleaser build matrix
    type: blob
    labels:
      - name: classification.ocm.software/platforms
        value: [darwin/amd64, darwin/arm64, linux/amd64, linux/arm64]
  - {name: image-sidecar, type: ociImage}   # ghcr.io/acme/sidecar:2.3.1 (Actions)
  - {name: binary,        type: blob}       # gh-release binary
```

```text
Before building, verify:
  ! …: resource "binary-tool" is a released binary (goreleaser); point its
    input at the built artifact rather than the source directory
  ! …: resource "binary" is a released binary (github-actions); …
```

### Seeing the decisions (`--loglevel debug`)

Every decision is logged with `slog`. The command is silent at the default log
level and emits a structured trace at `--loglevel debug`:

```console
ocm init ./my-service --loglevel debug
```

```text
level=DEBUG msg="init: extracted repository evidence" files_walked=2 manifest_kinds=[helm_chart] latest_tag=v0.33.0 license=""
level=DEBUG msg="init: classified repository" method=rules kind=helmChart maturity=active versioning_scheme=semver ships_helm=true monorepo=false image_hints=[ghcr.io/acme/my-service]
level=DEBUG msg="init: resolved version" component=root version=0.33.0 source=git-tag
level=DEBUG msg="init: assembled component" component=github.com/acme/my-service kind=helmChart resources=[helmChart:chart] todos=0
```

## Optional AI assist

For genuinely ambiguous repositories you can route the judgemental decisions
(kind, monorepo status) through the TypeSafe **Jev** decisions model:

```console
ocm init ./my-service --ai-assist
```

AI assist is **additive and non-blocking**: evidence-backed facts the rules
already know (provider, source, versions) stay authoritative, and if no API key
is configured the command prints a note and falls back to the deterministic
rules — it never blocks on a missing key.

Configure the endpoint, model, and credential hostname with the
`init.cli.config.ocm.software/v1alpha1` configuration type:

```yaml
type: generic.config.ocm.software/v1
configurations:
  - type: init.cli.config.ocm.software/v1alpha1
    endpoint: https://openrouter.ai/api/alpha/decisions
    model: typesafe/jev-1.13
    credentialHostname: openrouter.ai
  - type: credentials.config.ocm.software/v1
    consumers:
      - identity:
          type: Credentials/v1
          hostname: openrouter.ai
        credentials:
          - type: Credentials/v1
            properties:
              apiKey: <your-api-key>
```

| Field | Default | Description |
| --- | --- | --- |
| `endpoint` | `https://api.typesafe.ai/v1/systemone` | The Jev decisions endpoint. |
| `model` | `jev-latest` | The Jev model identifier. |
| `credentialHostname` | `api.typesafe.ai` | Identity hostname for resolving the API key. |

The API key is resolved from the OCM credential graph for `credentialHostname`
(the `apiKey` then `token` property), falling back to the `TYPESAFE_API_KEY` then
`OPENROUTER_API_KEY` environment variables.

## Flags

| Flag | Default | Description |
| --- | --- | --- |
| `--output`, `-o` | `component-constructor.yaml` | Output path. Overwrites an existing file with a note. |
| `--dry-run` | `false` | Print the YAML to stdout instead of writing a file. |
| `--ai-assist` | `false` | Refine ambiguous classifications with Jev; falls back to rules if no key. |
| `--min-confidence` | `0.75` | With `--ai-assist`, minimum model confidence to act on a decision. |
| `--offline` | `false` | Skip the GitHub Releases call; resolve versions from git tags only. |

The persistent logging flags control the decision trace: `--loglevel`
(`debug`/`info`/`warn`/`error`, default `warn` — use `debug` to see every
classification decision), `--logformat` (`text`/`json`), and `--logoutput`
(`stdout`/`stderr`).

## Emitted labels

Every component carries classification labels under `classification.ocm.software/`:

| Label | On | Value |
| --- | --- | --- |
| `maturity` | every component | `experimental` / `active` / `stable`, inferred conservatively from release tags |
| `versioning-scheme` | every component | `semver` / `prefixed_semver` / `calver` / `none` |
| `version-source` | every component | how the version was resolved (`github-release`, `git-tag`, `pseudo`, …) |
| `source-branch` | when known | the checked-out branch |
| `license` | when detected | the SPDX identifier of the root license |
| `platforms` | released-binary resources | the discovered `goos/goarch` matrix |
| `monorepo` | the root aggregate | `true` |

## What it cannot know

`ocm init` reads a local checkout. It does **not**:

- resolve transitive dependencies from manifest contents;
- inspect a remote OCI registry.

Image references are **discovered** where the repository states them — a Helm
`values.yaml`, a `Makefile` `IMG`, goreleaser, or a CI workflow — and used
directly. Only when no reference can be found (or a tag template cannot be fully
resolved) does the command fall back to a best-effort
`ghcr.io/<org>/<repo>:<version>` guess, which is always **reported as a TODO you
must verify**, never emitted silently.

The run summary lists each component's kind, resolved version and source, any
fields to verify, and the exact `ocm add cv` command to run next.

## See also

- [Component Constructor reference]({{< relref "docs/reference/component-constructor.md" >}}) —
  the schema of the file `ocm init` writes.
- The classification-chain design:
  [`docs/design/jev-repository-classification-chain.md`](https://github.com/open-component-model/open-component-model/blob/main/docs/design/jev-repository-classification-chain.md).
