---
title: "Vendor Repository Uploaders"
description: "How OCM's JFrog Artifactory and Sonatype Nexus uploaders move matched resources into a vendor repository during transfer and republish them as native access."
icon: "📤"
weight: 3
toc: true
aliases:
  - /docs/how-to/vendor-specific-apis/
---

## Overview

OCM can push the resources of a component version straight into a vendor artifact
repository while it transfers the component. Two uploaders implement this model:

- [`artifactory.uploader.transfer.config.ocm.software/v1alpha1`]({{< relref "docs/reference/transfer-configuration/artifactory-uploader.md" >}})
  for JFrog Artifactory, and
- [`nexus.uploader.transfer.config.ocm.software/v1alpha1`]({{< relref "docs/reference/transfer-configuration/nexus-uploader.md" >}})
  for Sonatype Nexus Repository 3.

Both follow the same server-side model: a resource that an uploader rule matches is
uploaded into one repository the way that repository's package type or format expects,
and its access is rewritten so that consumers resolve the artifact with their native
tooling (`helm pull`, `mvn`, `npm install`, `curl`). The uploaders are a transfer-time
concern; the OCM Kubernetes controller ignores them because they send content to
configured URLs from the controller pod.

## How Vendor Uploaders Work

### Rule matching: first match wins

Each uploader rule carries a CEL `match` expression that selects the resources it
handles, together with the target server `url`, the `repository` key, and an optional
`path`. During transfer, every resource is tested against the configured uploader
rules **in order**, and the **first matching rule wins** — so place more specific rules
before broader ones. A rule with no match is skipped. Neither uploader has a default
match, so a rule that selects nothing uploads nothing.

### Repository type is read from the server

An uploader does not assume what a repository is. At upload time it queries the server
for the repository's package type or format:

- Artifactory: `GET <url>/artifactory/api/repositories/<repository>` returns the
  package type (`helm`, `maven`, `npm`, `generic`).
- Nexus: `GET <url>/service/rest/v1/repositories/<repository>` returns the format
  (`helm`, `maven2`, `npm`, `raw`).

The uploader then uploads the content the way that type or format expects. This read
also decides whether the repository is a valid upload target.

### Only local/hosted repositories accept uploads

An upload needs a repository that physically stores content:

- Artifactory accepts uploads only into **local** (or federated) repositories. Remote
  and virtual repositories fail with `uploads need a local repository`.
- Nexus accepts uploads only into **hosted** repositories. Proxy and group repositories
  fail with `uploads need a hosted repository`.

An unsupported package type or format fails the transfer with a message that lists the
supported ones, for example `has package type "docker"; supported: helm, generic, maven, npm`
(Artifactory) or `has format "pypi"; supported: helm, raw, maven2, npm` (Nexus).

### Access is rewritten to native access

After a successful upload the uploader republishes the resource with the access type
that the repository's own clients understand, so downstream consumers never see an OCM
internal reference:

- A **Helm** repository publishes `Helm/v1` with a `helmRepository` pointing at the
  repository's Helm API and a `helmChart: <name>:<version>` taken from the chart itself
  (not from the OCM resource).
- Every other type publishes `Wget/v1` on the stored file, so the artifact can be
  downloaded by URL and resolved by Maven or npm against the repository's native API.

The name and version of a Helm chart or npm package always come from the artifact's own
metadata (`Chart.yaml`, `package.json`), not from the OCM resource name.

### Digest verification

The uploader verifies the resource's `genericBlobDigest/v1` SHA-256 or SHA-512 source
digest, and content the server already stores is not uploaded again. Where the server
can reject mismatching bytes on deploy (Artifactory, SHA-256) it does; otherwise
(Nexus, and SHA-512 on Artifactory) the uploader verifies after the upload and fails,
deleting a mismatching file. Content extracted from an OCI artifact is uploaded as a
gzipped OCI layout tar and gets the SHA-256 of the uploaded bytes.

### Existing files at the upload path

The two servers differ in how they treat a file already stored at the upload path:

- **Artifactory** stamps every deployed file with owner properties naming the resource,
  component and version it was uploaded for. Identical content is reused; a file whose
  properties name the same resource of the same component version is replaced (a
  repeated transfer); any other file causes the transfer to fail and asks for a
  different `path`.
- **Nexus** records no owner of a file. In `raw` and `maven2` repositories a stored file
  is **never overwritten** — identical content is reused, different content fails the
  transfer. In `helm` and `npm` repositories a stored package with identical content is
  reused, and different content under the same name and version fails when the
  repository disallows redeploys.

### Credentials

Both uploaders resolve credentials for the `HelmChartRepository` identity of the
repository's Helm API, falling back to the `Wget` identity of the server. A `Wget`
consumer configured with the bare host name (no path) covers every repository on the
server. See [Credential Resolution]({{< relref "docs/concepts/credential-resolution.md" >}})
for how OCM matches a request to a consumer.

## Related Documentation

- [Guide: Upload to JFrog Artifactory]({{< relref "docs/guides/transfer/upload-to-jfrog-artifactory.md" >}}) — Step-by-step per-format recipes
- [Guide: Upload to Sonatype Nexus]({{< relref "docs/guides/transfer/upload-to-sonatype-nexus.md" >}}) — Step-by-step per-format recipes
- [Reference: JFrog Artifactory Uploader]({{< relref "docs/reference/transfer-configuration/artifactory-uploader.md" >}}) — Full field and repository-behaviour reference
- [Reference: Sonatype Nexus Uploader]({{< relref "docs/reference/transfer-configuration/nexus-uploader.md" >}}) — Full field and repository-behaviour reference
- [Concept: Transfer and Transport]({{< relref "docs/concepts/transfer-concept.md" >}}) — How OCM moves component versions between repositories
