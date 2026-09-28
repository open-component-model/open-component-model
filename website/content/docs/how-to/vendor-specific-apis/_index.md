---
title: "Using Vendor-Specific APIs"
description: "Upload resources into JFrog Artifactory and Sonatype Nexus repositories during transfer, using each server's own repository APIs."
icon: "🏭"
weight: 16
toc: true
sidebar:
  collapsed: true
---

Artifact servers such as JFrog Artifactory and Sonatype Nexus Repository host many
repository types behind their own APIs. OCM's vendor uploaders read the type of the
target repository from the server and upload each resource the way that repository
type expects, so consumers fetch it with their usual tools.

## Guides in This Section

- **[Upload Resources to JFrog Artifactory]({{< relref "jfrog-artifactory.md" >}})** — Helm, Maven, npm and generic repositories
- **[Upload Resources to Sonatype Nexus]({{< relref "sonatype-nexus.md" >}})** — Helm, Maven, npm and raw repositories

## Related Documentation

- [Reference: Transfer Configuration]({{< relref "docs/reference/transfer-configuration.md" >}}) — uploader configuration fields and schemas
- [Tutorial: Configure Custom Uploads During Transfer]({{< relref "docs/tutorials/configure-custom-uploads.md" >}}) — route resources to custom upload targets
