---
title: "Key Differences Between OCM v1 and v2"
description: "Moving from OCM v1 to OCM v2: migration order, your v1 tasks in v2, breaking changes and every difference, with the guide for each."
icon: "arrow-autofit-right"
slug: "differences-v1-v2"
weight: 5
toc: true
---

OCM v2 is a rebuild of the OCM CLI, the Kubernetes controllers and the Go library. Both versions build on the
[OCM specification](https://github.com/open-component-model/ocm-spec), and OCM v2 reads most component versions that
OCM v1 created. Commands, flags, configuration, plugins and the library API changed, and some behavior changed without
an error message. This page gives you a migration order, shows how your daily OCM v1 tasks look in OCM v2, and lists
every difference with the guide that covers it.

This page compares the OCM v1 CLI 0.55 with the current OCM v2 release. The
[legacy documentation](https://ocm.software/legacy/docs/) still describes OCM v1. If you are new to OCM v2, start with
[Getting Started]({{< docref "docs/getting-started/_index.md" >}}) and [How OCM Works]({{< docref "docs/overview/how-ocm-works.md" >}}).

## Migrate Step by Step

Component versions that OCM v1 published keep working. You do not have to rebuild or re-sign them before you switch,
except for the cases listed under [Data Compatibility](#data-compatibility). Move in this order:

1. **Install OCM v2 next to OCM v1.** Both binaries are called `ocm`, and the Homebrew formula still installs OCM v1,
   so keep one of them under another name while you migrate. See
   [Install the OCM CLI]({{< docref "docs/getting-started/ocm-cli-installation.md" >}}).
2. **Give OCM v2 its own configuration file.** OCM v1 rejects most v2 configuration types, and OCM v2 fails on some
   v1 entries. Pass the v2 file with `--config`, which replaces the default search. See [Configuration](#configuration).
3. **Check your existing component versions with OCM v2.** Run `ocm get cv` and `ocm verify cv` against the
   repositories you publish to. These commands only read. Compare the results with
   [Data Compatibility](#data-compatibility).
4. **Move credentials and resolvers.** See
   [Migrate Legacy Credentials]({{< docref "docs/changelog/migrate-legacy-credentials.md" >}}) and
   [Migrate Legacy Resolvers]({{< docref "docs/changelog/migrate-legacy-resolvers.md" >}}).
5. **Move verification before signing.** OCM v1 cannot verify signatures that OCM v2 creates. Switch every verifier
   to OCM v2 first, then the signers. See [Migrate Signing Setup]({{< docref "docs/changelog/migrate-signing-setup.md" >}}).
6. **Move your builds.** Convert component constructors and transfer settings. See
   [Migrate Component Constructors]({{< docref "docs/changelog/migrate-component-constructors.md" >}}),
   [Migrate --upload-as Flags]({{< docref "docs/changelog/migrate-from-upload-as.md" >}}) and
   [Migrate from Sources to Resources]({{< docref "docs/changelog/migrate-from-sources.md" >}}).
7. **Update scripts and pipelines.** Check output formats and exit codes, see
   [Scripts and CI pipelines](#scripts-and-ci-pipelines). In GitHub Actions, use
   `open-component-model/ocm-setup-action@v2`.
8. **Move Kubernetes deployments.** See
   [Migrate from the Legacy OCM Controller]({{< docref "docs/changelog/migrate-from-legacy-ocm-controller.md" >}}).

## Your OCM v1 Tasks in OCM v2

| Task | OCM v1 | OCM v2 | Guide |
| ---- | ------ | ------ | ----- |
| Build a component version | `ocm add componentversions --create --file ./ctf component-constructor.yaml` | `ocm add cv --repository ./ctf --constructor component-constructor.yaml` | [Create Component Versions]({{< docref "docs/getting-started/create-component-version.md" >}}) |
| Inspect a component version | `ocm get cv ./ctf//acme.org/app:1.0.0 -o yaml` | `ocm get cv ./ctf//acme.org/app:1.0.0 -o yaml` | [Reference: ocm get cv]({{< docref "docs/reference/ocm-cli/ocm_get_component-version.md" >}}) |
| Configure registry credentials | Consumers in `~/.ocmconfig` | Consumers in the configuration file, mostly unchanged | [Configure Registry Credentials]({{< docref "docs/guides/transfer/configure-registry-credentials.md" >}}) |
| Sign | `ocm sign cv --signature acme --private-key rsa.priv <ref>` | `ocm sign cv --signature acme <ref>`, with the key in the configuration | [Sign with Plain RSA]({{< docref "docs/guides/sign/sign-with-plain-rsa.md" >}}) |
| Verify | `ocm verify cv --signature acme --public-key rsa.pub <ref>` | `ocm verify cv --signature acme <ref>`, with the key in the configuration | [Verify Plain RSA]({{< docref "docs/guides/sign/verify-with-plain-rsa.md" >}}) |
| Transfer to a registry | `ocm transfer cv --copy-resources <ref> ghcr.io/acme/target` | `ocm transfer cv <ref> ghcr.io/acme/target`, with an uploader configuration to copy resources | [Transfer Component Versions]({{< docref "docs/guides/transfer/transfer-component-versions.md" >}}) |
| Transfer across an air gap | `ocm transfer cv --copy-resources <ref> ./ctf` | `ocm transfer cv <ref> ./ctf`, with an uploader configuration | [Transfer Components across an Air Gap]({{< docref "docs/guides/transfer/air-gap-transfer.md" >}}) |
| Download a resource | `ocm download resources <ref> my-resource -O out` | `ocm download resource <ref> --identity name=my-resource --output out` | [Download Resources]({{< docref "docs/guides/transfer/download-resources.md" >}}) |
| Reference other components | References plus fallback resolvers | References plus glob resolvers | [Add Component References]({{< docref "docs/guides/pack/add-component-references.md" >}}) |
| Deploy to Kubernetes | `ocm-controller` with Flux | OCM controllers | [Deploy Helm Charts]({{< docref "docs/getting-started/deploy-helm-chart.md" >}}) |
| Look up types and fields | Help topics such as `ocm ocm-accessmethods` | `ocm describe types` | [Input and Access Types]({{< docref "docs/reference/input-and-access-types.md" >}}) |

## Breaking Changes

These changes can break a pipeline or change data without an error. Check them first.

| Change | Impact | What to do |
| ------ | ------ | ---------- |
| `ocm transfer cv` replaces a component version that already exists in the target, and drops the signatures stored there | OCM v1 skipped an identical version and refused a changed one without `--overwrite`. OCM v2 exits 0. | Sign after the transfer, or protect published versions with immutable tags in the registry. See [Transfer Component Versions]({{< docref "docs/guides/transfer/transfer-component-versions.md" >}}). |
| Running `ocm add cv` again replaces the component version and drops its signatures | A constructor that lacks a resource or reference removes it from the version | Build a new version instead of rebuilding a published one. See [Migrate Component Constructors]({{< docref "docs/changelog/migrate-component-constructors.md" >}}). |
| Copying Helm resources by value rewrites their digest | After `--copy-resources` or a local blob uploader, OCM v1 signatures fail in OCM v1 and OCM v2 ([ocm-project#1368](https://github.com/open-component-model/ocm-project/issues/1368)) | Transfer Helm resources by reference, or sign again after the transfer |
| Some OCM v1 signatures do not verify | See [Data Compatibility](#data-compatibility) | Re-sign with OCM v2. See [Migrate Signing Setup]({{< docref "docs/changelog/migrate-signing-setup.md" >}}). |
| OCM v1 cannot verify signatures that OCM v2 creates | OCM v2 signs with `jsonNormalisation/v4alpha3`, which OCM v1 does not know | Move all verifiers to OCM v2 before you sign with it |
| Some OCM v1 configuration entries make every command fail | `HashiCorpVault`, `GardenerConfig` and `NPMConfig` credential repositories, an inline `dockerConfig` object, and resolvers with the repository type `OCIRegistry` | Remove or convert them, see [Configuration](#configuration) |
| Most other OCM v1 configuration types are ignored without a message | For example `transport` with `recursive: true`: `ocm transfer cv` is then no longer recursive | Pass `--recursive` or use the v2 transfer configuration, see [Configuration](#configuration) |
| Credential matching is stricter | A consumer without `port` no longer matches a request with a port, and one without `hostname` matches nothing | See [Credentials](#credentials) |
| Script-facing output changed | `get cv` output formats, sort order and exit codes differ | See [Scripts and CI pipelines](#scripts-and-ci-pipelines) |
| The legacy controller and the new controllers cannot share a cluster | Both define `resources.delivery.ocm.software` | See [Migrate from the Legacy OCM Controller]({{< docref "docs/changelog/migrate-from-legacy-ocm-controller.md" >}}) |

## Specification Support

| Topic | OCM v1 | OCM v2 |
| ----- | ------ | ------ |
| Component descriptor schema | `v2` and `ocm.software/v3alpha1` | `v2` only |
| Repositories | OCI registries, CTF (directory, `tar`, `tgz`), component archives | OCI registries and CTF. Writes directory CTFs only, reads `tar` and `tgz`. No component archives. |
| Normalisation algorithms | `jsonNormalisation/v1`, `v2`, `v3` (default) | Signs with `jsonNormalisation/v4alpha3` (default, recommended), `v4alpha2` or `v4alpha1`. Verifies `v3` and `v1`, see [Data Compatibility](#data-compatibility). |
| Signing | RSA (`RSASSA-PKCS1-V1_5` default, `RSASSA-PSS`), certificate chains, Sigstore | RSA (`RSASSA-PSS` default), certificate chains, GPG, Sigstore keyless. PSS, GPG and Sigstore are not defined in the specification. |
| Sources | Supported | Deprecated in OCM v2, replaced by resources. The specification does not deprecate them. |
| Label `merge` | Supported | Dropped on read and transfer. Deprecated in the specification ([ocm-spec#155](https://github.com/open-component-model/ocm-spec/pull/155)). |

How OCM v2 stores component versions in OCI registries is described in
[Component Version Storage]({{< docref "docs/concepts/component-storage.md" >}}), the descriptor format in
[Component Descriptor]({{< docref "docs/reference/component-descriptor.md" >}}).

## CLI

The v2 CLI is built around the component version and has far fewer commands. Commands that edited or inspected one
part of a component version are gone. For all commands and flags, see the
[CLI reference]({{< docref "docs/reference/ocm-cli/ocm.md" >}}).

| OCM v1 | OCM v2 |
| ------ | ------ |
| `ocm add componentversions` | `ocm add component-version`, see [Migrate Component Constructors]({{< docref "docs/changelog/migrate-component-constructors.md" >}}) |
| `ocm add resources`, `add sources`, `add references`, `create componentarchive`, `create transportarchive` | No command. Describe the whole component version in one constructor and run `ocm add cv`, which creates a missing CTF directory. |
| `ocm get componentversions` | `ocm get component-version` |
| `ocm get resources`, `get sources`, `get references` | `ocm get cv -o yaml`, or `-o widetree` for resources |
| `ocm list componentversions`, `show versions` | `ocm get cv <component>` with `--constraint` or `--latest`. Versions are listed newest first. |
| `ocm sign componentversions`, `verify componentversions` | `ocm sign component-version`, `ocm verify component-version`, see [Migrate Signing Setup]({{< docref "docs/changelog/migrate-signing-setup.md" >}}) |
| `ocm transfer componentversions`, `transfer commontransportarchive` | `ocm transfer component-version`. A repository as source transfers all its component versions. See [Transfer Component Versions]({{< docref "docs/guides/transfer/transfer-component-versions.md" >}}). |
| `ocm download resources` | `ocm download resource`, see [Download Resources]({{< docref "docs/guides/transfer/download-resources.md" >}}) |
| `ocm download componentversions` | `ocm transfer cv <reference> <directory>` writes a CTF |
| `ocm install plugins` | `ocm download plugin` and `ocm plugin registry`, see [Plugins](#plugins) |
| `ocm create rsakeypair` | `openssl`, see [Generate Signing Keys]({{< docref "docs/guides/sign/generate-signing-keys.md" >}}) |
| `ocm controller install` | Helm, see [Set up Controller Environments]({{< docref "docs/getting-started/setup-controller-environment.md" >}}) |
| `ocm check componentversions` | `ocm get cv <reference> --recursive` fails when a referenced component version is missing |
| `ocm hash componentversions`, `sign hash` | No command yet ([open-component-model#1227](https://github.com/open-component-model/open-component-model/issues/1227)) |
| `ocm cache`, `clean cache` | No command. OCM v2 caches OCI metadata for 10 minutes in its temporary folder, see [Caching]({{< docref "docs/reference/configuration/caching.md" >}}). |
| `ocm bootstrap`, TOI packages | No command ([ocm-project#839](https://github.com/open-component-model/ocm-project/issues/839)). Deploy with the [OCM controllers]({{< docref "docs/concepts/ocm-controllers.md" >}}). |
| `ocm download cli` | No command ([ocm-project#1094](https://github.com/open-component-model/ocm-project/issues/1094)). See [Install the OCM CLI]({{< docref "docs/getting-started/ocm-cli-installation.md" >}}). |
| `ocm get artifacts`, `transfer artifacts`, `show tags`, `oci ...` | No command. `ocm transfer cv` copies component versions only. Use an OCI tool such as `oras` for plain OCI artifacts. |
| `ocm add routingslips`, `get routingslips`, `execute action`, `get pubsub`, `set pubsub`, `get credentials`, `get verified` | No command |

`ocm describe types` and `ocm get types` are new. They list the access, input, repository, credential and signing
types and their fields. Configuration types are not listed; see
[Configuration]({{< docref "docs/reference/configuration/_index.md" >}}).

### Scripts and CI pipelines

- **Command names.** `componentversions`, `componentversion` and `cv` still work. `component`, `comp` and `c` work for
  `get`, `sign`, `verify` and `transfer`, but not for `add`. The resource aliases `res` and `r` are gone.
- **Short flags changed meaning.** In `ocm add cv`, `-c` is `--constructor` (v1: `--create`) and `-r` is
  `--repository`. In `ocm get cv`, `-r` is gone and `--recursive` takes a depth. `-c`/`--constraints` is now
  `--constraint`. In `sign` and `verify`, `-s` is gone and `--signature` takes one name.
- **One reference per call.** `get`, `sign` and `verify` take one reference, and `sign` and `verify` need a version.
  `--repo` is gone: always pass the full reference, such as `ghcr.io/acme//acme.org/app:1.0.0` or
  `ctf::./archive//acme.org/app:1.0.0`. A reference can pin a digest, such as `...:1.0.0@sha256:<digest>`.
- **Rejected reference forms.** The `+` prefix that created a repository, `directory::`, `tgz::`, `ctf+tgz::`, JSON
  repository specifications and the type name `OCIRegistry::` fail. Use `ctf::`, `oci::` or `OCIRepository::`.
  `http://` needs `plainHTTP`, see
  [Migrate from http:// Prefixes]({{< docref "docs/changelog/migrate-from-http-prefixes.md" >}}).
- **Archives.** OCM v2 writes CTF directories only. To ship a `tgz`, pack the directory with `tar`.
- **Output.** `ocm get cv` prints a tree by default, newest version first. A script that takes the last line of the v1
  table now gets the oldest version. `-o json` prints a list instead of `{"items": [...]}`, and `-o yaml` prints one
  YAML list instead of one document per component version. `-o wide` is gone.
- **Exit codes.** `ocm get cv` with no matching version, and `ocm verify cv` on a component version without
  signatures, exit 1. OCM v1 exited 0.
- **Logging.** INFO messages go to stderr by default. Use `--loglevel warn` to quiet them. `--logfile`,
  `--logconfig` and `--logkeys` are gone.
- **Transfer.** `--overwrite`, `--enforce`, `--script`, `--uploader`, `--copy-local-resources` and
  `--omit-access-types` are gone. Local blobs are copied by default, and uploader configurations decide what else is
  copied. `--copy-resources` and `--upload-as` still work but are deprecated, see
  [Migrate --upload-as Flags]({{< docref "docs/changelog/migrate-from-upload-as.md" >}}). To review a transfer before it
  runs, write the plan to a file and replay it:

  ```bash
  ocm transfer cv --dry-run -o yaml ghcr.io/acme//acme.org/app:1.0.0 ctf::./archive > spec.yaml
  ocm transfer cv --transfer-spec spec.yaml
  ```

- **Download.** `-O` is `--output`, and the resource identity moves to `--identity`. Archives are extracted by
  default; `--extraction-policy disable` keeps them as they are. The executable bit is not set. Download handlers are
  replaced by `--transformer`, which supports `helm`.
- **Mixed pipelines.** OCM v2 writes CTF files read-only, so OCM v1 cannot write to a CTF that OCM v2 has written.
  Run all steps that write a CTF with the same version.

## Component Constructor

The constructor still describes one component or a `components:` list. The changes, with the steps to migrate, are in
[Migrate Component Constructors]({{< docref "docs/changelog/migrate-component-constructors.md" >}}):

- No templating: OCM v2 expands environment variables (`$NAME` and `${NAME}`) and nothing else. Every `$` counts.
- The file is validated before the build. `references:` must be `componentReferences:`, and `meta` and
  `repositoryContexts` are rejected. Only the first YAML document is read.
- Component references must resolve while the version is built.
- Input types keep their v1 names as deprecated aliases. `ociImage`, `docker`, `binary`, `spiff`, `maven`, `npm` and
  `ocm` inputs are gone. For images, see [Add OCI Artifacts]({{< docref "docs/guides/pack/add-oci-artifacts.md" >}}).
  For component versions, use [component references]({{< docref "docs/guides/pack/add-component-references.md" >}}).

For all types and fields, see
[Component Constructor]({{< docref "docs/reference/component-constructor.md" >}}) and
[Input and Access Types]({{< docref "docs/reference/input-and-access-types.md" >}}).

## Configuration

The file format is the same: a `generic.config.ocm.software/v1` document with a `configurations:` list.

**Discovery.** OCM v1 read the first of `~/.ocmconfig`, `~/.ocm/ocmconfig` and `~/.ocm/config`. OCM v2 merges every
file it finds: `$OCM_CONFIG`, `ocm/config` and `.ocmconfig` under `$XDG_CONFIG_HOME` or `~/.config`, `~/.ocm/config`,
`~/.ocmconfig`, then `.ocm/config` and `.ocmconfig` in the working directory and next to the binary. Later files take
priority. Running `ocm` inside a checkout therefore picks up the checkout's `.ocmconfig`. `--config` replaces the
search. On first start, OCM v2 writes `~/.ocm/config` with your Docker credentials if you have none (turn off with
`OCM_DISABLE_AUTO_CONFIG`), and it checks for new releases after each command (`OCM_DISABLE_VERSION_CHECK`).

**Configuration types:**

| OCM v1 type | OCM v2 |
| ----------- | ------ |
| `credentials.config.ocm.software` | Works, with changes, see [Credentials](#credentials) |
| `ocm.config.ocm.software` resolvers | Deprecated, still read. Use `resolvers.config.ocm.software/v1alpha1`, see [Migrate Legacy Resolvers]({{< docref "docs/changelog/migrate-legacy-resolvers.md" >}}). A resolver with `type: OCIRegistry` makes every command fail; use `OCIRepository`. |
| `http.config.ocm.software` | Works. The v2 type adds retries, TLS roots and per-host `plainHTTP`, see [HTTP Client]({{< docref "docs/reference/configuration/http-client.md" >}}). |
| `transport.ocm.config.ocm.software`, `uploader.ocm.config.ocm.software`, `scripts.ocm.config.ocm.software`, `oci.uploader.config.ocm.software` | Ignored. Use `transfer.config.ocm.software/v1alpha1` and the uploader configurations, see [Transfer Configuration]({{< docref "docs/reference/configuration/transfer-configuration/_index.md" >}}). |
| `keys.config.ocm.software` | Ignored. Keys are credentials, see [Configure Signing Credentials]({{< docref "docs/guides/sign/configure-signing-credentials.md" >}}). |
| `rootcerts.config.ocm.software` | Ignored. Use `rootCAsPEM` or `rootCAsPEMFile` in the [HTTP client configuration]({{< docref "docs/reference/configuration/http-client.md" >}}). |
| `downloader.ocm.config.ocm.software` | Ignored. Use `ocm download resource --transformer`. |
| `logging.config.ocm.software` | Ignored. Use `--loglevel`, `--logformat` and `--logoutput`. |
| `plugin.config.ocm.software` | The type name now configures the v2 plugin manager. v1 settings are dropped. |
| `attributes`, `memory.credentials`, `oci`, `cli.ocm`, `ocm.cmd`, `hasher`, `blobLimits.ocireg.ocm`, `merge` (all `.config.ocm.software`), config `sets` | Ignored |

Ignored types produce no message, even with `--loglevel debug`. Spiff expressions in the configuration file are not
evaluated, and the `--cred`, `-X` and `--config-set` flags are gone. New types configure signing, transfer, uploaders,
caching, versioning and the file system; see [Configuration]({{< docref "docs/reference/configuration/_index.md" >}}).

OCM v1 rejects most v2 configuration types, so one file cannot serve both CLIs unless it uses only credentials and
`http.config.ocm.software`.

## Credentials

The credential configuration keeps its format. The details are in
[Migrate Legacy Credentials]({{< docref "docs/changelog/migrate-legacy-credentials.md" >}}):

- `pathprefix` is converted to `path`, which is a glob: `*` matches one path segment and `**` any depth.
- A consumer without `port` no longer matches a request that has a port, such as `localhost:5000`. A consumer without
  `hostname` matches nothing.
- When several consumers match a request, OCM v2 does not prefer the most specific one, and the one it uses can change
  between runs. Do not configure overlapping consumers for the same host and path.
- Only `DockerConfig/v1` is built in as a credential repository. `HashiCorpVault`, `GardenerConfig` and `NPMConfig`
  make every command fail ([ocm-project#838](https://github.com/open-component-model/ocm-project/issues/838) tracks
  Vault). An inline `dockerConfig` must be a JSON string. `aliases` and memory credentials are dropped.
- Typed credentials such as `OCICredentials/v1` are new and optional. `Credentials/v1` keeps working.

For the model behind it, see [Credential System]({{< docref "docs/concepts/credential-system.md" >}}),
[Credential Consumer Identities]({{< docref "docs/reference/configuration/credential-consumer-identities.md" >}}) and
[Credential Types]({{< docref "docs/reference/configuration/credential-types.md" >}}).

## Transfer and Download

- **Existing versions.** `ocm transfer cv` replaces a component version that the target already has, without a
  warning, and drops the signatures stored in the target. This is intended: protect published versions with
  immutable tags in the registry.
- **What is copied.** Local blobs are copied, everything else stays by reference unless an uploader configuration
  selects it. Uploaders exist for OCI registries, local blobs, HTTP, Git, JFrog Artifactory and Sonatype Nexus. See
  [Transfer and Transport]({{< docref "docs/concepts/transfer-concept.md" >}}),
  [Transfer Configuration]({{< docref "docs/reference/configuration/transfer-configuration/_index.md" >}}) and
  [Vendor Repository Uploaders]({{< docref "docs/concepts/vendor-uploaders.md" >}}).
- **Resources that are not copied.** `npm`, `maven`, `ocm`, `none` and `s3/v1` resources stay by reference with a
  warning, and cannot be downloaded with OCM v2. Support for npm
  ([ocm-project#835](https://github.com/open-component-model/ocm-project/issues/835)) and Maven
  ([ocm-project#836](https://github.com/open-component-model/ocm-project/issues/836)) is planned. To publish Maven and
  npm packages during a transfer, see
  [Upload to JFrog Artifactory]({{< docref "docs/guides/transfer/upload-to-jfrog-artifactory.md" >}}) and
  [Upload to Sonatype Nexus]({{< docref "docs/guides/transfer/upload-to-sonatype-nexus.md" >}}).
- **Sources.** External sources stay by reference. A component version with a source stored as a local blob cannot be
  transferred. See [Migrate from Sources to Resources]({{< docref "docs/changelog/migrate-from-sources.md" >}}).
- **Air gaps** work as before with CTF directories, see
  [Transfer Components across an Air Gap]({{< docref "docs/guides/transfer/air-gap-transfer.md" >}}).

## Signing and Verification

- **Keys come from the configuration.** The v1 flags `--private-key`, `--public-key`, `--issuer`, `--ca-cert`,
  `--keyless` and `--algorithm` are gone, and `keys.config.ocm.software` is ignored. See
  [Migrate Signing Setup]({{< docref "docs/changelog/migrate-signing-setup.md" >}}) and
  [Configure Signing Credentials]({{< docref "docs/guides/sign/configure-signing-credentials.md" >}}).
- **New defaults.** New signatures use `RSASSA-PSS` and `jsonNormalisation/v4alpha3`. OCM v1 used
  `RSASSA-PKCS1-V1_5` and `jsonNormalisation/v3`. Keep the default `jsonNormalisation/v4alpha3`, see
  [Migrate Normalisation Algorithms]({{< docref "docs/changelog/migrate-normalisation-algorithms.md" >}}).
- **What `ocm verify cv` checks.** It verifies each signature against the descriptor digest. It does not download
  resources or referenced component versions to recompute their digests, as `ocm verify -V` did in OCM v1 ([#1437](https://github.com/open-component-model/ocm-project/issues/1437)).
  Resource digests are checked on download and when a transfer copies the resource. A component version without
  signatures fails.
- **Signers.** RSA with plain keys or PEM certificate chains, GPG, and Sigstore keyless. See the guides under
  [Sign]({{< docref "docs/guides/sign/_index.md" >}}).
- **Timestamps.** RFC 3161 timestamps from OCM v1 are checked without extra flags. Their time cannot prove when the
  signature was made, because OCM v1 timestamps the descriptor digest. `ocm verify cv
  --insecure-trust-legacy-timestamps` lets that time validate an expired signing certificate, so a signature created
  after the certificate expired can pass. Use the flag only for component versions you know OCM v1 signed in time.

## Data Compatibility

OCM v2 reads component versions that OCM v1 created in OCI registries and CTF archives. A plain `ocm transfer cv`
keeps their descriptors and signatures intact, and OCM v1 signatures still verify in OCM v1 and OCM v2 afterwards.
OCM v2 stores embedded OCI artifacts differently, see
[OCM v1 and OCM v2 storage]({{< docref "docs/concepts/component-storage.md#ocm-v1-and-ocm-v2" >}}).

This v1 data works in OCM v2 for get, verify, download and transfer:

- CTF directories and CTF `tar` and `tgz` archives, including transfer of a whole archive
- RSA signatures with `jsonNormalisation/v3`, the v1 default, with `RSASSA-PKCS1-V1_5` or `RSASSA-PSS` and SHA-256 or
  SHA-512, apart from the exceptions below
- Certificate chain signatures without the root certificate, and RFC 3161 timestamps
- Resources with the access types `ociArtifact`, `localBlob`, `localOciBlob`, `helm`, `github`, `wget` and `Git/v1`,
  under their v1 names. `s3` resources are read and transferred by reference. For the field changes of Git and S3, see
  [Input and Access Types]({{< docref "docs/reference/input-and-access-types.md#git-migration-from-ocm-v1" >}}).
- Component references with digests, `extraIdentity`, versions with `+` build metadata, and external sources

The following v1 data does not work in OCM v2:

| v1 data | Behavior in OCM v2 | Tracking |
| ------- | ------------------ | -------- |
| Component archives (`ocm create componentarchive`) | Rejected with `component archive ... is not supported`. Transfer them into a CTF with the v1 CLI first. | [#1406](https://github.com/open-component-model/ocm-project/issues/1406) |
| Descriptors in the `ocm.software/v3alpha1` schema | Cannot be read. The error does not name the schema yet, for example `parsing identity "name=,version="`. | [#1407](https://github.com/open-component-model/ocm-project/issues/1407) |
| OCI repositories with `componentNameMapping: sha256-digest` | Components are not found | [#1405](https://github.com/open-component-model/ocm-project/issues/1405) |
| Signatures with `jsonNormalisation/v2` | Cannot be verified | [#1403](https://github.com/open-component-model/ocm-project/issues/1403) |
| Signatures with `jsonNormalisation/v3` over a signed label with `value: null` | Digest mismatch | [#1402](https://github.com/open-component-model/ocm-project/issues/1402) |
| Signatures with `jsonNormalisation/v3` over reference labels with `merge` | Digest mismatch. The specification excludes this case. | [ocm-spec](https://github.com/open-component-model/ocm-spec/blob/9cc597a84484e24524b538442aa9d3f87adfac82/doc/04-extensions/04-algorithms/component-descriptor-normalization-algorithms.md#L261-L263) |
| Signatures with `jsonNormalisation/v1` | OCM v2 computes this algorithm as cc-utils did. Signatures from the OCM v1 CLI fail with a digest mismatch, for example for resource types `blob`, `helmChart`, `filesystem` and `ociArtifact`, or with provider labels. See [Migrate Signing Setup]({{< docref "docs/changelog/migrate-signing-setup.md" >}}). | [#1434](https://github.com/open-component-model/ocm-project/issues/1434) |
| Several v1 signatures with different hash or normalisation algorithms on a component version with references | Only the signature created last verifies | [#1436](https://github.com/open-component-model/ocm-project/issues/1436) |
| Certificate chains with an embedded self-signed root (v1 `create rsakeypair --ca-cert`) | Rejected | [#1376](https://github.com/open-component-model/ocm-project/issues/1376) |
| Certificate chain signatures whose `issuer` names only part of the certificate subject | Rejected with `issuer mismatch`. Signatures written by the v1 CLI record the full subject and are not affected. | [#1418](https://github.com/open-component-model/ocm-project/issues/1418) |
| Sigstore signatures from the v1 CLI | Cannot be verified. `ocm verify cv` without `--signature` fails as well. Re-sign with the v2 [Sigstore signer]({{< docref "docs/guides/sign/sign-with-sigstore.md" >}}). | [#1376](https://github.com/open-component-model/ocm-project/issues/1376) |
| Private keys encrypted with v1 `create rsakeypair --encrypt` | Cannot be read | |
| Helm resources copied with `--copy-resources` or a local blob uploader | Digest is rewritten and v1 signatures fail. The new digest changes with every transfer. | [#1368](https://github.com/open-component-model/ocm-project/issues/1368), [#1435](https://github.com/open-component-model/ocm-project/issues/1435) |
| Resources with the access type `git` or `git/v1alpha1` | Download and copy fail with a digest mismatch. Verify and transfer by reference work. | [#1433](https://github.com/open-component-model/ocm-project/issues/1433) |
| Resources with the access types `npm`, `maven`, `ocm` or `none` | Cannot be downloaded or copied | [#1376](https://github.com/open-component-model/ocm-project/issues/1376) |
| Resources with the access type `s3/v1` | `--copy-resources` keeps them by reference | [#1404](https://github.com/open-component-model/ocm-project/issues/1404) |
| Sources stored as local blobs | The component version cannot be transferred | [Migrate from Sources to Resources]({{< docref "docs/changelog/migrate-from-sources.md" >}}) |
| Label `merge` specifications | Dropped on read and transfer. Signatures still verify, because `merge` is not signed. | [#1371](https://github.com/open-component-model/ocm-project/issues/1371) |
| Routing slips | Kept as a label and transferred, but OCM v2 has no commands for them | |

All tracking issues are in `open-component-model/ocm-project` and collected in the
[OCM v1 to v2 compatibility epic](https://github.com/open-component-model/ocm-project/issues/1366).

## Plugins

OCM v2 has a new plugin protocol, and v1 plugins do not run in it. v2 plugins add repository types, access and input
types, credential repositories, signing handlers and blob transformers. Actions, value sets, label merge handlers and CLI
commands have no plugin counterpart. The v1 JFrog plugin is replaced by the built-in
[Artifactory uploader]({{< docref "docs/reference/configuration/transfer-configuration/artifactory-uploader.md" >}}).
The ECR plugin has no counterpart. The plugin directory moved from `~/.ocm/plugins` to `~/.config/ocm/plugins`. See
[Plugin System]({{< docref "docs/concepts/plugin-system.md" >}}).

## Controllers

The new controllers replace the legacy `ocm-controller`. Both use the API group `delivery.ocm.software` and define a
`Resource` kind with different schemas, so remove the legacy controller and its CRDs before you install the new one.

| Legacy kind | New kind |
| ----------- | -------- |
| `ComponentVersion` | `Repository` and `Component` |
| `ComponentVersion` with `destination` | `Replication` |
| `Resource` | `Resource`, with a new schema |
| `FluxDeployer` | `NamespacedDeployer` for plain manifests. Helm charts stay with a Flux `HelmRelease`. |
| `ComponentDescriptor`, `Snapshot` | None. The descriptor is in the `Component` status, and there is no in-cluster OCI registry. |
| `Configuration`, `Localization` | None. Localize at build time or in the `HelmRelease` values. |

`Discovery` is new, and the cluster-scoped `Deployer` is deprecated in favor of `NamespacedDeployer`. For the steps,
see [Migrate from the Legacy OCM Controller]({{< docref "docs/changelog/migrate-from-legacy-ocm-controller.md" >}}).
For the concepts, see [Kubernetes Controllers]({{< docref "docs/concepts/ocm-controllers.md" >}}) and the
[Controller CRDs]({{< docref "docs/reference/kubernetes-api/_index.md" >}}).

## Installation

Both CLIs are called `ocm`. The Homebrew formula `open-component-model/tap/ocm` still installs OCM v1. To install
OCM v2, see [Install the OCM CLI]({{< docref "docs/getting-started/ocm-cli-installation.md" >}}). In GitHub Actions,
`open-component-model/ocm-setup-action@v2` installs OCM v2. The v2 container image is
`ghcr.io/open-component-model/cli`.

## Go Library

The v2 library is a new API in the module `ocm.software/open-component-model/bindings/go`. Code written against
`ocm.software/ocm` has to be rewritten. The library is still evolving and its API is not yet stable. Start with the
package list in the
[bindings README](https://github.com/open-component-model/open-component-model/tree/main/bindings/go).
