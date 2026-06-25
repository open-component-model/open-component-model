---
title: "Replicate Component Versions with the Controller"
description: "Transfer a component version between two OCM repositories using the Replication controller."
icon: "🔁"
weight: 37
toc: true
---

## Goal

Use the OCM Kubernetes controller to transfer a component version, together with
the full graph of components it references, from one OCM repository to another.
The transfer re-runs automatically whenever the source `Component` resolves to a
new version.

## Prerequisites

- [Controller environment]({{< relref "setup-controller-environment.md" >}}) set up
- The OCM CLI installed, to build and push the source component
- A source and a target OCI registry you can push to, with credentials that have
  write access (for example a
  [ghcr.io](https://docs.github.com/en/packages/learn-github-packages/introduction-to-github-packages)
  token with `write:packages`)

If you already have a component version in a source repository, skip the first
step and point the `Component` at it.

## How it works

A `Replication` references two objects:

- `spec.componentRef`, a `Component` that resolves and verifies the **source**
  version. The version that gets transferred is the one recorded in that
  `Component`'s status, so it has already been successfully reconciled.
- `spec.targetRepositoryRef`, the `Repository` the content is written to.

When the source `Component`'s resolved version changes, the controller walks the
component's reference graph and transfers every referenced component version into
the target. It records the transferred version and digest in its status and
treats an unchanged digest as a no-op, so it never re-transfers content that is
already present.

Transfer behaviour (recursion depth, copy mode, upload type) and the registry
credentials are supplied as OCM configuration through `ocmConfig`. In the steps
below the configuration lives in a single `Secret` that the `Component`
propagates to the `Replication`.

## Steps

{{< steps >}}
{{< step >}}

### Build a source component graph

To show recursion in action, build a small graph: a parent component that
references a child, each carrying a blob. Replicating the parent pulls the child
along with it.

```bash
echo "parent payload" > parent.txt
echo "child payload" > child.txt

cat <<EOF > component-constructor.yaml
components:
  - name: ocm.software/examples/replication/child
    version: 1.0.0
    provider:
      name: ocm.software
    resources:
      - name: data
        type: blob
        version: 1.0.0
        input:
          type: file
          path: ./child.txt
  - name: ocm.software/examples/replication/parent
    version: 1.0.0
    provider:
      name: ocm.software
    resources:
      - name: data
        type: blob
        version: 1.0.0
        input:
          type: file
          path: ./parent.txt
    componentReferences:
      - name: child
        componentName: ocm.software/examples/replication/child
        version: 1.0.0
EOF
```

Push both components to the source registry:

```bash
ocm add cv --repository ghcr.io/<source-namespace> --constructor component-constructor.yaml
```

{{< callout context="note" title="Unique reference names" icon="outline/info-circle" >}}
Keep component reference names unique across the constructor. The constructor's
digest cache is keyed by the local reference identity, so reusing a name across
components can stamp the wrong digest into a reference.
{{< /callout >}}

{{< /step >}}
{{< step >}}

### Create the source and target `Repository`

Both repositories are plain `Repository` objects pointing at OCI registries. The
source holds the component you already published; the target is where the
transfer writes.

```bash
cat <<EOF > repositories.yaml
apiVersion: delivery.ocm.software/v1alpha1
kind: Repository
metadata:
  name: replication-source
spec:
  repositorySpec:
    baseUrl: ghcr.io/<source-namespace>
    type: OCIRepository
  interval: 10m
---
apiVersion: delivery.ocm.software/v1alpha1
kind: Repository
metadata:
  name: replication-target
spec:
  repositorySpec:
    baseUrl: ghcr.io/<target-namespace>
    type: OCIRepository
  interval: 10m
EOF
```

```bash
kubectl apply -f repositories.yaml
```

{{< /step >}}
{{< step >}}

### Create the transfer configuration and credentials

Store the transfer settings and the registry credentials in a single `Secret`
under the `.ocmconfig` key. The controller reads this as OCM configuration.

```bash
cat <<EOF > transfer-config.yaml
apiVersion: v1
kind: Secret
metadata:
  name: ocm-transfer-config
type: Opaque
stringData:
  .ocmconfig: |
    type: generic.config.ocm.software/v1
    configurations:
      - type: transfer.config.ocm.software
        recursive: -1
        copyMode: localBlob
        uploadType: ociArtifact
      - type: credentials.config.ocm.software
        consumers:
          - identity:
              type: OCIRegistry
              hostname: ghcr.io
            credentials:
              - type: Credentials
                properties:
                  username: <username>
                  password: <token>
EOF
```

```bash
kubectl apply -f transfer-config.yaml
```

The `transfer.config.ocm.software` entry controls the transfer itself:

- `recursive: -1` follows component references to unlimited depth (`0` disables
  recursion, a positive number limits it).
- `copyMode: localBlob` and `uploadType: ociArtifact` copy resource content into
  the target and re-upload OCI artifacts as artifacts rather than as plain blobs.

{{< callout context="note" title="Credential identities" icon="outline/info-circle" >}}
List a consumer for every host involved. If your source and target live on
different registries, add an entry per hostname. The `OCIRegistry` 
identity type cover the pull and push paths used during a
transfer.
{{< /callout >}}

{{< /step >}}
{{< step >}}

### Create the source `Component`

The `Component` resolves the version to replicate. Reference the transfer `Secret`
with `policy: Propagate` so the configuration and credentials flow on to the
`Replication`.

```bash
cat <<EOF > component.yaml
apiVersion: delivery.ocm.software/v1alpha1
kind: Component
metadata:
  name: replication-component
spec:
  component: ocm.software/examples/replication/parent
  repositoryRef:
    name: replication-source
  semver: 1.0.0
  interval: 10m
  ocmConfig:
    - kind: Secret
      name: ocm-transfer-config
      policy: Propagate
EOF
```

```bash
kubectl apply -f component.yaml
```

Wait for it to become ready:

```bash
kubectl get component replication-component -o wide
```

{{< /step >}}
{{< step >}}

### Create the `Replication`

The `Replication` ties the source `Component` to the target `Repository`. It
inherits the transfer configuration and credentials propagated from the
`Component`.

```bash
cat <<EOF > replication.yaml
apiVersion: delivery.ocm.software/v1alpha1
kind: Replication
metadata:
  name: replication-example
spec:
  componentRef:
    name: replication-component
  targetRepositoryRef:
    name: replication-target
EOF
```

```bash
kubectl apply -f replication.yaml
```

{{< callout context="note" title="Attaching config directly" icon="outline/info-circle" >}}
Instead of relying on propagation, you can reference the configuration `Secret`
directly from the `Replication` with its own `spec.ocmConfig`. The effective
configuration is the combination of what the `Component` propagates, what the
`Replication` declares, and the target `Repository`'s configuration.
{{< /callout >}}

{{< /step >}}
{{< step >}}

### Watch it run

```bash
kubectl get replication replication-example -w
```

The transfer proceeds in two stages:

- While the controller walks the component's reference graph through the
  resolution service, the `Ready` condition stays `False` with reason
  `ResolutionInProgress` (one pass per graph level, event-driven).
- While the transfer executes, the `TransferInProgress` condition is `True`.
- On completion the `Ready` condition flips to `True`,
  `status.lastTransferredVersion` and `status.lastTransferredDigest` are set, and
  `TransferInProgress` returns to `False`.

```bash
kubectl get replication replication-example -o yaml
```

Re-applying with the same source digest is a no-op, the controller short-circuits
on `lastTransferredDigest`.

{{< /step >}}
{{< step >}}

### Verify the target

Confirm the parent landed in the target repository:

```bash
ocm get cv ghcr.io/<target-namespace>//ocm.software/examples/replication/parent:1.0.0
```

Because the transfer is recursive, the referenced child is present too:

```bash
ocm get cv ghcr.io/<target-namespace>//ocm.software/examples/replication/child:1.0.0
```

{{< /step >}}
{{< /steps >}}

## Troubleshooting

When a transfer fails, the `Ready` condition is set to `False` with the error
message, and per-transformation failures are recorded in
`status.lastFailedTransferEvents`:

```bash
kubectl get replication replication-example -o jsonpath='{.status.lastFailedTransferEvents}'
```

### Symptom: `Ready=False` stuck on `ResolutionInProgress`

**Cause:** The controller is still discovering the component's reference graph, or
a referenced component version cannot be resolved from the source repository.

**Fix:** Confirm the source `Component` is ready and that every referenced
component version actually exists in the source repository. Resolution is
event-driven and advances one graph level per pass, so a large graph takes
several reconciliations.

### Symptom: authentication or "unauthorized" errors during transfer

**Cause:** The credentials in the transfer `Secret` do not cover one of the hosts
involved, or lack push permission on the target.

**Fix:** Ensure a `credentials.config.ocm.software` consumer exists for every
registry hostname (source and target), and that the target token has write
access (for example `write:packages` on ghcr.io).

### Symptom: nothing happens after applying the `Replication`

**Cause:** The source `Component` or the target `Repository` is not yet `Ready`.
The controller waits for both before transferring.

**Fix:** Check both objects:

```bash
kubectl get component replication-component -o wide
kubectl get repository replication-target -o wide
```

## Next Steps

- [Verify Component Versions in the Controller]({{< relref "verify-component-version-controller.md" >}}) -
  Verify signatures on the source version before it is replicated

## Related Documentation

- [Concept: Kubernetes Controllers]({{< relref "docs/concepts/ocm-controllers.md#replication" >}}) -
  How Replication fits alongside the reconciliation chain
- [Reference: Replication CRD]({{< relref "docs/reference/kubernetes-api/replication.md" >}}) -
  Full API specification
- [How-To: Configure Credentials for OCM Controllers]({{< relref "docs/how-to/configure-credentials-ocm-controllers.md" >}}) -
  Set up registry credentials for the controller
- [Example: `replication-simple`](https://github.com/open-component-model/open-component-model/tree/main/kubernetes/controller/examples/replication-simple) -
  A complete, runnable manifest set used by the controller end-to-end tests
