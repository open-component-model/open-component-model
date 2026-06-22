# E2E Test Harness — Design

This document describes the architecture of the controller's end-to-end test harness:
what scenarios are, how they're discovered, declared, executed, and parallelised in CI.
It is the source of truth for the harness; update it when the design changes.

## Status

- **State:** implemented. All stages are complete; the sharded CI workflow is active.
- **Replaces:** the monolithic `e2e_examples_test.go` / `e2e_credentials_test.go` /
  `e2e_applyset_test.go` files plus the single `hacks/setup.sh` script.
- **Supersedes:** `task test/e2e/example NAME=...` (removed).

## Architecture overview

<details>
<summary>what the harness does: discovery → execution → diagnostics (detailed)</summary>

```mermaid
graph LR
    subgraph "Scenario Discovery"
        A["examples/"] --> W["walkScenarios()"]
        B["test/e2e/scenarios/"] --> W
        W --> C["ScenarioConfig[]"]
    end

    subgraph "BeforeSuite (proc 1)"
        C --> BS["collectAndInstallRequires"]
        BS --> REQ["requires: install components"]
    end

    subgraph "Per-Scenario Execution"
        REQ --> R["runScenario(cfg)"]
        R --> PREP["prepare: transfer OCM components"]
        PREP --> HOOKS1["preDeployHooks"]
        HOOKS1 --> DEP["deploy: apply + waitFor"]
        DEP --> HOOKS2["postDeployHooks"]
        HOOKS2 --> HOOKS3["preAssertHooks"]
        HOOKS3 --> ASS["assert: fieldEquals"]
        ASS --> HOOKS4["postAssertHooks"]
        HOOKS4 --> HOOKS5["preCleanupHooks"]
        HOOKS5 --> CLN["cleanup + postCleanupHooks"]
    end

    subgraph "On Failure or Debug Mode"
        ASS -. "fail" .-> DBG["debug: kubectl commands"]
        DEP -. "fail" .-> DBG
    end
```

</details>

<details>
<summary>Setup composition flow</summary>

```mermaid
flowchart TD
    START["task test/e2e/setup/local"] --> CLUSTER["cluster.sh: kind + registry + RBAC"]
    CLUSTER --> FLAG{{"--all-components?"}}
    FLAG -- "yes" --> ALL["install all components/*.sh"]
    FLAG -- "no (default)" --> DONE1["cluster ready — components deferred"]

    subgraph "BeforeSuite (proc 1): collectAndInstallRequires"
        BS["walk both roots, filter by focus"] --> UNION["union requires: entries across matching scenarios"]
        UNION --> PARALLEL["run each unique component script once (errgroup)"]
    end

    DONE1 --> BS
    ALL --> BS
```
</details>

## Why this exists

The previous harness had two structural problems:

1. **Setup was monolithic.** `hacks/setup.sh` installed kind, flux, ArgoCD, kro, and the
   protected registries every time, regardless of whether a given scenario needed all of
   them. Iteration loops were dominated by setup cost. Scenarios that need none of those
   (e.g. `applyset-pruning`, which only needs the controller-manager) paid for the full
   stack.
2. **Adding a scenario was imperative.** New scenarios required either a new Go test file
   (credentials, applyset) or special-casing inside `e2e_examples_test.go` (the
   `argoproj.io` substring check, the `-configuration-localization` suffix check, the
   `ignoreExamples` map). The harness leaked scenario knowledge into Go.

The new harness replaces both with a declarative per-scenario `e2e.yaml`, a generic Go
runner, and per-component setup scripts the harness composes from each scenario's
`requires:` list.

## Audience separation

Scenarios fall into two distinct audiences:

- **User-facing demos** — readable, minimal, illustrative, referenced from the README.
  Live under `kubernetes/controller/examples/`.
- **Test-only fixtures** — exercise edge cases the demos do not (multi-stage choreography,
  protected-registry credentials, etc.). Live under
  `kubernetes/controller/test/e2e/scenarios/` and never appear in user docs.

A scenario has exactly one home. Both roots use the same `e2e.yaml` schema, the same
walker, and the same runner.

## Folder layout

