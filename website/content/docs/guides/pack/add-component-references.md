---
title: "Add Component References"
description: "Declare componentReferences in a constructor and push the referenced components, so an app component can depend on backend and frontend components."
weight: 40
toc: true
aliases:
  - /docs/tutorials/configure-resolvers/
  - /docs/how-to/resolve-components-from-multiple-repositories/
---

## Overview

A component can declare **references** to other components with the `componentReferences` field in its constructor.
This lets you compose a product out of independently versioned components — for example an **app** that depends on a
**backend** and a **frontend**. This guide covers the authoring side: creating and pushing the referenced components,
then declaring the references in the app's constructor.

The referenced components can live in the **same repository** as the app or in **separate repositories**. This guide
starts with both references in one shared repository, then points you at the cross-repository case. When the referenced
components live elsewhere, the CLI needs **resolvers** to locate them during a recursive push. This guide covers
both: first the authoring side, then configuring resolvers so the CLI can resolve the references.

For a high-level introduction to references, see the [Resolvers concept page]({{< relref "docs/concepts/resolvers.md" >}}).
To understand **why** component references don't include repository specifications and how resolvers fit into OCM's
location-independent design, see [Canonical Component Repositories]({{< relref "docs/concepts/canonical-components.md" >}}).

## What You'll Learn

- Create components and push them to OCI registries
- Declare `componentReferences` from one component to others
- Prepare the shared-repository and separate-repository cases

**Estimated time:** ~15 minutes

## Prerequisites

