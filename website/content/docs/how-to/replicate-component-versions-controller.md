---
title: "Replicate Component Versions with the Controller"
description: "Transfer a component version between two OCM repositories using the Replication controller."
icon: "🔁"
weight: 37
toc: true
---

## Goal

Use the OCM Kubernetes controller to transfer a component version, together with
the full graph of components it references (if recursion is enabled), from one OCM
repository to another. The transfer re-runs automatically whenever the source
`Component` resolves to a new version.

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
component's reference graph and transfers the component version into the target.
It records the transferred version and digest in its status and treats an unchanged
digest as a no-op, so it never re-transfers content that is already present.

Transfer behaviour (recursion depth, copy mode, upload type) and the registry
credentials are supplied as OCM configuration through `ocmConfig`. In the steps
below they live in two `Secret`s: the `Component` carries the credentials and
propagates them down, while the `Replication` declares the transfer settings
itself and references the `Component` to merge in those propagated credentials.

The configuration influences the way the transfer happens. For example, if `recursive`
is set to none zero number, it will copy all references of a component. `copyMode`
determines which resources are copied during a transfer operation. These options are the
same they are on the CLI side.

## Steps

{{< steps >}}
{{< step >}}

### Build a source component graph

To show recursion in action, build a small graph: a parent component that
references a child, each carrying a blob. Replicating the parent pulls the child
along with it.

<details>
  <summary>component-constructor.yaml</summary>

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

</details>

Push both components to the source registry:

```bash
ocm add cv --repository ghcr.io/<source-namespace> --constructor component-constructor.yaml
```

{{< /step >}}
{{< step >}}

### Create the source and target `Repository`

Both repositories are plain `Repository` objects pointing at OCI registries. The
source holds the component you already published; the target is where the
transfer writes.

<details>
  <summary>repositories.yaml</summary>

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

</details>

```bash
kubectl apply -f repositories.yaml
```

{{< /step >}}
{{< step >}}

### Create the transfer configuration and credentials

Store the configuration in two `Secret`s under the `.ocmconfig` key. The
`ocm-credentials` Secret holds the registry credentials and is propagated by the
`Component`; the `ocm-transfer-config` Secret holds the transfer settings and is
referenced directly by the `Replication`. The controller reads both as OCM
configuration.

<details>
  <summary>config.yaml</summary>

```bash
cat <<EOF > config.yaml
apiVersion: v1
kind: Secret
metadata:
  name: ocm-credentials
type: Opaque
stringData:
  .ocmconfig: |
    type: generic.config.ocm.software/v1
    configurations:
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
---
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
EOF
```

</details>

```bash
kubectl apply -f config.yaml
```

The `transfer.config.ocm.software` entry controls the transfer itself:

- `recursive: -1` follows component references; `0` disables recursion.
- `copyMode: localBlob` and `uploadType: ociArtifact` copy resource content into
  the target and re-upload OCI artifacts as artifacts rather than as plain blobs.
- using `copyMode: allResources` with `uploadType: ociArtifact` will initiate a
  streaming upload if the resource is an oci type (ociArtifact, for example).

{{< callout context="note" title="Propagating a single config" icon="outline/info-circle" >}}
Splitting the configuration is optional. If you set `policy: Propagate`, you can
keep everything (transfer settings and credentials) in one config on the
`Component` and propagate the whole thing down to the `Replication`, instead of
having the `Replication` declare its own transfer config.
{{< /callout >}}

{{< /step >}}
{{< step >}}

### Create the source `Component`

The `Component` resolves the version to replicate. Reference the `ocm-credentials`
`Secret` with `policy: Propagate` so the credentials flow on to the
`Replication`.

<details>
  <summary>component.yaml</summary>

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
      name: ocm-credentials
      policy: Propagate
EOF
```

</details>

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
declares the transfer settings through its own `ocmConfig`. Because declaring an
`ocmConfig` opts out of automatic inheritance from the parent, it also references
the `Component` explicitly to pull in the registry credentials the `Component`
propagates.

<details>
  <summary>replication.yaml</summary>

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
  ocmConfig:
    - kind: Secret
      name: ocm-transfer-config
    - kind: Component
      apiVersion: delivery.ocm.software/v1alpha1
      name: replication-component
EOF
```

</details>

```bash
kubectl apply -f replication.yaml
```

{{< callout context="note" title="Effective configuration" icon="outline/info-circle" >}}
The effective configuration the `Replication` reconciles with is the combination
of what it declares in its own `spec.ocmConfig`, what the `Component` propagates,
and the target `Repository`'s configuration.
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
