---
title: "Input and Access Types"
description: "How to add resources to a component version using input and access types"
weight: 44
toc: true
---

- [Overview](#overview)
  - [Input Types](#input-types)
    - [binary](#binary)
    - [dir](#dir)
    - [docker](#docker)
    - [dockermulti](#dockermulti)
    - [file](#file)
    - [helm](#helm)
    - [ociImage](#ociimage)
    - [spiff](#spiff)
    - [utf-8](#utf-8)
  - [Access Types](#access-types)
    - [gitHub](#github)
    - [helm](#helm-1)
    - [npm](#npm)
    - [ociArtifact](#ociartifact)
    - [s3](#s3)

## Overview

The Open Component Model spec supports multiple methods how to add resources to a component version. There are two different ways to add content: Input Type and Access Type.

An **Input type** adds content *by value*, along with the component descriptor and stores it in the same target repository where the component is stored. After pushing the content to the target registry this always resolves to the attribute

```yaml
relation: local
```

in a component descriptor.

An **Access Type** just adds content *by reference* to an external location, e.g., an OCI registry. It is a kind of pointer in a component descriptor. It resolves to the attribute

```yaml
relation: external
```

in a component descriptor.

The following input types are supported:

- binary
- dir
- docker
- dockermulti
- file
- helm
- ociImage
- spiff
- utf-8

Please use the latest ocm-cli to check available input types:

```bash
ocm add resources --help | grep ' - Input type' | sort -f
```

The following list of access types is supported:

- gitHub
- localBlob
- ociArtifact
- ociBlob
- s3

Please use the latest ocm-cli to check available access types:

```bash
ocm ocm-accessmethods | grep '  - Access type' | sort -f
```

Not all access and input types can be combined in useful ways with all artifact types. But the OCM specification does not define any restrictions on possible combinations.

The following sections give an overview and typical usage examples for access and input types. It does not describe the full list of possible fields and their meaning. For a complete list of attributes, please see the [command reference](/docs/reference/ocm-cli/add/resources/). The examples below are meant to be used in a component that looks like this:

```yaml
- name: github.com/open-component-model/megacomponent
  version: 0.1.0
```

### Input Types

#### binary

Allows to define resources with binary content being base64 encoded. Should only be used for smaller blobs.

```yaml
  resources:
  - name: noticeencoded
    type : blob
    input:
      data: VGhpcyBpcyBzb21lIGJhc2U2NCBlbmNvZGVkIGRhdGEK
      mediaType: text/plain
      compress: false
      type: binary
```

#### dir

Defines a resource from content of a directory in the local file system. It is packed with `tar` and optionally compressed.

```yaml
  resources:
  - name: megadir
    type : fileSystem
    input:
      type: dir
      path: ./logos
```

#### docker

Takes an image from the local docker registry and adds it as a resource. Requires a running docker daemon.

```yaml
  resources:
  - name: megaimage
    type : ociImage
    input:
      type: docker
      repository: images/mega
      path: megacomp:${VERSION}
```

if VERSION is set to 0.1.0 the following image is imported:

```sh
docker image ls
REPOSITORY                         TAG                       IMAGE ID       CREATED         SIZE
megacomp                           0.1.0                     9aab9cbca56e   5 days ago      7.46MB
```

The target location of the image can be set with the `repository` field. Here the resulting image will be stored at `<REPO_URL>/github.com/open-component-model/megacomponent/images/mega:1.10`.

#### dockermulti

Takes multiple images from the local docker registry and adds them as single multi-arch image. Requires a running docker daemon. The images have to be built for different architectures/os and need a unique tag identifying them. As docker does not support multi-arch images at the time of writing this is a workaround.

```yaml
  resources:
  - name: megaimagemulti
    type : ociImage
    input:
      type: dockermulti
      repository: images/megamulti
      variants:
        - megacomp:${VERSION}-linux-amd64
        - megacomp:${VERSION}-linux-arm64
```

if VERSION is set to 0.1.0 the following image is imported:

```sh
docker image ls
REPOSITORY                         TAG                       IMAGE ID       CREATED         SIZE
megacomp                           0.1.0-linux-amd64         96659c4f7a35   5 days ago      7.05MB
megacomp                           0.1.0-linux-arm64         64f209acb814   5 days ago      7.46MB
```

The target location of the image can be set with the `repository` field. Here the resulting image will be stored at `<REPO_URL>/github.com/open-component-model/megacomponent/images/megamulti:1.10`.

#### file

Imports a file from the local file system and adds it as a resource.

```yaml
  resources:
  - name: mega-file
    type: blob
    input:
      type: file
      path: ./logos/logo-image.png
```

#### helm

Imports a helm chart from the local file system and adds it as a resource.

```yaml
  resources:
  - name: mega-chart
    type: helmChart
    input:
      type: helm
      path: ./megachart
      repository: charts/mega
```

After transporting the corresponding component version to an OCI registry, the helm chart will be made available under `charts/mega` prefixed by the name of the component version. This auto-prefix can be disabled by using a leading slash `/charts/mega`. If the `repository` tag is omitted, the name of the helm chart from `Chart.yaml` will be used.

It is also possible to import a helm chart from a helm chart repository:

```yaml
  resources:
  - name: mariadb-chart
    type: helmChart
    input:
      type: helm
      helmRepository: https://charts.bitnami.com/bitnami
      path: mariadb
      version: 12.2.7
      repository: charts/mariadb
```

Here the helm chart version `12.2.7` is copied from the path `mariadb` in helm chart repository `https://charts.bitnami.com/bitnami`. After transporting the corresponding component version to an OCI registry, the helm chart will be made available under `charts/mariadb` prefixed by the name of the component version. This auto-prefix can be disabled by using a leading slash `/charts/mariadb`. If the `repository` tag is omitted, the name of the helm chart from `Chart.yaml` will be used. There are additional optional fields `caCert` and `caCertFile` to specify a TLS certificate for the helm chart repository.

#### ociImage

Takes an image that is located in an OCI registry and adds it as a resource.

```yaml
  resources:
  - name: mega-image
    type: ociImage
    input:
      type: ociImage
      path: gcr.io/google_containers/echoserver:1.10
      repository: images/echo
```

The target location of the image after transporting to an OCI registry can be set with the `repository` field. Here the resulting image will be prefixed with the name of the component version, e.g., `github.com/open-component-model/megacomponent/images/echo:1.10`. This auto-prefix can be disabled by using a leading slash `/images/echo`.

#### spiff

Processes a resource using the spiff templater and can provide values for variables.

```yaml
  resources:
  - name: mega-package
    type: toiPackage
    input:
      type: spiff
      mediaType: application/vnd.toi.ocm.software.package.v1+yaml
      path: packagespec.yaml
      values:
        RELEASE_NAME: megacomp
```

#### utf-8

Adds a resource from inline text.

```yaml
  resources:
  - name: noticeplain
    type : blob
    input:
      text: "Here is some text"
      mediaType: text/plain
      compress: false
      type: utf8
```

### Access Types

#### gitHub

Refers to a Git repository at a certain commit or tag.

```yaml
  resources:
  - name: git-ocm
    type: blob
    version: ${VERSION}
    access:
      type: gitHub
      repoUrl: https://github.com/open-component-model/ocm
      commit: 42cc249aec77aa64984b2b91eb0f3b96dd63aacd
```

#### helm

Refers to a helm chart located in a helm chart repository.

```yaml
  - name: mariadb-chart
    type: helmChart
    version: ${VERSION}
    access:
      type: helm
      helmChart: mariadb:12.2.7
      helmRepository: https://charts.bitnami.com/bitnami
```

#### npm

Refers to an npm package located in a Javascript package registry.

```yaml
  - name: prime-npm
    type: ocm/npmPackage
    version: ${VERSION}
    access:
      type: npm
      package: random-prime
      version: 4.0.0
      registry: https://registry.npmjs.org
```

#### ociArtifact

Refers to an image in an (external) OCI registry.

```yaml
  resources:
  - name: echo-image
    version: "1.10"
    type: ociImage
    access:
      type: ociArtifact
      imageReference: gcr.io/google_containers/echoserver:1.10
```

#### s3

Refers to an object in an AWS S3 store.

```yaml
  resources:
  - name: gardenlinux-meta
    type: blob
    version: ${VERSION}
    access:
      type: s3
      bucket: gardenlinux
      key: meta/singles/gcp-cloud-gardener-_prod-890.0-53b732
```
