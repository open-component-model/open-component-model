---
title: "Configure Controller Credentials"
description: "Configure authentication for OCM Controllers to access private OCM repositories: credential types, .ocmconfig, and Kubernetes secrets."
weight: 10
toc: true
aliases:
  - /docs/how-to/configure-credentials-ocm-controllers/
  - /docs/how-to/configure-credentials-for-ocm-controllers/
---

## Goal

Configure credentials to allow OCM Controllers to access OCM components stored in private OCI registries.

## You will end up with

- A Kubernetes secret containing registry credentials
- A `Role` that lets the service account of your OCM Controller resources read the secret
- OCM Controller resources configured to use these credentials
- Verified access to private OCM repositories

**Estimated time:** ~5 minutes

## Prerequisites

- [Controller environment]({{< relref "setup-controller-environment.md" >}}) set up (OCM Controllers, kro and Flux in a Kubernetes cluster)
- [OCM CLI]({{< relref "ocm-cli-installation.md" >}}) installed
- [kubectl](https://kubernetes.io/docs/tasks/tools/#kubectl) installed
- Credentials for your private OCI registry (username and password, Docker config file, or OCM CLI config file)
- The registry URL where your OCM components are stored

{{< callout context="caution" title="Security Warning" icon="outline/alert-triangle" >}}
Kubernetes secrets are only base64-encoded, not encrypted.
Ensure proper RBAC policies to restrict access to secrets containing credentials.
{{< /callout >}}

## Configure and propagate credentials for OCM resources

{{< steps >}}
{{< step >}}

### Create a Kubernetes secret with credentials

Choose one of two methods to create the secret:

{{< tabs "credential-methods" >}}
{{< tab "OCM Config (.ocmconfig)" >}}

Use this method if you already use the OCM CLI and have an `.ocmconfig` file configured with credentials.
[Configure Credentials for Multiple Registries]({{< relref "docs/guides/transfer/configure-registry-credentials.md" >}}) helps with creating this file.

#### Create the secret from your existing `.ocmconfig` file

Point the command to your existing `.ocmconfig` file. The secret key must be named `.ocmconfig` (with the dot).
The `--from-file` command automatically uses the filename as the key.

```bash
kubectl create secret generic ocm-secret --from-file=<path-to-your-.ocmconfig-file>
```

<details>
<summary>You should see this output</summary>

```text
secret/ocm-secret created
```
</details>

{{< /tab >}}

{{< tab "Docker Config (dockerconfigjson)" >}}

#### Option A: From existing Docker config file

```bash
kubectl create secret docker-registry ocm-secret --from-file=<path-to-your-docker-config-file>
```

<details>
<summary>You should see this output</summary>

```text
secret/ocm-secret created
```
</details>

#### Option B: Create from command line

```bash
kubectl create secret docker-registry ocm-secret \
  --docker-username=<your-username> \
  --docker-password=<your-password> \
  --docker-server=<your-registry-url>
```

<details>
<summary>You should see this output</summary>

```text
secret/ocm-secret created
```
</details>

{{< /tab >}}
{{< /tabs >}}
{{< /step >}}

{{< step >}}

### Allow the service account to read the secret

The controller reads the secret only if the service account of the referencing object may `get` it, see
[Configuration Access]({{< relref "ocm-controllers.md#configuration-access" >}}). Without `spec.serviceAccountName`,
that is the `default` service account of the object's namespace. Grant it access to this one secret:

```bash
kubectl create role ocm-config-reader --verb=get --resource=secrets --resource-name=ocm-secret
kubectl create rolebinding ocm-config-reader --role=ocm-config-reader --serviceaccount=default:default
```

`--serviceaccount` takes `<namespace>:<name>`. Adjust it if you work in another namespace or set
`spec.serviceAccountName`.

<details>
<summary>You should see this output</summary>

```text
role.rbac.authorization.k8s.io/ocm-config-reader created
rolebinding.rbac.authorization.k8s.io/ocm-config-reader created
```
</details>
<br>

Check the access:

```bash
kubectl auth can-i get secret/ocm-secret --as=system:serviceaccount:default:default
```

<details>
<summary>You should see this output</summary>

```text
yes
```
</details>

{{< /step >}}

{{< step >}}

### Reference the secret in OCM Controller resources

Add the `spec.ocmConfig` field to your OCM Controller resources to use the credentials.
Create a `repository.yaml` with a `Repository` resource.
Replace `<your-namespace>` with your actual namespace in the registry URL.

```yaml
apiVersion: delivery.ocm.software/v1alpha1
kind: Repository
metadata:
  name: my-repository
spec:
  repositorySpec:
    baseUrl: ghcr.io/<your-namespace>
    type: OCIRegistry
  interval: 1m
  ocmConfig:
    - kind: Secret
      name: ocm-secret
```

Apply the resource:

```bash
kubectl apply -f repository.yaml
```

<details>
<summary>You should see this output</summary>

```text
repository.delivery.ocm.software/my-repository created
```
</details>
<br>

Verify the resource is ready and can access your registry:

```shell
kubectl get repository my-repository
```

<details>
<summary>You should see this output</summary>

```text
NAME            TYPE          READY   AGE   STATUS
my-repository   OCIRegistry   True    45s   Successfully reconciled OCM repository
```
</details>

{{< /step >}}

{{< step >}}

### Credential inheritance for dependent resources

`ocmConfig` is propagated by default. A `Component`, `Resource`, or `NamespacedDeployer` resource will automatically
inherit the `ocmConfig` from the resource it references (e.g. a `Repository`) **if it does not specify its own
`ocmConfig`**. This means you do not need to repeat the credential configuration in every dependent resource.

Create a `component.yaml` that references the `Repository` you just created:

```yaml
apiVersion: delivery.ocm.software/v1alpha1
kind: Component
metadata:
  name: my-component
spec:
  component: <your-namespace>/my-component
  repositoryRef:
    name: my-repository
  semver: 1.0.0
  interval: 1m
```

The `Component` automatically inherits the `ocmConfig` from `my-repository` — no explicit `ocmConfig` field needed.
he controller checks the Component's service account before reading the inherited secret. Both objects use `default`
here, so the binding from the previous step covers it. If you give them different service accounts, bind each one.

Apply the resource:

```shell
kubectl apply -f component.yaml
```

<details>
<summary>You should see this output</summary>

```text
component.delivery.ocm.software/my-component created
```
</details>
<br>

Verify the resource is ready and can access your registry:

```shell
kubectl get component my-component
```

<details>
<summary>You should see this output</summary>

```text
NAME           COMPONENT                       VERSION   READY   AGE   STATUS
my-component   <your-namespace>/my-component   1.0.0     True    30s   Applied version 1.0.0
```
</details>

If you need the `Component` to use **additional** credentials on top of those from the `Repository`, specify both
in `ocmConfig`:

```yaml
spec:
  # ...existing configuration...
  ocmConfig:
    - kind: Secret
      name: another-ocm-secret
    - kind: Repository
      apiVersion: delivery.ocm.software/v1alpha1
      name: my-repository
```

{{< /step >}}

{{< /steps >}}

## Advanced: Prevent credential propagation

To prevent a resource from propagating its credentials to dependent resources, set `policy: DoNotPropagate` on
the relevant `ocmConfig` entry of that resource:

```yaml
apiVersion: delivery.ocm.software/v1alpha1
kind: Repository
metadata:
  name: my-repository
spec:
  repositorySpec:
    baseUrl: ghcr.io/<your-namespace>
    type: OCIRegistry
  interval: 1m
  ocmConfig:
    - kind: Secret
      name: ocm-secret
      policy: DoNotPropagate
```

## Troubleshooting

### Symptom: "failed to list versions: response status code 401: unauthorized"

**Cause:** The credentials are incorrect, missing, or the secret is not referenced in the resource.

**Fix:**

1. Verify the secret exists:
   ```bash
   kubectl get secret ocm-secret
   ```

2. Check the secret contains the correct credentials:
   ```bash
   kubectl get secret ocm-secret -o yaml
   ```

3. Ensure the `ocmConfig` field references the correct secret name in your resource.

### Symptom: "secrets \"ocm-secret\" is forbidden: service account default/default may not get it in namespace default"

**Cause:** The object's service account may not `get` a secret that its own or its inherited `ocmConfig` references.
The object is not ready with reason `GetConfigurationFailed`.

**Fix:** Bind a `Role` with `get` on the secret to the service account named in the message, as shown in
[Allow the service account to read the secret](#allow-the-service-account-to-read-the-secret). The controller
does not watch RBAC, so the object recovers on its next retry, at most 5 minutes later.

### Symptom: "failed to get service account default/my-reader: serviceaccounts \"my-reader\" not found"

**Cause:** `spec.serviceAccountName` names a service account that does not exist in the object's namespace.

**Fix:** Create it with `kubectl create serviceaccount my-reader`, or fix the name.

### Symptom: "failed to load configurations: secret does not contain supported keys [\".ocmconfig\" \".dockerconfigjson\"]"

**Cause:** When using the OCM config method, the secret must contain a key named either `.ocmconfig` or `.dockerconfigjson` — the controller checks for both and rejects secrets that contain neither.

**Fix:**

Recreate the secret with the correct key name, e.g., referencing an `.ocmconfig` file in the same folder:

```bash
kubectl delete secret ocm-secret
kubectl create secret generic ocm-secret --from-file=./.ocmconfig
```

The filename in `--from-file` must be `.ocmconfig` (with the dot).

### Symptom: "Component shows `Not Ready` with credential errors"

**Cause:** The referenced `Repository` has no `ocmConfig`, or the `Component` specifies its own `ocmConfig` that is
missing or references a non-existent resource.

**Fix:**

Ensure the `Repository` the `Component` references has a valid `ocmConfig` — the `Component` will inherit it
automatically. If the `Component` needs additional credentials, add them explicitly:

```yaml
spec:
  # ...existing configuration...
  ocmConfig:
    - kind: Secret
      name: ocm-secret
```

## Next Steps

- [Getting Started: Deploy a Helm Chart]({{< relref "deploy-helm-chart.md" >}}) - Use the OCM Controllers to deploy applications from component versions
- [Guides: Configure Credentials for Multiple Registries]({{< relref "docs/guides/transfer/configure-registry-credentials.md" >}}) - Configure credentials for multiple registries in an `.ocmconfig` file

## Related documentation

- [Concept: OCM Controllers]({{< relref "ocm-controllers.md" >}})
- [Concept: Credential System]({{< relref "credential-system.md" >}})
