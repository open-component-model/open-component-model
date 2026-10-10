---
title: "Upload to JFrog Artifactory"
description: "Upload resources into JFrog Artifactory helm, maven, npm and generic repositories during transfer, with credentials and overwrite rules."
weight: 130
toc: true
aliases:
  - /docs/how-to/vendor-specific-apis/jfrog-artifactory/
  - /docs/how-to/vendor-specific-apis/jfrog-artifactory/helm-charts/
  - /docs/how-to/vendor-specific-apis/jfrog-artifactory/maven-artifacts/
  - /docs/how-to/vendor-specific-apis/jfrog-artifactory/npm-packages/
  - /docs/how-to/vendor-specific-apis/jfrog-artifactory/generic-files/
---

{{< callout context="note" title="Start from the base transfer guide" >}}
This guide specializes the default transfer workflow for JFrog Artifactory
targets. If you have not transferred a component version before, read
[Transfer Component Versions]({{< relref "docs/guides/transfer/transfer-component-versions.md" >}})
first, then return here to route resources into Artifactory repositories.
{{< /callout >}}

The
[`artifactory.uploader.transfer.config.ocm.software/v1alpha1`]({{< relref "docs/reference/transfer-configuration/artifactory-uploader.md" >}})
uploader uploads the resources it matches into one Artifactory repository during
transfer. At upload time the uploader reads the package type from the server with
`GET <url>/artifactory/api/repositories/<repository>` and publishes the way that
type expects. For the server-side model shared by all vendor uploaders — type
detection, access rewrite, verification, first-match-wins — see
[Vendor Repository Uploaders]({{< relref "docs/concepts/vendor-uploaders.md" >}}).

The sections below share one scaffold (prerequisites, credentials, transfer,
verify, overwrite rules) and then give the per-format `match`/`path` snippet and
consumer command for each supported repository type.

## Repository types

