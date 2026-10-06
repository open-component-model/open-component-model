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

## Choose your server

| Server                      | Repository types          | Guide                                                                                              |
|-----------------------------|---------------------------|----------------------------------------------------------------------------------------------------|
| JFrog Artifactory           | helm, maven, npm, generic | [JFrog Artifactory]({{< relref "docs/how-to/vendor-specific-apis/jfrog-artifactory/_index.md" >}}) |
| Sonatype Nexus Repository 3 | helm, maven2, npm, raw    | [Sonatype Nexus]({{< relref "docs/how-to/vendor-specific-apis/sonatype-nexus/_index.md" >}})       |

## How vendor uploaders work

1. An uploader rule matches resources by their access type in the **source**
   component version. The first matching rule wins, see
   [Uploader configurations]({{< relref "docs/reference/transfer-configuration/_index.md#uploader-configurations" >}}).
2. At upload time the uploader reads the repository type from the server.
3. It uploads the resource the way that repository type expects.
4. It rewrites the resource in the target component version to a `Helm/v1` access
   for charts, or a `Wget/v1` access for files and packages. A `genericBlobDigest/v1`
   SHA-256 or SHA-512 source digest is verified.

## Related Documentation

- [Reference: Transfer Configuration]({{< relref "docs/reference/transfer-configuration/_index.md" >}}) — uploader configuration fields and schemas
- [Tutorial: Configure Custom Uploads During Transfer]({{< relref "docs/tutorials/configure-custom-uploads.md" >}}) — route resources to custom upload targets
