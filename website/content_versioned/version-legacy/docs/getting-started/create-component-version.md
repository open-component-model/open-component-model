---
title: "Create Component Versions"
description: "Learn how to create and store component versions using the OCM CLI."
icon: "📦"
weight: 23
toc: true
---

## Creating and Storing Component Versions

Component Versions are created using a `component-constructor.yaml` file, which is a description file that contains one or multiple components. The file describes the components and their artifacts - resources and sources, metadata in form of labels and references to other components.

Component Versions are locally stored in archives using the [Common Transfer Format (CTF)](https://github.com/open-component-model/ocm-spec/blob/main/doc/04-extensions/03-storage-backends/ctf.md). A CTF archive may contain any number of component versions and is used to transfer components to and between component repositories.

Note that a CTF archive itself is also an OCM repository, so it can be used as source or target for component transfer operations using the OCM CLI.

The command [`ocm add componentversions`](https://github.com/open-component-model/ocm/blob/main/docs/reference/ocm_add_componentversions.md)
directly creates a component version from a `component-constructor.yaml` file and stores it in a local CTF archive.

### Create a Component Version

In this example we will use the The `ocm` CLI tool to create a very basic component version that contains a local resource and a resource that is accessed from a remote location. The local resource is the `podinfo` Helm Chart and the referenced resource is a Docker image stored in an OCI registry.

We start by creating a test folder where we execute all required steps for this example and navigating into it:

```shell
mkdir /tmp/helloworld
cd /tmp/helloworld
```

Now we download the `podinfo` Helm Chart that we want to use as local resource and extract it:

```shell
helm repo add podinfo https://stefanprodan.github.io/podinfo
helm pull --untar podinfo/podinfo
```

Create a file `component-constructor.yaml`, which describes all elements of the component. You can use our public configuration schema to validate the configuration. The schema is available at `https://ocm.software/schemas/configuration-schema.yaml` and can be used in your editor to validate the configuration (e.g., in Visual Studio Code).

Component versions need to have at least a `name`, `version` and `provider` attribute. All other attributes are optional. Check out an [example component descriptor]({{< relref "docs/tutorials/component-descriptor-example.md" >}}) or the [OCM Specification](https://github.com/open-component-model/ocm-spec/blob/main/README.md) to see all available attributes.

As mentioned before our example component will just contain a Helm Chart and a Docker image as resources:

```yaml
# specify a schema to validate the configuration and get auto-completion in your editor
# yaml-language-server: $schema=https://ocm.software/schemas/configuration-schema.yaml
components:
- name: github.com/acme.org/helloworld
  # version needs to follow "relaxed" SemVer
  version: 1.0.0
  provider:
    name: acme.org
  resources:
    # local Helm chart resource
    - name: mychart
      type: helmChart
      input:
        type: helm
        path: ./podinfo
    # remote image resource
    - name: image
      type: ociImage
      version: 1.0.0
      access:
        type: ociArtifact
        imageReference: gcr.io/google_containers/echoserver:1.10
```

A resource is described either by its access information to a remote repository or by locally provided resources.

For remote access, the field `access` is used to describe the
[access method](https://github.com/open-component-model/ocm-spec/blob/main/doc/04-extensions/02-access-types/README.md).
The type field is used to specify the kind of access.

If the resource content is taken from local resources, the field `input` is used to specify
the access to the local resources. Similarly to the `access` attribute, the kind of the input source is described by the field `type`.

Available access and input types are described [here]({{< relref "docs/tutorials/input-and-access-types.md" >}}).

For more complex scenarios, the description files might use variable substitution (templating), see [Best Practices]({{< relref "docs/tutorials/best-practices.md#templating-the-resources" >}}).

### Add Component Version to CTF archive

To store our component version locally and to make it transportable, we now add it to a CTF archive
using the following command. The option `--create` is used to create a new CTF archive if it does not exist:

```shell
ocm add componentversions --create --file ctf-hello-world component-constructor.yaml
```

```shell
  processing component-constructor.yaml...
    processing document 1...
      processing index 1
  found 1 component
  adding component github.com/acme.org/helloworld:1.0.0...
    adding resource helmChart: "name"="mychart","version"="<componentversion>"...
    adding resource ociArtifact: "name"="image","version"="1.0.0"...
```

<details><summary>What happened?</summary>

The command creates the CTF archive (option `--create`) and adds the listed components
with the described resources.

```shell
  ctf-hello-world/
  ├── artifact-index.json
  └── blobs
      ├── sha256.125cf912d0f67b2b49e4170e684638a05a12f2fcfbdf3571e38a016273620b54
      ├── sha256.1cb2098e31e319df7243490464b48a8af138389abe9522c481ebc27dede4277b
      ├── sha256.974e652250ffaba57b820c462ce603fc1028a608b0fa09caef227f9e0167ce09
      └── sha256.d442bdf33825bace6bf08529b6f00cf0aacc943f3be6130325e1eb4a5dfae3a5
```

The transport archive's contents can be found in `artifact-index.json`. This file
contains the list of component version artifacts to be transported.

```shell
jq . ${CTF_ARCHIVE}/artifact-index.json
```

```json
{
  "schemaVersion": 1,
  "artifacts": [
    {
      "repository": "component-descriptors/github.com/acme/helloworld",
      "tag": "1.0.0",
      "digest": "sha256:d3cf4858f5387eaea194b7e40b7f6eb23460a658ad4005c5745361978897e043"
    }
  ]
}
```

The content of the transport archive is stored as OCI artifacts. Notice that the repository names of Component Version artifacts (found at `artifacts.respository`) are prefixed by `component-descriptors/`.

The component version is described as an OCI manifest:

```shell
jq . ${CTF_ARCHIVE}/blobs/sha256.d3cf4858f5387eaea194b7e40b7f6eb23460a658ad4005c5745361978897e043
```

```json
{
  "schemaVersion": 2,
  "mediaType": "application/vnd.oci.image.manifest.v1+json",
  "config": {
    "mediaType": "application/vnd.ocm.software.component.config.v1+json",
    "digest": "sha256:0dd94de11c17f995648c8e817971581bce4b016f53d4d2bf2fff9fcda37d7b95",
    "size": 201
  },
  "layers": [
    {
      "mediaType": "application/vnd.ocm.software.component-descriptor.v2+yaml+tar",
      "digest": "sha256:4ab29c8acb0c8b002a5037e6d9edf2d657222da76fee2a10f38d65ecd981d0c6",
      "size": 3072
    },
    {
      "mediaType": "application/vnd.oci.image.manifest.v1+tar+gzip",
      "digest": "sha256:b2dc5088f005d27ea39b427c2e67e91e2b6b80d3e85eca2476a019003c402904",
      "size": 16122
    }
  ]
}
```

Notice that the output of the component version above contains the component descriptor as one of the `layers`. It can be identified by its content type, which is `application/vnd.ocm.software.component-descriptor.v2+yaml+tar`. In this case, the component descriptor can be displayed with the following command:

```shell
tar xvf ${CTF_ARCHIVE}/blobs/sha256.4ab29c8acb0c8b002a5037e6d9edf2d657222da76fee2a10f38d65ecd981d0c6 -O - component-descriptor.yaml
```

```yaml
meta:
  schemaVersion: v2
component:
  name: github.com/acme/helloworld
  version: 1.0.0
  provider: acme.org
  componentReferences: []
  repositoryContexts: []
  resources:
  - access:
      localReference: sha256:b2dc5088f005d27ea39b427c2e67e91e2b6b80d3e85eca2476a019003c402904
      mediaType: application/vnd.oci.image.manifest.v1+tar+gzip
      referenceName: github.com/acme/helloworld/podinfo:6.7.0
      type: localBlob
    digest:
      ...
    name: mychart
    relation: local
    type: helmChart
    version: 1.0.0
  - access:
      imageReference: gcr.io/google_containers/echoserver:1.10
      type: ociArtifact
    digest:
      ...
    name: image
    relation: external
    type: ociArtifact
    version: 1.0.0
  sources: []
```

The other elements listed as `layers` describe the blobs for the local resources stored along with the component version. The digests can be seen in the `localReference` attributes of the component descriptor.

</details>