<details>
<summary>Full directory tree</summary>

```
kubernetes/controller/
  Taskfile.yml                                # RESOURCE_TIMEOUT default, focus passthrough
  examples/                                   # user-facing demos (also tested)
    README.md
    helm/
      fluxcd/                                 # Flux HelmRelease delivery
        kro/                                  # kro-based scenarios
          simple/
          simple-nested-status/
          nested/
          nested-signed/                      # ships ocm.software signing keypair
          signing/                            # ships ocm.software signing keypair
          configuration-localization/
        crossplane/                           # Crossplane delivery
          simple/
          signing/
          nested/
          nested-signed/
          simple-nested-status/
          configuration-localization/
          simple-fnc-kro/                     # function-kro variant
      argocd/                                 # ArgoCD Application delivery
        kro/                                  # kro-based scenarios
          simple/
          simple-nested-status/
          nested/
          nested-signed/                      # ships ocm.software signing keypair
          signing/                            # ships ocm.software signing keypair
          configuration-localization/
        crossplane/                           # Crossplane delivery
          simple/
          signing/
          nested/
          nested-signed/
          simple-nested-status/
          configuration-localization/
    kustomize/                                # split by delivery tool
      fluxcd/                                 # Flux Kustomization delivery
        simple/
        configuration-localization/
      argocd/                                 # ArgoCD Application delivery
        simple/
        configuration-localization/
    k8s-manifest/
      simple/
  test/e2e/
    DESIGN.md                                 # this file
    e2e_suite_test.go                         # SynchronizedBeforeSuite, component install
    e2e_runner.go                             # ScenarioConfig, walkScenarios, loadScenario, runScenario
    e2e_runner_test.go                        # unit tests for runner functions (no Ginkgo)
    e2e_scenarios_test.go                     # Ginkgo spec tree: discoverAndLoad, Describe("scenarios")
    cmd/
      shard/main.go                           # CI shard helper (Q14)
    setup/
      local.sh                                # entry: cluster + all components
      cluster.sh                              # kind cluster + registry + RBAC (always invoked by local.sh)
      components/                             # one script per component (Q3, Q7a)
        kro.sh
        crossplane.sh
        flux-source.sh
        flux-helm.sh
        flux-kustomize.sh
        argocd.sh
        protected-registry-basic-auth.sh
        protected-registry-docker-config-json.sh
    hooks/
      registry.go                             # central map literal (Q16)
      applyset.go                             # applyset-* hooks
    scenarios/                                # test-only fixtures
      applyset/
        pruning/
      credentials/
        basic-auth/
        docker-config-json/
```

</details>

Each scenario directory contains an `e2e.yaml` plus the fixture files the scenario
references (bootstrap, component-constructor, rgd, instance, k8s-manifest, signing keys,
`.ocmconfig`, etc.).

### Naming

Scenarios are identified by their **slash-separated path relative to their root**.
`${SCENARIO_SIMPLE_NAME}` is the dashed form
(`helm-fluxcd-simple`, `credentials-basic-auth`) for embedding in Kubernetes
resource names, where `/` is invalid.

### Discovery rule

The walker descends each root recursively. **The first directory containing an
`e2e.yaml` is treated as a scenario; the walker does not descend into it further.**
Nested scenarios are illegal and cause a load-time error. Directories without an
`e2e.yaml` are walked through, allowing intermediate grouping folders (`helm/`,
`helm/fluxcd/`, `credentials/`) and incidental files (`README.md`).

## The `e2e.yaml` schema

Minimal example:

```yaml
requires: [kro, flux-source, flux-helm]

deploy:
  - apply: bootstrap.yaml
    waitFor:
      - kubectl: "--for=create --for=condition=Ready=true rgd/${SCENARIO_SIMPLE_NAME}"
  - apply: instance.yaml
    waitFor:
      - kubectl: "--for=create --for=condition=Available deployment.apps/${SCENARIO_SIMPLE_NAME}-podinfo"
```

<details>
<summary>Full schema reference with all fields</summary>

