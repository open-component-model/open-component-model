# ADR: Bindings CI and Release Strategy

* **Status**: proposed
* **Deciders**: SIG Runtime
* **Date**: 2026-07-06

## Context and Problem Statement

This is the approximate dependency structure of the go modules stored inside the monorepo at time of writing:
```mermaid
flowchart TD
    A_cli["cli"]:::top
    A_k8s["kubernetes/controller"]:::k8s
    B_transfer["transfer"]
    C_helm["helm"]
    D_input_dir["input/dir"]
    D_input_file["input/file"]
    D_input_utf8["input/utf8"]
    D_plugin["plugin"]
    E_constructor["constructor"]
    E_sigstore["sigstore"]
    F_oci["oci"]
    F_signing["signing"]
    G_desc_norm["descriptor/normalisation"]
    G_gpg["gpg"]
    G_repository["repository"]
    G_rsa["rsa"]
    G_transform["transform"]
    H_credentials["credentials"]
    H_ctf["ctf"]
    H_desc_rt["descriptor/runtime"]
    H_http["http"]
    I_blob["blob"]
    I_configuration["configuration"]
    I_desc_v2["descriptor/v2"]
    I_generator["generator"]
    J_cel["cel"]
    J_dag["dag"]
    J_runtime["runtime"]

    I_blob --> J_runtime
    I_configuration --> J_runtime
    E_constructor --> G_desc_norm
    E_constructor --> F_oci
    H_credentials --> I_configuration
    H_credentials --> J_dag
    H_ctf --> I_blob
    G_desc_norm --> H_desc_rt
    H_desc_rt --> I_desc_v2
    I_desc_v2 --> J_runtime
    I_generator --> J_runtime
    G_gpg --> H_credentials
    G_gpg --> H_desc_rt
    C_helm --> D_plugin
    H_http --> I_configuration
    D_input_dir --> E_constructor
    D_input_file --> E_constructor
    D_input_utf8 --> E_constructor
    F_oci --> H_ctf
    F_oci --> H_http
    F_oci --> G_repository
    D_plugin --> E_constructor
    D_plugin --> F_signing
    G_repository --> I_blob
    G_repository --> H_credentials
    G_repository --> H_desc_rt
    G_rsa --> H_credentials
    G_rsa --> H_desc_rt
    F_signing --> G_desc_norm
    E_sigstore --> H_credentials
    E_sigstore --> F_signing
    B_transfer --> C_helm
    B_transfer --> G_transform
    G_transform --> J_cel
    G_transform --> H_credentials
    A_cli --> G_gpg
    A_cli --> D_input_dir
    A_cli --> D_input_file
    A_cli --> D_input_utf8
    A_cli --> G_rsa
    A_cli --> E_sigstore
    A_cli --> B_transfer
    A_k8s --> G_rsa
    A_k8s --> B_transfer

    classDef top fill:#ffb3b3,stroke:#cc6666
    classDef k8s fill:#b3ffb3,stroke:#66cc66
```

Each module has its own semantic version and release process. To propagate a change in e.g. `runtime` we have to:

1. release the module(s) 
2. bump `go.mod` & `go.sum` files in modules that depend on it (i.e. the next layer) 
3. Start again at `1.` for any module modified

On a central module like `runtime`, this would entail releasing step by step in at least 10 layers:

### Release layers (bottom-up)

