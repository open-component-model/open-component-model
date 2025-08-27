# Configuration

* Status: proposed
* Deciders: Gergely Brautigam, Fabian Burth, Jakob Moeller, Uwe Krueger
* Date: 2024-10-15

Technical Story:

Configuration is done before deployment. There are various ways ranging from plain substitution to
more complex CUE based configuration approaches. The reasons for configuration is quite
obvious. It gives a chance to provide multiple environments based on the same Component.
This means that there can be more than one `Configuration` object for a single Component
depending on how many environments we would like to set up / deploy to.

Let's walk through the current way of providing configuration options. There a plenty.
Then, we are going to walk through some more proposals that attempt to streamline the configuration
to be more useful or easier to follow.

## Ways of providing Configuration

There are multiple ways of providing Configuration values.

### Values

Plain values provided as inline values.

```yaml
apiVersion: delivery.ocm.software/v1alpha1
kind: Configuration
metadata:
  name: configuration-signed-backend
  namespace: ocm-system
spec:
  interval: 5s
  sourceRef:
    ...
  configRef:
    kind: Resource
    name: podinfo-signed
    resourceRef:
      name: config
      referencePath:
        - name: backend
  values:
    message: "This is a test message signed Backend"
```

### ValuesFrom

ValuesFrom can be one of the following three options:

* flux source
    * currently only GitRepository
* configmap
* component version resource

#### Flux Source

In case of a flux source, we take the values from a specific file and do the merge. An option `subPath`
can be used to further refine what value to take:

```yaml
  valuesFrom:
    fluxSource:
      sourceRef:
        kind: GitRepository # get the values from a git repository provided by flux
        name: flux-system
        namespace: flux-system
      path: ./values.yaml
      subPath: component-x-configs
```

#### ConfigMap

This one is pretty self-explanatory:

```yaml
  configMapSource:
    sourceRef:
      name: test-config-data
    key: values.yaml
    subPath: test.backend
```

Again, an optional `subPath` can be used to further refine value substitution.

#### Component, Resource or Artifact provider

This one can be used for values provided through another resource. This option
is convenient if values are bundled with another component and are shipped together
with the target component. Or are the end result of a Localization step. Anything
that can provide an Artifact (formerly known as Snapshot) can provide values.

```yaml
  sourceRef:
    apiVersion: delivery.ocm.software/v1alpha1
    kind: Resource
    name: podinfo
    namespace: ocm-system
    resourceRef:
      name: deployment
```

The above uses a component version and a specific resourceRef with the name `deployment`.

## Schema Validation

We aren't just providing values plain as is. We are also providing schema validation options
for these configurations. This is important to make sure that no breaking change happens between
versions or, if there _is_ a breaking change, to at least ensure that it is caught and can be communication up the call chain.

This is done by the component author. They provide the schema that validates the configuration
options. They can make sure, for example, that the replica count offers the best option
for the tool they are providing. Or that a certain field is filled / configured and cannot be
left empty, like the service account.

## Option 1 - Spiff

