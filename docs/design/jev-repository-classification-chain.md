# Design: Repository Classification Chain → OCM Component Versions (Jev)

Status: exploratory / prototype (not an ADR). Depends on an external proprietary model.
Prototype: `/tmp/jevdemo/classify_to_ocm.py`; proven end-to-end against the `ocm` CLI.

## Problem

Onboarding arbitrary source or OCI repositories into OCM is manual: a human reads the
repo, decides what the deliverable is, and hand-writes a `component-constructor.yaml`
(component name, version, provider, resources, sources, accesses/inputs). We want to
**automatically categorize a repository and produce a valid OCM component version**,
with a machine-checkable confidence signal on every decision so low-confidence cases
route to a human instead of producing garbage.

## Why Jev (TypeSafe System One), not an LLM

Jev (`jev-latest`, TypeSafe AI, released 2026-09-15) is a *System One* model: unstructured
state in → **typed probabilistic decisions** out. It is the right tool here because the
task is a fixed set of atomic classifications, not free-text generation:

| Property | Consequence for this chain |
| --- | --- |
| Output is a value from a **closed set** (Choice ≤255 options, Score ≤10 levels, Noul 0–1) | Every branch maps to a known OCM type string — code, not prose, owns the mapping. |
| **Cannot hallucinate / no type errors** | The classifier can never invent a nonexistent OCM access type. |
| **Calibrated `confidence`** on every Choice/Score | Directly drives auto-publish vs human-review gating. |
| **Parallel** sampling — all questions in one request, no context-rot | Whole taxonomy + all metadata questions cost ~one call. |
| 70–500 ms, $0.042/MTok in, output free | Cheap enough to run on every repo, every push. |

API surface used (`POST https://api.typesafe.ai/v1/systemone`, or the omp `judge()` primitive
which is the same API): three question types — `choice` (returns `choice`,
`probabilities`, `confidence`), `score` (ordered levels → `score`, `probabilities`,
`confidence`), `noul` (yes/no → `noul` 0–1). All keyed by ids you choose; all answered
independently against one `state`.

Doctrine applied (from the TypeSafe docs): **atomic questions, decomposed and combined in
code**; **speculative fan-out** (ask add-on questions unconditionally, let code decide
relevance); **confidence-gated routing** (act / verify / defer by confidence).

## The chain

```text
repo (local dir | OCI ref)
   │
   ▼  Stage 0  extract_state()  — deterministic, no model
compact JSON state:
   { detected_manifests, top_level_dirs, language_file_counts,
     ci_workflows, tag_count, latest_tag, head_commit, origin_url, readme_excerpt }
   │
   ▼  Wave 1  one Jev call (7 questions, parallel)
   ├─ primary_artifact   Choice over OCM resource-type taxonomy   → +confidence
   ├─ publishes_container Noul  ┐ speculative fan-out gates
   ├─ publishes_helm      Noul  ┘
   ├─ is_monorepo         Noul
   ├─ maturity            Score(experimental|active|stable)       → +confidence
   ├─ versioning_scheme   Choice(semver|prefixed_semver|calver|none) → +confidence
   └─ auto_publishable    Noul
   │
   ▼  Wave 2  one Jev call — modeling choices GATED by wave-1 nouls
   ├─ source_access   Choice(github|git_generic|local_dir)
   ├─ provider_kind   Choice(open_source_org|individual|vendor)
   ├─ image_access    Choice(oci_registry|local_build)     [only if publishes_container]
   └─ helm_input      Choice(packaged_dir|helm_repo)        [only if publishes_helm]
   │
   ▼  Stage 4  build_constructor()  — deterministic; CODE owns every OCM type string
component-constructor.yaml
   │
   ▼  ocm add cv  →  CTF archive / OCI registry  →  ocm get cv
real OCM component version (descriptor v2)
```

Two Jev waves, not one, because Wave 2's questions are only meaningful once Wave 1's
`publishes_container` / `publishes_helm` nouls have gated them — a genuine dependency, not
padding. Everything independent is batched into a single call per wave.

### Taxonomy = OCM resource types

The Stage-1 Choice options are the OCM resource-type / access leaves, grounded in the
constructor library's registered types (`bindings/go/constructor`, and the `input/*` +
`*/spec/access/*` packages):

- `ociImage` → resource `type: ociImage`, access `OCIImage/v1` (`imageReference`)
- `helmChart` → resource `type: helmChart`, input `Helm/v1` (`path` | `helmRepository`)
- `blob` → resource `type: blob`, input `File/v1` or `Dir/v1`
- `goModule` → resource `type: blob` + source `git` / access `GitHub/v1`
- source access → `GitHub/v1` (`repoUrl`, `commit`) or `Dir/v1` input

