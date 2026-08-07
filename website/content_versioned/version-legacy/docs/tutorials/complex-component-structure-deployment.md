---
title: "Structure Software Products with OCM"
description: "Learn how to structure complex software products using OCM."
icon: "🗂️"
weight: 43
toc: true
---

## Introduction

In this tutorial software products are comprised of logical units called [*components*]({{< relref "components.md" >}}). A component version consists of a set of technical [*artifacts*](https://github.com/open-component-model/ocm-spec/blob/main/doc/04-extensions/01-artifact-types/README.md) (e.g., Docker images, Helm charts, binaries, configuration data, etc.). Such artifacts are called *resources* in this specification. Resources are usually built from something, e.g., code in a git repo. Those are named *sources* in this specification.

OCM introduces a *Component Version* for every component version that
describes the resources, sources, and other component versions belonging to a particular
component version and how to access them.

Usually, however, real-life applications are composed of multiple components. For
example, an application might consist of a frontend, a backend, a database, and a web server.
During the software development process new
[*component versions*](https://github.com/open-component-model/ocm-spec/blob/main/doc/01-model/02-elements-toplevel.md#components-and-component-versions)
are created and third-party components might be consumed from a public registry and
updated from time to time.

Not all component version combinations of frontend, backend, database, etc. are
compatible and form a valid product version. In order to define reasonable
version combinations for the software product, we could use another feature of
OCM's *Component Version*, called a [*Component Reference*](https://github.com/open-component-model/ocm-spec/blob/main/doc/02-processing/01-references.md?plain=1#L45) (or reference in short), which allows
the aggregation of component versions.

For each sub-component and each version in use, there is a *Component Version*. For the
entire application, we introduce a new component that describes the overall software
product referencing all components. This describes the entire application or product.

A particular version of this application is again described by a *Component Version*,
which contains references to the *Component Versions* of its sub-components in their version in
use. You are not restricted to this approach. It is, e.g., possible to create multi-level
hierarchies or you could just maintain a list of component version combinations which build
a valid product release.

In a nutshell, OCM provides a simple approach to specify what belongs to a product version.
Starting with the *Component Version* for a product version and following the component
references, you could collect all artifacts belonging to this product version.

## Prerequisites

We assume that you have already read the guides in the [Getting Started]({{< relref "docs/getting-started/_index.md" >}}) section, as this guide discusses a more complex scenario.

## Constructing the Component

We are going to use [`podinfo`](https://github.com/stefanprodan/podinfo) in microservices mode. This describes a setup with multiple microservices forming a larger application.

`podinfo` has three services which we are going to model using individual component versions:

- backend
- frontend
- cache (redis)

We will use the following example application to demonstrate a multi-component structure using `podinfo`: [Podinfo Component](https://github.com/open-component-model/podinfo).

This repository contains the following items:

### Component File

The following component-constructor file describes four components: three components, each representing a `podinfo` microservice and one  *aggregated* component that brings together the `podinfo` components using *references*.  We refer to the aggregated component as the *product component*. A component-constructor file can contain one or multiple components and references to other components. The file is a YAML file and can be validated using the [OCM schema](https://ocm.software/schemas/configuration-schema.yaml).

```yaml
# specify a schema to validate the configuration and get auto-completion in your editor
# yaml-language-server: $schema=https://ocm.software/schemas/configuration-schema.yaml
components:

# -- product component
- name: ocm.software/podinfo
  version: 1.0.2
  labels:
  - name: ocm.software/labels/podinfo/purpose
    value:
      - kind: test
        type: manual
  provider:
    name: open-component-model
  componentReferences:
  - name: backend
    componentName: ocm.software/podinfo/backend
    version: 1.0.0
  - name: frontend
    componentName: ocm.software/podinfo/frontend
    version: 1.0.0
  - name: redis
    componentName: ocm.software/redis
    version: 1.0.0
  sources:
  - access:
      commit: ac0afafcf4aa333546634cba631f0090a0a4cbe3
      ref: refs/heads/main
      repoUrl: https://github.com/open-component-model/podinfo
      type: github
    name: github_com_open_component_model_podinfo
    type: git
    version: 1.0.0

# -- backend component
- name: ocm.software/podinfo/backend
  version: 1.0.0
  provider:
    name: open-component-model
  labels:
  - name: ocm.software/labels/podinfo/service
    value: backend
  resources:
  - name: config
    type: configdata.ocm.software
    input:
      type: file
      mediaType: application/yaml
      path: backend/config.yaml
      compress: true
  - name: image
    relation: external
    type: ociImage
    version: 6.2.0
    access:
      type: ociArtifact
      imageReference: ghcr.io/stefanprodan/podinfo:6.2.0
  - name: manifests
    type: kustomize.ocm.fluxcd.io
    input:
      type: dir
      path: backend/manifests
      compress: true
  sources:
  - access:
      commit: 9d294e85d8d3fe7803d1eccbf009619078d30cb9
      ref: refs/heads/main
      repoUrl: https://github.com/open-component-model/podinfo
      type: github
    name: github_com_open_component_model_podinfo
    type: git
    version: 1.0.0

# -- frontend component
- name: ocm.software/podinfo/frontend
  version: 1.0.0
  provider:
    name: open-component-model
  labels:
  - name: ocm.software/labels/podinfo/service
    value: frontend
  resources:
  - name: config
    type: configdata.ocm.software
    input:
      type: file
      mediaType: application/yaml
      path: frontend/config.yaml
      compress: true
  - name: image
    relation: external
    type: ociImage
    version: 6.2.0
    access:
      type: ociArtifact
      imageReference: ghcr.io/stefanprodan/podinfo:6.2.0
  - name: manifests
    type: kustomize.ocm.fluxcd.io
    input:
      type: dir
      path: frontend/manifests
      compress: true
  sources:
  - access:
      commit: 9d294e85d8d3fe7803d1eccbf009619078d30cb9
      ref: refs/heads/main
      repoUrl: https://github.com/open-component-model/podinfo
      type: github
    name: github_com_open_component_model_podinfo
    type: git
    version: 1.0.0

# -- redis component
- name: ocm.software/redis
  version: 1.0.0
  provider:
    name: open-component-model
  labels:
  - name: ocm.software/labels/podinfo/service
    value: redis
  resources:
  - name: config
    type: configdata.ocm.software
    input:
      type: file
      mediaType: application/yaml
      path: redis/config.yaml
      compress: true
  - name: image
    relation: external
    type: ociImage
    version: 6.0.1
    access:
      type: ociArtifact
      imageReference: redis:6.0.1
  - name: manifests
    type: kustomize.ocm.fluxcd.io
    input:
      type: dir
      path: redis/manifests
      compress: true
  sources:
  - access:
      commit: 9d294e85d8d3fe7803d1eccbf009619078d30cb9
      ref: refs/heads/main
      repoUrl: https://github.com/open-component-model/podinfo
      type: github
    name: github_com_open_component_model_podinfo
    type: git
    version: 1.0.0

```

With the components modeled we can start to build a component archive using the `ocm`  cli:

```sh
ocm add componentversions --create --file component-archive component-constructor.yaml
processing component-constructor.yaml...
  processing document 1...
    processing index 1
    processing index 2
    processing index 3
    processing index 4
found 4 components
adding component ocm.software/podinfo:1.0.2...
  adding reference ocm.software/podinfo/backend: "name"="backend","version"="1.0.0"...
  adding reference ocm.software/podinfo/frontend: "name"="frontend","version"="1.0.0"...
  adding reference ocm.software/redis: "name"="redis","version"="1.0.0"...
adding component ocm.software/podinfo/backend:1.0.0...
  adding resource configdata.ocm.software: "name"="config","version"="<componentversion>"...
  adding resource ociImage: "name"="image","version"="6.2.0"...
  adding resource kustomize.ocm.fluxcd.io: "name"="manifests","version"="<componentversion>"...
adding component ocm.software/podinfo/frontend:1.0.0...
  adding resource configdata.ocm.software: "name"="config","version"="<componentversion>"...
  adding resource ociImage: "name"="image","version"="6.2.0"...
  adding resource kustomize.ocm.fluxcd.io: "name"="manifests","version"="<componentversion>"...
adding component ocm.software/redis:1.0.0...
  adding resource configdata.ocm.software: "name"="config","version"="<componentversion>"...
  adding resource ociImage: "name"="image","version"="6.0.1"...
  adding resource kustomize.ocm.fluxcd.io: "name"="manifests","version"="<componentversion>"...
```

This will create a folder called `component-archive`. The structure of that should look something like this:

```sh
tree .
.
├── artifact-index.json
└── blobs
    ├── sha256.03ac3a7611e118d08fcf70e9b7be263c4a7082066f9763f71d8901d7fa2afc9d
    ├── sha256.118b6e8282ee1d335b1638a76a20022b6acc319177dbbce3089700da835afb6a
    ├── sha256.12073781e4fba95f19f046c51c90f0c4e1338d47afe4795bf6fcca163ae46eb8
    ├── sha256.1f239399104ec0cc7680956eb60960d212b3368609feb83dac2c95040d24b480
    ├── sha256.3c9c902ce013ca070a29634e4603c90063c96df632ef2c8e6b4447aaeb70b67e
    ├── sha256.3dc6209959eb782fa6f5f44892f66e9657276735bfb40407bd00ddca30d0a9d1
    ├── sha256.654debd65dbadbcee73e55b675980865ddf22acffcec166c59a5e48a213e4dd5
    ├── sha256.699ea8628e39256048cd1687c496fe64999a41f16f200ef5ce938ee9f19c37f0
    ├── sha256.70a47378c043721e3099801dec02c44b1dd9cdef0ebf79c55784eb4666bdbc29
    ├── sha256.773b28fb63f1195ff73e328744639ddc1c574d58c1e723d6e386fcd66b45bd9c
    ├── sha256.893be914eebd8230ef848ea82b3433c6201152f5d9925e7b5b8d68e0cec7133e
    ├── sha256.92991cf391167c928f3afe6891001f3dd325b64ce800cf34fad4c038141fc57f
    ├── sha256.98ca4d46130f5c09a704b3d8ee9af94de3c0ac73d7e990df53e64606c418fea8
    ├── sha256.a779270c2fea310835d3125de90e089e423c9730a98f1acdda328470d21fced0
    ├── sha256.a7dd532f80e8417ed33cf0c97328582847017895fc5146e499bdf4c94a9d17b5
    ├── sha256.cae4365f264251c616210707aa4765bd95f23fd22f98abc68bae9f58d6e4506d
    ├── sha256.ee79c92bbcce9e7a98f07c6577fd56dd45cf6f7c2d3115216ee249f42119030e
    └── sha256.f6a82a23220752c232e5f66ce46f0be28b27a5af19474072c77dac6d1feb0c16

2 directories, 19 files
```

These blobs contain the resources we described when modelling our podinfo application. If we `cat` a random blob we get
something like this:

```sh
cat sha256.3c9c902ce013ca070a29634e4603c90063c96df632ef2c8e6b4447aaeb70b67e
{"componentDescriptorLayer":{"mediaType":"application/vnd.ocm.software.component-descriptor.v2+yaml+tar","digest":"sha256:699ea8628e39256048cd1687c496fe64999a41f16f200ef5ce938ee9f19c37f0","size":2560}}%
```

Next, we transfer this component to an OCI registry of your choice. Here `<your-location>` for me was `ghcr.io/skarlso/demo-component`.

```sh
ocm transfer component ./component-archive <your-location>
transferring version "ocm.software/podinfo:1.0.2"...
...adding component version...
transferring version "ocm.software/podinfo/backend:1.0.0"...
...resource 0...
...resource 2...
...adding component version...
transferring version "ocm.software/podinfo/frontend:1.0.0"...
...resource 0...
...resource 2...
...adding component version...
transferring version "ocm.software/redis:1.0.0"...
...resource 0...
...resource 2...
...adding component version...
4 versions transferred
```

With the transfer completed, we now have a product `*Component Version*` that describes a set of sub-components using `*Component References*`. It bundles all required artifacts for a successful deployment of the complete product.

## Conclusion

We saw how to create a complex, multi-service architecture product component and store it in an OCI registry.
