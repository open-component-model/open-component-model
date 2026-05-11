# Replication Controller

* Status: proposed
* Deciders: @frewilhelm @Skarlso @fabianburth @jakobmoellerdev
* Date: 2026-04-16

Technical Story: [ocm-project#953](https://github.com/open-component-model/ocm-project/issues/953)

Supersedes: [previous replication ADR](../../kubernetes/controller/docs/adr/replication.md)

## Context and Problem Statement

The replication controller transfers component versions from a source to a target
repository. The previous ADR stored replication history in the CR status, which caused
etcd size pressure for unclear value. The library now provides Transformation Graph
Definitions for transfers, already used by the CLI via `bindings/go/transfer/`.

### Constraints

* TGDs for large component trees can approach or exceed Kubernetes storage practical limits due to etcd's default 1.5 MiB max request size.
* Only the latest resolved component version is replicated for now.

## Decision Drivers

* The controller should introduce as few new CRDs as possible.
* The design must allow splitting into separate CRDs later without breaking existing users.
* Transfer detection should be digest-based rather than version string comparisons.
* Transfer specs must remain inspectable for debugging without bloating etcd.

## Considered Options

* [Option 1: Single Replication CRD](#option-1-single-replication-crd)
* [Option 2: Replication + Transfer CRDs](#option-2-replication--transfer-crds)
* [Option 3: Replication + Job](#option-3-replication--job)

## Decision Outcome

Chosen option: **Option 1 (Single Replication CRD)**. Follows the existing async
resolution service pattern (worker pool + `ErrResolutionInProgress`). TGD building
and TGD execution are separated internally, so a future CRD split requires no
spec changes.

### CRD Design

```yaml
apiVersion: delivery.ocm.software/v1alpha1
kind: Replication
metadata:
  name: replicate-podinfo
  namespace: default
spec:
  componentRef:
    name: podinfo-component
    namespace: default

  # Ref of the `Repository` CRs that the transfer should happen to.
  targetRepositoryRef:
    name: target-repository
    namespace: default

  # Define a transfer config either by referencing a ConfigMap containing a transfer config or via
  # inlined values.
  # Follows the pattern from https://github.com/open-component-model/ocm-project/issues/869.
  transferConfig:
    name: transfer-config-map
    namespace: default
    # OR, name pending... 
    inlined:
      type: generic.config.ocm.software/v1
      configurations:
        - type: GlobalTransferOptions/v1alpha1 # Current not existing configuration type.
          spec:
            recursive: false
            copyMode: localBlob

  # References resolved in the Replication CR's namespace.
  ocmConfig:
    - name: my-ocm-config
      kind: Secret
      policy: Propagate

  suspend: false
```

An example ConfigMap containing transfer configuration:

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: transfer-config-map
  namespace: default
data:
  transferConfig: |
    type: generic.config.ocm.software/v1
    configurations:
      - type: GlobalTransferOptions/v1alpha1 # Current not existing configuration type.
        spec:
          recursive: false
          copyMode: localBlob
```

`copyMode`:

* `localBlob`: inline resource blobs into the component descriptor at the target. Default.
* `allResources`: transfer every resource as a standalone artifact; keep external references intact.

`recursive` controls whether referenced component versions are transferred alongside the root.

Source and target credentials are resolved through `ocmConfig`; no separate credential fields on the CR.

### Status Design

```yaml
status:
  conditions:
    - type: Ready
      status: "True"
      reason: TransferComplete
      message: "Successfully transferred component version 1.2.3"
    - type: TransferInProgress
      status: "False"
      reason: Idle

  lastTransferredVersion: "1.2.3"
  lastTransferredDigest: "sha256:abc123..."

  componentInfo:
    component: ocm.software/podinfo
    version: "1.2.3"
    digest: "sha256:abc123..."

  effectiveOCMConfig:
    - name: my-ocm-config
      kind: Secret
      policy: Propagate

  observedGeneration: 3
```

`componentInfo` reflects the currently observed source; `lastTransferredVersion`/`lastTransferredDigest` record the
last successful transfer. A mismatch means a transfer is pending, in-flight, or most recently failed; the `Ready`
and `TransferInProgress` conditions disambiguate which.

### Reconciliation Flow

Similar to the resolution service, this is the two-phase process for transfer.
Following the existing `ErrResolutionInProgress` pattern, phase 2 introduces a
transfer-specific in-progress sentinel error named `ErrTransferInProgress`.

#### Phase 1: Plan (build TGD)

```mermaid
flowchart TD
    A[Reconcile] --> B{spec.suspend?}
    B -->|Yes| C[Exit reconciliation]
    B -->|No| D[Read Component CR status]
    D --> E{Component Ready and digest present?}
    E -->|No| F[Requeue, wait for Component event]
    E -->|Yes| G{Source digest matches lastTransferredDigest?}
    G -->|Yes| H[No-op, requeue after interval]
    G -->|No| I[Load effective OCM config]
    I --> J[BuildGraphDefinition in memory]
    J --> K[Set TransferInProgress=True]
    K --> L[Proceed to Phase 2]
```

The reconciler gates on Component CR readiness: if the Component is not `Ready` or `status.componentInfo.digest` is
absent, the Replication requeues and waits for a Component event rather than acting on incomplete source state.

`suspend: true` short-circuits the entire flow.

#### Phase 2: Execute (run TGD)

Phase 2 is asynchronous. Submission returns immediately; the submitting reconcile exits and waits for the worker pool
to emit a completion event that triggers a new reconcile, which reads the result and updates status.

```mermaid
flowchart TD
    A[Submit TGD to worker pool] --> B[Exit reconciliation, wait for event]
    B -.completion event.-> C[Reconcile reads worker result]
    C --> D{Transfer result}
    D -->|Success| E[Update lastTransferredVersion/Digest, set Ready=True, TransferInProgress=False]
    D -->|Failure| F[Set Ready=False with error, TransferInProgress=False, emit warning Event]
    E --> G[Requeue after interval]
    F --> G
```

Both terminal branches clear `TransferInProgress=False`.

Stale condition check: on any reconcile entry, if `TransferInProgress=True` but the worker pool has no in-flight
key for this CR's UID (after pod crash or leader change), the condition is treated as stale, cleared, and Phase 1 runs again.

Burst reconciles for an already-submitted key return `ErrTransferInProgress` from the worker pool and exit without re-submitting;
the next completion event still triggers the status update above.

### Trigger Conditions

* Component CR digest differs from `status.lastTransferredDigest`.
* Replication CR spec changes (via `observedGeneration`).
* Interval elapsed.

The controller does not do drift detection. Transfers are content-addressed at the blob level so the registry takes care
of duplicates. Manual changes are resolved by bumping the Replication spec or waiting for the source digest to move.

### Worker Pool

Dedicated transfer worker pool, separate from resolution. Non-blocking submission backed by a bounded queue. If full, 
a retry error is emitted. On completion, emits an event to retrigger the reconciler. Burst reconciles for an in-flight
key return `ErrTransferInProgress` without re-submitting.

Transient source/target errors (network, 5xx, rate limit) retry with exponential
backoff inside the worker; terminal errors surface immediately as `Ready=False`.

### Transfer Spec Storage

TGDs are held in memory only. `BuildGraphDefinition` runs in Phase 1, the result is handed to the worker pool
in Phase 2, and the reference is dropped once the transfer terminates. Nothing is persisted.

Rationale:

* Read-only root filesystems are a common hardening baseline; writing scratch files would force an `emptyDir` or PVC
  mount on every deployment.
* TGDs regenerate cheaply from the source component and effective OCM config, so persistence buys nothing.
* Content-addressed trigger conditions (`sourceDigest`, `observedGeneration`) already make regeneration idempotent.

Persistence options considered and rejected:

* Inline on the CR / ConfigMap-backed storage: both hit Kubernetes object size limits and shift, rather than remove, etcd pressure.
* `emptyDir` / PVC scratch volume: adds deployment constraints (writable mount) for no durability benefit; a pod restart
  loses the file anyway since TGDs are regenerated on the next transfer, not rehydrated from disk.

### Watches

* **Component CR**: field index on `spec.componentRef`.
* **Target Repository CR**: field index on `spec.targetRepositoryRef`; retriggers when the target repo spec changes (URL, auth).
* **Worker pool event source**: retriggers on async completion.
* **Finalizer**: `delivery.ocm.software/replication-finalizer` for cleanup.

### Deletion Semantics

When a Replication CR is deleted:

1. Finalizer blocks removal; reconciler observes `deletionTimestamp`.
2. In-flight transfer is canceled via a per-item context keyed by CR UID. Workers honor cancellation at the next safe point.
3. Bounded drain (default 30s, controller flag) waits for worker acknowledgement.
4. Unregister event source; in-memory TGD reference is released with the worker item.
5. Finalizer removed, CR deleted.

If the drain times out, the finalizer is force-removed and a warning is logged; the in-flight goroutine is reclaimed on pod restart.

_**Note**_: A canceled transfer may leave partial or corrupted blobs at the target. This is expected since we are cancelling
a stream and is reconciled by the next replication run, which is digest-idempotent.

### Pros

* Minimal complexity for users.
* Resolution service already works.
* In-memory TGDs sidestep the etcd object size limit without requiring a writable filesystem.
* Internal plan/execute split enables future CRD separation.

### Cons

* Long transfers block a worker pool slot. Mitigated by configurable pool size.
* In-memory TGDs are not inspectable after a pod restart; debugging a failed transfer post-crash requires re-triggering reconciliation to regenerate the TGD.
* Transfer not independently observable as a K8s resource.
* Pod memory footprint scales with concurrent in-flight TGDs; bounded by the worker pool size.

## Pros and Cons of the Options

### Option 1: Single Replication CRD

#### Pro

* Minimal surface area.
* Follows resolution service pattern.
* Fastest to implement.
* Internal separation allows future split.

#### Con

* Transfer not independently trackable.
* TGDs are not inspectable outside of an in-flight reconcile; debugging a failed transfer requires re-triggering reconciliation or enabling debug logs to dump the generated TGD.

### Option 2: Replication + Transfer CRDs

#### Pro

* Clean separation of concerns.
* Transfer CRs are independently observable and retryable.

#### Con

* TGDs can exceed Kubernetes/etcd object size constraints, so external storage is still needed.
* Two CRDs from day one with no user demand.

### Option 3: Replication + Job

#### Pro

* Built-in retry, timeout, resource limits.
* Isolated execution.

#### Con

* Requires separate transfer executor image.
* State sharing adds complexity.
* Job lifecycle management is non-trivial.

## Future Evolution

1. **Multi-version replication**: extend `spec` with `versionConstraint`.
2. **CRD split**: internal phases map directly to Replication/Transfer CRDs.
3. **Transfer policies**: `replaceIfPresent`, `skipExisting`, etc.
4. **Multiple targets**: `ReplicationSet` CRD for fan-out.

## Links

* [ocm-project#953](https://github.com/open-component-model/ocm-project/issues/953)
* [Previous replication ADR](../../kubernetes/controller/docs/adr/replication.md)
* [Transfer CLI](../../cli/cmd/transfer/)
* [Resolution service](../../kubernetes/controller/internal/resolution/)