```yaml
# Optional. Overrides the global RESOURCE_TIMEOUT for this scenario.
# Applies to deploy[].waitFor timeouts.
timeout: 5m

# Required. Components the harness must install before the scenario runs. Each name
# corresponds to a script at test/e2e/setup/components/<name>.sh. Unknown names cause
# a load-time error. Order matters: dependencies first.
requires:
  - kro
  - flux-source
  - flux-helm
  - argocd

# Optional. OCM components to prepare (transfer to the local registry) before deploy.
# Each entry runs the equivalent of the current PrepareOCMComponent helper.
prepare:
  components:
    - constructor: component-constructor.yaml      # optional; auto-detected if file exists in scenario dir
      signingKey: ocm.software                     # optional; private key path
      ocmConfig: .ocmconfig                        # optional; --config for `ocm transfer`
      registry: ${PROTECTED_REGISTRY_BASIC_AUTH}   # optional; overrides IMAGE_REGISTRY for this component
      copyResources: true                          # optional; adds --copy-resources to transfer

# Optional. Hooks (named Go functions) chained in array order. Each phase wraps the
# corresponding harness step. Resolved against test/e2e/hooks/registry.go at
# Ginkgo describe-registration time (log.Fatalf on unknown names).
preDeployHooks: []
postDeployHooks: []
preAssertHooks: []
postAssertHooks: []
preCleanupHooks: []
postCleanupHooks: []

# Required. Ordered deploy steps. Each step may have:
#   - apply: <path>          — runs kubectl apply -f
#   - waitFor: [...]         — each entry is a kubectl wait invocation
# A wait failure is a deploy-step failure (not an assertion failure): assert: does
# not run, cleanup still does.
deploy:
  - apply: bootstrap.yaml
    waitFor:
      - kubectl: "--for=create --for=condition=Ready=true rgd/${SCENARIO_SIMPLE_NAME}"
        timeout: 2m                                # optional, overrides scenario-level timeout
  - apply: instance.yaml
    waitFor:
      - kubectl: "--for=create --for=condition=Available deployment.apps/${SCENARIO_SIMPLE_NAME}-podinfo"
      - kubectl: "--for=condition=Ready=true pod -l app.kubernetes.io/name=${SCENARIO_SIMPLE_NAME}-podinfo"

# Optional. Final-state validation, run after deploy completes. Not needed if
# the deploy's waitFor conditions are sufficient to guarantee success criteria.
assert:
  # Optional. Field-equality checks against rendered cluster state, expressed as
  # `kubectl get <resource> -o jsonpath=<jsonPath>` == <value>.
  fieldEquals:
    - resource: pod -l app.kubernetes.io/name=${SCENARIO_SIMPLE_NAME}-podinfo
      jsonPath: '{.items[0].spec.containers[0].image}'
      value: ${IMAGE_REGISTRY_HOST}/stefanprodan/podinfo:6.9.1

# Optional. kubectl commands to run for diagnostics. Each entry specifies a
# kubectl subcommand and a label for log grouping. When omitted, a default set
# runs (controller pods/logs, kro pods/events, RGD conditions). The runner
# executes these commands when the spec fails OR when the GitHub Actions
# workflow is in debug mode (RUNNER_DEBUG=1 or ACTIONS_STEP_DEBUG=true) — so
# a "Re-run with debug logging" yields a cluster snapshot on green runs too.
debug:
  - kubectl: get pods -n ${CONTROLLER_NAMESPACE} -o wide
    label: controller-pods
  - kubectl: logs -n ${CONTROLLER_NAMESPACE} deploy/ocm-k8s-toolkit-controller-manager --tail=80 --all-containers
    label: controller-logs
  - kubectl: get helmrelease -A -o wide
    label: helmreleases
```

</details>

### Templated variables

All string fields support `${VAR}` substitution. The variable list is fixed; unknown
references cause a load-time error.

