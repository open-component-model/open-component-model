---
title: "Upload Maven Artifacts to Sonatype Nexus"
slug: "maven-artifacts"
description: "Upload a JAR and its POM into a Sonatype Nexus maven2 hosted repository during transfer, so that Maven resolves the artifact."
weight: 2
toc: true
---

## Goal

Transfer a component version and upload a JAR and its POM into a Nexus maven2
hosted repository, so that Maven resolves the artifact from its coordinates.

## You'll end up with

- `demo-1.0.0.jar` and `demo-1.0.0.pom` stored under `com/example/demo/1.0.0/` in `maven-releases`
- Transferred resources with a `Wget/v1` access on the stored files

**Estimated time:** ~10 minutes

## Prerequisites

- The setup in [Sonatype Nexus]({{< relref "docs/how-to/vendor-specific-apis/sonatype-nexus/_index.md#prerequisites" >}}),
  with [credentials configured]({{< relref "docs/how-to/vendor-specific-apis/sonatype-nexus/_index.md#configure-credentials" >}})
- A Nexus maven2 hosted repository with release version policy, here `maven-releases`
- The `mvn` CLI

## Steps

### Add the JAR and the POM as resources

Upload the POM as a resource of its own next to the artifact. The component version
holds the JAR and its POM as two resources:

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

A `maven2` repository needs a `path` in the Maven repository layout
`<group path>/<artifactId>/<version>/<artifactId>-<version>[-<classifier>].<extension>`.
The uploader takes the coordinates from the path. Create `config.yaml` with the
credentials and one rule per file:

```yaml
type: generic.config.ocm.software/v1
configurations:
  - type: credentials.config.ocm.software
    consumers:
      - identity:
          type: Wget
          hostname: nexus.example.com
          scheme: https
        credentials:
          - type: WgetCredentials/v1
            username: <USERNAME>
            password: <PASSWORD_OR_USER_TOKEN>
  - type: nexus.uploader.transfer.config.ocm.software/v1alpha1
    match: {accessType: localBlob, name: jar}
    url: https://nexus.example.com
    repository: maven-releases
    path: '${"com/example/demo/" + resource.version + "/demo-" + resource.version + ".jar"}'
  - type: nexus.uploader.transfer.config.ocm.software/v1alpha1
    match: {accessType: localBlob, name: pom}
    url: https://nexus.example.com
    repository: maven-releases
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
  url: https://nexus.example.com/repository/maven-releases/com/example/demo/1.0.0/demo-1.0.0.jar
  mediaType: application/java-archive
```

Resolve the artifact with Maven:

```bash
mvn dependency:get -Dartifact=com.example:demo:1.0.0 \
  -DremoteRepositories=nexus::default::https://nexus.example.com/repository/maven-releases
```

## How the repository behaves

- **Release versions** go through the Nexus components API
  (`POST /service/rest/v1/components`), which updates `maven-metadata.xml`. Its
  `latest` and `release` are the highest version, not the most recently uploaded one:
  transferring `4.0.0` after `5.0.0` keeps `5.0.0`.
- **Snapshot versions** go into a snapshot repository such as `maven-snapshots` with a
  plain `PUT`, because the components API refuses them. Maven resolves them by their
  exact version, but they are not added to `maven-metadata.xml`. The repository's
  version policy decides what it accepts.
- **Stored files are never overwritten**, see
  [Existing files at the upload path]({{< relref "docs/how-to/vendor-specific-apis/sonatype-nexus/_index.md#existing-files-at-the-upload-path" >}}).
- A `path` outside the Maven layout, or no `path`, fails before anything is uploaded.
- A POM must declare the coordinates of its `path`: Nexus stores a POM under the coordinates
  it declares, so a mismatch fails before anything is uploaded.

## Troubleshooting

### Symptom: `path "…" is not in the Maven repository layout …`

**Cause:** `path` is missing or not in the Maven layout.

**Fix:** Set a `path` in the Maven layout.

### Symptom: `POM declares …, but path "…" is …`

**Cause:** The `groupId`, `artifactId` or `version` in the POM differ from those in `path`.

**Fix:** Set a `path` that matches the POM coordinates.

### Symptom: `Version policy mismatch, cannot upload SNAPSHOT content to RELEASE repositories`

**Cause:** A `-SNAPSHOT` version was routed to a release repository.

**Fix:** Route `-SNAPSHOT` versions to `maven-snapshots` with a second rule that
matches them by `version`, for example `match: {accessType: localBlob, name: jar, version: 2.0.0-SNAPSHOT}`.
Declare it before the release rule: the first matching rule wins.

For credential and overwrite errors, see
[Troubleshooting]({{< relref "docs/how-to/vendor-specific-apis/sonatype-nexus/_index.md#troubleshooting" >}}).

## Related documentation

- [How-to: Sonatype Nexus]({{< relref "docs/how-to/vendor-specific-apis/sonatype-nexus/_index.md" >}})
- [How-to: Upload Maven Artifacts to JFrog Artifactory]({{< relref "docs/how-to/vendor-specific-apis/jfrog-artifactory/maven-artifacts.md" >}})
- [Reference: Sonatype Nexus Uploader]({{< relref "docs/reference/transfer-configuration/nexus-uploader.md#repository-types" >}})
- [Sonatype: Maven Repositories](https://help.sonatype.com/en/maven-repositories.html)
