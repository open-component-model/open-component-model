---
title: Deploy an Application from Inline Manifests with OCM and kro
description: "Package an application as an OCM component and deploy it to Kubernetes with kro, using plain manifests and no Helm."
icon: "⚙️"
weight: 62
toc: true
---

You have an application to run on Kubernetes. You want to ship it the same way every time:
one versioned package that carries the container image and the instructions to run it.
When you move that package from a development registry to production, or into an air-gapped
cluster, the image references must follow automatically. You should never hand-edit a
manifest to fix a registry path.

This tutorial builds exactly that. You package an application as an OCM component, describe
how to run it with two kro `ResourceGraphDefinition`s (RGDs), and let the OCM controllers
deliver and localize it into your cluster. kro renders plain Kubernetes manifests directly,
so there is no Helm chart and no Flux or Argo CD in the path. We use
[Podinfo](https://github.com/stefanprodan/podinfo) as a stand-in for your application.

By the end, you will have:

- An OCM component that bundles an image and two RGDs
- A running Podinfo application, deployed and kept in sync by kro
- A localized image reference that points at your registry, injected automatically

## Prerequisites

- [Controller environment]({{< relref "setup-controller-environment.md" >}}) with the OCM
  Controllers and kro installed (no GitOps deployer needed)
- [OCM CLI]({{< relref "ocm-cli-installation.md" >}})
- `envsubst` (part of `gettext`; on macOS install it with `brew install gettext`)
- An OCI registry you can push to, for example [ghcr.io](https://docs.github.com/en/packages/learn-github-packages/introduction-to-github-packages)

{{< callout context="note" title="Custom RBAC" icon="outline/lock" >}}
The OCM Deployer applies the kro `ResourceGraphDefinition`s, so the OCM controller's
`ServiceAccount` needs `resourcegraphdefinitions.kro.run`. kro then creates the objects the
RGDs define (the `Podinfo` instance, the OCM `Resource`, the Deployment and Service), managed
by kro's own `ServiceAccount`. Apply the [Custom RBAC]({{< relref "custom-rbac.md" >}}) guide
once, then verify the OCM controller `ServiceAccount` (not your admin user):

```bash
kubectl auth can-i create resourcegraphdefinitions.kro.run \
  --as=system:serviceaccount:ocm-k8s-toolkit-system:ocm-k8s-toolkit-controller-manager
```

This must print `yes` before you continue. You may also see a line like
`Warning: resource 'resourcegraphdefinitions' is not namespace scoped in group 'kro.run'`.
That is harmless. The answer is the `yes` printed below it.
{{< /callout >}}

Set your registry location:

```bash
export GITHUB_USERNAME=<your-github-username>
export OCM_REPO=ghcr.io/$GITHUB_USERNAME/ocm-tutorial
```

## How it works

You deliver **two** RGDs inside one OCM component:

- **The app RGD (`podinfo`)** defines a reusable API for your application. It registers a
  `Podinfo` custom resource and renders a Deployment and a Service. Its schema exposes
  `image`, `message`, and `replicas`. This is the building block a developer owns.
- **The system RGD (`system`)** composes the application. It reads the localized image from
  the OCM component and creates a `Podinfo` instance wired with that image and your message.

This split is deliberate, and it is the real payoff of dropping Helm. The app RGD turns your
application into a first-class Kubernetes API: a `Podinfo` kind with a typed schema, so
operators run `kubectl get podinfo`, set fields with validation, and read status like any
built-in resource. It stays generic, with no OCM coupling. The system RGD holds the
OCM-specific wiring, reading the localized image and composing the app into an instance. A
Helm chart gives you templated YAML and a release object; these two RGDs give you a queryable
API plus a composition layer. That power has a cost: you define the API yourself and deliver
two RGDs instead of one chart, so for a one-off install of someone else's chart a HelmRelease
is still simpler.

The OCM controllers deliver both RGDs. A `Repository` and `Component` fetch the component,
and one `Resource` + `Deployer` pair per RGD applies it to the cluster. You then create a
single `System` instance, and the chain converges to a running Podinfo.

**Localization** is what keeps the image correct. When you transfer the component with
`--copy-resources --upload-as ociArtifact`, OCM copies the image into your registry and
rewrites its reference. The system RGD injects that rewritten reference into the `Podinfo`
instance, so the workload always pulls from the registry the component was delivered to.

Here you build one app, but the shape is bigger. Each app is a reusable building block, a
**leaf** that knows how to run one thing. The system RGD is the composer that stacks leaves
into a product: with one app there is one leaf, with a real product there are many, and a
single instance drives them all. For one app you could fold both RGDs into one; keeping them
split is what lets the pattern grow. The [Scaling to a whole product](#scaling-to-a-whole-product)
section at the end shows how.

<details>
<summary>Architecture diagram</summary>

```mermaid
%%{init: {"theme":"base", "themeVariables": {"edgeLabelBackground":"#ffffff"}, "flowchart": {"wrappingWidth": 320}}}%%
flowchart TB
    subgraph background[ ]
        direction TB
        subgraph ocmRepo[OCM Repository]
            subgraph ocmCV[OCM Component Version: system]
                direction RL
                ocmResImage[Resource: Image]
                ocmResPodinfo[Resource: RGD podinfo]
                ocmResSystem[Resource: RGD system]
            end
        end

        subgraph k8sCluster[Kubernetes Cluster]
            subgraph bootstrap[OCM Controllers]
                k8sRepo[Repository]
                k8sComponent[Component]
                k8sResPodinfo[Resource: RGD podinfo]
                k8sDeployerA[Deployer A]
                k8sResSystem[Resource: RGD system]
                k8sDeployerB[Deployer B]
            end
            subgraph kro[kro]
                rgdPodinfo[RGD: podinfo]
                crdPodinfo[CRD: Podinfo]
                rgdSystem[RGD: system]
                crdSystem[CRD: System]
                subgraph instanceSystem[Instance: System]
                    k8sResImg[Resource: Image]
                    podinfoInstance[Podinfo instance]
                    k8sResImg -- image reference localized --> podinfoInstance
                end
            end
            podinfoInstance ==> workload["Workload: Deployment + Service"]
        end

        ocmRepo --> k8sRepo
        k8sRepo ==> k8sComponent
        k8sComponent ==> k8sResPodinfo
        k8sComponent ==> k8sResSystem
        k8sResPodinfo --> k8sDeployerA
        k8sResSystem --> k8sDeployerB
        k8sDeployerA ==> rgdPodinfo
        k8sDeployerB ==> rgdSystem
        rgdPodinfo ==> crdPodinfo
        rgdSystem ==> crdSystem
        crdSystem eInstSys@-.-> instanceSystem
        crdPodinfo eInstPod@-.-> podinfoInstance
        rgdPodinfo eKnative@-- kro-native: templated directly --> workload
    end

    eKnative@{stroke-dasharray: 5 5}
    eInstSys@{stroke-width: 2px}
    eInstPod@{stroke-width: 2px}

    classDef ocm fill:#e8def8,stroke:#8e68d0,color:#1a1a1a
    classDef reconciledBy fill:#def2e4,stroke:#4d8a4d,color:#1a1a1a
    classDef k8sObject fill:#d7e4fb,stroke:#3c63c8,color:#1a1a1a
    classDef cluster fill:#f3f4f6,stroke:#6b7280,color:#1a1a1a

    class ocmRepo,ocmCV,ocmResImage,ocmResPodinfo,ocmResSystem ocm;
    class k8sCluster cluster;
    class bootstrap,kro,instanceSystem reconciledBy;
    class k8sRepo,k8sComponent,k8sResPodinfo,k8sDeployerA,k8sResSystem,k8sDeployerB,rgdPodinfo,rgdSystem,crdPodinfo,crdSystem,k8sResImg,podinfoInstance,workload k8sObject;

    linkStyle default color:#1a1a1a
```

<div style="display:inline-block;background:#fff;border:1px solid #d0d5dd;border-radius:8px;padding:10px 14px;font-size:14px;color:#1a1a1a;line-height:2">
<div style="display:flex;flex-wrap:wrap;gap:18px;align-items:center">
<span style="display:inline-flex;align-items:center;gap:7px;white-space:nowrap"><span style="display:inline-block;width:14px;height:14px;border-radius:3px;border:1.5px solid #8e68d0;background:#e8def8"></span>OCM content</span>
<span style="display:inline-flex;align-items:center;gap:7px;white-space:nowrap"><span style="display:inline-block;width:14px;height:14px;border-radius:3px;border:1.5px solid #4d8a4d;background:#def2e4"></span>reconciled by controllers</span>
<span style="display:inline-flex;align-items:center;gap:7px;white-space:nowrap"><span style="display:inline-block;width:14px;height:14px;border-radius:3px;border:1.5px solid #3c63c8;background:#d7e4fb"></span>Kubernetes object</span>
</div>
<div style="display:flex;flex-wrap:wrap;gap:18px;align-items:center">
<span style="display:inline-flex;align-items:center;gap:7px;white-space:nowrap"><svg width="36" height="12" aria-hidden="true"><line x1="0" y1="6" x2="30" y2="6" stroke="#333333" stroke-width="3.2"></line><polygon points="36,6 30,3 30,9" fill="#333333"></polygon></svg>creates</span>
<span style="display:inline-flex;align-items:center;gap:7px;white-space:nowrap"><svg width="36" height="12" aria-hidden="true"><line x1="0" y1="6" x2="30" y2="6" stroke="#333333" stroke-width="1"></line><polygon points="36,6 30,3 30,9" fill="#333333"></polygon></svg>references / data flow</span>
<span style="display:inline-flex;align-items:center;gap:7px;white-space:nowrap"><svg width="36" height="12" aria-hidden="true"><line x1="0" y1="6" x2="30" y2="6" stroke="#333333" stroke-width="1.4" stroke-dasharray="3 3"></line><polygon points="36,6 30,3 30,9" fill="#333333"></polygon></svg>instantiated as</span>
</div>
</div>

</details>

## Create the component

Work in a fresh directory:

```bash
mkdir -p kro-tutorial && cd kro-tutorial
```

### Define the component

The component bundles three resources: the Podinfo image and the two RGD files.

```bash
cat > component-constructor.yaml << 'EOF'
components:
  - name: ocm.software/ocm-k8s-toolkit/system
    version: "1.0.0"
    provider:
      name: ocm.software
    resources:
      - name: image-podinfo
        type: ociArtifact
        version: "1.0.0"
        access:
          type: OCIImage/v1
          imageReference: "ghcr.io/stefanprodan/podinfo:6.11.1@sha256:8fa56908408de98f24aed2a162b1bb42c0b98df7abfcc5a76a14a8be510457c5"
      - name: rgd-podinfo
        type: blob
        version: "1.0.0"
        input:
          type: File/v1
          path: ./rgd-podinfo.yaml
      - name: rgd-system
        type: blob
        version: "1.0.0"
        input:
          type: File/v1
          path: ./rgd-system.yaml
EOF
```

### Write the app RGD

This RGD registers the `Podinfo` API and renders the workload. kro processes the resources
in order and templates values from the instance schema.

```bash
cat > rgd-podinfo.yaml << 'EOF'
apiVersion: kro.run/v1alpha1
kind: ResourceGraphDefinition
metadata:
  name: podinfo
spec:
  schema:
    apiVersion: v1alpha1
    kind: Podinfo
    spec:
      image: string
      message: string | default="Hello from OCM and kro"
      replicas: integer | default=1
  resources:
    - id: deployment
      readyWhen:
        - ${deployment.status.conditions.exists(c, c.type == 'Available' && c.status == 'True')}
      template:
        apiVersion: apps/v1
        kind: Deployment
        metadata:
          name: ${schema.metadata.name}
        spec:
          replicas: ${schema.spec.replicas}
          selector:
            matchLabels:
              app: ${schema.metadata.name}
          template:
            metadata:
              labels:
                app: ${schema.metadata.name}
            spec:
              containers:
                - name: podinfo
                  image: ${schema.spec.image}
                  ports:
                    - containerPort: 9898
                  env:
                    - name: PODINFO_UI_MESSAGE
                      value: ${schema.spec.message}
    - id: service
      template:
        apiVersion: v1
        kind: Service
        metadata:
          name: ${schema.metadata.name}
        spec:
          type: ClusterIP
          selector:
            app: ${schema.metadata.name}
          ports:
            - port: 80
              targetPort: 9898
EOF
```

### Write the system RGD

This RGD reads the localized image from the OCM component and creates a `Podinfo` instance
with it. The `resourceImage` node fetches the image reference. The `app` node assembles the
pod image as `registry`/`repository`@`digest` from the three fields `toOCI()` exposes under
`resourceImage.status.additional.oci`, then passes it with your message to the `Podinfo`
instance. Pinning the digest (not the tag) locks in the exact image OCM localized.

```bash
cat > rgd-system.yaml << 'EOF'
apiVersion: kro.run/v1alpha1
kind: ResourceGraphDefinition
metadata:
  name: system
spec:
  schema:
    apiVersion: v1alpha1
    kind: System
    spec:
      message: string | default="Hello from OCM and kro"
      replicas: integer | default=1
  resources:
    - id: resourceImage
      readyWhen:
        - ${resourceImage.status.conditions.exists(c, c.type == 'Ready' && c.status == 'True')}
      template:
        apiVersion: delivery.ocm.software/v1alpha1
        kind: Resource
        metadata:
          name: system-image-resource
        spec:
          componentRef:
            name: system-component
          resource:
            byReference:
              resource:
                name: image-podinfo
          additionalStatusFields:
            oci: resource.access.toOCI()
    - id: app
      readyWhen:
        - ${app.status.conditions.exists(c, c.type == 'Ready' && c.status == 'True')}
      template:
        apiVersion: kro.run/v1alpha1
        kind: Podinfo
        metadata:
          name: podinfo
        spec:
          image: ${resourceImage.status.additional.oci.registry}/${resourceImage.status.additional.oci.repository}@${resourceImage.status.additional.oci.digest}
          message: ${schema.spec.message}
          replicas: ${schema.spec.replicas}
EOF
```

## Build and transfer

Build the component version locally (`cv` is short for component version). This reads
`component-constructor.yaml` from the current directory and creates a `transport-archive`.

```bash
ocm add cv
```

On success it prints a summary table:

```text
 COMPONENT                           │ VERSION │ PROVIDER
─────────────────────────────────────┼─────────┼──────────────
 ocm.software/ocm-k8s-toolkit/system │ 1.0.0   │ ocm.software
```

Transfer it to your registry. `--copy-resources --upload-as ociArtifact` copies the image
into your registry as a pullable OCI artifact and rewrites its reference. Both flags matter:
`--copy-resources` copies the referenced resources, and `--upload-as ociArtifact` is what
makes the image land as a pullable artifact instead of an internal blob.

```bash
ocm transfer cv --copy-resources --upload-as ociArtifact \
  "transport-archive//ocm.software/ocm-k8s-toolkit/system:1.0.0" $OCM_REPO
```

The command streams `level=INFO` progress lines. It succeeded when the last line reads:

```text
... level=INFO msg="Transferring component versions: operation finished"
```

Confirm localization. The image reference now points at your registry, not `ghcr.io/stefanprodan`:

```bash
ocm get cv "$OCM_REPO//ocm.software/ocm-k8s-toolkit/system:1.0.0" -o yaml | grep imageReference
```

```text
imageReference: ghcr.io/<your-username>/ocm-tutorial/stefanprodan/podinfo:6.11.1
```

Only the registry and path changed. The original tag stays in the reference. At deploy
time the system RGD resolves this to a digest-pinned reference (`...@sha256:...`), which is
what the running pod uses. You will see that digest form in the verification step below.

{{< callout context="note" title="Make the package readable by the cluster" icon="outline/lock" >}}
A freshly pushed ghcr.io package is **private**. For this tutorial, make the package public
in your GitHub package settings so the cluster can pull it without credentials.

To keep it private instead, see [Configure Credentials for Controllers]({{< relref "/docs/how-to/configure-credentials-ocm-controllers.md" >}})
and add the credentials to the resources as described in the collapsible below.
{{< /callout >}}

<details>
<summary>Using a private registry</summary>

Create a pull secret from a token with `read:packages`:

```bash
kubectl create secret docker-registry ghcr-secret \
  --docker-username=$GITHUB_USERNAME \
  --docker-password="$(gh auth token)" \
  --docker-server=ghcr.io
```

Then wire it into three places:

1. The `Repository` in `bootstrap.yaml`. Credentials propagate to the Component and Resources:

   ```yaml
   spec:
     repositorySpec:
       baseUrl: $OCM_REPO
       type: OCIRegistry
     interval: 1m
     ocmConfig:
       - kind: Secret
         name: ghcr-secret
   ```

2. The `resourceImage` node in `rgd-system.yaml`, so the controller can read the image:

   ```yaml
   additionalStatusFields:
     oci: resource.access.toOCI()
   ocmConfig:
     - kind: Secret
       name: ghcr-secret
   ```

3. The pod in `rgd-podinfo.yaml`, so the kubelet can pull the image. Add under the pod `spec`:

   ```yaml
   spec:
     imagePullSecrets:
       - name: ghcr-secret
     containers:
       - name: podinfo
   ```

Rebuild and transfer the component after editing the RGDs.

</details>

## Deploy the application

### Deliver both RGDs

The bootstrap resources are OCM controller objects: a `Repository` and `Component` fetch the
component from your registry, then a `Resource` + `Deployer` pair per RGD applies it to the
cluster. That is six objects for two RGDs, boilerplate that grows with every RGD you deliver.

```bash
cat > bootstrap.yaml << 'EOF'
apiVersion: delivery.ocm.software/v1alpha1
kind: Repository
metadata:
  name: system-repository
spec:
  repositorySpec:
    baseUrl: $OCM_REPO
    type: OCIRegistry
  interval: 1m
---
apiVersion: delivery.ocm.software/v1alpha1
kind: Component
metadata:
  name: system-component
spec:
  component: ocm.software/ocm-k8s-toolkit/system
  repositoryRef:
    name: system-repository
  semver: "1.0.0"
  interval: 1m
---
apiVersion: delivery.ocm.software/v1alpha1
kind: Resource
metadata:
  name: system-rgd-podinfo
  namespace: default
spec:
  componentRef:
    name: system-component
  resource:
    byReference:
      resource:
        name: rgd-podinfo
---
apiVersion: delivery.ocm.software/v1alpha1
kind: Deployer
metadata:
  name: system-deployer-podinfo
spec:
  resourceRef:
    name: system-rgd-podinfo
    namespace: default
---
apiVersion: delivery.ocm.software/v1alpha1
kind: Resource
metadata:
  name: system-rgd-system
  namespace: default
spec:
  componentRef:
    name: system-component
  resource:
    byReference:
      resource:
        name: rgd-system
---
apiVersion: delivery.ocm.software/v1alpha1
kind: Deployer
metadata:
  name: system-deployer-system
spec:
  resourceRef:
    name: system-rgd-system
    namespace: default
EOF
```

Apply them. `envsubst` fills in `$OCM_REPO`:

```bash
envsubst < bootstrap.yaml | kubectl apply -f -
```

Wait for both RGDs to reach `Active`. This usually takes a few seconds:

```bash
kubectl get rgd
```

```text
NAME      APIVERSION   KIND      STATE    READY   AGE
podinfo   v1alpha1     Podinfo   Active   True    12s
system    v1alpha1     System    Active   True    11s
```

The `podinfo` RGD registers the `Podinfo` CRD, and the `system` RGD registers the `System`
CRD. The system RGD references the `Podinfo` kind. If it becomes `Active` before the podinfo
RGD, it recovers on its own once the `Podinfo` CRD is registered, so the order does not matter.

### Create the System instance

One instance drives the whole chain:

```bash
cat > instance.yaml << 'EOF'
apiVersion: kro.run/v1alpha1
kind: System
metadata:
  name: system
spec:
  message: "Hello from OCM and kro"
  replicas: 1
EOF
kubectl apply -f instance.yaml
```

Wait for it to converge. This usually takes around 30 seconds, mostly the image pull:

```bash
kubectl get system,podinfo
```

```text
NAME                    STATE    READY   AGE
system.kro.run/system   ACTIVE   True    30s

NAME                      STATE    READY   AGE
podinfo.kro.run/podinfo   ACTIVE   True    28s
```

## Verify

Check that the pod runs the localized image. The reference points at your registry and pins a
digest:

```bash
kubectl get pods -l app=podinfo -o jsonpath='{.items[0].spec.containers[0].image}'
```

```text
ghcr.io/<your-username>/ocm-tutorial/stefanprodan/podinfo@sha256:8fa56908408de98f24aed2a162b1bb42c0b98df7abfcc5a76a14a8be510457c5
```

Check that your message reached the container:

```bash
kubectl get pods -l app=podinfo \
  -o jsonpath='{.items[0].spec.containers[0].env[?(@.name=="PODINFO_UI_MESSAGE")].value}'
```

```text
Hello from OCM and kro
```

Call the application and see the message in its response:

```bash
kubectl port-forward svc/podinfo 9898:80 &
sleep 2
curl -s http://localhost:9898/ | grep message
kill %1   # stops the port-forward (assumes no other background jobs)
```

The `sleep` gives the port-forward a moment to open. Without it, `curl` may run before the
tunnel is ready and return nothing.

```text
"message": "Hello from OCM and kro",
```

Change the message or replica count by editing `instance.yaml` and re-applying. kro
reconciles the running workload.

{{< callout context="caution" title="Going to production: kro needs explicit permissions" icon="outline/lock" >}}
This tutorial uses a dev-friendly kro install with broad permissions. A hardened cluster is
stricter, and RBAC is fixed up front. That matters here, because your RGDs register **new**
kinds at runtime. kro can create the CRD, but the moment it creates an *instance* of that
kind, a least-privilege role denies it.

Grant kro's service account the kinds its RGDs create and manage. For this tutorial that is
`podinfos.kro.run` (the instance), `resources.delivery.ocm.software` (the localized image),
and `deployments` + `services` (the workload). The OCM controller separately needs
`resourcegraphdefinitions.kro.run`, which you set up in Prerequisites.
{{< /callout >}}

## Clean up

Delete the instance before the RGDs. Deleting an RGD while its instances still exist can
strand them on a finalizer.

```bash
kubectl delete system system
envsubst < bootstrap.yaml | kubectl delete -f -
```

kro keeps the `Podinfo` and `System` CRDs after the RGDs are gone. Remove them if you want a
clean cluster:

```bash
kubectl delete crd podinfoes.kro.run systems.kro.run
```

## Troubleshooting

**RGD stuck `Inactive`**: the system RGD cannot compile until the `Podinfo` CRD exists. Check
that the podinfo RGD is `Active` (`kubectl get rgd`). It converges automatically.

**Resource not `Ready` / image pull errors**: the cluster cannot read the package. Make the
ghcr.io package public, or follow the private-registry steps above.

**Nothing happens after applying bootstrap**: check the controller logs.

```bash
kubectl logs -n ocm-k8s-toolkit-system deploy/ocm-k8s-toolkit-controller-manager --tail=40
```

**RBAC denied**: re-check the impersonation command in Prerequisites and apply the
[Custom RBAC]({{< relref "custom-rbac.md" >}}) guide.

## What you learned

- Packaged an application as an OCM component with an image and two RGDs
- Used a system RGD to compose an app RGD and inject a localized image
- Delivered both RGDs with the OCM controllers, no Helm or GitOps tooling
- Confirmed the workload runs the localized image and your message

## Scaling to a whole product

One app is the small case. Real products are bigger: many apps and services, shipped and
versioned together. The good news is you already have the building block.

What you wrote is an **app RGD**. It gives one application a typed Kubernetes API, the
`Podinfo` kind, with a schema and status. It knows how to run one thing and nothing else.
That self-contained piece is a **leaf**. A real product has many leaves: a database, a
gateway, a handful of services.

On top sits the **system RGD**. Its job is composition: it creates one instance of each leaf,
passes values between them, and orders them so nothing starts before what it depends on. You
already built a tiny version. Our system RGD created a single `Podinfo` instance. Add more
leaves and reference them, and one `System` instance drives the entire product.

One thing is still hand-written: the delivery plumbing in `bootstrap.yaml`. Today you list a
`Resource` + `Deployer` pair for every RGD yourself. For a dozen apps that is a wall of
boilerplate, and every version bump means editing that file again.

The pattern that scales moves the plumbing *into* an RGD. An **installer RGD** templates those
`Resource` + `Deployer` pairs itself, one per app, all sharing a single `Component` you pass
in by name:

```yaml
resources:
  - id: appResource
    template:
      apiVersion: delivery.ocm.software/v1alpha1
      kind: Resource
      metadata: { name: app-rgd }
      spec:
        componentRef:
          name: ${schema.spec.componentRef}   # one component, passed in
        resource:
          byReference:
            resource: { name: rgd-podinfo }
  - id: appDeployer
    template:
      apiVersion: delivery.ocm.software/v1alpha1
      kind: Deployer
      metadata: { name: app-rgd }
      spec:
        resourceRef: { name: ${appResource.metadata.name} }
    readyWhen:
      - ${appDeployer.status.conditions.exists(c, c.type == "Ready" && c.status == "True")}
```

Now `bootstrap.yaml` shrinks to almost nothing. It delivers the installer RGD once, and the
running instance takes over its own delivery. Bump the component version and the whole graph
reconciles. No more editing bootstrap files by hand.

This is not a new tool. It is the same `delivery.ocm.software/Resource` you already used for
the localized image, now applied to delivery itself. One limit stays honest: an RGD cannot
deliver the RGD that defines its own kind, because it would have to be running before its own
instance exists. So app RGDs are still delivered by `bootstrap.yaml`, and the installer
manages everything downstream.

## Next steps

- [Deploy an Application from a Helm Chart with OCM and kro]({{< relref "deploy-helm-chart-bootstrap.md" >}}) compares this with the Helm approach
- [kro Resource Graph Definitions](https://kro.run/docs/getting-started/deploy-a-resource-graph-definition/) for composing more applications
- [Configure Credentials for Controllers]({{< relref "/docs/how-to/configure-credentials-ocm-controllers.md" >}}) for private registries
