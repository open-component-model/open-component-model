# Examples

User-facing demos for the OCM Kubernetes Controller Toolkit. Each subfolder is
a runnable, copy-as-starting-point example showing one way to wire OCM into a
delivery flow.

For setup instructions see the [`Getting Started`](../README.md#getting-started)
section of the controller README.

> **Audience.** Folders here are demos a human user might read or fork.
> Test-only fixtures (corner cases the e2e suite needs but a user would not
> copy) live under [`../test/e2e/scenarios/`](../test/e2e/scenarios/) instead.
> The split — and how the e2e runner discovers both — is described in
> [`../test/e2e/DESIGN.md`](../test/e2e/DESIGN.md).

---

## Layout

Examples are grouped by delivery family:

```
examples/
├── helm/                    # Helm chart delivery, split by delivery tool
│   └── fluxcd/              # Flux HelmRelease
│       └── kro/             # kro-based scenarios
│           ├── simple/
│           ├── signing/
│           ├── nested/
│           ├── nested-signed/
│           ├── simple-nested-status/
│           └── configuration-localization/
├── kustomize/               # Kustomize delivery, split by delivery tool
│   └── fluxcd/              # Flux Kustomization
│       ├── simple/
│       └── configuration-localization/
└── k8s-manifest/            # raw manifest delivery
    └── simple/
```

---

## Families

### `helm/` — Helm chart delivery, split by delivery tool

OCM publishes the chart (and any referenced image resources) to OCI; the chart
is then delivered into the cluster by a delivery tool.

#### `helm/fluxcd/kro/` — Flux HelmRelease (kro)

OCM resource → kro `ResourceGraphDefinition` → `OCIRepository` → `HelmRelease`.

| Folder | Shows |
|---|---|
| `simple/` | Smallest end-to-end: chart from OCM, kro + Flux release. Start here. |
| `signing/` | Signed component; controller verifies the signature before resource access. |
| `nested/` | Component reference chain — chart resource lives in a referenced component. |
| `nested-signed/` | Signed nested component; signature traverses the reference. |
| `simple-nested-status/` | Same as `simple/` but uses the nested `oci:` status field shape (`additional.oci.{registry,repository,tag,digest}`) instead of flat fields. |
| `configuration-localization/` | OCM configuration + localization rules rewriting image references and env vars at delivery time, applied via `HelmRelease.spec.values`. |

### `kustomize/` — Kustomize delivery, split by delivery tool

OCM publishes the kustomize tree (and any referenced image resources); the
tree is then delivered into the cluster by a delivery tool.

#### `kustomize/fluxcd/` — Flux Kustomization

OCM resource → Flux `GitRepository` → Flux `Kustomization`.

| Folder | Shows |
|---|---|
| `simple/` | Plain Kustomize delivery. |
| `configuration-localization/` | Configuration + localization applied to a Kustomize tree via Flux `Kustomization.spec.patches`. |

### `k8s-manifest/` — Plain manifest delivery

| Folder | Shows |
|---|---|
| `simple/` | OCM resource carrying a raw Kubernetes manifest, delivered without Flux. |

---

## Running an example

The e2e suite runs every example in this directory automatically. To run one
locally:

```sh
# All examples
task kubernetes/controller:test/e2e

# A single example (regex over family/tool/scenario)
task kubernetes/controller:test/e2e -- --focus="helm/fluxcd/kro/simple"

# A whole family
task kubernetes/controller:test/e2e -- --focus="^helm/"
```

See [`../test/e2e/DESIGN.md`](../test/e2e/DESIGN.md) §"Operator UX" for the
full command surface.

---

## Adding a new example

1. Decide on the family (`helm/`, `kustomize/`, `k8s-manifest/`) — or propose a
   new one in your PR description if none fit. For `helm/` and `kustomize/`,
   also pick the delivery tool sub-folder (`fluxcd/`). Within `helm/fluxcd/`,
   also pick the operator sub-folder (`kro/` for kro-based scenarios). Add the
   scenario under both delivery tool variants if it should exist for each tool.
2. Create `examples/<family>[/<tool>][/<operator>]/<scenario>/` with at minimum:
   - `component-constructor.yaml`
   - `bootstrap.yaml`
   - `e2e.yaml` (declares how the e2e runner deploys and asserts)
3. Add a row to the family table above.
4. Follow [`../test/e2e/AUTHORING.md`](../test/e2e/AUTHORING.md) for the
   `e2e.yaml` schema, naming variables, and hook conventions.

If your scenario only exists to exercise an edge case the suite needs — not as
a pattern a user would copy — put it under `test/e2e/scenarios/` instead.