Aka, YAML templating using [Spiff++](https://github.com/mandelsoft/spiff).

Substitution rules are generated with plain spiff after nodes are correctly configured. This
is important because default values need to be handled correctly. In other words,
users' values have to be merged correctly with the defaults taken from the component version itself.

The current Configuration object looks like this:

```yaml
apiVersion: delivery.ocm.software/v1alpha1
kind: Configuration
metadata:
  name: configuration-signed-backend
  namespace: ocm-system
spec:
  interval: 5s
  sourceRef: # defines where to get what we want to configure
    ...
  configRef:
    kind: Resource
    name: podinfo-signed
    resourceRef:
      name: config
      version: 1.0.0
      referencePath:
        - name: backend
  values:
    message: "This is a test message signed Backend"
```

Here, the Configuration is provided by the component author using a proprietary configuration
object called `ConfigData`. That data looks something like this:

```yaml
apiVersion: config.ocm.software/v1alpha1
kind: ConfigData
metadata:
  name: ocm-config-pipeline-backend
  labels:
    env: test
configuration:
  defaults:
    replicas: 1
    cacheAddr: tcp://redis:6379
    message: Hello, world!
  schema:
    type: object
    additionalProperties: false
    properties:
      replicas:
        type: integer
      cacheAddr:
        type: string
      message:
        type: string
  rules:
  - value: (( replicas ))
    file: manifests/deploy.yaml
    path: spec.replicas
  - value: (( cacheAddr ))
    file: manifests/configmap.yaml
    path: data.PODINFO_CACHE_SERVER
  - value: (( message ))
    file: manifests/configmap.yaml
    path: data.PODINFO_UI_MESSAGE
localization:
- resource:
    name: image
  file: manifests/deploy.yaml
  image: spec.template.spec.containers[0].image
```

We mingle Localization within the same data type so people don't have to use a different
configuration object for the same substitution principle.

In here, we have rules for configuration and the schema that is used to
verify the configuration values. We also have default values that will be merged
with the user's values. The end-result is a sort of three-way merge. `file` determines
where the change needs to happen and `path` determines the location in the file.

At the end of the day, no matter where the configuration values are coming from, a plain
`Localization` is performed using the OCM library:

```go
 if err := localize.Substitute(rules, virtualFS); err != nil {
  return "", fmt.Errorf("localization substitution failed: %w", err)
 }
```

**Pros**:

* This is working right now and can handle a multitude of complex yaml scenarios.
* It is relatively easy to use from the user's perspective because the component author
  sets up the configuration similar to helm values. User's only need to consider
  the values they would like to set up.
* It can be used multiple times.
* The deployment works out of the box, because the configuration is not part of
  the actual manifest.

**Cons**:

* Proprietary configuration is difficult to understand and with spiff++, this
  approach introduces yet another templating language users will need to
  understand and get used to.
* Only works with YAML and nothing else.

## Option 2 - Go Templating

One of the more popular options would be to use Go templating. We could provide certain
functions that make life easier for configuration providers too. Alternatively,
Masterminds already have a plethora of useful functions that users are aware of and could
use without problems: <https://masterminds.github.io/sprig/>

One of the functions is a `default` function that would be very useful for component
providers to provide values in case users don't.

Consider the following deployment with Go templating:

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: controller-manager
  namespace: system
  labels:
    control-plane: controller-manager
spec:
  selector:
    matchLabels:
      control-plane: controller-manager
  replicas: 1
  template:
    metadata:
      labels:
        control-plane: controller-manager
    spec:
      containers:
      - command:
        - /manager
        image: {{ .Image | default "ghcr.io/open-component-model/ocm-k8s-toolkit:latest" }}
        name: manager
```

User provided values through the configuration object would look something like this:

```yaml
apiVersion: delivery.ocm.software/v1alpha1
kind: Configuration
metadata:
  name: configuration-signed-backend
  namespace: ocm-system
spec:
  interval: 5s
  sourceRef: # defines where to get what we want to configure
    ...
  configRef:
    ...
  values:
    Image: "myregistry.io/org/ocm-k8s-toolkit:v0.0.1"
```

Once the templating is run, the user's configuration values are loaded into the right struct and
applied to the templated deployment.

**Pros**:

* Users are already familiar with go templating through extensive usage in Helm.
* Users have options to opt out from configuring anything as default values will
  then be applied instead.

**Cons**:

* Component authors will have to provide the templating inside their deployment.
* Providing default values will have to be in addition to templating so the tooling
  can apply any defaults if they aren't provided by the user.
* Things aren't working until templating is applied to the manifest files
    * I believe this isn't that huge of an issue since people are already used to templated
    files through extensive use of Helm.

## Option 3 - Patch Strategic Merge

Right now, `PatchStrategicMerge` is only available for Localization, but it could be extended
to be part of the Configuration chain, since it's basically the same operation.

In case of multiple complex configurations, for example, multiple sites that are part of
different regions, and we would like to always apply regional configs, it would be
easy to provide a git repository with paths like these:

* sites/eu-west-1/deployment.yaml
* sites/eu-west-2/deployment.yaml
* sites/us-east-1/deployment.yaml

These deployment files could contain partial or full deployment configurations and could
be applied through merging with original deployment content:

```yaml
patchStrategicMerge:
  source:
    sourceRef:
      kind: GitRepository
      name: gitRepo
      namespace: default
    path: "sites/eu-west-1/deployment.yaml"
  target:
    path: "merge-target/merge-target.yaml"
```

**Pros**:

* Easy to configure.
* No extra templating language needed to be understood.

**Cons**:

* Rigid patching.
* No default values.
* It is possible to overwrite existing values.
* It needs a valid yaml to work.

## Option 3.5 - Combining Patch Strategic Merge with Go Templating

In order to avoid people having to template their manifests, we can offer an alternative
solution where we combine the patch strategic merge option with a version of the Manifest
that is templated.

Consider a component version providing the following Deployment manifest:

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: my-deployment
spec:
  replicas: 2
```

The user would provide a strategic merge patch like this:

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: my-deployment
spec:
  replicas: {{ .Replicas }}
```

Then we would provide a configuration:

```yaml
apiVersion: delivery.ocm.software/v1alpha1
kind: Configuration
metadata:
  name: configuration-signed-backend
  namespace: ocm-system
spec:
  interval: 5s
  sourceRef: # defines the original chart, should probably also contain the path to the resource
    ...
  configRef: # defines the strategic merge patch file, can come from anywhere
    ...
  values:
    Replicas: 3 # gets injected automatically
```

And after merging we would perform the templating.

**Pros**:

* The deployment remains compatible and applyable.
* The provider doesn't have to bother with templating.

**Cons**:

* The same rigidness as with strategic merge patch, meaning it only works with valid YAML.
* The user needs to be completely aware of how the manifest looks like in order to do a
  valid merge.

# Option 4

Localization introduced a hybrid approach to templating and configuration. Since
effective configuration follows the same path as Localization; the only difference being
that Configuration objects can be applied multiple times, while Localization is a single
shot entity; we can apply the same principles here too.

Applying the same principle of fetching a config object and then determining the location
of the value to be changed we could use something like this:

```yaml
apiVersion: delivery.ocm.software/v1alpha1
kind: ConfigurationConfig
metadata:
  name: deployment-configuration
spec:
  rules:
  - yamlsubst:
      source:
        # using a resource
        resource:
          name: my-config-values
        # using a direct value
        value: 2
      target:
        file:
          path: values.yaml
          value: global.replica
```

The value would be used to find the relevant exchange value in the referenced
configuration object as well. Be that a value file or a ConfigMap it isn't
relevant.

_Note_: We might be able to even re-name this object to something like
MutationConfig that would perform either operation based on the referencing
Object.

Once these values are defined, we would follow the same principles outlined
in the Localization [## Decision Outcome](localization.md#decision-outcome).

## Decision Outcome

In the end, we essentially decided for option 4 - to use the same mechanism and
custom resources as the localization uses to perform the actual value
substitution.

### Examples

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
    # < one of ResourceConfig, Resource, LocalizedResource, ConfiguredResource >
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
