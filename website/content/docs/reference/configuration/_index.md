---
title: "Configuration"
description: "Reference for the configuration types in the OCM configuration file (.ocmconfig): credentials, resolvers, transfer, HTTP, caching, and versioning."
icon: "⚙️"
weight: 6
toc: true
sidebar:
  collapsed: true
---

OCM reads its settings from a single configuration file, by default
`$HOME/.ocmconfig`. The file carries a central
`generic.config.ocm.software/v1` document whose `configurations:` list holds one
or more typed configuration entries. Each page in this section is the technical
reference for one of those configuration types: its schema, fields, defaults,
and precedence rules.

```yaml
type: generic.config.ocm.software/v1
configurations:
  - type: credentials.config.ocm.software/v1
    # ...
  - type: ocm.config.ocm.software/v1
    # ...
  - type: transfer.config.ocm.software/v1alpha1
    # ...
```

## Configuration references

| Reference | Covers |
| --- | --- |
| [Caching Configuration]({{< relref "caching.md" >}}) | OCI caches, cache location, limits, admission rules, and remote policy. |
| [Credential Consumer Identities]({{< relref "credential-consumer-identities.md" >}}) | Identity types and attributes OCM matches credentials against. |
| [Credential Types]({{< relref "credential-types.md" >}}) | Built-in typed credential types and their fields. |
| [HTTP Checksum Configuration]({{< relref "http-checksum.md" >}}) | Schema, checksum modes, precedence, and the access-side fast path. |
| [HTTP Client Configuration]({{< relref "http-client.md" >}}) | Timeouts, retries, TLS trust, and per-host merge semantics. |
| [Resolver Configuration]({{< relref "resolver.md" >}}) | Repository resolution, component name patterns, and version constraints. |
| [Transfer Configuration]({{< relref "transfer-configuration/_index.md" >}}) | Transfer settings, uploader configurations, resource matching, and CEL expressions. |
| [Versioning Configuration]({{< relref "versioning.md" >}}) | Scheme catalog, custom schemes, and OCI tag constraints. |
