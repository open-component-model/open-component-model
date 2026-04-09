---
title: "How to use the OCM CLI container image"
description: "Use the OCM CLI container image to create and get a component version."
icon: "📦"
weight: 13
toc: true
---

## Goal

This How-To will show you how to use the OCM CLI container image to create and get a component version.

## You'll end up with

- A component version created in an OCM repository using the OCM CLI container image.

**Estimated time:** ~5 minutes

## Prerequisites

- [OCM CLI]({{< relref "docs/getting-started/ocm-cli-installation.md" >}}) installed.
- Access to an OCI registry. We will use GitHub Container Registry (GHCR) in this example.
- Required credentials/keys available.
- Docker CLI installed ([Docker Engine](https://docs.docker.com/engine/install/)/[Docker Desktop](https://docs.docker.com/desktop/)).
- A container image CLI installed, e.g. [Docker](https://docs.docker.com/engine/install/),
  [Podman](https://podman.io/docs/installation), or [nerdctl](https://github.com/containerd/nerdctl).
- Internet access to pull the OCM CLI container image and the application image used in the example.

## Steps

{{< steps >}}
{{< step >}} 

### Create a simple component constructor

We will start by creating a simple component constructor. This will be used to create a component version in the 
next step.

```bash
cat > component-constructor.yaml << EOF
components:
- name: ocm.software/how-to-container-image-usage
  version: 1.0.0
  provider:
    name: test.domain
  resources:
   - name: image
     type: ociImage
     version: 1.0.0
     access:
       type: ociArtifact
       imageReference: ghcr.io/stefanprodan/podinfo:6.7.1
EOF
```

{{< /step >}}
{{< step >}}

### Configure credentials in an OCM config

We want to create the component version in a repository that requires authentication. In the following example, we
will use GitHub Container Registry (GHCR) as the repository and use a Personal Access Token (PAT) for authentication.

```bash
cat > .ocmconfig << EOF
type: generic.config.ocm.software/v1
configurations:
- type: credentials.config.ocm.software
  consumers:
   - identity:
       type: OCIRegistry
       hostname: ghcr.io
       port: "443"
       scheme: https
     credentials:
      - type: Credentials
        properties:
          username: <user>
          password: <PAT>
EOF
```
<br>

{{< callout title="Note" icon="outline/info-circle" >}}
If you do not use `ghcr.io` as your registry, make sure to update the `identity` fields in the configuration accordingly.
{{< /callout >}}

{{< /step >}}
{{< step >}}

### Create the component version using the OCM CLI container image

Now we can use the OCM CLI container image to create the component version in the repository. We will mount the current
directory containing the `component-constructor` and `.ocmconfig` to the container image and run the `add cv` command.

```bash
docker run --rm \
  -v "$(pwd)":/workspace:ro \
  -w /workspace \
  ghcr.io/open-component-model/cli:latest \
  add cv \
  --constructor component-constructor.yaml \
  --repository ghcr.io/<user> \
  --config ".ocmconfig"
```

<details>
  <summary>Expected output</summary>

```text
 COMPONENT                                 │ VERSION │ PROVIDER
───────────────────────────────────────────┼─────────┼─────────────
 ocm.software/how-to-container-image-usage │ 1.0.0   │ test.domain
```
</details>
<br>

{{< callout title="Note" icon="outline/info-circle" >}}
You can also mount certificates to the container image by adding `-v /etc/ssl/certs/:/etc/ssl/certs/:ro`
to your command (Depending on your OS, the path to the certificates may be different).
{{< /callout >}}

{{< /step >}}
{{< step >}}

### Get the component version using the OCM CLI container image

After we created the component version, we can use the OCM CLI container image to get the component version from the
repository.

```bash
docker run --rm \
  -v "$(pwd)":/workspace:ro \
  -w /workspace \
  ghcr.io/open-component-model/cli:latest \
  get cv \
  ghcr.io/<user>//ocm.software/how-to-container-image-usage:1.0.0 -oyaml \
  --config ".ocmconfig"
```

<details>
  <summary>Expected output</summary>

```text
- component:
    componentReferences: null
    name: ocm.software/how-to-container-image-usage
    provider: test.domain
    repositoryContexts: null
    resources:
    - access:
        imageReference: ghcr.io/stefanprodan/podinfo:6.7.1@sha256:862ca45e61b32392f7941a1bdfdbe5ff8b6899070135f1bdca1c287d0057fc94
        type: ociArtifact
      digest:
        hashAlgorithm: SHA-256
        normalisationAlgorithm: genericBlobDigest/v1
        value: 862ca45e61b32392f7941a1bdfdbe5ff8b6899070135f1bdca1c287d0057fc94
      name: image
      relation: external
      type: ociImage
      version: 1.0.0
    sources: null
    version: 1.0.0
  meta:
    schemaVersion: v2
```
</details>

{{< /step >}}
{{< /steps >}}

## Getting help

- [Community Support]({{< relref "community/community.md" >}})
- [Open an Issue](https://github.com/open-component-model/open-component-model/issues)
