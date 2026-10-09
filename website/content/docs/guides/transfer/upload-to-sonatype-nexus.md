---
title: "Upload to Sonatype Nexus"
description: "Upload resources into Sonatype Nexus Repository 3 hosted helm, maven2, npm and raw repositories during transfer, with credentials and overwrite rules."
weight: 140
toc: true
aliases:
  - /docs/how-to/vendor-specific-apis/sonatype-nexus/
  - /docs/how-to/vendor-specific-apis/sonatype-nexus/helm-charts/
  - /docs/how-to/vendor-specific-apis/sonatype-nexus/maven-artifacts/
  - /docs/how-to/vendor-specific-apis/sonatype-nexus/npm-packages/
  - /docs/how-to/vendor-specific-apis/sonatype-nexus/raw-files/
---

{{< callout context="note" title="Start from the base transfer guide" >}}
This guide specializes the default transfer workflow for Sonatype Nexus
targets. If you have not transferred a component version before, read
[Transfer Component Versions]({{< relref "docs/guides/transfer/transfer-component-versions.md" >}})
first, then return here to route resources into Nexus hosted repositories.
{{< /callout >}}

The
[`nexus.uploader.transfer.config.ocm.software/v1alpha1`]({{< relref "docs/reference/transfer-configuration/nexus-uploader.md" >}})
uploader uploads the resources it matches into one Nexus hosted repository during
transfer. At upload time the uploader reads the format from the server with
`GET <url>/service/rest/v1/repositories/<repository>` and publishes the way that
format expects. For the server-side model shared by all vendor uploaders — type
detection, access rewrite, verification, first-match-wins — see
[Vendor Repository Uploaders]({{< relref "docs/concepts/vendor-uploaders.md" >}}).

The sections below share one scaffold (prerequisites, credentials, transfer,
verify, overwrite rules) and then give the per-format `match`/`path` snippet and
consumer command for each supported repository format.

## Repository types