| Variable | Value | Example |
|---|---|---|
| `${SCENARIO_SIMPLE_NAME}` | scenario folder with `/` → `-` (k8s-safe) | `helm-fluxcd-simple` |
| `${IMAGE_REGISTRY_HOST}` | host:port, no scheme | `image-registry:5000` |
| `${PROTECTED_REGISTRY_BASIC_AUTH}` | basic-auth registry URL | `http://localhost:31002` |
| `${PROTECTED_REGISTRY_DOCKER_CONFIG_JSON}` | dcj registry URL | `http://localhost:31003` |
| `${CONTROLLER_NAMESPACE}` | controller-manager namespace | `ocm-k8s-toolkit-system` |

`${PROTECTED_REGISTRY_*}` resolve only when the scenario lists the corresponding
component in `requires:`.

<details>
<summary>Lifecycle (per scenario)</summary>

```mermaid
sequenceDiagram
    participant Runner as runScenario()
    participant Cluster as Kind Cluster
    participant OCM as OCM CLI
    participant Hooks as hooks/registry.go

    Runner->>OCM: prepare: ocm transfer ctf → registry
    Runner->>Hooks: preDeployHooks
    Runner->>Cluster: deploy: kubectl apply + waitFor
    Runner->>Hooks: postDeployHooks
    Runner->>Hooks: preAssertHooks
    Runner->>Cluster: assert: jsonpath checks
    Runner->>Hooks: postAssertHooks
    Runner->>Hooks: preCleanupHooks
    Runner->>Cluster: cleanup: DeferCleanup (kubectl delete)
    Runner->>Hooks: postCleanupHooks
    alt On failure or RUNNER_DEBUG/ACTIONS_STEP_DEBUG set
        Runner->>Cluster: debug: kubectl diagnostic commands
    end
```

</details>

## The runner

[`e2e_runner_test.go`](./e2e_runner_test.go) is the execution engine: `ScenarioConfig`, `walkScenarios`,
`loadScenario`, `runScenario`, and all supporting helpers, together with pure Go unit tests (no Ginkgo). The Ginkgo spec tree lives in
[`e2e_scenarios_test.go`](./e2e_scenarios_test.go), which calls `discoverAndLoad` at package-init
time and registers one `It()` per discovered scenario via `runScenario`.

### Debug commands

Each scenario may declare a `debug:` array of `kubectl` commands. The runner
runs them via `DeferCleanup`, prefixing each line with `[DEBUG] <label>:`.
The trigger is the OR of two conditions:

1. **The spec failed** — `CurrentSpecReport().Failed()` returns true. This is
   the historical behaviour and stays the default.
2. **The GitHub Actions workflow is in debug mode** — `RUNNER_DEBUG=1` (set
   when an operator picks "Re-run with debug logging" in the Actions UI) or
   `ACTIONS_STEP_DEBUG=true` (step-level debug, typically toggled via a repo
   variable or secret). Either flag is sufficient.

The second trigger means a debug-mode re-run on a green pipeline still yields
the cluster snapshot. Operators no longer need to manufacture a failure to
inspect controller pods, kro events, or RGD conditions on a passing run.
Locally the same applies: prepend `RUNNER_DEBUG=1` to the test command to
force diagnostics on success.

If a scenario omits `debug:`, a built-in default set runs (controller
pods/logs, kro pods/events, RGD conditions) — see `defaultDebugCommands` in
[`e2e_runner_test.go`](./e2e_runner_test.go). The detection helper is `isWorkflowDebug`
in the same file.

### Hook registry

Hooks are registered as a single map literal in
[`test/e2e/hooks/registry.go`](./hooks/registry.go). `discoverAndLoad` is called
at Ginkgo `Describe()` registration time (package-init). For each scenario,
`loadScenario` → `validateHookRefs` resolves every name in every `*Hooks` array
against the registry and calls `log.Fatalf` (aborting the process) if any
reference is unknown. This catches typos before any cluster work begins.

## Setup composition

[`test/e2e/setup/local.sh`](./setup/local.sh) is the single entrypoint operators use to set up a fresh kind
cluster. By default it only runs [`cluster.sh`](./setup/cluster.sh) (kind cluster + host registry + RBAC).
Component installation is deferred to the e2e runner, which installs each scenario's
dependencies on demand via `requires:`. Pass `--all-components` to also pre-install
every component script — useful for fast local iteration when you want focused runs
to start instantly without waiting for component setup.

