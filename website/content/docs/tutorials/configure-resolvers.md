---
title: "Working with Resolvers"
description: "Configure resolvers to recursively resolve component references from a shared repository."
icon: "🔍"
weight: 80
toc: true
---

## Overview

When a component has **references** to other components stored in different repositories, the CLI needs to know where to
find them. **Resolvers** map component name patterns to repositories so the CLI can automatically locate referenced
components during recursive operations. For a high-level introduction, see the [Resolvers concept page]({{< relref "docs/concepts/resolvers.md" >}}).
For configuration details and pattern syntax, see the [Resolver Configuration Reference]({{< relref "docs/reference/resolver-configuration.md" >}}).

## What You'll Learn

- Create components with references and push them to OCI registries
- Write a resolver configuration that maps component name patterns to repositories
- Combine credentials and resolvers in a single `.ocmconfig` file
- Use `--recursive` to resolve a component graph across repositories

**Estimated time:** ~20 minutes

## Prerequisites

- The [OCM CLI](https://github.com/open-component-model/open-component-model) installed
- Access to at least one OCI registry (e.g., `ghcr.io`, Docker Hub, or a private registry)
- A GitHub account with a personal access token

## Scenario

This tutorial walks through a hands-on example with three components — a **backend**, a **frontend**, and an **app**
that references both. The app lives in its own repository, while its component references (backend and frontend
components) are stored in a shared repository. When you recursively resolve the app, the CLI needs resolvers to locate the referenced
components in their repositories.

{{< steps >}}

{{< step >}}
**Set up your environment**

Before starting, set an environment variable for your GitHub username to simplify command inputs, and create a working directory:

```bash
export GITHUB_USERNAME=<your-github-username>
mkdir /tmp/ocm-resolver-tutorial && cd /tmp/ocm-resolver-tutorial
```

The environment variable will be used in repository paths throughout the tutorial.

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
If you are re-running this tutorial and the component versions already exist, add
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
          type: utf8
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
          type: utf8
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
          type: utf8
          text: "app deployment configuration"
```

{{< /step >}}

{{<callout context="note">}}
⚠️ Do not call `add cv` yet — we need to set up resolvers first so the CLI can find the referenced components during
recursive resolution.
If you'd try to push your app-component now, the CLI would reject it because it can't find the referenced backend and
frontend components in the app repository.
{{</callout>}}

{{< step >}}

**Recursively Resolve the App with Resolvers**

Before we can push the app-component that references the backend and frontend components,
we need to set up resolvers so the CLI can find the referenced components during recursive resolution.
Update your `.ocmconfig` file with resolvers that map the component references to their repository:

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

{{< /step >}}

{{< step >}}
**Push the App Component**

After declaring the resolvers pointing to backend and frontend, you are able to push the **app component**
to the **app repository**:

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

{{< /step >}}

{{< step >}}
**Verify the Components**

Check that all three components exist in their respective repositories:

```bash
ocm get cv ghcr.io/$GITHUB_USERNAME/ocm-resolver-tutorial-deps//ocm.software/tutorials/backend:1.0.0 --config .ocmconfig
ocm get cv ghcr.io/$GITHUB_USERNAME/ocm-resolver-tutorial-deps//ocm.software/tutorials/frontend:1.0.0 --config .ocmconfig
ocm get cv ghcr.io/$GITHUB_USERNAME/ocm-resolver-tutorial//ocm.software/tutorials/app:1.0.0 --config .ocmconfig
```

The outputs of each command should show the respective component version with its provider.
{{< /step >}}

{{< /steps >}}

{{<callout context="tip" title="Resolving from Multiple Repositories">}}
In the tutorial above, both component references share a single repository. In practice, components often live in
**separate repositories**.

See the how-to guide:
[How to Resolve Components Across Multiple Registries]({{< relref "docs/how-to/resolve-components-from-multiple-repositories.md" >}})
for a guide on how to configure resolvers with multiple repositories.
{{</callout>}}

## What you've learned

- How to push components with references to separate repositories
- How to configure resolvers in an `.ocmconfig` file
- How to use `--recursive` to resolve a component graph across repositories

## Check your understanding

1. Why do you need resolvers when working with component references?
2. What file do you configure to map component name patterns to repositories?
3. How do you enable recursive resolution with the OCM CLI?

{{< details "Answers">}}

1. Resolvers tell the CLI where to find referenced components that are stored in different repositories.
2. The `.ocmconfig` file contains resolver configurations.
3. Use the `--recursive` flag with commands like `ocm get cv`.

{{< /details >}}

## Cleanup

Remove the working directory created during this tutorial:

```bash
rm -rf /tmp/ocm-resolver-tutorial
```

## What Comes Next

Now that you know how to configure resolvers, you can:

- Learn more about component
  references: [Referencing Components](https://github.com/open-component-model/ocm-spec/blob/main/doc/02-processing/01-references.md).
- Explore credential configuration: See [Understand Credential Resolution]({{< relref "docs/tutorials/credential-resolution.md" >}}) for
  authentication options when working with registries.
- Set up air-gapped environments: Use CTF archives with resolvers for offline component resolution. Learn about the
  Common Transport Format in [Creating a Component Version]({{< relref "docs/getting-started/create-component-version.md" >}}).

## Related Documentation

- [Resolvers]({{< relref "docs/concepts/resolvers.md" >}}) — High-level introduction to resolvers
- [Resolver Configuration Reference]({{< relref "docs/reference/resolver-configuration.md" >}}) — Full configuration
  schema, repository types, and pattern syntax
- [Component Identity]({{< relref "docs/concepts/component-identity.md" >}}) — Core concepts behind component versions, identities, and
  references
- [Understand Credential Resolution]({{< relref "docs/tutorials/credential-resolution.md" >}}) — Configure credentials for OCI
  registries, Helm repositories, and more
- [Creating a Component Version]({{< relref "docs/getting-started/create-component-version.md" >}}) — Build component
  versions and work with CTF archives
- [Input and Access Types]({{< relref "docs/reference/input-and-access-types.md" >}}) — Reference for resource input types (by value)
  and access types (by reference)
- [Signing and Verification]({{< relref "docs/tutorials/signing/plain.md" >}}) — Sign and verify component versions with
  cryptographic keys
- [Migrate from Deprecated Resolvers]({{< relref "docs/how-to/migrate-from-deprecated-resolvers.md" >}}) — Replace deprecated fallback
  resolvers with glob-based resolvers