| Layer | Module | Direct internal dependencies |
|-------|--------|------------------------------|
| 0 | `cel`, `dag`, `runtime` | - |
| 1 | `blob`, `configuration`, `descriptor/v2`, `generator` | `runtime` |
| 2 | `credentials` | `configuration`, `dag`, `runtime` |
| 2 | `ctf` | `blob` |
| 2 | `descriptor/runtime` | `descriptor/v2`, `runtime` |
| 2 | `http` | `configuration`, `runtime` |
| 3 | `descriptor/normalisation` | `descriptor/runtime`, `descriptor/v2`, `runtime` |
| 3 | `gpg`, `rsa` | `credentials`, `descriptor/runtime`, `runtime` |
| 3 | `repository` | `blob`, `configuration`, `credentials`, `descriptor/runtime`, `runtime` |
| 3 | `transform` | `cel`, `credentials`, `dag`, `runtime` |
| 4 | `oci` | `blob`, `configuration`, `credentials`, `ctf`, `descriptor/runtime`, `descriptor/v2`, `http`, `repository`, `runtime` |
| 4 | `signing` | `descriptor/normalisation`, `descriptor/runtime`, `runtime` |
| 5 | `constructor` | `blob`, `credentials`, `dag`, `descriptor/normalisation`, `descriptor/runtime`, `descriptor/v2`, `oci`, `repository`, `runtime` |
| 5 | `sigstore` | `credentials`, `descriptor/runtime`, `runtime`, `signing` |
| 6 | `input/dir`, `input/file`, `input/utf8` | `blob`, `constructor`, `runtime` |
| 6 | `plugin` | `blob`, `configuration`, `constructor`, `credentials`, `descriptor/runtime`, `descriptor/v2`, `repository`, `runtime`, `signing` |
| 7 | `helm` | `blob`, `configuration`, `constructor`, `credentials`, `descriptor/runtime`, `descriptor/v2`, `http`, `oci`, `plugin`, `repository`, `runtime` |
| 8 | `transfer` | `blob`, `configuration`, `credentials`, `dag`, `descriptor/runtime`, `descriptor/v2`, `helm`, `oci`, `repository`, `runtime`, `signing`, `transform` |
| 9 | `cli` | `blob`, `cel`, `configuration`, `constructor`, `credentials`, `ctf`, `dag`, `descriptor/normalisation`, `descriptor/runtime`, `descriptor/v2`, `gpg`, `helm`, `http`, `input/dir`, `input/file`, `input/utf8`, `oci`, `plugin`, `repository`, `rsa`, `runtime`, `signing`, `sigstore`, `transfer`, `transform` |
| 9 | `kubernetes/controller` | `blob`, `configuration`, `credentials`, `ctf`, `descriptor/normalisation`, `descriptor/runtime`, `descriptor/v2`, `helm`, `oci`, `plugin`, `repository`, `rsa`, `runtime`, `signing`, `transfer`, `transform` |

This complexity is currently managed by the developers and can get particularly challenging when logic has to be adjusted across multiple modules at once.
This ADR is concerned with the possible ways the developer experience could be optimized in regard to development of the bindings and which tradeoffs we want to make.

## Decision Drivers

* **Inter-module development friction**: A conceptually single change that spans multiple modules requires multiple PRs and releases to be implemented end-to-end.
* **Inter-module regressions**: If a breaking change in a module is not caught by reviewers or existing tests (within that module), the release process can get blocked down the line. Inter-module integration is currently not automatically tested in PRs, and thus there is no guarantee that the CI catches errors like this.
* **Release process complexity**: Presently, the release process is done manually and reflects the underlying complexity of the dependencies between modules.
* **CI complexity and scalability**: As with the release, the underlying complexity manifests in the CI. But currently, each module can be tested independently and changes have to be introduced one module at a time. This automatically limits the scope of what has to be built per single PR.
* **External consumer ease of use**: The modularization gives consumers fine-granular control over what (transitive) dependencies they want to introduce into their projects.
* **Project-wide high impact**: Any change made to the development and/or release process has project-wide implications. Mistakes here can block the entire team, not just single developers. But the same can be true for uncaught inter-module regressions.

## Considered Options

* Option 1: `go.work` approach in CI - We add `go.work` files to always test all bindings against each other's bleeding edge versions.
* Option 2: Monolithic binding library(ies) - We bundle multiple (or all) bindings into a smaller set of larger libraries.
* Option 3: Accept status quo - it's mostly in a working state without having to invest additional development time.
* Option 4: Partial solutions - Perhaps there is a decent trade-off somewhere in between the all-or-nothing approaches above.

## Decision Outcome

