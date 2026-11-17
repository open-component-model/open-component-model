---
title: "Migrate Component Constructors"
icon: "arrow-autofit-right"
slug: "migrate-component-constructors"
description: "Move OCM v1 component constructors and ocm add commands to ocm add cv: command flags, templating, input types and fields that OCM v2 rejects."
weight: 80
toc: true
---

## Goal

Build the component versions that you built with the OCM v1 CLI with `ocm add component-version` (`ocm add cv`) of the
OCM v2 CLI. This covers `ocm add componentversions` with a component constructor, and the step-by-step flow with
`ocm create componentarchive`, `ocm add resources` and `ocm add references`.

{{< callout context="caution" >}}
OCM v2 replaces every `$NAME` and `${NAME}` in the constructor file with the environment variable `NAME` before it
parses the file. Unset variables become empty strings, and `$$` is no escape. The v1 default templater replaced only
`${NAME}`, so shell snippets and other text with a `$` can change without an error. See
[step 2](#2-replace-templating-with-environment-variables).
{{< /callout >}}

## Why Migrate?

The OCM v2 CLI builds component versions with one command, `ocm add cv`. It has no commands that add single resources
or references, and it does not write component archives. It validates the constructor against a JSON schema before it
builds anything, so some v1 constructors fail until you change them. Several v1 input types have no v2 implementation.

For an overview of all changes, see
[Key Differences Between OCM v1 and v2]({{< docref "docs/changelog/differences-v1-v2.md" >}}).

## Prerequisites

- [OCM CLI]({{< docref "docs/getting-started/ocm-cli-installation.md" >}}) v2 installed
- Your v1 component constructor, settings files and the scripts that call `ocm add componentversions`
- The OCM v1 CLI, only if your constructor uses Go or spiff templates

## Steps

The examples migrate this v1 constructor. It takes `COMPONENT` from a settings file and `VERSION` from the command line:

```yaml
name: ${COMPONENT}
version: ${VERSION}
provider:
  name: acme.org
resources:
  - name: config
    type: blob
    input:
      type: file
      path: config.yaml
      mediaType: application/yaml
  - name: image
    type: ociImage
    version: 6.9.1
    input:
      type: ociImage
      path: ghcr.io/stefanprodan/podinfo:6.9.1
references:
  - name: base
    componentName: acme.org/base
    version: 1.0.0
```

### 1. Replace the commands

| OCM v1 | OCM v2 |
| --- | --- |
| `ocm add componentversions --create --file <ctf> <constructor>` | `ocm add cv --repository <ctf> --constructor <constructor>` |
| `--create`, `--force` | No flag. `ocm add cv` creates the CTF directory if it is missing. It replaces a component version that already exists and drops its signatures. OCM v1 `--create --force` emptied the whole archive; OCM v2 keeps the other component versions. |
| `--type tar`, `--type tgz` | No flag. Build into a directory and pack it with `tar -C transport-archive -czf transport-archive.tgz artifact-index.json blobs`. OCM v1 and v2 read the result. OCM v2 cannot write to a `.tgz` archive. |
| `--version` | No flag. Set `version: ${VERSION}` in the constructor. |
| `KEY=value` arguments, `--settings`, `--addenv`, `--templater` | Environment variables, see step 2 |
| `ocm create componentarchive`, `ocm add resources`, `ocm add references`, `ocm transfer componentarchive` | One constructor with all resources and references, built with one `ocm add cv` call into the target repository |

Without flags, `ocm add cv` reads `component-constructor.yaml` from the current directory and writes to
`./transport-archive`.

{{< tabs "migrate-constructor-commands" >}}
{{< tab "Before" >}}

```bash
ocm add componentversions --create --file ./transport-archive --settings settings.yaml component-constructor.yaml VERSION=1.0.0
```

Or step by step:

```bash
ocm create componentarchive --file ca --provider acme.org acme.org/demo 1.0.0
ocm add resources --file ca resources.yaml
ocm add references --file ca references.yaml
ocm transfer componentarchive ca transport-archive
```

{{< /tab >}}
{{< tab "After" >}}

```bash
export COMPONENT=acme.org/demo
VERSION=1.0.0 ocm add cv --repository ./transport-archive --constructor component-constructor.yaml
```

For the step-by-step flow, copy the entries of `resources.yaml` under `resources:` and the entries of
`references.yaml` under `componentReferences:` of one constructor.

{{< /tab >}}
{{< /tabs >}}

OCM v2 resolves every component reference while it builds. The referenced component version must be in the same
constructor, in the target repository, or in a repository that your
[resolvers]({{< docref "docs/concepts/resolvers.md" >}}) point to. OCM v1 added references without this check.

### 2. Replace templating with environment variables

OCM v1 rendered the constructor with a templater, `subst` by default. The values came from `KEY=value` arguments,
`--settings` files and, with `--addenv`, the environment. OCM v2 has no templater. It expands environment variables in
the file and then parses the result.

If your constructor uses `${NAME}` placeholders with string values only, keep the file and set the values as
environment variables, as shown in step 1. To keep the values in a file, write `KEY=value` lines to `build.env` and
load it with `set -a; . ./build.env; set +a` before you run `ocm add cv`. If a variable is not set, the schema check
fails with `got null, want string` for the field that uses it.

Check every `$` in the constructor. The two versions treat them differently:

| In the constructor | OCM v1 (`subst`) | OCM v2 |
| --- | --- | --- |
| `${NAME}` | Value of `NAME` from arguments or settings | Value of the environment variable `NAME` |
| `$NAME` | Unchanged | Value of the environment variable `NAME` |
| `$1` | Unchanged | Empty |
| `$$` | `$` | Empty |
| `${NAME}` with `NAME` not set | Empty | Empty |

Files that you add with an input, such as `File/v1` or `Dir/v1`, are not expanded. Move scripts and other text with a
`$` into such files. For a `$` in the constructor itself, set a variable to `$` and use it:

```yaml
  - name: hint
    type: PlainText
    input:
      type: UTF8/v1
      text: 'Run: echo ${DOLLAR}HOME'
```

```bash
DOLLAR='$' ocm add cv
```

The resource contains `Run: echo $HOME`.

Go and spiff templates (`--templater go`, `--templater spiff`) need a rendering step. The v1 CLI renders the
constructor with `--dry-run` and writes it to a file without building anything:

```bash
ocm add componentversions --dry-run -O rendered.yaml --templater go --settings settings.yaml component-constructor.yaml VERSION=1.0.0
```

Build the rendered file with the v2 CLI:

```bash
ocm add cv --repository ./transport-archive --constructor rendered.yaml
```

Check `rendered.yaml` first. The v1 CLI writes each component as a separate YAML document, and OCM v2 reads only the
first document of a file. Put several components under a `components:` list. The rendered file can also still contain
`$` characters that OCM v2 expands.

### 3. Replace input types

| OCM v1 input | OCM v2 |
| --- | --- |
| `dir`, `file`, `utf8`, `helm`, `wget`, `git` | `Dir/v1`, `File/v1`, `UTF8/v1`, `Helm/v1`, `Wget/v1`, `Git/v1`. The v1 names still work and print a deprecation warning. |
| `ociArtifact`, `ociImage` | The `OCIImage/v1` access type, see below |
| `docker`, `dockermulti` | An image in a registry, or an OCI image layout file, see below |
| `binary` | Decode the `data` value with `base64 -d` into a file and add the file with `File/v1` |
| `maven`, `npm` | `Wget/v1` with the URL of a single package file, for example `https://registry.npmjs.org/left-pad/-/left-pad-1.3.0.tgz`. npm ([ocm-project#835](https://github.com/open-component-model/ocm-project/issues/835)) and Maven ([ocm-project#836](https://github.com/open-component-model/ocm-project/issues/836)) support is planned. To publish packages to a Maven or npm repository during a transfer, see [Upload to JFrog Artifactory]({{< docref "docs/guides/transfer/upload-to-jfrog-artifactory.md" >}}) and [Upload to Sonatype Nexus]({{< docref "docs/guides/transfer/upload-to-sonatype-nexus.md" >}}). |
| `ocm` | Reference the other component version instead, see [Add Component References]({{< docref "docs/guides/pack/add-component-references.md" >}}) |
| `spiff` | No equivalent |

The `utf8` input no longer accepts `mediaType`. For text with a custom media type, write the text to a file and add it
with `File/v1` and `mediaType`.

#### Images from a registry

The v1 `ociImage` input copied the image into the component version as a local blob. OCM v2 has no such input. Use
the `OCIImage/v1` access type instead:

```yaml
  - name: image
    type: ociImage
    version: 6.9.1
    relation: external
    access:
      type: OCIImage/v1
      imageReference: ghcr.io/stefanprodan/podinfo:6.9.1
```

`ocm add cv` pins the reference to the image digest. The component version references the image, and a plain
`ocm transfer cv` keeps that reference. To copy the image into the target, as the v1 input did, transfer with a local
blob uploader. Save this as `transfer.ocmconfig`:

```yaml
type: generic.config.ocm.software/v1
configurations:
  - type: localblob.uploader.transfer.config.ocm.software/v1alpha1
```

```bash
ocm transfer cv ctf::./transport-archive//acme.org/demo:1.0.0 ctf::./target-archive --config transfer.ocmconfig
```

The image is then a `LocalBlob/v1` in the target. The copy contains all platforms of the image; the v1 `platforms`
filter has no equivalent. For other uploader rules, see
[Migrate --upload-as Flags]({{< docref "docs/changelog/migrate-from-upload-as.md" >}}).

#### Images from a local Docker daemon

No v2 input reads images from a Docker daemon. Either push the image to a registry and reference it as shown above, or
add it as an OCI image layout file:

```yaml
  - name: image
    type: ociImage
    version: 1.0.0
    input:
      type: File/v1
      path: image.tar
      mediaType: application/vnd.ocm.software.oci.layout.v1+tar
```

The tar file must contain `oci-layout` and `index.json` at its root. Check the file that your build tool writes before
you add it:

```bash
tar -tf image.tar | grep -E '^(\./)?(oci-layout|index\.json)$'
```

A gzip-compressed layout works as well. For an unpacked layout directory, use `Dir/v1` with the same `mediaType`. A tar
file in the legacy Docker archive format, with a `manifest.json` but no `oci-layout`, fails with
`invalid OCI Image Layout`. For a full walkthrough, see
[Add OCI Artifacts]({{< docref "docs/guides/pack/add-oci-artifacts.md" >}}).

### 4. Fix fields that OCM v2 rejects or ignores

| OCM v1 constructor | OCM v2 | Change |
| --- | --- | --- |
| `references:` | Fails: `additional properties 'references' not allowed` | Rename to `componentReferences:` |
| `meta.configuredSchemaVersion` | Fails: `additional properties 'meta' not allowed` | Remove it. OCM v2 writes schema version `v2`. |
| `repositoryContexts` | Fails: `additional properties 'repositoryContexts' not allowed` | Remove it |
| `mediaType` on a `utf8` input | Fails: `unknown object member name "mediaType"` | See step 3 |
| Input paths outside the constructor directory, such as `../config.yaml` | Fails: `path escapes from parent` | Move the files, or set `--working-directory` to a directory that contains them |
| Several YAML documents separated by `---` | Builds the first document only, without an error | Put all components under `components:` |
| `options.skipDigestGeneration` on a resource | Ignored. OCM v2 computes the digest. | Use `ocm add cv --skip-reference-digest-processing`. It applies to all resources with an access. |
| `merge` on a label | Dropped from the descriptor | Remove it |

Here is the example constructor after steps 1 to 4:

```yaml
name: ${COMPONENT}
version: ${VERSION}
provider:
  name: acme.org
resources:
  - name: config
    type: blob
    input:
      type: File/v1
      path: config.yaml
      mediaType: application/yaml
  - name: image
    type: ociImage
    version: 6.9.1
    relation: external
    access:
      type: OCIImage/v1
      imageReference: ghcr.io/stefanprodan/podinfo:6.9.1
componentReferences:
  - name: base
    componentName: acme.org/base
    version: 1.0.0
```

### 5. Build and check the component version

Build it with the command from step 1, then inspect the result:

```bash
ocm get cv ./transport-archive//acme.org/demo:1.0.0 -o yaml
```

`ocm add cv` prints no deprecation warnings once all types use their v2 names. The descriptor differs from the one
that OCM v1 built from the same input. Some media types change, for example `application/x-tar+gzip` instead of
`application/x-tgz` for a compressed `dir` input, and Helm charts are stored as OCI manifests. The digests of these
resources change as well. Build a new component version instead of replacing a published or signed one.

## Related Documentation

- [Key Differences Between OCM v1 and v2]({{< docref "docs/changelog/differences-v1-v2.md" >}}) - All changes from v1
  to v2
- [Reference: Component Constructor]({{< docref "docs/reference/component-constructor.md" >}}) - Constructor fields
- [Reference: Input and Access Types]({{< docref "docs/reference/input-and-access-types.md" >}}) - All input and access
  types and their fields
- [Migrate from Sources to Resources]({{< docref "docs/changelog/migrate-from-sources.md" >}}) - Replace `sources`
  and `srcRefs` in the constructor