```sh
# Default: cluster only — components installed on demand by the runner
task test/e2e/setup/local

# Pre-install all components — good for rapid focused iteration
task test/e2e/setup/local -- --all-components
```

Before any scenario runs, `SynchronizedBeforeSuite` (proc 1) calls
`collectAndInstallRequires`: it walks both roots, unions the `requires:` entries
across all focus-matching scenarios, and runs each unique component script exactly
once via an errgroup. When `--all-components` was used at cluster-setup time,
these script invocations are idempotent no-ops.

Each component script must be **idempotent**: invoking it on a cluster that already has
the component installed must succeed without changes.

## CI sharding

[`test/e2e/cmd/shard`](./cmd/shard/main.go) is a small Go program that:

1. Walks both roots, enumerates scenario names (same logic as `walkScenarios`).
2. By default, assigns one shard per scenario. Pass `--shards=N` to group
   scenarios into N round-robin buckets instead.
3. Emits `matrix=...` to `$GITHUB_OUTPUT` as a JSON array, one entry per shard.

The GitHub Actions workflow consumes the matrix:

```yaml
jobs:
  discover:
    outputs:
      matrix: ${{ steps.discover.outputs.matrix }}
    steps:
      - uses: actions/checkout@v4
      - id: discover
        run: go run ./test/e2e/cmd/shard >> "$GITHUB_OUTPUT"

  e2e:
    needs: discover
    strategy:
      fail-fast: false
      matrix:
        include: ${{ fromJSON(needs.discover.outputs.matrix) }}
    steps:
      - uses: actions/checkout@v4
      - run: task kubernetes/controller:test/e2e/setup/local
      - run: task kubernetes/controller:test/e2e -- "${{ matrix.focus }}"
```

Each shard provisions only the cluster (the default); component installation is deferred
to the runner's `requires:` step, so each shard only installs what its scenario needs.

<details>
<summary>CI sharding diagram</summary>

```mermaid
graph TD
    subgraph "discover job"
        D1["checkout"] --> D2["go run cmd/shard"]
        D2 --> D3["matrix=JSON → GITHUB_OUTPUT"]
    end

    D3 --> E1["shard 0: applyset/pruning"]
    D3 --> E2["shard 1: credentials/basic-auth"]
    D3 --> E3["shard 2: helm/argocd/kro/simple"]
    D3 --> E4["..."]
    D3 --> E5["shard N: kustomize/fluxcd/simple"]

    subgraph "each shard (parallel)"
        S1["kind create cluster"] --> S2["requires: install components"]
        S2 --> S3["helm install controller"]
        S3 --> S4["runScenario(focus)"]
        S4 --> S5["debug: on failure or workflow debug mode"]
    end

    E1 --> S1
    E2 --> S1
    E3 --> S1
    E5 --> S1
```

</details>

## Operator UX

Single Taskfile target, optional positional regex passed to Ginkgo `--focus=`:

| Command | Effect |
|---|---|
| `task test/e2e` | run all scenarios |
| `task test/e2e -- helm/fluxcd/kro/simple` | run one scenario |
| `task test/e2e -- helm/fluxcd/` | run all thirteen Flux helm scenarios |
| `task test/e2e -- helm/argocd/` | run all twelve ArgoCD helm scenarios |
| `task test/e2e -- helm/` | run all 25 helm scenarios |
| `task test/e2e -- credentials/` | run both credentials scenarios |
| `task test/e2e -- examples` | run only the `Context("examples")` block (30 demos) |
| `task test/e2e/fresh -- helm/fluxcd/kro/simple` | teardown + setup + run one scenario from scratch |
| `task test/e2e/setup/local` | provision kind cluster (components installed on demand by runner) |
| `task test/e2e/setup/local -- --all-components` | provision kind cluster + pre-install all components |
| `task test/e2e/teardown` | delete kind cluster and registry |
| `RESOURCE_TIMEOUT=10m task test/e2e` | bump global timeout |

The retired `task test/e2e/example NAME=foo` is replaced by `task test/e2e -- foo`.

## Worked examples

<details>
<summary>1a. Flux-only helm demo (<code>examples/helm/fluxcd/kro/simple/e2e.yaml</code>)</summary>

