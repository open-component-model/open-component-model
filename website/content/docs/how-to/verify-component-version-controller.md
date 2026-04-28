---
title: "Verify Component Versions in the Controller"
description: "Configure the OCM controller to verify component version signatures on reconciliation."
weight: 36
toc: true
---

## Goal

Configure the OCM Kubernetes controller to automatically verify component version signatures before reconciling resources.

## You'll end up with

- A Component resource that ensures signature verification

**Estimated time:** ~5 minutes

## Prerequisites

- [Controller environment]({{< relref "setup-controller-environment.md" >}}) set up
- A [signed component version]({{< relref "docs/how-to/sign-component-version.md" >}}) in a local CTF archive
- The public key file at `/tmp/keys/public-key.pem` (from [Generate Signing Keys]({{< relref "docs/how-to/generate-signing-keys.md" >}}))
- Access to an OCI registry (e.g., [ghcr.io](https://docs.github.com/en/packages/learn-github-packages/introduction-to-github-packages))

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

### Prepare the public key

Base64-encode your public key for use in Kubernetes resources:

```bash
cat /tmp/keys/public-key.pem | base64 | tr -d '\n'
```

Save the output - you will need it in the next step.

{{< /step >}}

{{< step >}}

### Create the Repository resource

Create and apply a Repository that points to your OCI registry:

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

### Create the Component resource with verification

Create and apply a Component that references the repository and configures signature verification.
Choose one of the following approaches:

{{< tabs "verification-method" >}}
{{< tab "Inline Value" >}}

Embed the base64-encoded public key directly in the Component resource:

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
  verify:
    - signature: default
      value: <base64-encoded-public-key>
EOF
```

Replace `<base64-encoded-public-key>` with the output from the previous step.

```bash
kubectl apply -f component.yaml
```

{{< /tab >}}
{{< tab "Kubernetes Secret" >}}

Store the public key in a Kubernetes Secret and reference it from the Component resource:

```bash
cat <<EOF > signing-secret.yaml
apiVersion: v1
kind: Secret
metadata:
  name: signing-verification-secret
data:
  default: <base64-encoded-public-key>
EOF
```

{{< callout type="info" >}}
The key in the Secret's `data` field must match the signature name used during signing.
If you signed with `--signature prod`, use `prod` as the key name.
{{< /callout >}}

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
  verify:
    - signature: default
      secretRef:
        name: signing-verification-secret
EOF
```

```bash
kubectl apply -f signing-secret.yaml -f component.yaml
```

{{< /tab >}}
{{< /tabs >}}

{{< /step >}}

{{< step >}}

### Verify the component is ready

Check that the Component resource reconciles successfully with verification:

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

If verification fails, the component will not become ready and an error condition will be set.

{{< /step >}}
{{< /steps >}}

## How verification protects component references

When a component version contains references to other component versions, those references can include digests. If present, the controller uses them to verify the integrity of referenced components automatically:

1. The controller verifies the **signature** on the parent component version
2. When resolving resources from referenced components, it checks whether each reference includes a **digest**
3. If a digest is present, the controller verifies the referenced component's integrity against it
4. If a digest is missing, the controller logs a warning and skips the integrity check for that reference

A signed component version does not necessarily contain digests on its references. To get the full transitive trust chain, ensure your component versions include reference digests before signing. The `ocm sign cv` command warns when references lack digests.

## Troubleshooting

### Symptom: "signature verification failed for signature default"

**Cause:** The public key does not match the private key used to sign the component version.

**Fix:** Ensure you are using the correct public key that corresponds to the private key used during signing. Verify the signature name matches:

```bash
ocm get cv ghcr.io/<your-namespace>//github.com/acme.org/helloworld:1.0.0 -o yaml | grep -A 3 "signatures:"
```

### Symptom: "signature default not found in component"

**Cause:** The component version does not contain a signature with the name specified in the `verify` configuration.

**Fix:** Check which signatures exist on the component version and ensure the `signature` field in your `verify` configuration matches:

```bash
ocm get cv ghcr.io/<your-namespace>//github.com/acme.org/helloworld:1.0.0 -o yaml | grep -A 3 "signatures:"
```

### Symptom: "secret not found" or "failed to get secret"

**Cause:** The Secret referenced in `secretRef` does not exist in the same namespace as the Component resource.

**Fix:** Ensure the Secret is created in the same namespace:

```bash
kubectl get secret signing-verification-secret -n <component-namespace>
```

### Symptom: "secret does not contain key for signature verification"

**Cause:** The Secret does not contain a data entry matching the signature name.

**Fix:** The key in the Secret's `data` field must exactly match the `signature` field in the `verify` configuration. If your signature is named `default`, the Secret must have a `default` key:

```yaml
data:
  default: <base64-encoded-public-key>
```

## Next Steps

- [Getting Started: Deploy Helm Charts]({{< relref "deploy-helm-chart.md" >}}) - Deploy resources from verified component versions

## Related Documentation

- [Concept: Signing and Verification]({{< relref "docs/concepts/signing-and-verification-concept.md" >}}) - Understand how OCM signing works
- [How-To: Verify Component Versions (CLI)]({{< relref "docs/how-to/verify-component-version.md" >}}) - Verify signatures using the CLI
- [How-To: Configure Credentials for OCM Controllers]({{< relref "docs/how-to/configure-credentials-ocm-controllers.md" >}}) - Set up registry credentials for the controller