Chosen [Option 2](#option-2-monolithic-binding-libraryies): "Monolithic binding library(ies)"

Justification:

* The cost of option 1 is unacceptably high at the moment. 
* Implementation of option 2 is expected to be relatively low effort
* Primary technical blocker was disproved by experiment
* If we guard the modularity through the CI, we improve the developer experience significantly while losing nothing

### Option 1: `go.work`

This was the initially preferred solution. An [ADR](https://github.com/open-component-model/open-component-model/pull/2930) for adopting it was written and [prototype for the CI](https://github.com/matthiasbruns/open-component-model/pull/115) was implemented.

This proved that several of the development hindrances could be solved with this approach:

* Multi-module changes in a single PR: The core friction of inter-module development can be alleviated this way.
* Guarding against regressions: All libraries run with all the newest version for their sibling libraries. Many - maybe most - inter-module regressions could be caught this way.
* Dependency-graph-aware CI execution and release PoC: The potential impact of code changes as well as the release process depend on tool-support that is aware of the dependency chain.

But while working on it, it also became clear that there were several unexpected complications and side effects of this approach that made the trade-off unfavorable.

#### Release automation for sequential tagging would be mandatory

We **cannot** tag all bindings at once. To write e.g. `blob/go.sum`'s checksum for `runtime@v0.0.9`, the Go toolchain must fetch `runtime@v0.0.9` from the proxy, but the proxy only serves it after the tag is pushed. This forces a "layer-by-layer" release design.

This on its own is not different from the current release process. But `go.work` flips the equation: Currently, every version is pinned and every semver explicit - with `go.work` no version is pinned and all semvers are implicit (as in implicitly always on the next version).

Consequently, what would be tested on main and in PRs would always be different from the actual currently released library behavior. Releases have to happen with `go.work` disabled. 
An individual hotfix for one library is suddenly not necessarily safe to execute, because the tested behavior is always on the bleeding edge of all libraries. Thus, a release of **any** binding would require a release of **every** binding, because the `go.mod` file would only become the source of truth during the release process. 

> Technically, this could be avoided, but the only way to avoid it would be to implement a separate release process just for hotfix versions. This process would have to pin layers below the change and test all layers above. It seems unlikely that this effort would be worth the investment; rather, a hotfix would simply trigger a release of all bindings.

The regular release of all bindings would require automation, hence we would have to build this layered release process. Since Go releases are effectively eternal, any mistake or edge-case in this automation could have cumbersome and high-impact ramifications.

#### Conditional test execution and release depend on an evaluated dependency tree

Without additional tooling, all PRs would run all test suites on every build. Turn-around time and CI load would increase significantly. 

This comes with the territory, as the goal is that more inter-module interaction is tested; more test executions in PRs is a desired outcome. 
But this is only true for features that actually span multiple modules. For any single-module change, the number of tests executed would increase significantly. Especially when everything runs on every commit.

> One relevant caveat about visible test scope: When thinking about a feature implementation end-to-end, running all the tests once can actually be fewer test runs than the alternative - because either way all bumped libraries would have to be tested as well, it would just happen in individual PRs.

To optimize this, the CI would have to consider the dependency tree of the modules and (as for the layered release process above) would have to use it to determine what to run in what order. While this is feasible to implement, it would likely again be tooling that we have to implement and maintain ourselves. 

#### Modularity & Dependency Tree Stability

Due to the current design the CI enforces a strict modularity between the modules and a staged rollout. If a developer mistakenly includes a file from another module, this fails in the CI without any additional logic (due to the sparse checkout). Since we would still need to enforce the modular design - and would require it to be able to release - we would now have to guard against it in another way.

#### `go.work` masks inconsistent `go.mod`

When `go.work` is active, Go resolves all internal imports from the local tree, **never consulting `go.mod` pins**. Hence, `go.mod` and `go.sum` can vary arbitrarily, be inconsistent, or reference non-existent versions. 

Resolution via MVS can also be inconsistent with the consumer experience, as resolution with `go.work` enabled can differ from the `go.mod` based resolution consumers would get.

### Option 2: Monolithic binding library(ies)

Instead of managing each module as an independent library, we could merge some or even all bindings into a singular library. This approach would not rely on `go.work` and thus would sidestep some of the associated downsides. Instead, we would remove the individual `go.mod` files with one `go.mod` at `./bindings/go/`. The module path would be `ocm.software/open-component-model/bindings/go`, making all existing import paths sub-packages of that module and thereby a non-breaking change for consumers (only the `require` target would have to change).

The release process and CI setup would also be simple: one version, one tag - but irreversible. Once consumers depend on the monolithic module path, splitting it back out is a breaking change everywhere. The other presented options are reversible experiments; this one isn't.

Consumers would no longer be able to consume mixed versions of different libraries, since they all share one version. Though it seems unlikely that this use case needs to be preserved.

Modularity could still be enforced, but it would have to be done through additional tooling, similar to the `go.work` approach (e.g., depguard, import path restrictions in golangci-lint).

Most tests would run on every commit, a shared downside with the `go.work` approach.

Consumers could still choose to consume only individual modules of the bundled library, and in principle Go would optimize the unused parts away. 

#### Security scans and reflection

For this approach to be feasible, it is paramount that we don't break the consumer experience in two key ways:

* Go might not optimize away the unused libraries due to reflection or cascading imports. Consumers would then receive all our (transitive) dependencies into their BOM.
* Security scans would then trigger on anything inside our entire dependency tree. This was and is a big pain-point in OCM v1. A consumer that e.g. only needs `descriptor/v2` could also receive the entire helm SDK and thus be impacted by any security vulnerabilities discovered within.

At present, this would not be the case for a monolithic release of v2, see [investigation](https://github.com/jneisener/open-component-model/tree/worktree-monolithic-merge-test) for details.

The risk exists that this isolation could be broken in the future by introducing cascading imports (as OCM v1 did with `compdesc/init.go` → `signing/handlers` → `sigstore`) or something similar. The multi-module structure currently guards against this structurally: it's not possible to import across module boundaries without an explicit `require`. In a monolithic library, this guard would be replaced by something else.

#### Guard existing modularity 

We don't want to fundamentally change the structure of the modules, just ease the development and release process. To ensure the design does not get diluted over time, we need additional tool-support to take the place of what the independent versioning and sparse-checkout currently achieve.

##### `depguard` (`golangci-lint`)

[`depguard`](https://golangci-lint.run/docs/linters/configuration/#depguard) is currently [disabled](https://github.com/open-component-model/open-component-model/blob/62bd8025ecaa10fb6b0675821e7a3d77432ea0b7/golangci.yml), but already supported by our existing tools.
With `depguard` we could define rules per-package about what they're allowed to import. We could e.g. have an allow-list per binding layer:

```yaml
linters:
  settings:
    depguard:
      rules:
        # Lower-layer bindings must not import higher layers
        descriptor-v2:
          files:
            - "**/bindings/go/descriptor/v2/**"
          deny:
            - pkg: "ocm.software/open-component-model/bindings/go/helm"
              desc: "descriptor/v2 must not import helm"
            - pkg: "ocm.software/open-component-model/bindings/go/oci"
              desc: "descriptor/v2 must not import oci"
            - pkg: "ocm.software/open-component-model/bindings/go/transfer"
              desc: "descriptor/v2 must not import transfer"
            # ... etc for all higher layers
        # Runtime (layer 0) must not import any other binding
        runtime:
          files:
            - "**/bindings/go/runtime/**"
          deny:
            - pkg: "ocm.software/open-component-model/bindings/go/"
              desc: "runtime must not import other bindings"
          allow:
            - pkg: "ocm.software/open-component-model/bindings/go/runtime"
```

Downside: Verbose. We'd need a rule per layer, and the deny lists grow as bindings are added. Lists need to be maintained.

As we expect the dependency graph to stay pretty stable over time this might be good enough already. Either way, it should underline the feasibility of the approach.

##### Direct file references

The sparse checkout currently prevents e.g. `testdata, _ := os.ReadFile("../../../oci/testdata/fixture.tar")`, but since this sidesteps `import` they would not be caught by `depguard`.
If we see a risk that this does not get caught in reviews, we could probably guard against it in CI by searching for occurrences of `..` that leave the module boundary.

### Option 3: Accept status quo

There were reasons behind most of the decisions that led to the current repository structure and many of the reasons are still valid today:

* Forced intentionality: It's a lot of work to introduce an inter-module change; this should force a mindful approach when working on such an issue. Potentially hot-take: The process is a feature, not a bug.
* Separation of concerns: One or multiple of the modules could be taken over by other teams and developed independently. While speculative and long-term, coupling the modules closer together would likely eliminate that possibility.
* Learnings from v1: OCM v1 was not designed in a modular way which contributed to the overall state of being unmaintainable. Maybe the v2 design overshot the modularity goal, but the driving decisions behind it were the right ones for long-term success of the product.
* Partial automation via Renovate: For non-breaking changes, the layer-by-layer version bumping can be automated through Renovate's dependency update PRs, already reducing the manual overhead without requiring custom tooling.
* `go.work` can be used (and already is) for local development of features that cross module boundaries. Getting the changes merged still requires individual PRs, but local development is already friction-less in this regard.
* Over time the expectation is that especially lower layers stabilize and that inter-module implementation efforts are the exception, not the norm.

### Option 4: Partial solutions

None of the above options are optimal; each has its own undesirable tradeoffs. It's still an open question whether we can find a compromise that brings us some of the benefits of one of the options without all the associated costs.

#### Scheduled `go.work`-based builds on main

With a generated `go.work` file we could run all tests on a schedule for main. This would make inter-module regressions more discoverable (though only after a PR is already merged) without affecting the release process or requiring extensive tooling. Via the schedule we would also have direct control over how much additional CI overhead we generate.

This would only make sense if we react to a failure of the scheduled build though, so it would be something additional for the entire team to pay attention to.

#### Selective `go.work`-based builds in PRs

Similar to the idea above, a complete test-suite run like this could also be executed on PRs, e.g., for anything layer 2 or lower.

#### Incremental migration to `go.work`

One way to make option 1 more feasible could be to gradually migrate to it over time. E.g., by going top-down through the layers, a first iteration could include only `transfer` and `helm` in a committed `go.work` file. The CI and release process could then be built up iteratively over time and the initial impact on the release process would be minimal.

The downsides of option 1 still all apply, but the investment would not have to be made in a big-bang, and the risk would be more manageable. It would also be easier to revert a single step of this migration individually.

#### PR scoped implementation of option 1

We could ignore `go.work` files on main and only leverage them for PR builds. This way we could solve inter-module development friction as well as inter-module regressions - all while not affecting the release process at all.

The tradeoff would be that PRs and main test different things. A PR could pass all its tests (against bleeding-edge sibling versions) while the same code on main fails (against pinned released versions) - because the PR tests a future state that hasn't been released yet. Similar to the scheduled build option above, this would only make sense if the team proactively reacts to the state of main. Main would remain in a broken state until all pending releases have happened and the pinned modules are up-to-date with the bleeding edge state that was tested inside the PR.

#### Automated PR impact analysis

We have the dependency tree logic in the PoC and could use it to automatically comment the PR with its impact on higher layers, nudging developers to double-check their changes. Or prompt one of the review AI agents with that specific information.

## Pros and Cons of the Options

### Option 1: `go.work`

Pros:

* Solves inter-module development friction & inter-module regressions
* Relies on builtin Go feature (at least partially)

Cons:

* Shifts complexity from development time to release time, but does not reduce complexity
* Increases CI complexity; what the CI tests is not what consumers receive
* Would require a large time investment

### Option 2: Monolithic binding library(ies)

Pros:

* Solves inter-module development friction & inter-module regressions
* Simple to implement, simple release process
* Simple migration - no consumer import path changes required

Cons:

* Danger of breaking the consumer experience 
  * *Though we should be able to guard against in the CI*
* Danger of diluting the design and creating new forms of tech debt (e.g. by re-introducing the coupling problems that made OCM v1 unmaintainable)
  * *Though we should be able to guard against in the CI*
* Higher CI load because more tests run on every commit

### Option 3: Accept status quo

Pros:

* No additional time investment required 
* No risk of introducing instability

Cons:

* Inter-module development friction & inter-module regressions remain unaddressed
* Questionable scaling for newly added modules

### Option 4: Partial solutions

Different pro/contra depending on the different sub-options, see discussion in the section.

## Conclusion

We remain cautious of hidden complexity and unexpected landmines. But we believe that with a monolithic release of a **still highly modular** library we can get the best of both worlds: frictionless inter-module development, continuous inter-module regression testing, a simple release process, manageable CI complexity, while maintaining consumer ease of use.

The primary risk we accept is that modularity is no longer strictly enforced through the `go.mod` boundaries, but we should be able to minimize that risk by enforcing modularity in the CI e.g. via `depguard`.

## Appendix

### Impact of monolithic restructuring on consumer `go.sum` files
This describes the experiments and outcome persisted in https://github.com/jneisener/open-component-model/commit/a58aa4816fc7b42d56a82009f72d6070c99869ae.

#### Question

If we merge all bindings into a single Go module, does a consumer that only imports `descriptor/v2` get polluted with `helm`/`oci`/`k8s` dependencies in their `go.sum`?

#### Method

1. Create a consumer that imports only `descriptor/v2` and `runtime`
2. Record its `go.mod`/`go.sum` against the current multi-module structure (baseline)
3. Merge all bindings into a single `ocm.software/open-component-model/bindings/go` module
4. Re-tidy the consumer (same imports, same source code)
5. Compare `go.mod`/`go.sum`

#### Results

##### Baseline (multi-module, separate go.mod per binding)

- **go.sum**: 24 lines
- **Indirect deps**: 5 (`json-canonicalization`, `jsonschema/v6`, `yaml/v2`, `x/text`, `sigs.k8s.io/yaml`)
- No `helm`, `oci`, `k8s`, or other heavy deps present

##### After monolithic merge (single go.mod for all bindings)

- **go.sum**: 26 lines
- **Indirect deps**: 5 (same set, minor version differences)
- **Still no `helm`, `oci`, `k8s`, or other heavy deps present**

##### Comparison

| Metric | Multi-module | Monolithic | Delta |
|--------|-------------|------------|-------|
| go.sum lines | 24 | 26 | +2 |
| Indirect deps | 5 | 5 | 0 |
| helm SDK present | No | No | - |
| k8s client-go present | No | No | - |
| oras present | No | No | - |
| Binary builds | ✓ | ✓ | - |

The +2 lines in `go.sum` are due to minor version resolution differences (e.g., `go.yaml.in/yaml/v2 v2.4.4` vs `v2.4.3`), not new dependencies.

#### Conclusion

**Go's dead code elimination works correctly even with a monolithic module.**

A consumer importing only `descriptor/v2` from the monolithic library gets the same minimal dependency footprint as with the multi-module structure. The 135 dependencies of the full monolithic library (including `helm` SDK, `k8s` client-go, `oras`, `testcontainers`, etc.) are NOT pulled into the consumer's `go.sum`.

This is because:
1. Go resolves dependencies at the **package** level, not the module level
2. `go mod tidy` only includes packages reachable from the consumer's imports
3. The `init()` + `reflect.TypeOf()` pattern in `runtime/registry.go` does NOT cause cross-contamination because each binding registers into its own local scheme - there is no global scheme that imports all bindings

##### Caveat

This result holds as long as the monolithic library **does not** have a root-level package that imports all sub-packages. If such a package existed (e.g., for a convenience "import all" pattern), and the consumer imported it, all transitive dependencies would be pulled in.

---

#### Why did it happen in OCM v1?

Create a consumer that imports only `ocm.software/ocm/api/ocm/compdesc` (component descriptors - the v1 equivalent of `descriptor/v2`) and examine what gets pulled in.

##### Findings

Importing just `compdesc` pulls in **205 OCM packages**, including:
- 27 `sigstore`/`cosign`/`rekor` packages
- 21 `docker`/`oci`/`helm`/`vault`/`k8s` packages
- The entire credentials system, vault integration, docker config, etc.

##### Import Chain

```
compdesc/init.go
  └─ _ "ocm.software/ocm/api/tech/signing/handlers"    (blank import)
       └─ handlers/init.go
            └─ _ "ocm.software/ocm/api/tech/signing/handlers/sigstore"
            └─ _ "github.com/sigstore/cosign/v3/pkg/providers/all"
```

The `compdesc` package has its own `init.go` that blank-imports `signing/handlers`, which in turn blank-imports ALL signing handler implementations including sigstore.

Additionally:
```
compdesc -> credentials -> config -> datacontext -> ...
          -> signing/handlers -> sigstore (+ cosign + rekor + fulcio)
          -> credentials/extensions/repositories -> vault + dockerconfig + gardener
```

##### Conclusion

The difference is **not** monolithic vs multi-module alone - and not reflection on its own. It's the `init.go` cascade(s).