```yaml
requires: [kro, flux-source, flux-helm]

deploy:
  - apply: bootstrap.yaml
    waitFor:
      - kubectl: "--for=create --for=condition=Ready=true rgd/${SCENARIO_SIMPLE_NAME}"
  - apply: instance.yaml
    waitFor:
      - kubectl: "--for=create --for=condition=Available deployment.apps/${SCENARIO_SIMPLE_NAME}-podinfo"
      - kubectl: "--for=condition=Ready=true pod -l app.kubernetes.io/name=${SCENARIO_SIMPLE_NAME}-podinfo"
```

`component-constructor.yaml` is auto-detected. `argocd` is *not* in `requires:`.

</details>

<details>
<summary>1b. ArgoCD-only helm demo (<code>examples/helm/argocd/kro/simple/e2e.yaml</code>)</summary>

```yaml
requires: [argocd]

deploy:
  - apply: bootstrap.yaml
    waitFor:
      - kubectl: "--for=create --for=condition=Ready=true rgd/${SCENARIO_SIMPLE_NAME}"
  - apply: instance.yaml
    waitFor:
      - kubectl: "--for=create --for=jsonpath={.status.sync.status}=Synced --for=jsonpath={.status.health.status}=Healthy applications.argoproj.io/${SCENARIO_SIMPLE_NAME} -n argocd"
      - kubectl: "--for=create --for=condition=Available deployment.apps/${SCENARIO_SIMPLE_NAME}-argocd-podinfo -n default-argocd"
```

No `OCIRepository` or `HelmRelease`. `flux-source`/`flux-helm` are *not* in `requires:`.

</details>

<details>
<summary>1c. Crossplane Composition demo (<code>examples/helm/fluxcd/crossplane/simple/e2e.yaml</code>)</summary>

```yaml
requires:
  - crossplane
  - flux-source
  - flux-helm

deploy:
  - apply: bootstrap.yaml
    waitFor:
      - kubectl: "--for=create --for=condition=Ready=true repository/${SCENARIO_SIMPLE_NAME}-repository -n default"
      - kubectl: "--for=create --for=condition=Ready=true component/${SCENARIO_SIMPLE_NAME}-component -n default"
  - apply: resource.yaml
    waitFor:
      - kubectl: "--for=create --for=condition=Ready=true resource/${SCENARIO_SIMPLE_NAME}-resource-xrd -n default"
  - apply: deployer.yaml
    waitFor:
      - kubectl: "--for=create --for=condition=Ready=true deployer/${SCENARIO_SIMPLE_NAME}-deployer"
      - kubectl: "--for=create --for=condition=Established=true xrd/helmfluxcdsimples.examples.ocm.software"
  - apply: instance.yaml
    waitFor:
      - kubectl: "--for=create --for=condition=Ready=true helmfluxcdsimples/${SCENARIO_SIMPLE_NAME}"
      - kubectl: "--for=create --for=condition=Available deployment.apps/${SCENARIO_SIMPLE_NAME}-podinfo"
      - kubectl: "--for=condition=Ready=true pod -l app.kubernetes.io/name=${SCENARIO_SIMPLE_NAME}-podinfo"
```

The OCM Deployer delivers the XRD and Composition; `bootstrap.yaml` covers the OCM stack,
`resource.yaml` and `deployer.yaml` wire the Crossplane delivery, and `instance.yaml` creates
the XR instance.

</details>

<details>
<summary>2. Multi-stage test choreography (<code>test/e2e/scenarios/applyset/pruning/e2e.yaml</code>)</summary>

