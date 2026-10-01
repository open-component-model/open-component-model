---
title: "Upload npm Packages to JFrog Artifactory"
slug: "npm-packages"
description: "Upload npm package resources into a JFrog Artifactory npm repository during transfer, so that npm install finds them."
weight: 3
toc: true
---

## Goal

Transfer a component version and upload its npm package into an Artifactory npm
repository, so that `npm install` finds it.

## You'll end up with

- The package tarball stored in the Artifactory npm repository `npm-local`
- A transferred resource with a `Wget/v1` access on the stored tarball

**Estimated time:** ~10 minutes

## Prerequisites

- The setup in [JFrog Artifactory]({{< relref "docs/how-to/vendor-specific-apis/jfrog-artifactory/_index.md#prerequisites" >}}),
  with [credentials configured]({{< relref "docs/how-to/vendor-specific-apis/jfrog-artifactory/_index.md#configure-credentials" >}})
- An Artifactory npm repository, here `npm-local`
- A resource that holds an npm package tarball (`package/package.json` plus the package files)
- The `npm` CLI

## Steps

### Configure the uploader

Add the credentials and one uploader rule for the `my-package` resource to your
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
    match: resource.access.isType("LocalBlob") && resource.name == "my-package"
    url: https://myorg.jfrog.io
    repository: npm-local
```

### Run the transfer

```bash
ocm transfer cv ctf::./src//ocm.software/demo:1.0.0 ctf::./target
```

The uploader logs the package Artifactory indexed:

```text
msg="artifactory indexed the npm package" resource="name=my-package,version=2.0.0" package=my-package@2.0.0
```

### Verify

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

## How the repository behaves

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

## Troubleshooting

### Symptom: `content of resource … is not an npm package: …`

The full message reads
`content of resource … is not an npm package: artifactory recorded no npm.name and npm.version for …`.

**Cause:** The resource matched by an npm repository uploader holds no npm package
tarball. Artifactory deletes the uploaded file again.

**Fix:** Narrow `match` to the package resources, or route other resources to a
generic repository.

### Symptom: `npm error notarget No matching version found for … with a date before …`

**Cause:** The npm client is configured with `min-release-age` (or `before`), which
hides versions published less than that many days ago, including freshly transferred
ones.

**Fix:** Install with `--min-release-age=0`, or wait until the version is old enough.

For credential and overwrite errors, see
[Troubleshooting]({{< relref "docs/how-to/vendor-specific-apis/jfrog-artifactory/_index.md#troubleshooting" >}}).

## Related documentation

- [How-to: JFrog Artifactory]({{< relref "docs/how-to/vendor-specific-apis/jfrog-artifactory/_index.md" >}})
- [How-to: Upload npm Packages to Sonatype Nexus]({{< relref "docs/how-to/vendor-specific-apis/sonatype-nexus/npm-packages.md" >}})
- [Reference: JFrog Artifactory Uploader]({{< relref "docs/reference/transfer-configuration/artifactory-uploader.md#repository-types" >}})
- [JFrog: npm Repositories](https://jfrog.com/help/r/jfrog-artifactory-documentation/npm-repositories)
