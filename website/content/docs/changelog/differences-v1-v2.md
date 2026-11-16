---
title: "Key Differences Between OCM v1 and v2"
description: "What changes from OCM v1 to OCM v2 in the CLI, configuration, signing, stored data, plugins, controllers and library, with links to the migration guides."
icon: "arrow-autofit-right"
slug: "differences-v1-v2"
weight: 5
toc: true
---

OCM v2 is a rebuild of the OCM CLI, Kubernetes controllers and Go library. Both versions implement the same
[OCM specification](https://github.com/open-component-model/ocm-spec), and OCM v2 reads most component versions
that OCM v1 created. The tooling around that data changed, though: commands, flags, configuration types, plugins and
the library API. Use this page to estimate the effort and the risks of a migration. Each section links to the
migration guide for the change, where one exists.

The [legacy documentation](https://ocm.software/legacy/docs/) still describes OCM v1.

## At a Glance

| Area | OCM v1 | OCM v2 | Migration guide |
| ---- | ------ | ------ | --------------- |
| CLI | Many verbs and nouns (`add resources`, `create transportarchive`, `bootstrap`, ...) | A small set around `component-version`: `add`, `get`, `sign`, `verify`, `transfer`, `download` | [CLI commands](#cli-commands) |
| Component constructor | Templating with Go templates, spiff or subst, plus settings files | Environment variable expansion only | [Component constructor](#component-constructor) |
| Resolvers | Fallback resolvers in `ocm.config.ocm.software` | Glob-based `resolvers.config.ocm.software/v1alpha1` | [Migrate Legacy Resolvers]({{< docref "docs/changelog/migrate-legacy-resolvers.md" >}}) |
| Credentials | Untyped `Credentials/v1`, `pathprefix`, Vault and Gardener repositories | Same format, `path` instead of `pathprefix`, optional typed credentials | [Migrate Legacy Credentials]({{< docref "docs/changelog/migrate-legacy-credentials.md" >}}) |
| Transfer | `--copy-resources`, `--upload-as`, `--uploader`, transfer scripts | Uploader configurations matched per resource with CEL | [Migrate --upload-as Flags]({{< docref "docs/changelog/migrate-from-upload-as.md" >}}) |
| Plain HTTP | `http://` prefix on the registry URL | `plainHTTP` per host in the HTTP configuration | [Migrate from http:// Prefixes]({{< docref "docs/changelog/migrate-from-http-prefixes.md" >}}) |
| Sources | Supported | Deprecated, replaced by resources and the `ocm.software/artifact-references` label | [Migrate from Sources to Resources]({{< docref "docs/changelog/migrate-from-sources.md" >}}) |
| Signing | Keys and algorithm passed as CLI flags, default `RSASSA-PKCS1-V1_5` with `jsonNormalisation/v3` | Keys from the credential configuration, default `RSASSA-PSS` with `jsonNormalisation/v4alpha3` | [Migrate Normalisation Algorithms]({{< docref "docs/changelog/migrate-normalisation-algorithms.md" >}}) |
| Storage | Component archives, CTF, OCI registries | CTF and OCI registries | [Data compatibility](#data-compatibility) |
| Plugins | Executables that add access methods, uploaders, actions and more | A new plugin protocol and plugin registry | [Plugins](#plugins) |
| Controllers | `ocm-controller` with Snapshots, Flux and an in-cluster registry | `Repository`, `Component`, `Resource` and `Deployer` without Flux | [Controllers](#controllers) |
| Go library | `ocm.software/ocm` | `ocm.software/open-component-model/bindings/go`, a new API | [Go library](#go-library) |

## CLI Commands

The v2 CLI is organized around the component version. Most v1 commands that edit or inspect a single part of a
component version are gone; you work with the whole component version instead.

| OCM v1 | OCM v2 |
| ------ | ------ |
| `ocm add componentversions` | `ocm add component-version` |
| `ocm create transportarchive`, `ocm create componentarchive` | No command. `ocm add cv` creates the CTF archive if it does not exist. |
| `ocm add resources`, `add sources`, `add references` | No command. Edit the component constructor and run `ocm add cv` again. |
| `ocm get componentversions` | `ocm get component-version` |
| `ocm get resources`, `get sources`, `get references` | `ocm get cv -o yaml`, or `-o widetree` for a resource overview |
| `ocm download resources` | `ocm download resource` |
| `ocm download componentversions`, `download artifacts` | No command |
| `ocm transfer componentversions` | `ocm transfer component-version` |
| `ocm transfer commontransportarchive`, `transfer artifacts` | `ocm transfer cv`, which accepts a CTF archive as source and target |
| `ocm sign componentversions`, `verify componentversions` | `ocm sign component-version`, `ocm verify component-version` |
| `ocm create rsakeypair` | No command. See [Generate Signing Keys]({{< docref "docs/guides/sign/generate-signing-keys.md" >}}). |
| `ocm install plugins` | `ocm download plugin` and `ocm plugin registry` |
| `ocm list`, `show versions`, `show tags` | `ocm get cv <component>` with `--constraint` or `--latest` |
| `ocm bootstrap`, TOI packages | No command. Deploy with the [OCM controllers]({{< docref "docs/concepts/ocm-controllers.md" >}}). |
| `ocm controller install` | No command. Install the controllers with Helm. |
| `ocm hash`, `sign hash`, `check`, `execute action`, `set pubsub`, `get routingslips`, `get credentials`, `get verified`, `clean cache` | No command |

`ocm describe types` and `ocm get types` are new: they list the types each subsystem accepts and their fields, which
replaces most of the v1 help topics such as `ocm ocm-accessmethods`.

The v1 spellings `componentversions`, `componentversion` and `cv` still work. `get`, `sign`, `verify` and `transfer`
also accept `component` and `comp`. `add` does not.

### References and flags

- **References.** Component references keep the form `<repository>//<component>:<version>`, for example
  `ghcr.io/acme//acme.org/app:1.0.0` or `ctf::./archive//acme.org/app:1.0.0`. The `--repo` flag is gone, so always pass
  the full reference. A reference can pin a digest, such as `...:1.0.0@sha256:<digest>`.
- **Lookup.** The `--lookup` flag is gone. Configure resolvers instead, see
  [Migrate Legacy Resolvers]({{< docref "docs/changelog/migrate-legacy-resolvers.md" >}}).
- **Transfer.** `--overwrite`, `--enforce`, `--script`, `--uploader`, `--copy-local-resources` and `--omit-access-types`
  are gone. Local blobs are copied by default, and uploader configurations decide what else is copied.
  `ocm transfer cv --dry-run -o yaml` writes the planned transfer to a file that `--transfer-spec` replays after review.
- **Download.** `-O` becomes `--output`, download handlers become `--transformer`, and the resource identity moves to
  `--identity`.
- **Output.** `ocm get cv` prints a tree by default. The other formats are `table`, `widetree`, `yaml`, `json` and
  `ndjson`.

Scripts and CI pipelines that call the v1 CLI need a review against the
[CLI reference]({{< docref "docs/reference/ocm-cli/ocm.md" >}}).

## Component Constructor

The constructor still describes one component or a `components:` list with resources, sources and references, see
[Component Constructor]({{< docref "docs/reference/component-constructor.md" >}}). The differences:

- **No templating.** `--templater`, `--settings` and `--addenv` are gone, and so are spiff and Go templates. OCM v2
  expands environment variables (`${VAR}` or `$VAR`) in the file before it parses it. Render more complex templates
  before you call `ocm add cv`.
- **File paths** must stay inside the working directory or its subdirectories.
- **Input types** keep their v1 names as aliases where a v2 type exists:

| OCM v1 input | OCM v2 |
| ------------ | ------ |
| `dir`, `file`, `utf8`, `helm`, `wget`, `git` | `Dir/v1`, `File/v1`, `UTF8/v1`, `Helm/v1`, `Wget/v1`, `Git/v1`. The v1 names still work. |
| `ociArtifact`, `ociImage`, `docker`, `dockermulti` | No input type. Reference the image with the `OCIImage/v1` access type, or add an OCI layout file with `File/v1` and the media type `application/vnd.ocm.software.oci.layout.v1+tar`. No input type reads images from a local Docker daemon. |
| `binary`, `spiff`, `maven`, `npm`, `ocm` | No input type |

`S3/v2` is a new input type. For all types and fields, see
[Input and Access Types]({{< docref "docs/reference/input-and-access-types.md" >}}).

## Configuration and Credentials

The configuration file keeps its format: a `generic.config.ocm.software/v1` document with a `configurations:` list,
read from `$HOME/.ocmconfig` or the file passed with `--config`. Several configuration types changed:

- **Credentials** work mostly unchanged. `pathprefix` is renamed to `path`. `HashiCorpVault/v1`, `GardenerConfig/v1`
  and `NPMConfig/v1` credential repositories are not available. See
  [Migrate Legacy Credentials]({{< docref "docs/changelog/migrate-legacy-credentials.md" >}}).
- **Resolvers** in `ocm.config.ocm.software` are deprecated in favor of `resolvers.config.ocm.software/v1alpha1`. See
  [Migrate Legacy Resolvers]({{< docref "docs/changelog/migrate-legacy-resolvers.md" >}}).
- **Transfer** settings move to uploader configurations. An `uploadType` field fails config loading. See
  [Migrate --upload-as Flags]({{< docref "docs/changelog/migrate-from-upload-as.md" >}}).
- **New types** configure signing (`signing.config.ocm.software/v1alpha1`), the HTTP client
  (`http.config.ocm.software/v1alpha1`), caching and versioning. See
  [Configuration]({{< docref "docs/reference/configuration/_index.md" >}}).
- **No v2 counterpart** exists for `attributes`, `keys`, `rootcerts`, `logging`, `memory.credentials` and the v1
  `scripts`, `transport`, `uploader` and `downloader` configuration types. Logging is set with the `--loglevel`,
  `--logformat` and `--logoutput` flags.

## Signing

- **Keys come from the configuration.** The v1 flags `--private-key`, `--public-key`, `--issuer`, `--ca-cert`,
  `--keyless` and `--algorithm` are gone. Keys are credentials, and the signer is set in
  `signing.config.ocm.software/v1alpha1`. See
  [Configure Signing Credentials]({{< docref "docs/guides/sign/configure-signing-credentials.md" >}}).
- **Defaults changed.** New signatures use `RSASSA-PSS` and `jsonNormalisation/v4alpha3`. OCM v1 used
  `RSASSA-PKCS1-V1_5` and `jsonNormalisation/v3`. To keep `RSASSA-PKCS1-V1_5`, configure it in the signer.
  New signatures can only use `jsonNormalisation/v4alpha1` to `v4alpha3`.
- **Signers.** OCM v2 signs with RSA (plain keys or PEM certificate chains), GPG and Sigstore. The v1 signing-service
  handlers have no counterpart.
- **Verifying v1 signatures.** RSA signatures created with `jsonNormalisation/v1` or `jsonNormalisation/v3` verify.
  For the exceptions, see [Data compatibility](#data-compatibility). Timestamps from the v1 CLI need
  `ocm verify cv --insecure-trust-legacy-timestamps`.

## Data Compatibility

OCM v2 reads component versions that OCM v1 created in OCI registries and CTF archives, including the v1 names of
access types such as `ociArtifact`, `localBlob`, `localOciBlob`, `ociBlob`, `helm`, `github`, `git` and `s3`. It
stores embedded OCI artifacts differently, see
[OCM v1 and OCM v2 storage]({{< docref "docs/concepts/component-storage.md#ocm-v1-and-ocm-v2" >}}).
The following v1 data does not work in OCM v2:

| v1 data | Behavior in OCM v2 |
| ------- | ------------------ |
| Component archives (`ocm create componentarchive`) | Rejected with `component archive ... is not supported`. Transfer them into a CTF archive with the v1 CLI first. |
| `maven`, `npm` and `ocm` access types | No access type to read them |
| Sigstore signatures from the v1 CLI | Cannot be verified. Re-sign with the v2 [Sigstore signer]({{< docref "docs/guides/sign/sign-with-sigstore.md" >}}). |
| Signatures with `jsonNormalisation/v2` | Cannot be verified |
| Certificate chains with an embedded self-signed root (v1 `create rsakeypair --ca-cert`) | Rejected |
| Git resources built with v1 CLI 0.52.x or older | Digest mismatch on verify |
| Descriptors in the `ocm.software/v3alpha1` schema | Cannot be read. OCM v2 reads `schemaVersion: v2` only. |
| OCI repositories with `componentNameMapping: sha256-digest` | Components are not found |
| Label `merge` specifications | Dropped on read and transfer |

Decisions on these items are tracked in the
[OCM v1 to v2 compatibility epic](https://github.com/open-component-model/ocm-project/issues/1366). Routing slips,
pubsub, TOI packages and actions have no v2 support.

Reading OCM v2 output with OCM v1 is not supported. The v1 CLI reads local blobs in the v2 format from v0.36.0 on, but
newer v2 features may not work with OCM v1.

## Plugins

OCM v2 uses a new plugin protocol, so v1 plugins do not run in OCM v2. v2 plugins extend repositories, credential
repositories, input and resource types, signing handlers and blob transformers. `ocm download plugin` downloads a
plugin binary from a component version, and `ocm plugin registry` lists the plugins a registry offers. v1 extension points such as actions, value sets, label merge handlers and CLI
commands have no counterpart. See [Plugin System]({{< docref "docs/concepts/plugin-system.md" >}}).

## Controllers

The new controllers replace the legacy `ocm-controller`. The `ComponentVersion` resource becomes `Component`, which
references a new `Repository` resource. `FluxDeployer` becomes `Deployer`, which applies plain manifests itself
without Flux. `ComponentDescriptor`, `Snapshot`, `Configuration` and `Localization` are gone, and so is the in-cluster
OCI registry. Configuration and localization move to build time. For a walkthrough, read
[What changed between legacy and new OCM Controllers](/blog/2026-03-16-ocm-controllers-differences/) and
[OCM Controllers]({{< docref "docs/concepts/ocm-controllers.md" >}}).

## Go Library

The v2 library is a new API in the module `ocm.software/open-component-model/bindings/go`, so code written against
`ocm.software/ocm` has to be rewritten. The v1 context and registry model is replaced by typed objects in a
`runtime.Scheme` and plugin-backed registries. The library is still evolving and its API is not yet stable. Start
with the package list in the
[bindings README](https://github.com/open-component-model/open-component-model/tree/main/bindings/go).