| Package type | Section                                       | Published access |
|--------------|-----------------------------------------------|------------------|
| `helm`       | [Helm charts](#helm-charts)                   | `Helm/v1`        |
| `maven`      | [Maven artifacts](#maven-artifacts)           | `Wget/v1`        |
| `npm`        | [npm packages](#npm-packages)                 | `Wget/v1`        |
| `generic`    | [Generic files](#generic-files)               | `Wget/v1`        |

### Unsupported repositories

| Repository                   | Error                                                             | Alternative                                                |
|------------------------------|-------------------------------------------------------------------|------------------------------------------------------------|
| `docker`                     | `has package type "docker"; supported: helm, generic, maven, npm` | Transfer images to the OCI registry host of the repository |
| Remote or virtual repository | `uploads need a local repository`                                 | Upload into the local repository behind it                 |

## Prerequisites

- [OCM CLI]({{< relref "docs/getting-started/ocm-cli-installation.md" >}}) installed
- A component version in a CTF or OCI repository
- An Artifactory **local** (or federated) repository
- A user that may deploy into the repository **and read its configuration**

## Configure credentials

The uploader resolves credentials for the `HelmChartRepository` identity of
`<url>/artifactory/api/helm/<repository>`, falling back to the `Wget` identity of
`<url>/artifactory/<repository>`. A `Wget` consumer without a path covers every
repository on the server:

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
```

{{< callout context="caution" >}}
`hostname` is the bare host name. A value such as `https://myorg.jfrog.io`
matches no request, so the upload is sent without credentials and fails with `401`.
{{< /callout >}}

## Existing files at the upload path

When a file is already stored at the upload path, the uploader decides by its content
and owner properties:

- **Same content:** the stored file is reused.
- **Owner properties name the same resource of the same component version:** the file
  is replaced. This is a repeated transfer.
- **Anything else:** the transfer fails and asks for a different `path`.

The property names are listed in the
[reference]({{< relref "docs/reference/transfer-configuration/artifactory-uploader.md#owner-properties" >}}).

## Helm charts

Transfer a component version and upload its Helm chart into an Artifactory `helm`
repository (here `helm-local`), so that `helm pull` finds it. The chart resource is
published with a `Helm/v1` access.

Add the credentials and one uploader rule for the `chart` resource to your
`.ocmconfig` in the working directory (merged with `$HOME/.ocmconfig`):

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
    match: resource.access.isType("LocalBlob") && resource.name == "chart"
    url: https://myorg.jfrog.io
    repository: helm-local
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
  helmRepository: https://myorg.jfrog.io/artifactory/api/helm/helm-local
  helmChart: mychart:0.1.0
```

Pull the chart with Helm:

```bash
helm pull mychart --version 0.1.0 --repo https://myorg.jfrog.io/artifactory/api/helm/helm-local
```

**How the repository behaves**

- The chart is stored under `<component>/<component version>/<resource>-<resource version>.tgz`.
  A custom `path` must end in `.tgz`.
- The published chart name and version come from the chart itself, not from the resource.
- The repository must not enforce chart name and version in file names (*Helm
  Enforce Layout*): the file name comes from the OCM resource, not from the chart.
- A second transfer uploads nothing: the uploader logs
  `reused helm chart content already stored in the helm repository`.

If `content of resource … is not a helm chart` appears, the matched resource holds no
packaged chart; narrow `match` to the chart resources, or route other resources to a
generic repository.

## Maven artifacts

Transfer a component version and upload a JAR and its POM into an Artifactory `maven`
repository (here `maven-local`), so that Maven resolves the artifact from its
coordinates.

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

Maven finds a file only under the Maven layout
`<group path>/<artifactId>/<version>/<artifactId>-<version>.<ext>`. Add the
credentials and one rule per file, each with a CEL `path` in that layout, to your
`.ocmconfig`:

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
    match: resource.access.isType("LocalBlob") && resource.name == "jar"
    url: https://myorg.jfrog.io
    repository: maven-local
    path: '${"com/example/demo/" + resource.version + "/demo-" + resource.version + ".jar"}'
  - type: artifactory.uploader.transfer.config.ocm.software/v1alpha1
    match: resource.access.isType("LocalBlob") && resource.name == "pom"
    url: https://myorg.jfrog.io
    repository: maven-local
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
  url: https://myorg.jfrog.io/artifactory/maven-local/com/example/demo/1.0.0/demo-1.0.0.jar
  mediaType: application/java-archive
```

Resolve the artifact with Maven:

```bash
mvn dependency:get -Dartifact=com.example:demo:1.0.0 \
  -DremoteRepositories=ocm::default::https://myorg.jfrog.io/artifactory/maven-local
```

**How the repository behaves**

- **`latest` and `release`** in `maven-metadata.xml` are the highest version in the
  repository, not the most recently uploaded one. Transferring `0.9.0` after `1.0.0`
  keeps `1.0.0` as `latest` and `release`.
- **Snapshots:** a `-SNAPSHOT` version is stored under a unique timestamped version,
  for example `…/2.0.0-SNAPSHOT/demo-2.0.0-20260928.183609-1.jar`. The published
  `Wget/v1` URL points at that file, so it keeps returning the transferred bytes after
  later snapshots.
- **Without a Maven-layout `path`** the file is stored under the default path. It can
  be downloaded from its `Wget/v1` URL, but Maven does not see it.

If `mvn dependency:get` does not find the version, either no POM was uploaded or the
`path` is not in the Maven layout — upload the POM as its own resource with a
Maven-layout `path`.

## npm packages

Transfer a component version and upload its npm package into an Artifactory `npm`
repository (here `npm-local`), so that `npm install` finds it. The resource must hold
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
          hostname: myorg.jfrog.io
          scheme: https
        credentials:
          - type: WgetCredentials/v1
            identityToken: <ARTIFACTORY_IDENTITY_TOKEN>
  - type: artifactory.uploader.transfer.config.ocm.software/v1alpha1
    match: resource.access.isType("LocalBlob") && resource.name == "my-package"
    url: https://myorg.jfrog.io
    repository: npm-local
```

Run the transfer. The uploader logs the package Artifactory indexed:

```text
msg="artifactory indexed the npm package" resource="name=my-package,version=2.0.0" package=my-package@2.0.0
```

Inspect the result:

```bash
ocm get cv ctf::./target//ocm.software/demo:1.0.0 -o yaml
```

The `my-package` resource has a `Wget/v1` access on the stored tarball:

```yaml
access:
  type: Wget/v1
  url: https://myorg.jfrog.io/artifactory/npm-local/ocm.software/demo/1.0.0/my-package-2.0.0.tgz
```

Install the package with npm:

```bash
npm install my-package@2.0.0 \
  --registry https://myorg.jfrog.io/artifactory/api/npm/npm-local/
```

**How the repository behaves**

- The tarball is stored under `<component>/<component version>/<resource>-<resource version>.tgz`.
  Artifactory reads its `package.json` and serves the version through its npm API.
- **Package name and version come from `package.json`**, not from the OCM resource.
  The file name and a custom `path` do not matter to npm, but a custom `path` must
  end in `.tgz`: Artifactory only indexes `.tgz` files as packages.
- **The `latest` dist-tag moves to the most recently deployed version**, also to an
  older one: transferring `1.0.0` after `2.0.0` makes `1.0.0` the `latest`. Restore it
  after transferring older versions:

  ```bash
  npm dist-tag add my-package@2.0.0 latest \
    --registry https://myorg.jfrog.io/artifactory/api/npm/npm-local/
  ```

- A second transfer reuses the stored tarball: `reused content already stored in the artifactory repository`.

If `content of resource … is not an npm package` appears, the matched resource holds
no npm package tarball; narrow `match` to the package resources, or route other
resources to a generic repository. If `npm error notarget No matching version found …
with a date before …` appears, the npm client has `min-release-age` (or `before`) set
— install with `--min-release-age=0`, or wait until the version is old enough.

## Generic files

Transfer a component version and upload its resources as plain files into an
Artifactory `generic` repository (here `generic-local`), so that consumers download
them by URL. Any access type is accepted and the content is stored as is.

Add the credentials and one uploader rule for all `LocalBlob` resources to your
`.ocmconfig`:

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
    match: resource.access.isType("LocalBlob")
    url: https://myorg.jfrog.io
    repository: generic-local
    # The default path written out, a starting point for your own layout:
    # path: '${component.name + "/" + component.version + "/" + resource.name + "-" + resource.version}'
```

Run the transfer and inspect the result:

```bash
ocm transfer cv ctf::./src//ocm.software/demo:1.0.0 ctf::./target
ocm get cv ctf::./target//ocm.software/demo:1.0.0 -o yaml
```

The `payload` resource has this access:

```yaml
access:
  type: Wget/v1
  url: https://myorg.jfrog.io/artifactory/generic-local/ocm.software/demo/1.0.0/payload-1.0.0
  mediaType: text/plain
```

Download the file:

```bash
curl -fsSL -H "Authorization: Bearer <ARTIFACTORY_IDENTITY_TOKEN>" https://myorg.jfrog.io/artifactory/generic-local/ocm.software/demo/1.0.0/payload-1.0.0
```

**How the repository behaves**

- Any access type is accepted and the content is stored as is.
- OCI images are uploaded as one gzipped OCI layout tar
  (`application/vnd.ocm.software.oci.layout.v1+tar+gzip`).
- A second transfer reuses the stored file: `reused content already stored in the artifactory repository`.

## Troubleshooting

### Symptom: `failed detecting the type of … repository …: GET … returned status 401`

**Cause:** No credentials were found for the server, so the uploader asked anonymously.

**Fix:** Configure credentials as shown above, with the bare host name in `hostname`.

### Symptom: `failed detecting the type of … repository …: GET … returned status 403`

**Cause:** The user may deploy but not read the repository settings.

**Fix:** Grant read access to the repository configuration, or use a user that has it.

### Symptom: `… already stores a file that was not uploaded for this resource …; refusing to overwrite it, configure a different path`

**Cause:** Artifactory stores a file at the upload path that belongs to another
resource, component version or was not uploaded by OCM.

**Fix:** Configure a `path` that is unique for the resource, or remove the stored file.

## Related documentation

- [Concept: Vendor Repository Uploaders]({{< relref "docs/concepts/vendor-uploaders.md" >}})
- [Upload to Sonatype Nexus]({{< relref "docs/guides/transfer/upload-to-sonatype-nexus.md" >}})
- [Reference: JFrog Artifactory Uploader]({{< relref "docs/reference/transfer-configuration/artifactory-uploader.md" >}})
- [Reference: Transfer Configuration]({{< relref "docs/reference/transfer-configuration/_index.md" >}})
- [Upload to a Custom Target]({{< relref "docs/guides/transfer/upload-to-custom-target.md" >}})
- [Concept: Transfer and Transport]({{< relref "docs/concepts/transfer-concept.md" >}})
- [Credential Consumer Identities]({{< relref "docs/reference/credential-consumer-identities.md" >}})
- [JFrog: Local Repositories](https://jfrog.com/help/r/jfrog-artifactory-documentation/local-repositories)
- [JFrog: Helm Chart Repositories](https://jfrog.com/help/r/jfrog-artifactory-documentation/helm-chart-repositories)
- [JFrog: Maven Repositories](https://jfrog.com/help/r/jfrog-artifactory-documentation/maven-repositories)
- [JFrog: npm Repositories](https://jfrog.com/help/r/jfrog-artifactory-documentation/npm-repositories)
- [JFrog: Generic Repositories](https://jfrog.com/help/r/jfrog-artifactory-documentation/generic-repositories)
