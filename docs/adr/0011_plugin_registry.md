# Plugin registry

* **Status**: proposed
* **Deciders**: Plugin registry with automatic plugin discovery
* **Date**: 2025.09.24

**Technical Story**:

Provide a way to distribute, download, and discover plugins for OCM.

---

## Context and Problem Statement

Right now, if you want to download an OCM plugin, you need to know the exact name of the component version and the name
of the resource containing the plugin binary.

For example:

```shell
ocm download plugin ghcr.io/open-component-model/ocm//ocm.software/plugins/ecrplugin:0.27.0 --resource-name demo
```

Compare this to other ecosystems like helm or npm, where you can simply run `helm install my-chart` or `npm install package-name`.

What we need is a way for users to discover plugins from registries, install them by name, and manage them like any other package manager would.
We also want to support multiple registries so enterprises can have their own private plugin collections alongside public ones.

---

## Decision Drivers

We want to reuse as much of the existing infrastructure as possible rather than reinventing the wheel.
The plugin download mechanism already works well, and component versions are a core part of how OCM operates.

We also need this to feel native to OCM, using component versions, references, and credentials the way everything else does.
It should scale to support multiple registries, including private ones.
We can also leverage the sign/verify system for verifying plugins.
This is outlined under [ComponentVersion based plugins system](#componentversion-based-plugins-system).

Another approach is to use a manifest-based registry, like helm or npm.
This would be a lot more work to maintain, but it would be more familiar to users of existing registries.
This is outlined under [Alternative manifest-based plugins system](#alternative-manifest-based-plugins-system).

---

## Outcome

The decision is to use a Component-Version-based registry. OCM components are integral to the OCM ecosystem, and we want
to reuse as much of the existing infrastructure as possible.

---

## Out of Scope

Dealing with duplicate plugins from different registries is out of the scope of this ADR. That's a complex problem and needs 
to be handled in a greater scope. Figuring out what should happen, which plugin would take precedence, should we error
out, or should we just pick one of them. We can discuss this in a separate ADR.

---

## ComponentVersion based plugins system

### Registry Component Structure

The Component Version approach treats a plugin registry like any other OCM component, but instead of containing resources
directly, it contains references to individual plugin components.
Like a table of contents that points to where all the plugins actually live:

```yaml
# Registry Component: ghcr.io/ocm/registry//ocm.software/plugin-registry:v1.0.0
name: ocm.software/plugin-registry
version: v1.0.0
provider: ocm.software
labels:
  - name: category
    value: <plugin-type>
  - name: registry
    value: official
  - name: description
    value: Official OCM plugin registry
  # Further values can be added here

references:
  - name: ecrplugin
    version: 0.27.0
    component: ocm.software/plugins/ecrplugin
    digest:
      hashAlgorithm: SHA-256
      normalisationAlgorithm: jsonNormalisation/v1
      value: abc123...
  - name: helminput
    version: 0.5.2
    component: ocm.software/plugins/helminput
    digest:
      hashAlgorithm: SHA-256
      normalisationAlgorithm: jsonNormalisation/v1
      value: def456...
  - name: cvedb
    version: 1.2.0
    component: enterprise.corp/plugins/cvedb
    digest:
      hashAlgorithm: SHA-256
      normalisationAlgorithm: jsonNormalisation/v1
      value: ghi789...
```

### Individual Plugin Component Structure

Each plugin is just a regular OCM component version containing the actual plugin binaries for different platforms.
This part doesn't change from how plugins work now:

```yaml
# Plugin Component: ocm.software/plugins/ecrplugin:0.27.0
name: ocm.software/plugins/ecrplugin
version: 0.27.0
provider: ocm.software
labels:
  - name: category
    value: <plugin-type>
  - name: registry
    value: official
  - name: description
    value: Official ECR plugin
resources:
  - name: demo                    # Plugin binary name
    type: ocmPlugin
    version: 0.27.0
    extraIdentity:
      architecture: amd64
      os: linux
    access:
      type: localBlob
      localReference: sha256:ac3f34...
  - name: demo
    type: ocmPlugin
    version: 0.27.0
    extraIdentity:
      architecture: amd64
      os: darwin
    access:
      type: localBlob
      localReference: sha256:f1764c...
```

## User Workflow

CLI command could be added for convenience to add registries (but out of scope for this proposal):

```bash
ocm plugin registry add official ghcr.io/ocm/registry//ocm.software/plugin-registry
```

The configuration for the registry is stored in the OCM configuration file. We also configure any resolvers and credentials
needed to access the registry:

```yaml
type: generic.config.ocm.software/v1
configurations:
  - type: plugin.registry.config.ocm.software
    registries:
      - ocm.software/plugin-registry # Official registry URL would be defaulted and overridden by the user if needed
  - type: resolvers.config.ocm.software/v1alpha1
    resolvers:
      - componentNamePattern: ocm.software/plugin-registry
        repository:
          type: OCIRegistry/v1
          baseUrl: ghcr.io
          subPath: open-component-model/plugins
  - type: credentials.config.ocm.software
    consumers:
      - identity:
          type: OCIRegistry/v1
          hostname: ghcr.io
        credentials:
          - type: Credentials/v1
            properties:
              username: gituser
              password: password
```

From there, the user can list plugins from all registries or a single registry:

```bash
# all registries
ocm plugin registry list
NAME                             VERSION   DESCRIPTION                     REGISTRY
ocm.software/plugin/ecrplugin    0.27.0    AWS ECR repository plugin       ocm.software/plugin-registry
ocm.software/plugin/helminput    0.5.2     Helm input method plugin        ocm.software/plugin-registry
example.com/plugin/cvedb         1.2.0     CVE database integration        example.com/plugin-registry
```

```bash
# single registry
ocm plugin registry list --registry ocm.software/plugin-registry
NAME                             VERSION   DESCRIPTION                     REGISTRY
ocm.software/plugin/ecrplugin    0.27.0    AWS ECR repository plugin       ocm.software/plugin-registry
ocm.software/plugin/helminput    0.5.2     Helm input method plugin        ocm.software/plugin-registry
```

And install plugins by name:

```bash
# Would look for the plugin in the configured registry 
ocm plugin registry install ocm.software/plugin/ecrplugin

# Or install a specific version if you need to
ocm plugin registry install ocm.software/plugin/ecrplugin@0.26.0
```

New plugins can be published to the registry by authorized CI pipelines, using the component constructor
file for the original root component. During this process a new component version is created and published to the registry.

All other operations, like pushing a new version of the plugin is done via regular `ocm add cv` commands with the component
constructor file of the plugin.

A plugin needs to be identifiable by its full component identity to avoid conflicts with other plugins. 

## Alternative manifest-based plugins system

### Registry Index Structure

The alternative approach works more like Helm chart repositories, basically a manifest file hosted somewhere that lists
all the available plugins and where to download them from:

```yaml
# https://plugins.ocm.software/index.yaml
apiVersion: v1
entries:
  ecrplugin:
    - name: ecrplugin
      version: 0.27.0
      description: AWS ECR repository plugin
      home: https://github.com/ocm/ecrplugin
      sources:
        - https://github.com/ocm/ecrplugin
      created: "2025-09-01T10:00:00Z"
      digest: sha256:abc123...
      downloads:
        - os: linux
          architecture: amd64
          url: https://plugins.ocm.software/ecrplugin/0.27.0/ecrplugin-linux-amd64
          sha256: ac3f340100668ff6e6c8f43284b6763893d652edb2333f9a8e97f21478c3e393
        - os: darwin
          architecture: amd64
          url: https://plugins.ocm.software/ecrplugin/0.27.0/ecrplugin-darwin-amd64
          sha256: f1764c6d84cdcae35886d02441de1cc81c7df02a3f5bb7710a1418bfe1b985c0
        - os: windows
          architecture: amd64
          url: https://plugins.ocm.software/ecrplugin/0.27.0/ecrplugin-windows-amd64.exe
          sha256: c81bf976fec0007476b738af769947e75544ca1661369e8d30403bab6553224e

  helmInput:
    - name: helmInput
      version: 0.5.2
      description: Helm input method plugin
      home: https://github.com/ocm/helmInput
      created: "2025-08-15T14:30:00Z"
      digest: sha256:def456...
      downloads:
        - os: linux
          architecture: amd64
          url: https://plugins.ocm.software/helminput/0.5.2/helminput-linux-amd64
          sha256: 112f227eb5310d4b9f85c0bf5ca0ad97464c0ea3f54c936bdecdebba01143c6a

generated: "2025-09-24T10:00:00Z"
```

### Index Generation

Registry maintainers update the index by:

```bash
# Generate index.yaml from plugin directory
ocm plugin registry generate-index ./plugins/ > index.yaml

# Upload to registry
aws s3 cp index.yaml s3://plugins.ocm.software/
aws s3 sync ./plugins/ s3://plugins.ocm.software/
```

### User Workflow for Index-based registries

The user workflow largely remains the same as outlined in [User Workflow](#user-workflow). The underlying implementation
changes, of course, to use the index instead of the registry component.

## Pros and Cons

### Component Version Registry Approach

#### Pros

It integrates with everything ocm already does; repositories, credentials, signatures, verification, distribution,
download, upload, etc.
It can include private registries and enterprise deployments, and it's consistent with how other OCM artifacts work.

We can reuse a large portion of the existing `ocm download plugin` implementation, and existing workflows keep working.
Component descriptors also give you metadata, labels, and provenance information. This command then would be replaced
by the new command structure.

#### Cons

It does require some ocm knowledge to host registries.
Plugin publishers need to understand the concept of component versions, and setting up registries requires ocm tooling.
There's also a bit more metadata overhead compared to simple file downloads.
However, this can be further simplified with various commands that hide away this complexity. Simple things like
`ocm plugin registry add` or `ocm plugin registry create` can be added to make it easier to get started.

However, to alleviate the complexity somewhat, OCM should provide tooling to simplify the process of publishing plugins.
Either by providing convenience commands or by providing a plugin publishing pipeline that can be used with CI/CD such
as a dedicated GitHub action.

### Manifest Index Registry Approach

#### Pros

The manifest approach has the advantage of being familiar with existing systems like helm, npm, etc., that people already know.
It's simple for plugin authors to publish binaries, lightweight in terms of metadata, and you can host it on simple web servers or object storage.
You don't need any ocm knowledge to set up a registry. Anyone can host a registry anywhere as long as the manifest is accessible.

#### Cons

We'd need to build all the code. Meaning, caching, authentication, discovery, etc. It's a lot of work to maintain and support.
It's inconsistent with how other ocm artifacts work, provides less structured metadata about plugins, and doesn't have
built-in integrity verification beyond basic checksum.

## Conclusion

Recommendation: Component Version Registry Approach

Reasons:

1. Uses a lot of existing code
2. Uses the existing sign/verify flow for plugins
3. Consistent with the existing ocm ecosystem, like landscaper.
4. Authentication, credentials, everything already exists.