```yaml
requires: []                       # only controller-manager (always present)

prepare:
  components:
    - constructor: component-constructor.yaml
    - constructor: component-constructor-2.yaml

deploy:
  - apply: bootstrap.yaml
  - apply: bootstrap-deployer.yaml
    waitFor:
      - kubectl: "--for=create --for=condition=Available deployment.apps/${SCENARIO_SIMPLE_NAME}-podinfo"
      - kubectl: "--for=condition=Ready=true pod -l app.kubernetes.io/name=${SCENARIO_SIMPLE_NAME}-podinfo"
      - kubectl: "--for=create --for=condition=Available deployment.apps/${SCENARIO_SIMPLE_NAME}-podinfo-2"
      - kubectl: "--for=condition=Ready=true pod -l app.kubernetes.io/name=${SCENARIO_SIMPLE_NAME}-podinfo-2"

postAssertHooks:
  - applysetPatchToV2          # kubectl patch component … semver=2.0.0
  - applysetAssertPruning      # podinfo-2 deployment is pruned, podinfo remains
  - applysetDeleteDeployer     # kubectl delete deployer
  - applysetAssertCascade      # all derived resources gone
```

</details>

<details>
<summary>3. Protected-registry credentials (<code>test/e2e/scenarios/credentials/basic-auth/e2e.yaml</code>)</summary>

```yaml
requires: [protected-registry-basic-auth, kro, flux-source, flux-helm]

prepare:
  components:
    - ocmConfig: .ocmconfig
      registry: ${PROTECTED_REGISTRY_BASIC_AUTH}

deploy:
  - apply: bootstrap.yaml
    waitFor:
      - kubectl: "--for=create --for=condition=Ready=true rgd/basic-auth -n default"
  - apply: bootstrap-instance.yaml
    waitFor:
      - kubectl: "--for=create --for=condition=Available deployment.apps/basic-auth-podinfo -n default"
      - kubectl: "--for=condition=Ready=true pod -l app.kubernetes.io/name=basic-auth-podinfo -n default"
```

`constructor:` is omitted — auto-detected from `component-constructor.yaml`.

</details>

<details>
<summary>4. Docker-config-json registry credentials (<code>test/e2e/scenarios/credentials/docker-config-json/e2e.yaml</code>)</summary>

```yaml
requires: [protected-registry-docker-config-json, kro, flux-source, flux-helm]

prepare:
  components:
    - ocmConfig: .ocmconfig
      registry: ${PROTECTED_REGISTRY_DOCKER_CONFIG_JSON}

deploy:
  - apply: bootstrap.yaml
    waitFor:
      - kubectl: "--for=create --for=condition=Ready=true rgd/docker-config-json -n default"
  - apply: instance.yaml
    waitFor:
      - kubectl: "--for=create --for=condition=Available deployment.apps/docker-config-json-podinfo -n default"
      - kubectl: "--for=condition=Ready=true pod -l app.kubernetes.io/name=docker-config-json-podinfo -n default"
```

</details>

<details>

## Decision summary

<summary>Decision summary (Q1–Q16)</summary>