Deeper hierarchies (the docs' *hierarchical classification* cookbook) apply beam search
over Choice probabilities; here a two-level tree (family → modeling) suffices, so greedy
selection with confidence gating is enough.

### Confidence gating (`CONF_ACT = 0.75`)

Every Choice/Score decision passes through `_pick(answer, fallback)`: use the model's
choice only when `confidence ≥ CONF_ACT`, else fall back to the **safest OCM modeling**
(`blob` for artifact kind, `local_dir` for source — a bundled directory always works).
The separate `auto_publishable` noul is the top-level route: `false` ⇒ emit the YAML but
flag for human review rather than auto-running `ocm add cv`. The chain records the minimum
choice/score confidence across all decisions in its trace for observability.

Classification results are also persisted onto the component as signed-off labels
(`classification.ocm.software/{maturity,versioning-scheme,auto-publishable}`), so the
decision provenance travels with the component version.

## Grounding: OCM construction contract

`component-constructor.yaml` schema (from `bindings/go/constructor/spec/v1/constructor.go`):
`ComponentConstructor{ Components[] }`; each `Component` has `ComponentMeta`(name,version),
`Provider{name,labels}`, `Resources[]`, `Sources[]`, `References[]`. A `Resource`/`Source`
carries `ElementMeta` + `Type` + exactly one of `AccessOrInput.Access` / `.Input` (raw
type-tagged specs). CLI: `ocm add component-version -r <repo> -c <file> --working-directory <dir>`
(`bindings/go/cli/cmd/add/component-version/cmd.go`); read back with `ocm get cv <ref> -o yaml`.

## Verification (proven, not asserted)

Built the `ocm` CLI (`task bindings/go/cli:build`). Ran the chain against a clean synthetic
Helm-chart repo; real Jev returned `primary_artifact=helmChart`, `provider_kind=vendor`,
`maturity=stable`, `versioning_scheme=semver`, `auto_publishable=true`. The emitted YAML was fed to `ocm add cv` (→ CTF archive) and read
back with `ocm get cv`: the component version `github.com/acme/hello-chart:1.4.2` exists,
with the chart packaged as a `LocalBlob/v1` (SHA-256 digest present), a `GitHub/v1` source,
and all three classification labels persisted in the descriptor.

Three real modeling bugs were caught **because** the output was validated against the actual
builder, not eyeballed:

1. duplicate resource when the primary-artifact branch and the `publishes_helm` fan-out both
   emitted `chart` → dedup by resource name;
2. hardcoded Helm `path` → derive the chart directory from the detected `Chart.yaml` location;
3. `GitHub/v1.commit` must be a full 40-char SHA → extractor uses `git rev-parse HEAD`, not `--short`.

## Monorepo fan-out (implemented)

When the root `is_monorepo` noul fires, the chain recurses instead of emitting one
component:

1. **Sub-deliverable discovery** (deterministic): group detected *deliverable* manifests
   (`go.mod`, `Chart.yaml`, `Dockerfile`, `package.json`, `Cargo.toml`, `pyproject`) by
   directory; drop test/fixture/tooling paths (`testdata`, `examples`, `hack`, `.github`,
   …); collapse a child dir into an ancestor sub-deliverable (a controller and its
   `controller/chart` become one component).
2. **Per-subtree classification**: each sub-deliverable dir gets its own scoped
   `extract_state` → `classify` → one component. Input paths are rewritten relative to the
   repo root so a single `--working-directory` at the repo builds them all.
3. **Root aggregation**: emit a thin root component whose `componentReferences[]` point at
   every sub-component (name + version), plus a `git` source for the repo itself.

Proven end-to-end on a synthetic `acme/platform` monorepo: Jev classified three
sub-deliverables — `services/api` → `ociImage`, `deploy/platform-chart` → `helmChart`,
`libs/common` → `blob` — and the root `github.com/acme/platform` referenced all three. All
four built in one `ocm add cv` and resolved with `ocm get cv --recursive -o tree` into the
correct nested tree.

## Version discovery (implemented)

Versions are discovered from the repository itself, not templated. Three signals, resolved
per sub-deliverable with explicit precedence:

1. **Git branch** — `git rev-parse --abbrev-ref HEAD`, recorded as a
   `classification.ocm.software/source-branch` label.
2. **Git tags, namespaced** — the full tag list is parsed into version *namespaces* by
   prefix (`api/v0.21.1` → namespace `api`, version `0.21.1`; bare `v3.3.1` → root
   namespace). Each namespace's latest is chosen by a real **semver comparator** (release >
   prerelease; numeric identifier ordering), not string sort — so `kustomize` `5.8.1` beats
   `0.9.x`.
