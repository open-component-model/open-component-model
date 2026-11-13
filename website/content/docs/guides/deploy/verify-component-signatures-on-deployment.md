---
title: "Verify Component Signatures on Deployment"
description: "Configure parent-component signature verification and understand reference-digest checks during resource resolution and deployment."
weight: 70
toc: true
aliases:
  - /docs/how-to/verify-component-version-controller/
  - /docs/how-to/verify-component-versions-in-the-controller/
---

## Goal

Configure the OCM Kubernetes controller to automatically verify component version signatures before reconciling resources.

## You'll end up with

- A Secret holding an OCM configuration that requests signature verification
- A `Component` resource that reconciles only if the signature verifies

**Estimated time:** ~5 minutes

## Prerequisites

- [Controller environment]({{< relref "setup-controller-environment.md" >}}) set up
- A [signed component version]({{< relref "docs/guides/sign/sign-with-plain-rsa.md" >}}) in a local CTF
  archive
- The public key file at `/tmp/keys/public-key.pem`
  (from [Generate Signing Keys]({{< relref "generate-signing-keys.md" >}}))
- Access to an OCI registry
  (e.g., [ghcr.io](https://docs.github.com/en/packages/learn-github-packages/introduction-to-github-packages))

## Steps

{{< steps >}}
{{< step >}}

### Transfer the signed component version to the registry

Push your signed component version from the local CTF archive to a remote OCI registry:

```bash
ocm transfer cv /tmp/helloworld/transport-archive//github.com/acme.org/helloworld:1.0.0 ghcr.io/<your-namespace>
```

Verify the upload:

```bash
ocm get cv ghcr.io/<your-namespace>//github.com/acme.org/helloworld:1.0.0
```

<details>
<summary>Expected output</summary>

```text
 NESTING  COMPONENT                       VERSION  PROVIDER  IDENTITY
 └─       github.com/acme.org/helloworld  1.0.0    acme.org  name=github.com/acme.org/helloworld,version=1.0.0
```
</details>

{{< /step >}}

{{< step >}}

### Prepare the verification configuration

The controller takes verification from the central OCM configuration, the same
`signing.config.ocm.software` and `credentials.config.ocm.software` entries the CLI uses. Two
entries are needed: one naming the signature to verify, and one supplying the public key to
verify it with.

```bash
cat > ocmconfig.yaml <<EOF
type: generic.config.ocm.software/v1
configurations:
- type: signing.config.ocm.software/v1alpha1
  signature: default
  verifier:
    type: RSASigningConfiguration/v1alpha1
- type: credentials.config.ocm.software
  consumers:
  - identity:
      type: RSA/v1alpha1
      algorithm: RSASSA-PSS
      signature: default
    credentials:
    - type: Credentials/v1
      properties:
        publicKeyPEM: |
$(sed 's/^/          /' /tmp/keys/public-key.pem)
EOF
```

The public key is embedded as PEM, indented under `publicKeyPEM`. No base64 encoding is needed.

{{< callout context="note" title="Note" icon="outline/info-circle" >}}
Only an entry that names a `signature` requests verification of that signature. An entry without
one merely supplies the verifier that named entries fall back to, so it does not turn verification
on by itself.

The `verifier` field is optional and defaults to RSASSA-PSS. Set it to select a different
verification handler; the controller ships with `RSASigningConfiguration/v1alpha1`,
`GPGSigningConfiguration/v1alpha1`, and `SigstoreVerificationConfiguration/v1alpha1` handlers
registered.
{{< /callout >}}

Store the configuration in a Secret. The controller reads it from the `.ocmconfig` key:

```bash
kubectl create secret generic signing-verification-secret --from-file=.ocmconfig=ocmconfig.yaml
```

{{< /step >}}

{{< step >}}

### Create the `Repository` resource

Create and apply a `Repository` that points to your OCI registry:

```bash
cat <<EOF > repository.yaml
apiVersion: delivery.ocm.software/v1alpha1
kind: Repository
metadata:
  name: helloworld-repository
spec:
  repositorySpec:
    baseUrl: ghcr.io/<your-namespace>
    type: OCIRegistry
  interval: 10m
EOF
```

```bash
kubectl apply -f repository.yaml
```

{{< /step >}}

{{< step >}}

### Create the `Component` resource with verification

Create and apply a `Component` that references the repository and the configuration Secret:

```bash
cat <<EOF > component.yaml
apiVersion: delivery.ocm.software/v1alpha1
kind: Component
metadata:
  name: helloworld-component
spec:
  component: github.com/acme.org/helloworld
  repositoryRef:
    name: helloworld-repository
  semver: ">=1.0.0"
  interval: 10m
  ocmConfig:
    - apiVersion: v1
      kind: Secret
      name: signing-verification-secret
EOF
```

```bash
kubectl apply -f component.yaml
```

{{< callout context="note" title="Note" icon="outline/info-circle" >}}
The Secret is looked up in the `Component`'s own namespace unless the reference sets a
`namespace` field. A `Resource` inherits this configuration from its referenced `Component`;
a `NamespacedDeployer` or `Deployer` inherits it through its referenced `Resource`. These
consumers re-verify the configured signatures on the parent component version.
{{< /callout >}}

{{< callout context="caution" title="Ensure component version verification" icon="outline/alert-triangle" >}}
Specifying a different `ocmConfig` in an object referencing the `Component` (e.g. a `Resource`) drops the inherited config including the verifier configuration. As a result, the component version won't be verified again! To keep the verifier configuration and enforce component version verification, reference the parent objects, too.
{{< /callout >}}

{{< /step >}}

{{< step >}}

### Verify the `Component` is ready

Check that the `Component` resource reconciles successfully with verification:

```bash
kubectl get component helloworld-component
```

<details>
<summary>Expected output</summary>

```text
NAME                   COMPONENT                        VERSION   READY   AGE   STATUS
helloworld-component   github.com/acme.org/helloworld   1.0.0     True    98s   Applied version 1.0.0
```
</details>

To confirm the signature was actually verified (and not served from the resolver cache without
re-running verification), check the controller logs for the per-signature `verified signature`
entry:

```bash
kubectl logs -n ocm-k8s-toolkit-system deploy/ocm-k8s-toolkit-controller-manager | grep "verified signature"
```

<details>
<summary>Expected output</summary>

```text
{"level":"info","ts":"2026-04-28T15:58:14Z","msg":"verified signature","signature":"default","verifier":"RSASigningConfiguration/v1alpha1"}
```
</details>

If verification fails, the `Component` will not become ready and an error condition will be set.

<details>
<summary>Check for failure</summary>

```bash
kubectl get component helloworld-component
```

```text
NAME                   COMPONENT                        VERSION   READY   AGE   STATUS
helloworld-component   github.com/acme.org/helloworld             False   7s    signature verification failed for signature default: missing public key, required for plain RSA signatures
```
</details>

{{< /step >}}
{{< /steps >}}

## When component references are verified {#how-verification-protects-component-references}

The steps above verify the signatures on the component version selected by the `Component` CR.
The `Component` controller does **not** fetch referenced component versions or verify resource
content. Its Ready condition therefore does not mean the whole component graph has been verified.

Reference-digest checks happen when a `Resource`, `NamespacedDeployer`, or `Deployer` resolves a
selected resource from a referenced component version. The path is defined on the `Resource`
in `spec.resource.byReference.referencePath`.

At each hop with a complete reference digest, the resolved child's descriptor digest is
compared with the digest recorded by its parent. A mismatch makes the CR performing the check
NotReady. Only the selected path is traversed, not every reference in the parent. If
`referencePath` is omitted, the resource is selected directly from the parent component version
and no reference traversal is needed.

Children reached through the path are checked against reference digests, not their own
signatures. `verificationPolicy` controls the separate check of the **selected resource's
content**; `Never` does not disable configured parent signatures or checks of complete
reference digests.

For example, if the signed parent contains a component reference named `application`, and that
child contains a resource named `manifests`, select it with a `Resource` like this:

```yaml
apiVersion: delivery.ocm.software/v1alpha1
kind: Resource
metadata:
  name: application-manifests
spec:
  componentRef:
    name: helloworld-component
  resource:
    byReference:
      referencePath:
        - name: application
      resource:
        name: manifests
  verificationPolicy: Always
```

`application` is the reference's name in the parent descriptor, not the name of another
Kubernetes `Component` CR. To deploy the selected manifests, point a `NamespacedDeployer` at
`application-manifests` as described in [Deploy Kubernetes Manifests with the OCM Deployer]({{< relref "deploy-manifests-with-deployer.md" >}}).

Reference digests are computed and added automatically by `ocm add cv`. The `ocm sign cv`
command checks that the component version is safely digestible and warns if any reference or
resource digests are missing.

{{< callout context="caution" title="A verified parent does not always authenticate its children" icon="outline/alert-triangle" >}}
If any selected reference has an absent or incomplete digest, that child is resolved without
digest or signature verification, even under `verificationPolicy: Always`. That policy verifies
the selected resource against its own descriptor; it does not establish a complete trust chain.

For end-to-end integrity, record complete, valid digests on every reference along the selected
path and a usable digest on the selected resource. Re-sign affected parent descriptors after
updating them.
{{< /callout >}}

## Troubleshooting

Inspect the Ready condition on the CR whose reconciliation failed. Parent-signature failures
can appear on the `Component` or on a consumer re-verifying it. Reference-path failures appear
on the `Resource`, or on the `NamespacedDeployer` / `Deployer` when it re-resolves the path;
the parent `Component` may remain Ready.

```bash
kubectl get component <name> -o jsonpath='{.status.conditions[?(@.type=="Ready")].message}'
kubectl get resource <name> -o jsonpath='{.status.conditions[?(@.type=="Ready")].message}'
kubectl get namespaceddeployer <name> -o jsonpath='{.status.conditions[?(@.type=="Ready")].message}'
```

For a cluster-scoped `Deployer`, use `kubectl get deployer <name>` with the same JSONPath.

### Symptom: "signature verification failed for signature ..."

**Cause:** The verification credential (public key or certificate) does not match the private
key used to sign the component version.

**Fix:** Ensure you are using the correct verification credential that corresponds to the
private key used during signing. Verify the signature name matches by inspecting the component
version:

```bash
ocm get cv ghcr.io/<your-namespace>//github.com/acme.org/helloworld:1.0.0 -o yaml | grep -A 5 "signatures:"
```

### Symptom: "signature ... not found in component"

**Cause:** The component version does not contain a signature with the name given in the
`signing.config.ocm.software` entry.

**Fix:** Check which signatures exist on the component version and ensure the `signature` field
of your signing configuration entry matches:

```bash
ocm get cv ghcr.io/<your-namespace>//github.com/acme.org/helloworld:1.0.0 -o yaml | grep -A 5 "signatures:"
```

### Symptom: "digest mismatch ... for component version ...:..."

**Cause:** A parent component version includes a reference to another component version with a
recorded digest. When the controller resolves that reference, the actual content does not match
the recorded digest. This typically means the referenced component was modified or re-published
after the parent recorded its digest.

**Fix:** Inspect the parent component version's references to identify the digest mismatch:

```bash
ocm get cv ghcr.io/<your-namespace>//github.com/acme.org/parent-component:1.0.0 -o yaml
```

Look at the `componentReferences:` section and their `digest` fields. To resolve, rebuild the
parent component version with correct reference digests, re-sign it, and then publish it.

### Symptom: "not safely digestible" warning

The component version does not satisfy OCM's digest consistency rules:

- Component references must have complete digests (hash algorithm, normalisation algorithm, value)
- Resources with access must have complete digests
- Resources without access must not carry a digest

**Behaviour:** Incomplete digests are logged as a warning but never skip signature verification.
Configured signatures are always verified; a missing or invalid signature still fails the
`Component`, `Resource`, or `Deployer`. A separate, stricter rule applies to the resource a
`Resource` selects: under the default `verificationPolicy: Always` the selected resource must
record a usable digest. The failure message distinguishes two cases:

- `has no digest to verify`: the digest is missing, excluded from verification, or has neither
  `hashAlgorithm` nor `value`. This includes a digest containing only `normalisationAlgorithm`.
  Rebuild the component version with a usable recorded digest for the selected resource.
- `has an invalid digest`: exactly one of `hashAlgorithm` and `value` is missing, the value is
  malformed, or the hash algorithm is unsupported. Correct the digest fields and value, using
  SHA-256 or SHA-512.

After correcting the descriptor, re-sign it as applicable and publish it. The `ocm sign cv`
command warns when a component version is not safely digestible.

To explicitly skip the resource-digest check, set `verificationPolicy: Never`; configured
component signatures are still verified.

### Symptom: "failed to get Secret" or "secret does not contain supported keys"

**Cause:** The Secret named in `ocmConfig` does not exist in the `Component`'s namespace, or it
does not carry the configuration under the `.ocmconfig` key.

**Fix:** Check that the Secret exists and holds an `.ocmconfig` entry:

```bash
kubectl get secret signing-verification-secret -n <component-namespace> -o jsonpath='{.data.\.ocmconfig}' | base64 -d
```

### Symptom: "missing public key, required for plain RSA signatures"

**Cause:** No credentials entry matched the consumer identity the verifier asked for, so
verification ran without a key. Consumer identities are matched exactly, and the identity is built
from the signature being verified: its name and its algorithm. An entry for `signature: default`
therefore does not serve a signature named `prod`, and an `RSASSA-PSS` entry does not serve an
`RSASSA-PKCS1-V1_5` signature.

**Fix:** Check the signature's name and algorithm on the component version, then make the consumer
identity match exactly:

```bash
ocm get cv ghcr.io/<your-namespace>//github.com/acme.org/helloworld:1.0.0 -o yaml | grep -A 8 "signatures:"
```

```yaml
- type: credentials.config.ocm.software
  consumers:
  - identity:
      type: RSA/v1alpha1
      algorithm: RSASSA-PSS
      signature: default
```

### Symptom: the `Component` becomes ready without verifying anything

**Cause:** The signing configuration contains no entry naming a signature. An entry without a
`signature` field only supplies the verifier, so nothing is requested.

**Fix:** Add the signature name to the entry:

```yaml
- type: signing.config.ocm.software/v1alpha1
  signature: default
```

## Next Steps

- [Getting Started: Deploy Helm Charts]({{< relref "deploy-helm-chart.md" >}}) -
  Deploy resources from verified component versions

## Related Documentation

- [Concept: Signing and Verification]({{< relref "docs/concepts/signing-and-verification-concept.md" >}}) -
  Understand how OCM signing works
- [Guides: Verify Component Versions (CLI)]({{< relref "docs/guides/sign/_index.md" >}}) -
  Verify signatures using the CLI
- [Guides: Configure Credentials for OCM Controllers]({{< relref "docs/guides/deploy/configure-controller-credentials.md" >}}) -
  Set up registry credentials for the controller