| # | Decision | Choice | Rationale |
|---|---|---|---|
| Q1 | Driving problem | Fix setup time **and** declarative scenario addition | Both pains are real; one without the other is a half-fix. |
| Q2 | Per-scenario contract | `e2e.yaml` | Single file authors edit; the alternative was Go test code per scenario. |
| Q3 | Component provisioning | Opaque scripts under `setup/components/<name>.sh` | New components = drop a script; harness has no taxonomy to migrate. |
| Q4 | Cluster lifecycle | Persistent local, ephemeral CI shards | Optimises both audiences: dev iteration speed, CI hermeticity. |
| Q5 | Folder layout | Audience-split: `examples/` (demos) vs `test/e2e/scenarios/` (test-only), family-grouped | Honors the audience split; one home per scenario, no duplication. |
| Q5b | Helm sub-grouping | Within `examples/helm/`, split by delivery tool (`helm/fluxcd/`, `helm/argocd/`) and then by operator (`kro/`, `crossplane/`). Each scenario uses **only** its named tool and operator. | Removes the implicit "Flux + ArgoCD always shipped together" coupling; gives readers an unambiguous reference for each tool/operator combination; the runner can skip the unused stack via `requires:`. |
| Q6 | Interpreter | Generic runner + named Go hooks (chainable arrays) | Imperative escape hatch without per-scenario Go file. |
| Q7a | `requires:` shape | Opaque list, validated against script existence | Avoids schema lock-in as the component taxonomy evolves. |
| Q7b | `prepare:` shape | Structured `components: []` block | The full preparation matrix is small and finite; structure beats hooks. |
| Q7c | Hook phases | Six (pre/post × deploy/assert/cleanup) | Symmetric seams cover known needs (`applyset-pruning`, future cleanup). |
| Q7d | Assertion DSL | `fieldEquals: []` only | Simple field-equality covers all active scenarios; `resources[]` and `assert.kubectl[]` were dead weight. |
| Q7e | Templating | `${VAR}` envsubst, fixed list | Stays declarative; rejects Go templates' Turing-complete invitation. |
| Q8 | Cleanup | Default no-op; per-spec `DeferCleanup` deletes applied manifests | `cascadeFromBootstrap` was speculative and unused; removed. |
| Q9 | Registration & discovery | Recursive walk, `e2e.yaml`-presence discriminator, slash-named, descend-stops-at-first-`e2e.yaml` | Auto-discovery; family grouping in folders; no manual registration. |
| Q10 | `deploy:` shape | Ordered step list with `apply`+`waitFor` | Author controls intermediate waits; common case stays terse. |
| Q11 | Timeout layering | Global default + scenario override + per-waitFor override | Scenario-level handles the common case; per-step `timeout:` covers outlier waits without bumping the whole scenario. |
| Q12 | Global timeout source | `RESOURCE_TIMEOUT` env var (`scenarioTimeout()` in runner); `go test -timeout` controls the Go test binary wall-clock limit | Operator surface is the runner env var; 10m default when unset. |
| Q13 | Invocation | Ginkgo `--focus=` via Taskfile `{{.CLI_ARGS}}` | Reuses Ginkgo's existing matcher; no harness regex code. |
| Q14 | CI sharding | Dynamic `cmd/shard`, one shard per scenario by default | Self-updating as scenarios are added/removed; pass `--shards=N` to group into fewer buckets. |
| Q15 | `description:` field | Omitted; optional `README.md` next to `e2e.yaml` | Folder name is self-documenting; descriptions invite marketing copy. |
| Q16 | Hook registry | Map literal in `hooks/registry.go`, validated at Ginkgo describe-registration time (`log.Fatalf` on unknown name) | Single source of truth; typos surface before any cluster work. |

</details>

<details>

## Migration plan 

<summary>Migration plan (Stages 1–6, all complete)</summary>

All stages have landed. The legacy test files (`e2e_examples_test.go`,
`e2e_credentials_test.go`, `e2e_applyset_test.go`) and the monolithic
`hacks/setup.sh` have been removed. The record below is preserved for historical
context only.

### Stage 1 — runner skeleton

- Land `e2e_runner.go`, `walkScenarios`, `mustLoadE2EYaml`, the variable substitution
  helper, and the `runScenario` orchestration loop.
- Land `test/e2e/hooks/registry.go` with an empty `Registry` map.
- Land `cmd/shard` (CI shard helper).
- Refactor `e2e_suite_test.go` to call the new `BeforeSuite`.

### Stage 2 — split setup into per-component scripts

- Decompose `hacks/setup.sh` into `setup/local.sh` + `setup/components/*.sh`.
- Each component script must be idempotent.

### Stage 3 — migrate examples scenarios

- Move each user-facing example into the family-grouped layout under `examples/`.
- Author `e2e.yaml` for each migrated scenario.
- Delete the legacy `e2e_examples_test.go`.

### Stage 4 — migrate test-only scenarios

- Move `test/e2e/testdata/` → `test/e2e/scenarios/credentials/`.
- Author `e2e.yaml` plus the `applyset*` hooks.
- Delete `e2e_credentials_test.go` and `e2e_applyset_test.go`.

### Stage 5 — wire CI sharding

- Update the GitHub Actions workflow to use the `discover` + matrix pattern.
- Default: one shard per scenario; pass `--shards=N` to reduce parallelism if needed.

### Stage 6 — cleanup

- Remove `task test/e2e/example` from the Taskfile.
- Update README and any docs that linked `examples/<name>/` to the new paths.
- Remove `hacks/` if `setup.sh` is the only thing left.

Each stage was independently mergeable and left the suite passing.

</details>
