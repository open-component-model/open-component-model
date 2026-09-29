---
title: "Upload npm Packages to Sonatype Nexus"
slug: "npm-packages"
description: "Upload npm package resources into a Sonatype Nexus npm hosted repository during transfer, so that npm install finds them."
weight: 3
toc: true
---

## Goal

Transfer a component version and upload its npm package into a Nexus npm hosted
repository, so that `npm install` finds it.

## You'll end up with

- The package tarball stored in the Nexus npm repository `npm-hosted`
- A transferred resource with a `Wget/v1` access on the stored tarball

**Estimated time:** ~10 minutes

## Prerequisites

- The setup in [Upload Resources to Sonatype Nexus]({{< relref "docs/how-to/vendor-specific-apis/sonatype-nexus/_index.md#prerequisites" >}}),
  with [credentials configured]({{< relref "docs/how-to/vendor-specific-apis/sonatype-nexus/_index.md#configure-credentials" >}})
- A Nexus npm hosted repository, here `npm-hosted`
- A resource that holds an npm package tarball (`package/package.json` plus the package files)
- The `npm` CLI

## Steps

### Configure the uploader

Create `config.yaml` with the credentials and one uploader rule for the `my-package` resource:

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
    match:
      accessType: localBlob
      name: my-package
    url: https://nexus.example.com
    repository: npm-hosted
```

### Run the transfer

```bash
ocm transfer cv --config config.yaml ctf::./src//ocm.software/demo:1.0.0 ctf::./target
```

Nexus does not accept plain uploads of package tarballs, so the uploader uses the
components API (`POST /service/rest/v1/components` with the tarball as `npm.asset`).

### Verify

```bash
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

## How the repository behaves

- Nexus reads name and version from `package.json` and stores the tarball under
  `<name>/-/<name>-<version>.tgz` (`@scope/<name>/-/<name>-<version>.tgz` for scoped
  packages).
- The uploader does not change the package: its name, including any scope, is the one
  in `package.json`.
- **The `latest` dist-tag is the highest release version**, not the most recently
  uploaded one: transferring `1.0.0` after `2.0.0` keeps `2.0.0` as `latest`, and a
  prerelease such as `3.0.0-rc.1` does not become `latest`.
- **A second transfer reuses the stored tarball**: the uploader finds it by its SHA-256.
- `path` is not supported: Nexus decides where packages are stored.

## Troubleshooting

### Symptom: `POST …/service/rest/v1/components returned status 400: … Name and version are mandatory fields`

**Cause:** The resource content is not an npm package.

**Fix:** Narrow `match` to the package resources, or route other resources to a raw
repository.

### Symptom: the upload fails with `409`

**Cause:** The repository already stores the version with other content, and
redeploy is disabled.

**Fix:** Publish the package under a new version, or remove the stored version.

For credential and overwrite errors, see
[Troubleshooting]({{< relref "docs/how-to/vendor-specific-apis/sonatype-nexus/_index.md#troubleshooting" >}}).

## Related documentation

- [How-to: Upload Resources to Sonatype Nexus]({{< relref "docs/how-to/vendor-specific-apis/sonatype-nexus/_index.md" >}})
- [How-to: Upload npm Packages to JFrog Artifactory]({{< relref "docs/how-to/vendor-specific-apis/jfrog-artifactory/npm-packages.md" >}})
- [Reference: Sonatype Nexus Uploader]({{< relref "docs/reference/transfer-configuration/nexus-uploader.md#repository-types" >}})
