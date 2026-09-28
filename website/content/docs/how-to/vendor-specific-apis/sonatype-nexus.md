---
title: "Upload Resources to Sonatype Nexus"
description: "Transfer component versions and upload their resources into Sonatype Nexus Repository 3 hosted repositories: Helm charts, Maven artifacts, npm packages and raw files."
weight: 2
toc: true
---

## Goal

Transfer a component version and upload its resources into Sonatype Nexus
Repository 3 hosted repositories, so that consumers fetch them with their usual
tools (`helm`, `mvn`, `npm`, `curl`).

## You'll end up with

- Resources stored in Nexus hosted repositories of the right format
- A transferred component version whose resources point at those repositories
  (`Helm/v1` for charts, `Wget/v1` for files)

**Estimated time:** ~15 minutes

## How the Nexus uploader works

The
[`nexus.uploader.transfer.config.ocm.software/v1alpha1`]({{< relref "docs/reference/transfer-configuration.md#nexusuploadertransferconfigocmsoftwarev1alpha1" >}})
uploader routes the resources it matches to one hosted repository. At upload time it
reads the repository format from `GET <url>/service/rest/v1/repositories/<repository>`
and picks the upload method for it:

```mermaid
flowchart LR
    R[Matched resource] --> D{Format}
    D -->|helm| H[Upload chart, publish Helm/v1]
    D -->|raw / maven2 / npm| F[Upload file, publish Wget/v1]
    D -->|anything else| X[Transfer fails]
```

| Format   | Uploaded content                                      | Published access                          |
|----------|-------------------------------------------------------|-------------------------------------------|
| `helm`   | The Helm chart found in the resource                  | `Helm/v1` (`helmRepository`, `helmChart`) |
| `raw`    | The resource content as is                            | `Wget/v1` on the stored file              |
| `maven2` | The resource content as one file of a Maven component | `Wget/v1` on the stored file              |
| `npm`    | The npm package tarball, as is                        | `Wget/v1` on the stored tarball           |

