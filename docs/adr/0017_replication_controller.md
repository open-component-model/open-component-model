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

  transferOptions:
    recursive: false
    copyMode: localBlob     # localBlob | allResources

  # References resolved in the Replication CR's namespace.
  ocmConfig:
    - name: my-ocm-config
      kind: Secret
      policy: Propagate

  suspend: false
```

`transferOptions` is an inline form and is of `apiextensionsv1.JSON` type, trading
CRD-level schema validation for forward compatibility.

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
    A[Reconcile] --> S{spec.suspend?}
    S -->|Yes| SX[No-op]
    S -->|No| A1[Read Component CR status]
    A1 --> A2{Component Ready and digest present?}
    A2 -->|No| A3[Requeue, wait for Component event]
    A2 -->|Yes| C{Source digest == lastTransferredDigest?}
    C -->|Yes| E[No-op, requeue after interval]
    C -->|No| F[Load effective OCM config]
    F --> G[BuildGraphDefinition]
    G --> H[Write TGD to filesystem]
    H --> I[Set TransferInProgress=True]
    I --> J[Proceed to Phase 2]
```

The reconciler gates on Component CR readiness: if the Component is not `Ready` or `status.componentInfo.digest` is
absent, the Replication requeues and waits for a Component event rather than acting on incomplete source state.

`suspend: true` short-circuits the entire flow.

#### Phase 2: Execute (run TGD)

Phase 2 is asynchronous. Submission returns immediately; the submitting reconcile exits and waits for the worker pool
to emit a completion event that triggers a new reconcile, which reads the result and updates status.

```mermaid
flowchart TD
    A[Submit TGD to worker pool] --> B{ErrTransferInProgress?}
    B -->|Yes| C[Exit reconciliation, wait for event]
    B -->|No, submission accepted| C
    C -.completion event.-> R[Reconcile reads worker result]
    R --> D{Transfer result}
    D -->|Success| E[Update lastTransferredVersion/Digest, set Ready=True, TransferInProgress=False]
    D -->|Failure| G[Set Ready=False with error, TransferInProgress=False, emit warning Event]
    E --> I[Requeue after interval]
    G --> I
```

Both terminal branches clear `TransferInProgress=False`.

Stale condition on any reconcile entry, if `TransferInProgress=True` but the worker pool has no in-flight 
key for this CR's UID (after pod crash or leader change), the condition is treated as stale, cleared, and Phase 1 runs again.

### Trigger Conditions

* Component CR digest differs from `status.lastTransferredDigest`.
* Replication CR spec changes (via `observedGeneration`).
* Interval elapsed.

The controller does not do drift detection. Transfers are content-addressed at the blob level so the registry takes care
of duplicates. Manual changes are resolved by bumping the Replication spec or waiting for the source digest to move.

### Worker Pool

Dedicated transfer worker pool, separate from resolution. Non-blocking submission backed by a bounded queue. If full
a retry error is emitted. On completion, emits an event to retrigger the reconciler. Burst reconciles for an in-flight
key return `ErrTransferInProgress` without re-submitting.

Transient source/target errors (network, 5xx, rate limit) retry with exponential
backoff inside the worker; terminal errors surface immediately as `Ready=False`.

### Transfer Spec Storage

TGDs are written to a scratch volume:

* Default: `emptyDir`. TGDs regenerate cheaply on pod restart.
* Optional: PVC for operators who need persistence across restarts.
* Path: `/var/run/ocm/transfer-specs/{namespace}-{name}-{sourceDigest}-{confighash}.json`.
* GC on CR deletion (finalizer) or when a newer `(sourceDigest, confighash)` pair supersedes the file.

Compressed inline storage on the CR and ConfigMap-backed storage were considered and rejected: 
both still hit Kubernetes object size limits and shift, rather than remove, the etcd pressure.

### Watches

* **Component CR**: field index on `spec.componentRef`.
* **Target Repository CR**: field index on `spec.targetRepositoryRef`; retriggers when the target repo spec changes (URL, auth).
* **ConfigMap referenced by `transferOptions` (ref form)**: field index; edits retrigger reconciliation.
* **Worker pool event source**: retriggers on async completion.
* **Finalizer**: `delivery.ocm.software/replication-finalizer` for cleanup.

### Deletion Semantics

When a Replication CR is deleted:

1. Finalizer blocks removal; reconciler observes `deletionTimestamp`.
2. In-flight transfer is canceled via a per-item context keyed by CR UID. Workers honor cancellation at the next safe point.
3. Bounded drain (default 30s, controller flag) waits for worker acknowledgement.
4. GC: remove TGD file, unregister event source.
5. Finalizer removed, CR deleted.

If the drain times out, the finalizer is force-removed and a warning is logged; the in-flight goroutine is reclaimed on pod restart.

_**Note**_: A canceled transfer may leave partial or corrupted blobs at the target. This is expected since we are cancelling
a stream and is reconciled by the next replication run, which is digest-idempotent.

### Pros

* Minimal complexity for users.
* Resolution service already works.
* Filesystem negates the limit from etcd.
* Internal plan/execute split enables future CRD separation.

### Cons

* Long transfers block a worker pool slot. Mitigated by configurable pool size.
* Filesystem TGD storage lost on pod restart. Regenerated on the next transfer,
  not on the next reconcile, so a pod restart while in sync leaves no TGD on
  disk until the source or target drifts. Post-success inspection across
  restarts requires a PVC.
* If filesystem isn't available because of read-only disks, this does not work.
* Transfer not independently observable as a K8s resource.
* A pod crash between TGD write and status update can leave an orphan file;
  reclaimed on the next reconcile or finalizer run.

## Pros and Cons of the Options

### Option 1: Single Replication CRD

#### Pro

* Minimal surface area.
* Follows resolution service pattern.
* Fastest to implement.
* Internal separation allows future split.

#### Con

* Transfer not independently trackable.
* Debugging needs filesystem access.

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