| Format   | Section                               | Published access |
|----------|---------------------------------------|------------------|
| `helm`   | [Helm charts](#helm-charts)           | `Helm/v1`        |
| `maven2` | [Maven artifacts](#maven-artifacts)   | `Wget/v1`        |
| `npm`    | [npm packages](#npm-packages)         | `Wget/v1`        |
| `raw`    | [Raw files](#raw-files)               | `Wget/v1`        |

### Unsupported repositories

| Repository                  | Error                                                  | Alternative                                 |
|-----------------------------|--------------------------------------------------------|---------------------------------------------|
| `pypi` and other formats    | `has format "pypi"; supported: helm, raw, maven2, npm` | Upload with the format's own client         |
| Proxy or group repositories | `uploads need a hosted repository`                     | Upload into the hosted repository behind it |

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

## Existing files at the upload path

Nexus records no owner of a file, so the uploader never replaces stored content:

- **`raw` and `maven2`:** a stored file is never overwritten, even when the repository
  allows redeploys. Same content is reused; different content fails the transfer.
- **`helm` and `npm`:** Nexus stores them under a path derived from the package. A stored
  package with the same content is reused. Different content under the same name and
  version fails when the repository disallows redeploys.

## Helm charts

Transfer a component version and upload its Helm chart into a Nexus `helm` hosted
repository (here `helm-hosted`), so that `helm pull` finds it. The chart is listed in
the repository's `index.yaml` and the resource is published with a `Helm/v1` access.

Add the credentials and one uploader rule for the `chart` resource to your
`.ocmconfig` in the working directory (merged with `$HOME/.ocmconfig`):

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
    match: resource.access.isType("LocalBlob") && resource.name == "chart"
    url: https://nexus.example.com
    repository: helm-hosted
```

Run the transfer and inspect the result:

```bash
ocm transfer cv ctf::./src//ocm.software/demo:1.0.0 ctf::./target
ocm get cv ctf::./target//ocm.software/demo:1.0.0 -o yaml
```

The `chart` resource has this access:

```yaml
access:
  type: Helm/v1
  helmRepository: https://nexus.example.com/repository/helm-hosted
  helmChart: mychart:0.1.0
```

Pull the chart with Helm:

```bash
helm pull mychart --version 0.1.0 --repo https://nexus.example.com/repository/helm-hosted
```

**How the repository behaves**

- Nexus stores the chart as `<name>-<version>.tgz` from its `Chart.yaml` and adds it
  to `index.yaml`.
- `path` is not supported: Nexus decides where charts are stored.
- A second transfer reuses the stored chart, whether the repository allows redeploys
  or not.
- Different content under a chart name and version that is already stored fails when
  the repository disallows redeploys.

If `content of resource … is not a helm chart` appears, the matched resource holds no
packaged chart; narrow `match` to the chart resources, or route other resources to a
raw repository.

## Maven artifacts

Transfer a component version and upload a JAR and its POM into a Nexus `maven2` hosted
repository (here `maven-releases`), so that Maven resolves the artifact from its
coordinates. Upload the POM as a resource of its own next to the artifact:

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

A `maven2` repository needs a `path` in the Maven repository layout
`<group path>/<artifactId>/<version>/<artifactId>-<version>[-<classifier>].<extension>`.
The uploader takes the coordinates from the path. Add the credentials and one rule per
file to your `.ocmconfig`:

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
    match: resource.access.isType("LocalBlob") && resource.name == "jar"
    url: https://nexus.example.com
    repository: maven-releases
    path: '${"com/example/demo/" + resource.version + "/demo-" + resource.version + ".jar"}'
  - type: nexus.uploader.transfer.config.ocm.software/v1alpha1
    match: resource.access.isType("LocalBlob") && resource.name == "pom"
    url: https://nexus.example.com
    repository: maven-releases
    path: '${"com/example/demo/" + resource.version + "/demo-" + resource.version + ".pom"}'
```

Run the transfer and inspect the result:

```bash
ocm transfer cv ctf::./src//ocm.software/demo:1.0.0 ctf::./target
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

**How the repository behaves**

- **Release versions** go through the Nexus components API
  (`POST /service/rest/v1/components`), which updates `maven-metadata.xml`. Its
  `latest` and `release` are the highest version, not the most recently uploaded one:
  transferring `4.0.0` after `5.0.0` keeps `5.0.0`.
- **Snapshot versions** go into a snapshot repository such as `maven-snapshots` with a
  plain `PUT`, because the components API refuses them. Maven resolves them by their
  exact version, but they are not added to `maven-metadata.xml`. The repository's
  version policy decides what it accepts.
- **Stored files are never overwritten**, see
  [Existing files at the upload path](#existing-files-at-the-upload-path).
- A `path` outside the Maven layout, or no `path`, fails before anything is uploaded.
- A POM must declare the coordinates of its `path`: Nexus stores a POM under the coordinates
  it declares, so a mismatch fails before anything is uploaded.

Troubleshooting the Maven layout:

- `path "…" is not in the Maven repository layout …` — `path` is missing or not in
  the Maven layout. Set a `path` in the Maven layout.
- `POM declares …, but path "…" is …` — the `groupId`, `artifactId` or `version` in
  the POM differ from those in `path`. Set a `path` that matches the POM coordinates.
- `Version policy mismatch, cannot upload SNAPSHOT content to RELEASE repositories` —
  a `-SNAPSHOT` version was routed to a release repository. Route `-SNAPSHOT` versions
  of both files to `maven-snapshots` with a rule declared **before** both release
  rules, because the first matching rule wins. The resource names match the file
  extensions, so one rule can build both paths:

  ```yaml
    - type: nexus.uploader.transfer.config.ocm.software/v1alpha1
      match: resource.access.isType("LocalBlob") && resource.name in ["jar", "pom"] && resource.version.endsWith("-SNAPSHOT")
      url: https://nexus.example.com
      repository: maven-snapshots
      path: '${"com/example/demo/" + resource.version + "/demo-" + resource.version + "." + resource.name}'
  ```

## npm packages

Transfer a component version and upload its npm package into a Nexus `npm` hosted
repository (here `npm-hosted`), so that `npm install` finds it. The resource must hold
an npm package tarball (`package/package.json` plus the package files).

Add the credentials and one uploader rule for the `my-package` resource to your
`.ocmconfig`:

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
    match: resource.access.isType("LocalBlob") && resource.name == "my-package"
    url: https://nexus.example.com
    repository: npm-hosted
```

Run the transfer. Nexus does not accept plain uploads of package tarballs, so the
uploader uses the components API (`POST /service/rest/v1/components` with the tarball
as `npm.asset`). Inspect the result:

```bash
ocm transfer cv ctf::./src//ocm.software/demo:1.0.0 ctf::./target
ocm get cv ctf::./target//ocm.software/demo:1.0.0 -o yaml
```

For a package named `@acme/demo`, the `my-package` resource has this access:

```yaml
access:
  type: Wget/v1
  url: https://nexus.example.com/repository/npm-hosted/@acme/demo/-/demo-2.0.0.tgz
```

Install the package with npm:

```bash
npm install @acme/demo@2.0.0 --registry https://nexus.example.com/repository/npm-hosted/
```

**How the repository behaves**

- Nexus reads name and version from `package.json` and stores the tarball under
  `<name>/-/<name>-<version>.tgz` (`@scope/<name>/-/<name>-<version>.tgz` for scoped
  packages).
- The uploader does not change the package: its name, including any scope, is the one
  in `package.json`.
- **The `latest` dist-tag is the highest release version**, not the most recently
  uploaded one: transferring `1.0.0` after `2.0.0` keeps `2.0.0` as `latest`, and a
  prerelease such as `3.0.0-rc.1` does not become `latest`.
- **A second transfer reuses the stored tarball**: the uploader finds it by its digest.
- `path` is not supported: Nexus decides where packages are stored.

If `POST …/service/rest/v1/components returned status 400: … Name and version are
mandatory fields` appears, the resource content is not an npm package; narrow `match`
to the package resources, or route other resources to a raw repository. A `409` means
the repository already stores the version with other content and redeploy is disabled
— publish under a new version, or remove the stored version.

## Raw files

Transfer a component version and upload its resources as plain files into a Nexus
`raw` hosted repository (here `raw-hosted`), so that consumers download them by URL.
Any access type is accepted and the content is stored as is.

Add the credentials and one uploader rule for all `LocalBlob` resources to your
`.ocmconfig`:

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
    match: resource.access.isType("LocalBlob")
    url: https://nexus.example.com
    repository: raw-hosted
    # optional, defaults to <component>/<component version>/<resource>-<resource version>
    path: '${"files/" + resource.name + ".txt"}'
```

Run the transfer and inspect the result:

```bash
ocm transfer cv ctf::./src//ocm.software/demo:1.0.0 ctf::./target
ocm get cv ctf::./target//ocm.software/demo:1.0.0 -o yaml
```

The `notes` resource has this access:

```yaml
access:
  type: Wget/v1
  url: https://nexus.example.com/repository/raw-hosted/files/notes.txt
  mediaType: text/plain
```

Download the file:

```bash
curl -fsSL -u <USERNAME>:<PASSWORD_OR_USER_TOKEN> https://nexus.example.com/repository/raw-hosted/files/notes.txt
```

**How the repository behaves**

- Any access type is accepted and the content is stored as is.
- OCI images are uploaded as one gzipped OCI layout tar
  (`application/vnd.ocm.software.oci.layout.v1+tar+gzip`).
- A stored file is never overwritten, see
  [Existing files at the upload path](#existing-files-at-the-upload-path).
  A file with different content fails the transfer:

  ```text
  nexus repository "raw-hosted" already stores a different file at …; the uploader never overwrites files in raw repositories, configure a different path
  ```

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

## Related documentation

- [Concept: Vendor Repository Uploaders]({{< relref "docs/concepts/vendor-uploaders.md" >}})
- [Upload to JFrog Artifactory]({{< relref "docs/guides/transfer/upload-to-jfrog-artifactory.md" >}})
- [Reference: Sonatype Nexus Uploader]({{< relref "docs/reference/transfer-configuration/nexus-uploader.md" >}})
- [Reference: Transfer Configuration]({{< relref "docs/reference/transfer-configuration/_index.md" >}})
- [Upload to a Custom Target]({{< relref "docs/guides/transfer/upload-to-custom-target.md" >}})
- [Concept: Transfer and Transport]({{< relref "docs/concepts/transfer-concept.md" >}})
- [Credential Consumer Identities]({{< relref "docs/reference/credential-consumer-identities.md" >}})
- [Sonatype: REST and Integration API](https://help.sonatype.com/en/rest-and-integration-api.html)
- [Sonatype: Helm Repositories](https://help.sonatype.com/en/helm-repositories.html)
- [Sonatype: Maven Repositories](https://help.sonatype.com/en/maven-repositories.html)
- [Sonatype: npm Registry](https://help.sonatype.com/en/npm-registry.html)
- [Sonatype: Raw Repositories](https://help.sonatype.com/en/raw-repositories.html)
