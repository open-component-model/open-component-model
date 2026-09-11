---
title: "Verify Component Versions in the Controller"
description: "Configure the OCM controller to verify component version signatures on reconciliation."
icon: "🔍"
weight: 36
toc: true
---

## Goal

Configure the OCM Kubernetes controller to automatically verify component version signatures before reconciling resources.

## You'll end up with

- A Secret holding an OCM configuration that requests signature verification
- A `Component` resource that reconciles only if the signature verifies

**Estimated time:** ~5 minutes

## Prerequisites

- [Controller environment]({{< relref "setup-controller-environment.md" >}}) set up
- A [signed component version]({{< relref "sign-component-version.md" >}}) in a local CTF
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
COMPONENT                          │ VERSION │ PROVIDER
───────────────────────────────────┼─────────┼──────────────
github.com/acme.org/helloworld     │ 1.0.0   │ acme.org
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
        public_key_pem: |
$(sed 's/^/          /' /tmp/keys/public-key.pem)
EOF
```

The public key is embedded as PEM, indented under `public_key_pem`. No base64 encoding is needed.

{{< callout context="note" title="Note" icon="outline/info-circle" >}}
Only an entry that names a `signature` requests verification of that signature. An entry without
one merely supplies the verifier that named entries fall back to, so it does not turn verification
on by itself.

The `verifier` field is optional and defaults to RSASSA-PSS. Set it to select a different
verification handler, for example `SigstoreVerificationConfiguration/v1alpha1`.
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
`namespace` field. The configuration also propagates: a `Resource` or `Deployer` that references
this `Component` inherits it and verifies the same signature.
{{< /callout >}}

{{< /step >}}

{{< step >}}

### Verify the `Component` is ready

Check that the `Component` resource reconciles successfully with verification:

```bash
kubectl get component helloworld-component -o wide
```

<details>
<summary>Expected output</summary>

```text
NAME                   READY                   AGE
helloworld-component   Applied version 1.0.0   98s
```
</details>

To confirm the signature was actually verified, check the controller logs:

```bash
kubectl logs -n ocm-k8s-toolkit-system deploy/ocm-k8s-toolkit-controller-manager | grep "verifying signature"
```

<details>
<summary>Expected output</summary>

```text
{"level":"info","ts":"2026-04-28T15:58:14Z","msg":"verifying signature","component":"github.com/acme.org/helloworld","version":"1.0.0"}
```
</details>

If verification fails, the `Component` will not become ready and an error condition will be set.

<details>
<summary>Check for failure</summary>

```bash
kubectl get component helloworld-component -o wide
```

```text
NAME                   READY                                                                                                        AGE
helloworld-component   signature verification failed for signature default: missing public key, required for plain RSA signatures   7s
```
</details>

{{< /step >}}
{{< /steps >}}

## How verification protects component references

Component references can carry digests. When the controller resolves a reference that includes a
digest, it computes a fresh digest of the referenced component and compares it against the
recorded value. If they do not match, reconciliation fails.

Reference digests are computed and added automatically by `ocm add cv`. The `ocm sign cv`
command checks that the component version is safely digestible and warns if any reference or
resource digests are missing.

## Troubleshooting

When verification fails, the `Component` resource's Ready condition is set to `False` with the
error message. Check it with:

```bash
kubectl get component <name> -o jsonpath='{.status.conditions[?(@.type=="Ready")].message}'
```

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

### Symptom: "not safely digestible" event

The `Component` becomes Ready, but a Kubernetes event with severity error is emitted containing
"not safely digestible".

**Cause:** The component version does not satisfy OCM's digest consistency rules:

- Component references must have complete digests (hash algorithm, normalisation algorithm, value)
- Resources with access must have complete digests
- Resources without access must not carry a digest

Without consistent digests, signature verification is skipped because the normalised form cannot
be reliably computed.

**Fix:** Rebuild the component version with consistent digests, re-sign it, and then publish it.
The `ocm sign cv` command warns when a component version is not safely digestible.

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
- [How-To: Verify Component Versions (CLI)]({{< relref "verify-component-version.md" >}}) -
  Verify signatures using the CLI
- [How-To: Configure Credentials for OCM Controllers]({{< relref "docs/how-to/configure-credentials-ocm-controllers.md" >}}) -
  Set up registry credentials for the controller
