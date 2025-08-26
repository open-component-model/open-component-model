# Localization

* Status: proposed
* Deciders: Fabian Burth, Uwe Krueger, Gergely Brautigam, Jakob Moeller
* Date: 2024-10-08

## Motivation

OCM and any delivery associated with it should allow self-contained delivery of components.
This means that the components should be able to be transferred between different environments without any manual intervention or
the need to explicitly prepare values in the environment for the sake of referencing e.g. new registries that have been used
while transferring / replicating the component.

This is especially important for the continuous deployment of components in a CI/CD pipeline.
Essentially we want to deliver a system that allows to deliver any component (independent of deployment technologies such as Helm or Kustomize)
to any environment without the need to adjust the component itself if its respective location has changed.

Once the component is transferred, this information in the chart is incorrect. (see below for details)

We do not want to touch the original chart because it may have been signed, so the only way is to substitute the value
in the chart with the new value dynamically.

Technical Story:

The term *localization* was termed in the context of the
[open component model](https://ocm.software/) for the process of adjusting
the resource locations specified in deployment instructions to a particular
target environment.  

Currently, the primary use case is kubernetes as target runtime environment, and
therefore, the typical deployment instructions are kubernetes manifests, helm
charts or kustomization overlays.

Example:

Assume we have the following component in a public oci registry:

```yaml
apiVersion: ocm.software/v3alpha1 
kind: ComponentVersion
metadata:
  name: github.com/open-component-model/myapp
  provider: 
    name: ocm
  version: v1.0.0 
repositoryContexts: 
- baseUrl: ghcr.io
  componentNameMapping: urlPath
  subPath: open-component-model
  type: OCIRegistry
spec:
  resources: 
  - name: myimage 
    relation: external 
    type: ociImage 
    version: v1.0.0
    access: 
      type: ociArtifact 
      imageReference: ghcr.io/open-component-model/myimage
  - name: mydeploymentinstruction
    relation: external
    type: k8s-manifest
    version: v1.0.0
    access:
      type: ociArtifact
      imageReference: ghcr.io/open-component-model/mydeploymentinstruction:v1.0.0
```

The kubernetes manifest (called `mydeploymentinstruction` here) looks like this:

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: mypod
spec:
  containers:
  - name: mypod
    image: ghcr.io/open-component-model/mydeploymentinstruction:v1.0.0
```

Now, we transfer the component and copy all resources into a private registry in
our environment at myprivateregistry.com. After that, the component looks like
this:

```yaml
apiVersion: ocm.software/v3alpha1
kind: ComponentVersion
metadata:
  name: github.com/open-component-model/myapp
  provider:
    name: ocm
  version: v1.0.0
repositoryContexts:
  - baseUrl: ghcr.io
    componentNameMapping: urlPath
    subPath: open-component-model
    type: OCIRegistry
  - baseUrl: myprivateregistry.com
    componentNameMapping: urlPath
    subPath: open-component-model
    type: OCIRegistry
spec:
  resources:
    - name: myimage
      relation: external
      type: ociImage
      version: v1.0.0
      access:
        type: ociArtifact
        imageReference: myprivateregistry.com/open-component-model/myimage:v1.0.0
    - name: mydeploymentinstruction
      relation: external
      type: k8s-manifest
      version: v1.0.0
      access:
        type: ociArtifact
        imageReference: myprivateregistry.com/open-component-model/mydeploymentinstruction:v1.0.0
```

The resources - and thus, also the kubernetes manifest called
`mydeploymentinstruction` - remain unchanged. There are really just copied
without any mutation. Consequently, the pod image reference still points to the
old image location before the transfer
(ghcr.io/open-component-model/mydeploymentinstruction:v1.0.0)
instead of to the new image location after transfer (myprivateregistry.com/open-component-model/myimage)
that's also specified in the `myimage` resource.

So, it looks like this:

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: mypod
spec:
  containers:
    - name: mypod
      image: ghcr.io/open-component-model/mydeploymentinstruction:v1.0.0
```

But this would be an issue - especially if the target environment does not have
access to the source environment (thus, if ghcr.io/open-component-model/mydeploymentinstruction:v1.0.0
is not reachable from the target environment).

So instead, the image references in the `mydeploymentinstruction` resource have
to be adjusted to the image reference in the `myimage` resource:

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: mypod
spec:
  containers:
    - name: mypod
      image: myprivateregistry.com/open-component-model/myimage:v1.0.0
```

This adjustment is what we call *localization*.

## Context and Problem Statement

The above described process should now be automated for the context of the
*ocm-k8s-toolkit* to enable "ocmops" based continuous deployments based on ocm
components.

Multiple different solutions have been suggested.

## Decision Drivers <!-- optional -->

**Driver 1:** User Experience / Ease of Use / Ease of Adoption

The current localization controllers impose deep knowledge of the underlying replacement frameworks in OCM.
In the
official [Example](https://ocm.software/docs/tutorials/structuring-and-deploying-software-products-with-ocm/#localization),
we propose a system that is based on file based replacements due to reliance on the [OCM Localization Tooling](https://github.com/open-component-model/ocm/blob/main/api/ocm/ocmutils/localize/README.md).

This tooling forces one to adopt:

1. Localization and Substitution Rules that need to be learned from scratch by platform engineers trying to adopt the tooling.
2. It is incredibly hard to understand based on these Rules which actual substitutions are being done,
   as its a description framework with many layers of abstraction before the actual modification of the manifest:

For an example of this configuration process, see Option 3 (leaving the Status Quo in place).

This is a very complex way of describing a simple substitution rule for an image. Platform OPS are used
to a more direct way of describing the substitution rules via e.g. kustomize overlays ore helm values inserted directly
into the desired state of a deployable resource. Consider e.g. ArgoCDs Application Resource:

```yaml
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: sealed-secrets
  namespace: argocd
spec:
  project: default
  source:
    chart: sealed-secrets
    repoURL: https://bitnami-labs.github.io/sealed-secrets
    targetRevision: 1.16.1
    helm:
      releaseName: sealed-secrets
      valueFiles:
      - deploy-values.yaml
  destination:
    server: "https://kubernetes.default.svc"
    namespace: kubeseal
```

It is important to note that these tools can afford this because they rely on *existing* templating capabilities of Helm
or Kustomize.
Since they can operate on already rendered versions, they are free to template as they wish or introduce simple substitutions.

**Driver 2:** Transparency of Operation

The current localization controllers do not provide a good way to understand what substitutions are being done.
This is a problem because it is hard to understand what is being done in the localization process and what the actual
resulting manifest will look like, as well as how to fixup potentially broken localizations.

**Driver 3:** Flexibility and Plugability of own Localization Process

The current localization controllers do not provide a good way to introduce custom localization processes.
This is a problem because it is hard to introduce custom localization processes in case the default localization
is inapplicable to us.

## Considered Options

**Option 1 - Pre-Processing / Templating**

Use a known templating system and / or language to introduce simple substitution rules into the component manifest resolution.

The easiest way to approach this problem can be to inject our own templating / pre-processing behavior into the component manifest resolution.
This would allow us to introduce simple substitution rules that are easy to understand and can be easily adopted by platform engineers.

Templating alone does introduce however new complexity in that we need to decide to either introduce
our own templating system or adopt modification of existing templating systems in OCM core.

We can choose here to for example become opinionated and allow localized injection of Helm values, kustomize overlays
or go template values into the manifest after resolution.

Consider the following example:

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: mypod
spec:
  containers:
    - name: mypod
      image: ghcr.io/open-component-model/mydeploymentinstruction:v1.0.0
```

This could be localized via the kustomize ImageTagTransformer (or any other transformer for that matter) with:

```yaml
apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
images:
  - name: ghcr.io/open-component-model/mydeploymentinstruction
    newName: myprivateregistry.com/open-component-model/myimage
```

This implementation is pluggable in that it is based on kustomize Transformers and so we could add additional transformers where required.
Popular transforms for ConfigMaps and other substitutions exist.

We can then wrap this implementation in a reference such as:

```yaml
apiVersion: delivery.ocm.software/v1alpha1
kind: Localization
metadata:
  name: backend-localization
  namespace: ocm-system
spec:
  configRef:
    kind: ComponentVersion
    name: podinfocomponent-version
    namespace: ocm-system
    resourceRef:
      name: config
      referencePath:
        - name: backend
      version: 1.0.0
  interval: 10m0s
  sourceRef:
    kind: Kustomization
    # alternatively a kustomization file could be provided or a reference to a component version with the values inside.
    images:
      - name: ghcr.io/open-component-model/mydeploymentinstruction
        newName: myprivateregistry.com/open-component-model/myimage
```

**Option 2 - Literal Substitution Rules**

Generate literal substitution rules as manifest for the localization controller
based on templates embedded in the component.

If we allow component maintainers to define arbitrary placeholder literals in their component specification,
we can resolve them with a templating language of our choice (e.g. Go templates or spiff++ or any other).

This would allow us to generate a literal substitution rule manifest that can be used by the localization controller.

Consider the following example manifest:

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: mypod
spec:
  containers:
    - name: mypod
      image: {{ .localized.image }}
```

for a value based (simple) substitution, or the following (more complex example) for a Go Template based substitution:

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: mypod
spec:
  containers:
    - name: mypod
      image: {{ .ResolveFromComponentVersion "ocm-system" "podinfocomponent-version" "1.0.0", "config" "backend" }}
```

This could be localized via a Go Template resolution by reading a values file:

```yaml
localized:
  image: myprivateregistry.com/open-component-model/myimage:v1.0.0
```

or by providing the given "ResolveFromComponentVersion" resolver function that is able to resolve the given
component version and resource reference like the old localization controller did.

This can then be localized via a Go Template resolution:

```yaml
apiVersion: delivery.ocm.software/v1alpha1
kind: Localization
metadata:
  name: backend-localization
  namespace: ocm-system
spec:
  configRef:
    kind: ComponentVersion
    name: podinfocomponent-version
    namespace: ocm-system
    resourceRef:
      name: config
      referencePath:
        - name: backend
      version: 1.0.0
  interval: 10m0s
  sourceRef:
    kind: GoTemplate
    # alternatively a values file could be provided or a reference to a component version with the values inside.
    values:
      localized:
        image: myprivateregistry.com/open-component-model/myimage:v1.0.0
```

**Option 3 - Component-Based Subsitution Rules (previous solution)**

<https://github.com/open-component-model/ocm-controller/blob/main/docs/architecture.md#localization-controller>

Example:

```yaml
apiVersion: delivery.ocm.software/v1alpha1
kind: Localization
metadata:
  name: backend-localization
  namespace: ocm-system
spec:
  configRef:
    kind: ComponentVersion
    name: podinfocomponent-version
    namespace: ocm-system
    resourceRef:
      name: mydeploymentinstruction
      version: 1.0.0
  interval: 10m0s
  patchStrategicMerge: 
    source:
      sourceRef: 
        kind: GitRepository 
        name: gitRepo 
        namespace: default 
      path: "sites/eu-west-1/deployment.yaml" 
    target: 
      # Alternatively allow looking up via kind/name/namespace? This is not present currently, but could be implemented
      # kind: Deployment
      # name: deployment-in-mydeploymentinstruction
      # namespace: ocm-system
      path: "merge-target/merge-target.yaml"
```

**Option 4 - Substitution With Mutating Webhooks**

In this approach, any resources created by the Flux controllers will be redirected to a statically registered Webhook that manually
filters and mutates the resources before they are applied to the cluster. This is a very targeted approach mainly aiming
at image substitution for Pods, but technically other pod specifications may also be used.

For the example pod definition POST request to the API Server

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: mypod
spec:
  containers:
    - name: mypod
      image: ghcr.io/open-component-model/mydeploymentinstruction:v1.0.0
```

The mutating webhook would then be able to substitute the image reference to the new location:

```yaml
apiVersion: admissionregistration.k8s.io/v1
kind: MutatingWebhookConfiguration
webhooks:
  - name: ocm.software/localization-mutating-webhook
    objectSelector:
      matchExpressions:
        - key: ocm.software/localization
          operator: NotIn
          values: ["disabled"]
        - key: owner
          operator: In
          values: ["ocm.software"]
    namespaceSelector:
      matchExpressions:
        - key: ocm.software/localization
          operator: NotIn
          values: ["disabled"]
    rules:
      - operations: ["CREATE","UPDATE"]
        apiGroups: ["*"]
        apiVersions: ["*"]
        resources: ["*"]
        scope: "Namespaced"
    sideEffects: None
    failurePolicy: Fail # Ignore ?
```

This will allow ocm to effectively get a request for every resource in the cluster that is controlled by it (in this case through an owner label, can be arbitrary)

In this instance one could configure a localization mapping as:

```yaml
apiVersion: delivery.ocm.software/v1alpha1
kind: Localization
metadata:
  name: backend-localization
  namespace: ocm-system
spec:
  replacements:
    images:
      - from: ghcr.io/open-component-model/mydeploymentinstruction
        to: myprivateregistry.com/open-component-model/myimage
        for:
          kind: Pod
          apiVersion: v1
# optionally added field paths for more specific replacements
#          fieldPaths: 
#          - spec.containers[0].image 
```

## Decision Outcome

In the end we decided to form a hybrid that is mainly based on Option 3, but also allows for templating via Option 1.

An example for `LocalizationConfig`  may look like this for a simple Helm Chart:

```yaml
apiVersion: delivery.ocm.software/v1alpha1
kind: LocalizationConfig
metadata:
  name: deployment-localization
spec:
  rules:
  - yamlsubst:
      source:
        resource:
          name: my-image-with-my-app-inside
      target:
        file:
          path: values.yaml
          value: deploy.image
```

This LocalizationConfig can be applied via 2 ways:

1. It is added as a Resource together with the Component to allow for a self-contained delivery.
2. It is referenced in the Cluster where the Controllers are deployed, to allow for easy migration of workloads.

The LocalizationConfig will contain a set of rules that can be applied to the target.

The config is applied through a `LocalizedResource`:

```yaml
apiVersion: delivery.ocm.software/v1alpha1
kind: LocalizedResource
metadata:
  name: deployment-localization
spec:
  # config is a resource in the same component as the target resource and contains the localization config
  config:
    kind: < one of LocalizationConfig, Resource, LocalizedResource, ConfiguredResource >
    name: < name of the object in kubernetes >
  target:
    kind: Resource
    name: my-helm-chart-resource
    apiVersion: delivery.ocm.software/v1alpha1

```

### Resolution of the LocalizationConfig into the ResourceConfig

To make it transparent to users what exactly has been localized, the actual replacement rules need to be resolved into the target resource.
This happens through a second step by translating a LocalizationConfig / LocalizedResource into a ResourceConfig and ConfiguredResource.

A sample translated from localization would then look like this:

```yaml
apiVersion: delivery.ocm.software/v1alpha1
kind: ResourceConfig
metadata:
  name: deployment-localization
spec:
  rules:
  - source:
      value: ghcr.io/open-component-model/myimage
    target:
      file:
        path: values.yaml
        value: deploy.image
```

As one can observe this translation is only affecting the source field.
This is the only case where the controller has to resolve the actual value of the source by introspecting the
component descriptor.

### Examples

#### Example 1 (Main Use Case)

```yaml
apiVersion: delivery.ocm.software/v1alpha1
kind: LocalizedResource
metadata:
  name: my-localized-helm-chart
spec:
  # target is the resource that will be localized
  target:
    kind: Resource
    name: my-helm-chart
  # config is a resource in the same component as the target resource and contains the localization config
  config:
    # so here, another Resource CR has to have been created for the ocm resource 
    # containing the LocalizationConfig CR 
    kind: Resource 
    name: my-localization-config
```

This will require packaging the LocalizedResource together with the ComponentVersion:

```yaml
apiVersion: ocm.software/v3alpha1 
kind: ComponentVersion
metadata:
  name: github.com/open-component-model/myapp
  provider: 
    name: ocm
  version: v1.0.0 
repositoryContexts: 
- baseUrl: ghcr.io
  componentNameMapping: urlPath
  subPath: open-component-model
  type: OCIRegistry
spec:
  resources: 
  - name: my-image-with-my-app-inside 
    relation: external 
    type: ociImage 
    version: v1.0.0
    access: 
      type: ociArtifact 
      imageReference: ghcr.io/open-component-model/myimage
  - name: my-helm-chart
    relation: external
    type: helm-chart
    version: v1.0.0
    access:
      type: ociArtifact
      imageReference: ghcr.io/open-component-model/helmchart:v1.0.0
  - name: my-localization-config
    relation: external
    type: localization-config
    version: v1.0.0
    access:
      type: ociArtifact
      imageReference: ghcr.io/open-component-model/localizationconfig:v1.0.0
```

The resource `my-localization-config` would contain the LocalizationConfig:

```yaml
apiVersion: delivery.ocm.software/v1alpha1
kind: LocalizationConfig
metadata:
  name: my-helm-chart-localization
spec:
  rules:
  - yamlsubst:
      source:
        resource:
          name: my-image-with-my-app-inside
      target:
        file:
          path: values.yaml
          value: deploy.image
```

The following result would appear through a ConfiguredResource:

```yaml
apiVersion: delivery.ocm.software/v1alpha1
kind: ConfiguredResource
metadata:
  name: my-localized-helm-chart
spec:
  target:
    kind: Resource
    name: my-helm-chart
  # config is a resource in the same component as the target resource and contains the localization config
  config:
    kind: ResourceConfig
    name: my-helm-chart-localization
    apiVersion: delivery.ocm.software/v1alpha1
---
apiVersion: delivery.ocm.software/v1alpha1
kind: ResourceConfig
metadata:
  name: my-helm-chart-localization
spec:
  rules:
  - source:
      value: ghcr.io/open-component-model/myimage
    target:
      file:
        path: values.yaml
        value: deploy.image
```

#### Minimal Example 2 (Cluster Reference)

```yaml
apiVersion: delivery.ocm.software/v1alpha1
kind: LocalizedResource
metadata:
  name: my-localized-helm-chart
spec:
  # target is the resource that will be localized
  target:
    name: my-helm-chart
    kind: Resource
  # config is a resource in the same component as the target resource and contains the localization config
  config:
    kind: LocalizationConfig
    name: my-localization-config
    namespace: ocm-configs
```

In this instance it is not necessary to package the LocalizedResource together with the ComponentVersion, but the LocalizationConfig
needs to be present in the Cluster where the Controllers are deployed, which is generally contradictive to our deployment story.
However, it is much easier to test and to run with existing Helm Charts without repackaging eventually already existing OCM components
or Charts, and so should provide a better transition point. It also allows testing Localizations without the need to constantly
repackage the Components.

#### Complex Configuration Example with Templating

A typical use case for Localization might be to work within structures or files that do not conform to YAML or JSON.
Even within Kubernetes, one does not need to look far to spot an example case through the use of ConfigMaps:

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: my-config
data:
  config.properties: |
    registry=ghcr.io
```

In this case, it becomes hard to work with the standard substitution engine because of its reliance on YAML / JSON.
For this reason, we also offer a templating approach powered by GoTemplates. Consider the following template file:

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: my-config
data:
  game.properties: |
    registry=ocm{ .Registry }
```

We can enable a templating configuration like this:

```yaml
apiVersion: delivery.ocm.software/v1alpha1
kind: LocalizationConfig
metadata:
  name: deployment-localization
spec:
  rules:
    - goTemplate:
        file:
          path: templates/deployment.yaml
        delimiters:
          left: "ocm{"
          right: "}"
        data:
          registry: myprivateregistry.com
```

Chosen option: "[option 1]", because [justification. e.g., only option, which meets k.o. criterion decision driver | which resolves force force | … | comes out best (see below)].

### Positive Consequences <!-- optional -->

* We now have a localization ecosystem in place that we can plugin to the Substitution Engine via the
  .transformation.type field in the LocalizationConfig CRD.
* We can now localize all resources, irrespective of YAML/JSON structure, via GoTemplates.
* We can now localize with a LocalizationConfig coming from both the ComponentVersion and the Cluster, to allow for
  flexible deployment patterns.
* We now allow for contributions by introducing the option for custom TransformationTypes. Examples could include
  kustomize Transformers or Heuristics based substitution.

### Negative Consequences <!-- optional -->

* LocalizationConfig CRD needs to be understood
* Most Substsitution References are file / path based because we do not want to make assumptions about the templating
  language used.
* The substitution logic can only really be tested from within a cluster, so we might want to expose the substitution
  behavior in a test harness.
* Using the GoTemplate substitution requires a good understanding of GoTemplates, as well as knowledge of our additional
  templating functions,
  such as "OCMResourceReference".

## Pros and Cons of the Options <!-- optional -->

### Option 1 - Pre-Processing / Templating

Advantages:

* Easy to understand and adopt for platform engineers
* Pluggable via kustomize transformers
* Can be used in conjunction with existing templating systems if injected into the resolution process
* Still allows default deployments without any localization
* Good, because the component is not invalid/inconsistent after the transport.

Disadvantages:

* Introduces a second templating step (that can be mitigated with resolution caching)
* Requires yet another templating step to be invoked (slower) in the resolution process
* Binds us to any used templating engine (be that kustomizes Transformers or not)
* Not easily possible to separate Localization from Configuration, meaning one Kustomization per Environment with non unified field specs.
* Bad, because the deployment instructions are not more valid manifest / helm
  charts / kustomize overlays.

### Option 2 - Literal Substitution Rules

Advantages:

* Allows a good mix between ease of use for simple substitution rules and complex substitution rules via resolution of functions or Go Templates.
* Is simple enough to be understood by any platform engineer with little exposure to go and cloud native technologies.
* Pluggable via custom functions that can be introduced to the Localization template parser.

Disadvantages:

* Requires a templating language to be introduced and one can no longer deploy the component version without Localization data present, except when defaults are programmed in.
* Binds us to a templating engine (Go Templates in this case)
* Not easily possible to separate Localization from Configuration, meaning one Go Template per Environment with non unified field specs. Alternatively the resolution mechanism
  would have to be able to resolve multiple values files.

### Option 3 - Component-Based Subsitution Rules (previous solution)

Advantages:

* Allows for a very fine grained control over the localization process
* Allows for separation of Localization and Configuration in a very fine grained level (2 separate CRDs)
* Allows to take over codebase from previous ocm controller
* Leverages existing replacement framework present in OCM with support for spiff++ and complex substitution rules

Disadvantages:

* New substitution / templating framework for most ops to learn
* Has issues with status reporting on actually replaced items in the Localization Custom Resource
* Requires us to write lots of code for a simple substitution rule
* Forces the concept of Localization separate from Configuration for all Scenarios

### Option 4 - Substitution With Mutating Webhooks

Advantages:

* Very easy to understand for all kubernetes experienced Ops
* Allows for fully decoupled localization process

Disadvantages:

* Requires Configuration of a Mutating Webhook in all clusters where the OCM controllers are deployed
* Requires the Mutating Webhook to be always online to avoid disruptions of resource creation/updates with Fail policy set.
* Will incur additional cost that scales with the amount of workloads and resources in the cluster, may prove disastrous in large clusters

## Links <!-- optional -->

* Configuration Process refined through [Configuration ADR](configuration.md)
