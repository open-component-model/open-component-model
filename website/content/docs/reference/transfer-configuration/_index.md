---
title: "Transfer Configuration"
description: "Reference for OCM transfer configuration: transfer settings, uploader configurations and resource matching."
icon: "🚚"
weight: 7
toc: true
sidebar:
  collapsed: true
---

This page is the technical reference for OCM transfer configuration. For a
task-oriented walkthrough of routing resources to a custom upload target, see the
[Configure Custom Uploads During Transfer]({{< relref "docs/tutorials/configure-custom-uploads.md" >}})
tutorial. For the conceptual model, see
[Transfer and Transport]({{< relref "docs/concepts/transfer-concept.md" >}}).

## Configuration Types

Transfer behaviour is controlled by configuration types embedded in the
standard OCM configuration file. They are carried as entries inside the central
`generic.config.ocm.software/v1` configuration and may appear together:

```yaml
type: generic.config.ocm.software/v1
configurations:
  - type: transfer.config.ocm.software/v1alpha1
    copyMode: allResources
    uploadType: ociArtifact
  - type: http.uploader.transfer.config.ocm.software/v1alpha1
    match:
      accessType: Wget/v1
    targetURL: '${"https://mytarget.registry.com/uploads" + url(resource.access.url).path}'
    method: PUT
```

| Type                                                         | Purpose                                                                                                  | Reference                                                                                                    |
|--------------------------------------------------------------|----------------------------------------------------------------------------------------------------------|--------------------------------------------------------------------------------------------------------------|
| `transfer.config.ocm.software/v1alpha1`                      | Global transfer settings: recursion, which resources are copied and how.                                 | [Transfer Settings]({{< relref "docs/reference/transfer-configuration/transfer-settings.md" >}})             |
| `http.uploader.transfer.config.ocm.software/v1alpha1`        | Per-match rule that streams a resource to a custom HTTP target.                                          | [HTTP Uploader]({{< relref "docs/reference/transfer-configuration/http-uploader.md" >}})                     |
| `artifactory.uploader.transfer.config.ocm.software/v1alpha1` | Per-match rule that uploads a resource into a JFrog Artifactory helm, generic, maven or npm repository.  | [JFrog Artifactory Uploader]({{< relref "docs/reference/transfer-configuration/artifactory-uploader.md" >}}) |
| `nexus.uploader.transfer.config.ocm.software/v1alpha1`       | Per-match rule that uploads a resource into a Sonatype Nexus hosted helm, raw, maven2 or npm repository. | [Sonatype Nexus Uploader]({{< relref "docs/reference/transfer-configuration/nexus-uploader.md" >}})          |

By default the CLI looks for configuration in `$HOME/.ocmconfig`. Pass
`--config <file>` to use a different file. The corresponding CLI flags
(`--recursive`, `--copy-resources`, `--upload-as`) override the transfer config
when set.

## Uploader Configurations

An **uploader configuration** streams resources that match a rule to a custom
target instead of the default download-and-embed path. The config type dedicates
each uploader to a specific target:
`http.uploader.transfer.config.ocm.software/v1alpha1` streams to an HTTP endpoint;
`artifactory.uploader.transfer.config.ocm.software/v1alpha1` uploads into a JFrog
Artifactory local repository; `nexus.uploader.transfer.config.ocm.software/v1alpha1`
uploads into a Sonatype Nexus hosted repository. Each entry is an independent
rule; you may declare several.

During transfer, the **first** uploader whose `match` applies to a resource wins,
and it takes precedence over `copyMode`/`uploadType` for that resource. Because
matching is first-match, declare more specific rules before broader ones.

`match.accessType` is compared with the resource access in the **source**
component version, not with the access the resource would get in the target. A
resource added with an `ociArtifact` access matches `OCIImage/v1`, `ociArtifact`
or any other alias of that type; it does not match `LocalBlob/v1`, even if a
plain transfer would store it as a local blob. The transfer logs a warning for
every uploader that matched no resource.

## Routing Resources to Different Targets

Because a rule can match on identity as well as access type, several resources of
the **same** access type can be routed to **different** targets. List the specific
rules first; a final rule without `name`/`version`/`extraIdentity` acts as a catch-all:

```yaml
configurations:
  - type: transfer.config.ocm.software/v1alpha1
    copyMode: allResources
  # Docs go to the docs bucket.
  - type: http.uploader.transfer.config.ocm.software/v1alpha1
    match:
      accessType: Wget/v1
      name: docs
    targetURL: '${"https://docs.example.com" + url(resource.access.url).path}'
  # Everything else Wget goes to the generic bucket.
  - type: http.uploader.transfer.config.ocm.software/v1alpha1
    match:
      accessType: Wget/v1
    targetURL: '${"https://blobs.example.com/" + resource.name + "/" + resource.version}'
```

The `targetURL` and `header` values of the HTTP uploader and the `path` of the vendor uploaders are
[CEL Expressions]({{< relref "docs/reference/transfer-configuration/cel-expressions.md" >}}).

## Notes

### Precedence

For a given resource, an uploader match takes precedence over the default handlers
and runs regardless of `copyMode`. A resource with no matching uploader follows the
normal `copyMode`/`uploadType` behaviour.

### Deterministic Plans

Transfer produces a deterministic transformation plan: components are processed in
sorted order and transformation identifiers are derived from stable hashes. The
plan is rendered with human-readable labels such as
`my-app@1.0.0 [Stream icons to mytarget.registry.com]`.

## Related Documentation

- [Configure Custom Uploads During Transfer]({{< relref "docs/tutorials/configure-custom-uploads.md" >}}) — tutorial that walks through an uploader end to end
- [Transfer and Transport]({{< relref "docs/concepts/transfer-concept.md" >}}) — the conceptual transfer model
- [Working with HTTP Resources]({{< relref "docs/tutorials/wget-http-resources.md" >}}) — the `Wget/v1` type produced by the HTTP streaming uploader
- [HTTP Client Configuration]({{< relref "docs/reference/http-client-configuration.md" >}}) — tuning the HTTP client used for the upload
