---
title: "Upload Maven Artifacts to JFrog Artifactory"
slug: "maven-artifacts"
description: "Upload a JAR and its POM into a JFrog Artifactory maven repository during transfer, so that Maven resolves the artifact."
weight: 2
toc: true
---

## Goal

Transfer a component version and upload a JAR and its POM into an Artifactory maven
repository, so that Maven resolves the artifact from its coordinates.

## You'll end up with

- `demo-1.0.0.jar` and `demo-1.0.0.pom` stored under `com/example/demo/1.0.0/` in `maven-local`
- Transferred resources with a `Wget/v1` access on the stored files

**Estimated time:** ~10 minutes

## Prerequisites

- The setup in [JFrog Artifactory]({{< relref "docs/how-to/vendor-specific-apis/jfrog-artifactory/_index.md#prerequisites" >}}),
  with [credentials configured]({{< relref "docs/how-to/vendor-specific-apis/jfrog-artifactory/_index.md#configure-credentials" >}})
- An Artifactory maven repository, here `maven-local`
- The `mvn` CLI

## Steps

### Add the JAR and the POM as resources

Artifactory only builds `maven-metadata.xml` for artifacts with a POM. Without one,
Maven cannot resolve the version. So the component version holds the JAR and its POM
as two resources:

```yaml
components:
  - name: ocm.software/demo
    version: 1.0.0
    provider: {name: ocm.software}
    resources:
      - name: jar
        type: blob
        version: 1.0.0
        input: {type: file/v1, path: ./demo-1.0.0.jar, mediaType: application/java-archive}
      - name: pom
        type: blob
        version: 1.0.0
        input: {type: file/v1, path: ./demo-1.0.0.pom, mediaType: application/xml}
```

### Configure the uploader

Maven finds a file only under the Maven layout
`<group path>/<artifactId>/<version>/<artifactId>-<version>.<ext>`. Create
`config.yaml` with the credentials and one rule per file, each with a CEL `path` in
that layout:

```yaml
type: generic.config.ocm.software/v1
configurations:
  - type: credentials.config.ocm.software
    consumers:
      - identity:
          type: Wget
          hostname: myorg.jfrog.io
          scheme: https
        credentials:
          - type: WgetCredentials/v1
            identityToken: <ARTIFACTORY_IDENTITY_TOKEN>
  - type: artifactory.uploader.transfer.config.ocm.software/v1alpha1
    match: {accessType: localBlob, name: jar}
    url: https://myorg.jfrog.io
    repository: maven-local
    path: '${"com/example/demo/" + resource.version + "/demo-" + resource.version + ".jar"}'
  - type: artifactory.uploader.transfer.config.ocm.software/v1alpha1
    match: {accessType: localBlob, name: pom}
    url: https://myorg.jfrog.io
    repository: maven-local
    path: '${"com/example/demo/" + resource.version + "/demo-" + resource.version + ".pom"}'
```

### Run the transfer

```bash
ocm transfer cv --config config.yaml ctf::./src//ocm.software/demo:1.0.0 ctf::./target
```

### Verify

```bash
ocm get cv ctf::./target//ocm.software/demo:1.0.0 -o yaml
```

The `jar` resource has this access:

```yaml
access:
  type: Wget/v1
  url: https://myorg.jfrog.io/artifactory/maven-local/com/example/demo/1.0.0/demo-1.0.0.jar
  mediaType: application/java-archive
```

Resolve the artifact with Maven:

```bash
mvn dependency:get -Dartifact=com.example:demo:1.0.0 \
  -DremoteRepositories=ocm::default::https://myorg.jfrog.io/artifactory/maven-local
```

## How the repository behaves

- **`latest` and `release`** in `maven-metadata.xml` are the highest version in the
  repository, not the most recently uploaded one. Transferring `0.9.0` after `1.0.0`
  keeps `1.0.0` as `latest` and `release`.
- **Snapshots:** a `-SNAPSHOT` version is stored under a unique timestamped version,
  for example `…/2.0.0-SNAPSHOT/demo-2.0.0-20260928.183609-1.jar`. The published
  `Wget/v1` URL points at that file, so it keeps returning the transferred bytes after
  later snapshots.
- **Without a Maven-layout `path`** the file is stored under the default path. It can
  be downloaded from its `Wget/v1` URL, but Maven does not see it.

## Troubleshooting

### Symptom: `mvn dependency:get` does not find the version

**Cause:** No POM was uploaded, or the `path` is not in the Maven layout.

**Fix:** Upload the POM as its own resource with a Maven-layout `path`.

For credential and overwrite errors, see
[Troubleshooting]({{< relref "docs/how-to/vendor-specific-apis/jfrog-artifactory/_index.md#troubleshooting" >}}).

## Related documentation

- [How-to: JFrog Artifactory]({{< relref "docs/how-to/vendor-specific-apis/jfrog-artifactory/_index.md" >}})
- [How-to: Upload Maven Artifacts to Sonatype Nexus]({{< relref "docs/how-to/vendor-specific-apis/sonatype-nexus/maven-artifacts.md" >}})
- [Reference: JFrog Artifactory Uploader]({{< relref "docs/reference/transfer-configuration/artifactory-uploader.md#repository-types" >}})
- [JFrog: Maven Repositories](https://jfrog.com/help/r/jfrog-artifactory-documentation/maven-repositories)
