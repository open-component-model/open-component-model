---
title: "Credential System"
description: "Why OCM manages credentials centrally and how its resolution model works."
icon: "🔑"
weight: 4
toc: true
---

OCM operations frequently interact with protected services — OCI registries, private repositories, signing infrastructure. Rather than requiring credentials at every command invocation, OCM provides a central credential system that decouples *what needs authentication* from *how credentials are supplied*.

This separation matters because the same component version may be stored in different registries across environments (development, staging, production), each with its own authentication scheme. A central credential system lets you configure credentials once and have OCM resolve the right ones automatically, regardless of where an operation runs.

## Design Approach

OCM's credential system is built around three ideas:

### Consumer-Based Matching

Every service that requires authentication is modeled as a **consumer** — described by a set of identity attributes such as a type (e.g., `OCIRegistry`), a hostname, and optionally a path or port. When an OCM operation needs to authenticate, it constructs a consumer identity from the target it is accessing and asks the credential system for a match.

This design means credentials are tied to *what you are accessing*, not to *which command you are running*. A push and a pull to the same registry use the same consumer entry.

```mermaid
flowchart TB
    subgraph consumer ["Consumer"]
        direction LR
        subgraph identities ["Identities"]
            id1["Identity 1<br>type: OCIRegistry<br>hostname: ghcr.io<br>path: my-org"]
            id2["Identity 2<br>type: OCIRegistry<br>hostname: ghcr.io<br>path: my-org/my-repo"]
        end
        subgraph credentials ["Credentials"]
            cred["type: OCICredentials/v1<br>username / password"]
        end
        identities --> credentials
    end
```

### Separation of Identity and Credentials

Consumer identities and credentials are configured independently. One set of credentials can serve multiple identities (e.g., a single token valid for several repositories on the same registry), and the same identity can be satisfied by different credential types depending on the environment.

This keeps configuration DRY and makes it straightforward to rotate credentials without touching identity definitions.

### Repositories as Fallback

Not all credential sources are static key-value pairs. Docker credential helpers, cloud IAM token endpoints, and external secret managers provide credentials dynamically. OCM models these as **repositories** — fallback sources consulted only when no direct consumer match is found. This lets you reuse existing credential infrastructure (e.g., a Docker `config.json`) without duplicating secrets into OCM's own configuration.

## Terminology

- **Consumer** — a service that requires authentication (e.g., an OCI registry)
- **Consumer Identity** — a set of key-value attributes that uniquely describe a consumer (type + attributes like `hostname`, `path`)
- **Credentials** — typed objects used to authenticate; stored natively in the credential graph
- **Credential Type** — defines how credentials are structured (e.g., `OCICredentials/v1` for OCI username/password/token, `DirectCredentials/v1` for a generic property map)
- **`DirectCredentials/v1`** — the universal legacy fallback (also accepted as `Credentials/v1`); stores credentials as an untyped `properties:` map; all configs from previous versions remain valid
- **Repository** — a fallback credential source checked only when no consumer entry matches (e.g., `DockerConfig/v1`)

## How Resolution Works

When an OCM operation needs credentials, the system follows a three-path precedence:

1. **Direct** — the consumer entry holds credentials inline (either a typed credential like `OCICredentials/v1` or a legacy `DirectCredentials/v1`). These are stored as leaf nodes in the credential graph and returned immediately on match.
2. **Indirect** — the credential entry refers to a plugin-backed source (e.g., `HashiCorpVault/v1alpha1`). At ingestion time the graph creates a DAG edge; at resolution time the plugin is called to fetch the actual credential. Chains of arbitrary depth are supported — credentials for one service can come from Vault, whose own credentials come from a direct entry.
3. **Repository fallback** — consulted only when neither path above yields a result. All configured repository plugins (e.g., `DockerConfig/v1`) are queried concurrently; the first success wins.

```mermaid
flowchart TB
    op(["OCM Operation"])
    op --> consumers

    subgraph consumers ["Consumers (checked first)"]
        direction TB
        id1["Identity<br>hostname: ghcr.io"] --> cred1["Direct credentials<br>OCICredentials/v1<br>username / password"]
        id2["Identity<br>hostname: quay.io"] --> cred2["Indirect credentials<br>HashiCorpVault/v1alpha1"] --> vault["Vault plugin<br>resolves at query time"]
    end

    op --> repos

    subgraph repos ["Repositories (fallback)"]
        direction TB
        docker["DockerConfig/v1<br>~/.docker/config.json"]
    end
```

{{< callout context="note" >}}
To see resolution in action, try the [Understand Credential Resolution]({{< relref "/docs/tutorials/credential-resolution.md" >}}) tutorial.
{{< /callout >}}

## Typed vs. Legacy Credentials

OCM supports both **typed** credential types and the legacy **`DirectCredentials/v1`** fallback.

**Typed credential types** (e.g., `OCICredentials/v1`, `HelmHTTPCredentials/v1`, `RSACredentials/v1`) use flat, named fields and are validated at configuration parse time:

```yaml
credentials:
  - type: OCICredentials/v1
    username: my-user
    password: my-password
```

**`DirectCredentials/v1`** stores credentials as an untyped `properties:` map. All existing `.ocmconfig` files using `Credentials/v1` continue to work unchanged — `Credentials/v1` is an alias for `DirectCredentials/v1`:

```yaml
credentials:
  - type: Credentials/v1
    properties:
      username: my-user
      password: my-password
```

For the full list of built-in typed credential types and their fields, see [Reference: Credential Types]({{< relref "/docs/reference/credential-types.md" >}}).

## What's Next?

- [Tutorial: Credential Resolution]({{< relref "/docs/tutorials/credential-resolution.md" >}}) — Learn how OCM picks the right credentials by experimenting with a config
- [How-To: Configure Credentials for Multiple Registries]({{< relref "/docs/how-to/configure-multiple-credentials.md" >}}) — Quick task-oriented setup
- [Tutorial: Credentials for OCM Controllers]({{< relref "/docs/how-to/configure-credentials-ocm-controllers.md" >}}) — How to provide credentials in Kubernetes environments

## Related Documentation

- [Reference: Consumer Identities]({{< relref "/docs/reference/credential-consumer-identities.md" >}}) — Complete list of identity types, attributes, and credential properties
- [Reference: Credential Types]({{< relref "/docs/reference/credential-types.md" >}}) — All built-in typed credential types and their fields