- The [OCM CLI](https://github.com/open-component-model/open-component-model) installed
- Access to at least one OCI registry (e.g., `ghcr.io`, Docker Hub, or a private registry)
- A GitHub account with a personal access token

## Scenario

This guide walks through a hands-on example with three components — a **backend**, a **frontend**, and an **app**
that references both. The app lives in its own repository, while its component references (backend and frontend
components) are stored in a shared repository.

{{< steps >}}

{{< step >}}
**Set up your environment**

Before starting, set an environment variable for your GitHub username to simplify command inputs, and create a working directory:

```bash
export GITHUB_USERNAME=<your-github-username>
mkdir /tmp/ocm-resolver-tutorial && cd /tmp/ocm-resolver-tutorial
```

The environment variable will be used in repository paths throughout the guide.

{{< /step >}}

{{<callout context="note">}}
For more information on creating a personal access token,
see [GitHub's documentation](https://docs.github.com/en/authentication/keeping-your-account-and-data-secure/creating-a-personal-access-token).

⚠️ The token must have the `write:packages` scope to allow pushing component versions
to [GitHub Container Registry](https://ghcr.io).
{{</callout>}}

{{< step >}}
**Authenticate with the Registry**

Log in to `ghcr.io` using a GitHub personal access token:

```bash
export GITHUB_TOKEN=<your-github-token>
echo $GITHUB_TOKEN | docker login ghcr.io -u $GITHUB_USERNAME --password-stdin
```

Your token needs to have write permissions for packages in order to push component versions to the registry.
{{< /step >}}

{{< step >}}
**Create the .ocmconfig**

Create an `.ocmconfig` next to the `constructor.yaml` files in the directory you will be working in with credentials for `ghcr.io`.
This file will be used in subsequent steps when pushing components and configuring resolvers:

```yaml
type: generic.config.ocm.software/v1
configurations:
  - type: credentials.config.ocm.software
    repositories:
      - repository:
          type: DockerConfig/v1
          dockerConfigFile: "~/.docker/config.json"
```

For more information about the OCM configuration file,
see [.ocmconfig documentation](https://github.com/open-component-model/ocm/blob/main/docs/reference/ocm_configfile.md).
{{< /step >}}

{{<callout context="tip">}}
If you are re-running this guide and the component versions already exist, add
`--component-version-conflict-policy replace` to the `ocm add cv` commands to overwrite existing versions.
{{</callout>}}

{{< step >}}
**Create and Push the Backend Component**

Create `backend-constructor.yaml`:

```yaml
components:
  - name: ocm.software/tutorials/backend
    version: "1.0.0"
    provider:
      name: ocm.software
    resources:
      - name: config
        version: "1.0.0"
        type: plainText
        input:
          type: UTF8/v1
          text: "backend service configuration"
```

Push it to the repository to store the component in. We will reference this later when we create the app component:

```bash
ocm add cv --repository ghcr.io/$GITHUB_USERNAME/ocm-resolver-tutorial-deps \
  --constructor backend-constructor.yaml --config .ocmconfig
```

<details>
  <summary>Expected output</summary>

```text
 COMPONENT                      │ VERSION │ PROVIDER
────────────────────────────────┼─────────┼──────────────
 ocm.software/tutorials/backend │ 1.0.0   │ ocm.software
```

</details>
{{< /step >}}

{{< step >}}
**Create and Push the Frontend Component**

Create `frontend-constructor.yaml`:

```yaml
components:
  - name: ocm.software/tutorials/frontend
    version: "1.0.0"
    provider:
      name: ocm.software
    resources:
      - name: config
        version: "1.0.0"
        type: plainText
        input:
          type: UTF8/v1
          text: "frontend service configuration"
```

Push it to the same repository:

```bash
ocm add cv --repository ghcr.io/$GITHUB_USERNAME/ocm-resolver-tutorial-deps \
  --constructor frontend-constructor.yaml --config .ocmconfig
```

<details>
  <summary>Expected output</summary>

```text
 COMPONENT                       │ VERSION │ PROVIDER
─────────────────────────────────┼─────────┼──────────────
 ocm.software/tutorials/frontend │ 1.0.0   │ ocm.software
```

</details>
{{< /step >}}

{{< step >}}
**Create the App Component**

Create `app-constructor.yaml`. Notice the `componentReferences` section — it declares dependencies on both the backend
and frontend components:

```yaml
components:
  - name: ocm.software/tutorials/app
    version: "1.0.0"
    provider:
      name: ocm.software
    componentReferences:
      - name: backend-service
        componentName: ocm.software/tutorials/backend
        version: "1.0.0"
      - name: frontend-service
        componentName: ocm.software/tutorials/frontend
        version: "1.0.0"
    resources:
      - name: config
        version: "1.0.0"
        type: plainText
        input:
          type: UTF8/v1
          text: "app deployment configuration"
```

{{< /step >}}

{{< /steps >}}

{{< callout context="caution" title="Set up resolvers before pushing the app" >}}
Do not run `ocm add cv` on the app component yet. The CLI would reject it, because it cannot find the referenced
backend and frontend components in the app repository until you configure **resolvers**. Keep this terminal and the
`/tmp/ocm-resolver-tutorial` workspace open and continue with the resolver setup below.
{{< /callout >}}

## Configure resolvers

Configure an `.ocmconfig` file with resolver entries so the OCM CLI can recursively
resolve component references, whether the referenced components share a single
repository or are distributed across several.

When a component has **references** to other components stored in different
repositories, the CLI needs to know where to find them. **Resolvers** map component
name patterns to repositories so the CLI can automatically locate referenced
components during recursive operations.

## Resolve references from a shared repository

When all referenced components live in one repository, add a resolver entry per
component (or a single glob entry that covers them). Combine credentials and
resolvers in the same `.ocmconfig`:

```bash
cat <<EOF > .ocmconfig
type: generic.config.ocm.software/v1
configurations:
  - type: credentials.config.ocm.software
    repositories:
      - repository:
          type: DockerConfig/v1
          dockerConfigFile: "~/.docker/config.json"
  - type: resolvers.config.ocm.software/v1alpha1
    resolvers:
      - repository:
          type: OCIRepository/v1
          baseUrl: ghcr.io
          subPath: $GITHUB_USERNAME/ocm-resolver-tutorial-deps
        componentNamePattern: "ocm.software/tutorials/frontend"
      - repository:
          type: OCIRepository/v1
          baseUrl: ghcr.io
          subPath: $GITHUB_USERNAME/ocm-resolver-tutorial-deps
        componentNamePattern: "ocm.software/tutorials/backend"
EOF
```

With the resolvers declared, you can push the referencing component into its own
repository — the CLI follows the references and resolves them from the shared
repository:

```bash
ocm add cv --repository ghcr.io/$GITHUB_USERNAME/ocm-resolver-tutorial \
  --constructor app-constructor.yaml --config .ocmconfig
```

<details>
  <summary>Expected output</summary>

```text
  COMPONENT                       │ VERSION │ PROVIDER
─────────────────────────────────┼─────────┼──────────────
 ocm.software/tutorials/app      │ 1.0.0   │ ocm.software
 ocm.software/tutorials/backend  │ 1.0.0   │
 ocm.software/tutorials/frontend │ 1.0.0   │
```

</details>

The CLI:

1. Finds `ocm.software/tutorials/app:1.0.0` in the specified app repository
2. Discovers the references to `ocm.software/tutorials/backend:1.0.0` and `ocm.software/tutorials/frontend:1.0.0`
3. Consults the resolvers — each component name matches a configured pattern
4. Looks up backend and frontend in `ghcr.io/$GITHUB_USERNAME/ocm-resolver-tutorial-deps`

Verify that all three components resolve:

```bash
ocm get cv ghcr.io/$GITHUB_USERNAME/ocm-resolver-tutorial-deps//ocm.software/tutorials/backend:1.0.0 --config .ocmconfig
ocm get cv ghcr.io/$GITHUB_USERNAME/ocm-resolver-tutorial-deps//ocm.software/tutorials/frontend:1.0.0 --config .ocmconfig
ocm get cv ghcr.io/$GITHUB_USERNAME/ocm-resolver-tutorial//ocm.software/tutorials/app:1.0.0 --config .ocmconfig
```

Each command shows the respective component version with its provider.

## Resolve references across multiple repositories

In practice, components often live in **separate repositories**. Add a resolver
entry for each registry where referenced components are stored.

{{< steps >}}

{{< step >}}
**Create an `.ocmconfig` with resolver entries**

Assume a root component `<root-component>` that references `<component-a>` and
`<component-b>`, each stored in a different repository. Replace the placeholder
values with your own registry, paths, and component names:

```yaml
type: generic.config.ocm.software/v1
configurations:
  - type: credentials.config.ocm.software
    repositories:
      - repository:
          type: DockerConfig/v1
          dockerConfigFile: "~/.docker/config.json"
  - type: resolvers.config.ocm.software/v1alpha1
    resolvers:
      - repository:
          type: OCIRepository/v1
          baseUrl: <registry>                 # e.g. ghcr.io
          subPath: <subpath-a>                # e.g. my-org/team-a
        componentNamePattern: "<component-a>" # e.g. my-org.example/component-a
      - repository:
          type: OCIRepository/v1
          baseUrl: <registry>                 # e.g. ghcr.io
          subPath: <subpath-b>                # e.g. my-org/team-b
        componentNamePattern: "<component-b>" # e.g. my-org.example/component-b
```

{{< /step >}}

{{< step >}}
**Resolve the root component recursively**

Point the CLI at the root component and pass your config file:

```bash
ocm add cv --repository <registry>/<root-subpath> \
  --constructor <constructor>.yaml \
  --config .ocmconfig
```

or

```bash
ocm get cv <registry>/<root-subpath>//<root-component>:<version> \
  --recursive=-1 --config .ocmconfig
```

`add cv` uploads a component version to the registry using the resolver config to locate referenced components.
`get cv --recursive` walks the full component graph and lists all transitively referenced components.

<details>
<summary>Example</summary>

```bash
ocm add cv --repository ghcr.io/my-org/components \
  --constructor root-constructor.yaml \
  --config .ocmconfig
```

```bash
ocm get cv ghcr.io/my-org/components//my-org.example/root-component:1.0.0 \
  --recursive=-1 --config .ocmconfig
```

</details>

{{< /step >}}

{{< step >}}
**Verify the output**

The output should list the root component and all transitively referenced components. If a referenced component is
missing, check that there is a matching resolver entry for it.

<details>
<summary>Example output</summary>

```text
COMPONENT                          │ VERSION │ PROVIDER
───────────────────────────────────┼─────────┼──────────
my-org.example/root-component      │ 1.0.0   │ my-org
my-org.example/component-a         │ 1.0.0   │
my-org.example/component-b         │ 1.0.0   │
```

</details>

{{< /step >}}

{{< /steps >}}

## Tips

- **If multiple components share a registry path**, use a glob pattern (e.g. `example.com/services/*`) instead of
  listing each component individually. See
  [Component Name Patterns]({{< relref "docs/reference/resolver-configuration.md#component-name-patterns" >}}) for the
  full syntax.
- **If different versions of the same component live in different registries**, use the `versionConstraint` field to
  route specific version ranges to the right repository. See
  [Version Constraints]({{< relref "docs/reference/resolver-configuration.md#version-constraints" >}}) for details.
- **If you need to transfer components to another registry**, use `ocm transfer cv --recursive` with a local blob uploader configuration (for example in `.ocmconfig` in the working directory) and
  a resolver configuration. See [Transfer Component Versions]({{< relref "docs/guides/transfer/transfer-component-versions.md" >}}).
- **Resolvers are evaluated in order** — place more specific patterns before broader ones so the right repository is
  matched first. See [Resolvers]({{< relref "docs/concepts/resolvers.md" >}}) for how first-match ordering decides the outcome.

## Next Steps

- [Migrate Legacy Resolvers]({{< relref "docs/guides/migrate/migrate-legacy-resolvers.md" >}}) — Replace deprecated
  fallback resolvers with glob-based resolvers

## Related Documentation

- [Resolvers]({{< relref "docs/concepts/resolvers.md" >}}) — High-level introduction to resolvers
- [Resolver Configuration Reference]({{< relref "docs/reference/resolver-configuration.md" >}}) — Full configuration
  schema, repository types, and pattern syntax
- [Component Identity]({{< relref "docs/concepts/component-identity.md" >}}) — Core concepts behind component versions, identities, and
  references
- [Creating a Component Version]({{< relref "docs/getting-started/create-component-version.md" >}}) — Build component
  versions and work with CTF archives
- [Input and Access Types]({{< relref "docs/reference/input-and-access-types.md" >}}) — Reference for resource input types (by value)
  and access types (by reference)
- Component
  references in the spec: [Referencing Components](https://github.com/open-component-model/ocm-spec/blob/main/doc/02-processing/01-references.md)