3. **GitHub Releases** — `list_releases` (via `gh` auth) grouped into the same namespaces,
   carrying `prerelease`/`draft` flags and publish dates.

**Per-subtree resolution** (`resolve_version(subdir, tags, releases)`): match the subdir to
the best tag/release namespace (longest path-tail prefix match), preferring a **GitHub
release** over a raw **git tag**, preferring stable over prerelease, falling back to the
root namespace, then to a `0.0.0+<commit>` pseudo-version. The chosen source is recorded as
a `classification.ocm.software/version-source` label (`github-release` | `git-tag` |
`git-tag-root-fallback`).

Proven: a synthetic monorepo with per-module tags (`services/api/v1.4.0`,
`deploy/platform-chart/v2.1.0`, `libs/common/v0.9.4`) built with each sub-component at its
own resolved version and the aggregate referencing those exact versions
(`ocm get cv --recursive -o tree` confirms). On the real `kubernetes-sigs/kustomize`
checkout, GitHub Releases drove `api`/`cmd/config` → `0.21.1` while unmatched subtrees fell
back to the root tag `3.3.1`; branch `master` and origin were captured.

## Model access (as run)

Jev is **not** on the chat/completions API. Two working transports:

- **OpenRouter** `POST https://openrouter.ai/api/alpha/decisions`, model
  `typesafe/jev-1.13` (Bearer OpenRouter key). Request body is the systemone shape
  (`state`, `questions`); calling `/chat/completions` returns
  *"is a decisions model … Use the /api/alpha/decisions endpoint"*. Note: the decisions
  schema discriminator is `noul` (not `bool`); a `bool` question type must be sent as
  `noul`.
- **TypeSafe direct** `POST https://api.typesafe.ai/v1/systemone` with a `TYPESAFE_API_KEY`.

The omp harness `judge()` primitive uses the same three-primitive contract, but only routes
to real Jev when a TypeSafe credential is configured; otherwise it falls back to a small
chat model that emulates the shape. That fallback is **not calibrated** — it returned
uniform `confidence: 1.0` on genuinely ambiguous repos, which is the tell. All numbers below
are from **real Jev** (`jev-1.13-20260917`, `provider: "TypeSafe"`) via OpenRouter.

## Corpus evaluation (20 varied OSS repos, real Jev)

Ran the chain over 20 deliberately varied repositories (Go/Rust/Python/JS libraries, CLI
binaries, container daemons, Helm-chart collections, and GitOps/service-mesh monorepos)
against hand labels. Stage 1 uses **decomposed atomic capability nouls** — `ships_container`,
`ships_helm`, `ships_binary`, `is_lang_package`, `is_source_library` — and code composes the
primary OCM kind by priority (image > chart > binary/blob > language package > source lib).

- **primary_artifact: 17/20 (85%)** · **is_monorepo: 17/20 (85%)** · **mean 0.46 s/repo**.
- Monorepo probability spread 0.05–0.96 (calibrated), not the fallback's uniform 1.0.
- The three artifact "misses" are label disagreements, not model errors, and the capability
  nouls expose why: `jq` reads as both binary (0.93) and image (0.96); `terraform` (bin 0.87,
  img 0.18) and `kustomize` (bin 0.81) are binary-first — Jev's pick is at least as defensible
  as the hand label. This is the payoff of decomposition: the single overloaded artifact-kind
  Choice (an earlier design) forced one answer and hid the ambiguity; atomic nouls let a repo
  be *both* an image and a binary and let code own the priority.

Raw results: `/tmp/jevcorpus/jev_corpus_results.json`.

## Limitations & next steps

- **Evidence breadth.** Stage 0 reads manifests, dir structure, CI, tags, README head; it
  does not deeply parse dependency manifests or inspect OCI repositories directly (local
  checkouts only).
- **Registry-published image tags.** `ociImage` `imageReference` is templated from the repo
  slug; version comes from tag/release discovery, but the actual published image tag in a
  registry is not yet queried.
- **Threshold tuning.** `CONF_ACT`/auto-publish thresholds are conservative defaults; tune
  against a labeled corpus per the *classification-using-confidence* cookbook (report the
  broader division/`blob` fallback when unsure of the specific type).
