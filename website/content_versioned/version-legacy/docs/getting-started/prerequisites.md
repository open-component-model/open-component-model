---
title: "Prerequisites"
description: "What you need to get started with OCM"
icon: "⚠️"
weight: 21
toc: true
---

This and the following chapters walk you through some basic steps to get started with OCM concepts and the OCM CLI.
You will learn how to create a component version, display and examine the component, and how to transport and sign it.

To follow the steps described in this section, you will need to:

## Install the OCM Command Line Interface (CLI)

The CLI is used to interact with component versions and registries. Install it like described in [Installing the OCM CLI]({{< relref "docs/getting-started/ocm-cli-installation.md" >}}).

## Obtain Access to an OCM Repository

This can be any OCI registry for which you have write permission (e.g., GitHub Packages). An OCM repository based on an OCI registry is identified by a leading OCI repository prefix. For example: `ghcr.io/<YOUR-ORG>/ocm`.

## Obtain Credentials for the CLI to Access the OCM Repository

### Using the Docker Configuration File

The easiest way to do this is to reuse your Docker configuration `json` file.

To do this, create a file named `.ocmconfig` in your home directory with the following content:

```yaml
type: generic.config.ocm.software/v1
configurations:
- type: credentials.config.ocm.software
  repositories:
    - repository:
        type: DockerConfig/v1
        # The path to the Docker configuration file
        dockerConfigFile: "~/.docker/config.json"
        propagateConsumerIdentity: true
- type: attributes.config.ocm.software
  attributes:
    cache: ~/.ocm/cache
```

### Using Basic Authentication

Alternatively, you can use basic authentication. Create a file named `.ocmconfig` in your home directory with the following content:

```yaml
type: generic.config.ocm.software/v1
configurations:
  - type: credentials.config.ocm.software
    consumers:
      - identity:
          type: ociRegistry
          hostname: <YOUR-REGISTRY>/<YOUR-REPO> # e.g. ghcr.io/acme/acme
        credentials:
          - type: Credentials
            properties:
              username: <YOUR-USERNAME>
              password: <YOUR-PASSWORD>
```

More information on the credentials topic can be seen by running the OCM CLI help topic command `ocm credential-handling`
and [in this guide]({{< relref "docs/tutorials/creds-in-ocmconfig.md" >}}) with many examples for different repository types.