Every other format fails the transfer before anything is uploaded. See
[Other repository formats](#other-repository-formats) for alternatives.

## Prerequisites

- [OCM CLI]({{< relref "docs/getting-started/ocm-cli-installation.md" >}}) installed
- A component version in a CTF or OCI repository
- A Nexus **hosted** repository
- A user that may upload into the repository **and read its settings**

## Configure credentials

The uploader resolves credentials for the `HelmChartRepository` identity of
`<url>/repository/<repository>`, falling back to its `Wget` identity. A `Wget`
consumer without a path covers every repository on the server:

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
```

{{< callout context="caution" >}}
`hostname` is the bare host name. A value such as `https://nexus.example.com`
matches no request, so the upload is sent without credentials and fails with `401`.
{{< /callout >}}

## Upload to Nexus

### Helm charts

```yaml
  - type: nexus.uploader.transfer.config.ocm.software/v1alpha1
    match:
      accessType: localBlob
      name: chart
    url: https://nexus.example.com
    repository: helm-hosted
```

Nexus stores the chart as `<name>-<version>.tgz` from its `Chart.yaml`, adds it to
`index.yaml` and the resource is published as:

```yaml
access:
  type: Helm/v1
  helmRepository: https://nexus.example.com/repository/helm-hosted
  helmChart: mychart:0.1.0
```

- `helm pull mychart --version 0.1.0 --repo https://nexus.example.com/repository/helm-hosted`
  returns the chart.
- A second transfer reuses the stored chart, whether the repository allows redeploys
  or not.
- `path` is not supported: Nexus decides where charts are stored.

### Raw files

```yaml
  - type: nexus.uploader.transfer.config.ocm.software/v1alpha1
    match:
      accessType: localBlob
    url: https://nexus.example.com
    repository: raw-hosted
    # optional, defaults to <component>/<component version>/<resource>-<resource version>
    path: '${"files/" + resource.name + ".txt"}'
```

The resource is published as `Wget/v1` on
`https://nexus.example.com/repository/raw-hosted/files/notes.txt`.

Nexus records no owner of a file, so the uploader **never overwrites** a stored file,
even when the repository allows redeploys. A file with the same content is reused; a
file with different content fails the transfer:

```text
nexus repository "raw-hosted" already stores a different file at …; the uploader never overwrites files in raw repositories, configure a different path
```

### Maven artifacts

Maven resolves an artifact from its Maven coordinates, so a `maven2` repository needs
a `path` in the Maven repository layout
`<group path>/<artifactId>/<version>/<artifactId>-<version>[-<classifier>].<extension>`.
The uploader takes the coordinates from the path. Upload the POM as a resource of its
own next to the artifact, one uploader rule per file type:

```yaml
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

The files are published as `Wget/v1` on
`https://nexus.example.com/repository/maven-releases/com/example/demo/1.0.0/demo-1.0.0.jar`
and `….pom`. Verify that Maven resolves the artifact:

```bash
mvn dependency:get -Dartifact=com.example:demo:1.0.0 \
  -DremoteRepositories=nexus::default::https://nexus.example.com/repository/maven-releases
```

Behavior to plan for:

- **Release versions** go through the Nexus components API
  (`POST /service/rest/v1/components`), which updates `maven-metadata.xml`. Its
  `latest` and `release` are the highest version, not the most recently uploaded one:
  transferring `4.0.0` after `5.0.0` keeps `5.0.0`.
- **Snapshot versions** go into a snapshot repository with a plain `PUT`, because the
  components API refuses them. Maven resolves them by their exact version, but they
  are not added to `maven-metadata.xml`. The repository's version policy decides what it
  accepts: a `-SNAPSHOT` version in a release repository fails with
  `Version policy mismatch, cannot upload SNAPSHOT content to RELEASE repositories`.
- **Stored files are never overwritten**, like in raw repositories: a file with the same
  content is reused, a different one fails the transfer.
- A `path` outside the Maven layout, or no `path`, fails before anything is uploaded:
  `path "…" is not in the Maven repository layout …`.

### npm packages

Point the uploader at an npm hosted repository. The resource must hold an npm package
tarball (`package/package.json` plus the package files):

```yaml
  - type: nexus.uploader.transfer.config.ocm.software/v1alpha1
    match:
      accessType: localBlob
      name: my-package
    url: https://nexus.example.com
    repository: npm-hosted
```

Nexus does not accept plain uploads of package tarballs, so the uploader uses the
components API (`POST /service/rest/v1/components` with the tarball as `npm.asset`).
Nexus reads name and version from `package.json` and stores the tarball under
`<name>/-/<name>-<version>.tgz` (`@scope/<name>/-/<name>-<version>.tgz` for scoped
packages). The resource is published as `Wget/v1` on the stored tarball, for example
`https://nexus.example.com/repository/npm-hosted/@ocm/demo/-/demo-2.0.0.tgz`.

Install it with npm:

```bash
npm install @ocm/demo@2.0.0 --registry https://nexus.example.com/repository/npm-hosted/
```

Behavior to plan for:

- **The `latest` dist-tag is the highest release version**, not the most recently
  uploaded one: transferring `1.0.0` after `2.0.0` keeps `2.0.0` as `latest`, and a
  prerelease such as `3.0.0-rc.1` does not become `latest`.
- **A second transfer reuses the stored tarball**: the uploader finds it by its SHA-256.
- **Content that is not an npm package** fails the transfer:
  `POST …/service/rest/v1/components returned status 400: … Name and version are mandatory fields`.
- **A version the repository already stores with other content** fails with `409` when
  redeploy is disabled.
- `path` is not supported: Nexus decides where packages are stored.

## Other repository formats

| Repository                  | Error                                                  | Alternative                                 |
|-----------------------------|--------------------------------------------------------|---------------------------------------------|
| `pypi` and other formats    | `has format "pypi"; supported: helm, raw, maven2, npm` | Upload with the format's own client         |
| Proxy or group repositories | `uploads need a hosted repository`                     | Upload into the hosted repository behind it |

## Troubleshooting

### Symptom: `failed detecting the type of … repository …: GET … returned status 401`

**Cause:** No credentials were found for the server, so the uploader asked anonymously.

**Fix:** Configure credentials as shown above, with the bare host name in `hostname`.

### Symptom: `failed detecting the type of … repository …: GET … returned status 403`

**Cause:** The user may deploy but not read the repository settings.

**Fix:** Grant read access to the repository configuration, or use a user that has it.

### Symptom: `nexus repository … already stores a different file at …; the uploader never overwrites files in … repositories`

**Cause:** The raw or maven2 repository already stores a file with different content at the
upload path, for example from another component version with the same resource
version.

**Fix:** Configure a `path` that is unique for the resource, or remove the stored file.

### Symptom: `content of resource … is not a helm chart`

**Cause:** The resource matched by a Helm repository uploader holds no packaged chart.

**Fix:** Narrow `match` to the chart resources, or route other resources to a raw
repository.

## Related documentation

- [How-to: Upload Resources to JFrog Artifactory]({{< relref "docs/how-to/vendor-specific-apis/jfrog-artifactory.md" >}})
- [Reference: Transfer Configuration]({{< relref "docs/reference/transfer-configuration.md" >}})
- [Tutorial: Configure Custom Uploads During Transfer]({{< relref "docs/tutorials/configure-custom-uploads.md" >}})
- [Concept: Transfer and Transport]({{< relref "docs/concepts/transfer-concept.md" >}})
- [Credential Consumer Identities]({{< relref "docs/reference/credential-consumer-identities.md" >}})
