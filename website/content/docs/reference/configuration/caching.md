---
title: "Caching"
description: "Reference for the OCI caches of the OCM CLI and controller: cache location, limits, admission rules, remote policy, and their configuration."
icon: "🗄️"
weight: 1
toc: true
aliases:
  - /docs/reference/caching-configuration/
---

This page is the technical reference for the on-disk caches OCM uses when it reads component versions from OCI
registries.

{{< callout context="note" title="What you can configure" >}}
The CLI enables the blob and the reference cache by default, the controller only the blob cache. You can set
**where** the caches live, through the temporary folder described in [Cache Location](#cache-location). With the
[`oci.cache.config.ocm.software`](#configuration) configuration type you can turn each cache on or off and set the
[remote policy](#remote-policy), the time to live, and the size limits.
{{< /callout >}}

## Caches

OCM keeps two caches for OCI component version repositories. Both are stored on disk, so their contents survive
between runs of the CLI and restarts of the controller.

| Cache           | Key                                     | Content                                         | Scope                                                    |
|-----------------|-----------------------------------------|-------------------------------------------------|----------------------------------------------------------|
| Blob cache      | Blob digest                             | Manifests, indexes, and component descriptors   | Shared by all registries and repositories in the process |
| Reference cache | Registry, repository, and tag or digest | The descriptor that a tag or digest resolves to | Shared by all registries and repositories in the process |

The blob cache stores content by digest, and a digest always identifies the same bytes. That makes it safe to share
the blob cache between repositories and credentials. OCM checks every fetched blob against its digest before it
writes the blob to disk.

The reference cache saves the registry request that resolves a tag, so repeated reads of the same component version
do not run into registry rate limits. A tag moved to a new digest in the registry therefore stays stale until its
entry expires, see [Moved Tags](#moved-tags).

Credentials are not part of either key, so rotating short-lived tokens does not make the caches useless. The `Always`
[remote policy](#remote-policy), which has the registry authorize every access, turns off the reference cache.

The controller uses only the blob cache unless the [configuration](#configuration-in-the-controller) turns on the
reference cache. One instance of each serves every namespace.

Resource layers, such as the image layers of an OCI image resource, are **not** cached. Only the blobs listed in
[Admission Rules](#admission-rules) are cached.

## Cache Location

Both caches are stored under the OCM temporary folder:

| Cache           | Directory                                                     |
|-----------------|---------------------------------------------------------------|
| Blob cache      | `<tempFolder>/ocm-oci-cas/blobs/<algorithm>/<hex>`            |
| Reference cache | `<tempFolder>/ocm-oci-refcache/refs/<sha256(namespace)>.json` |

`namespace` is `<registry>/<repository>`. Each reference cache file stores the resolved descriptors of one OCI
repository together with the time each descriptor was resolved, so a resolve rewrites only the file of its repository.

The directory names do not change between runs. Every OCM process that uses the same temporary folder reads and
writes the same cache.

### Temporary Folder in the CLI

The CLI takes the temporary folder from the first of these that is set:

1. The `--temp-folder` flag.
2. The `tempFolder` field of the `filesystem.config.ocm.software/v1alpha1` configuration type.
3. The temporary directory of the operating system (Go
   [`os.TempDir`](https://pkg.go.dev/os#TempDir)). On Linux and macOS this is `$TMPDIR`, or `/tmp` if `$TMPDIR` is
   empty. On Windows it is the first of `%TMP%`, `%TEMP%`, `%USERPROFILE%`, or the Windows directory.

If both the flag and the configuration field are set, the CLI uses the flag and logs a warning.

```yaml
type: generic.config.ocm.software/v1
configurations:
  - type: filesystem.config.ocm.software/v1alpha1
    tempFolder: /var/cache/ocm
```

The same temporary folder is also used for other short-lived files, for example extracted CTF archives
and downloaded resource content.

#### `filesystem.config.ocm.software/v1alpha1` Fields

| Field              | Type   | Default                              | Description                                                                                                             |
|--------------------|--------|--------------------------------------|-------------------------------------------------------------------------------------------------------------------------|
| `tempFolder`       | string | Operating system temporary directory | Directory for short-lived files and for the OCI caches.                                                                 |
| `workingDirectory` | string | Current working directory            | Base directory for resolving relative file paths. Not related to caching. Overridden by the `--working-directory` flag. |

If the configuration file contains more than one `filesystem.config.ocm.software/v1alpha1` entry, the last value set
for each field wins.

### Temporary Folder in the Controller

The controller does not accept `filesystem.config.ocm.software` (see
[OCM Controllers]({{< relref "docs/concepts/ocm-controllers.md" >}})). It always uses the temporary directory of the
operating system, so the cache is stored under `$TMPDIR` (or `/tmp`) of the controller container.

If the root filesystem of the container is read-only, mount a writable volume at that path, or set `TMPDIR` to a
writable volume. If the cache directory cannot be created, the controller does not start.

## Limits

Each cache applies these limits:

| Limit              | Default                              | Field ([Configuration](#configuration)) | Applies to                  | Behaviour                                                                                                                                          |
|--------------------|--------------------------------------|-----------------------------------------|-----------------------------|----------------------------------------------------------------------------------------------------------------------------------------------------|
| Maximum entries    | `100000` (blob), `10000` (reference) | `reference.maxEntries`                  | Blob cache, reference cache | When the limit is reached, the least recently used entry is removed. The reference cache counts the tags and digests of all repositories together. |
| Time to live       | `10m`                                | `blob.ttl`, `reference.ttl`             | Blob cache, reference cache | Entries older than this are removed.                                                                                                               |
| Maximum blob size  | `20 MiB`                             | `blob.maxBlobSize`                      | Blob cache                  | Larger blobs are downloaded as usual but not cached.                                                                                               |
| Maximum total size | No limit (CLI), `1 GiB` (controller) | `blob.maxTotalSize`                     | Blob cache                  | When the limit is exceeded, least recently used entries are removed until the cache fits.                                                          |

When the blob cache removes an entry, it also deletes the file. When the reference cache is loaded from disk,
entries resolved longer than the time to live ago are not loaded. When the blob cache is loaded from disk, every valid
file is loaded and gets a new time to live, because content addressed by digest cannot become stale. If the files
exceed the maximum total size, the least recently written ones are removed.

CLI runs in parallel that use the same cache directory lead to undefined behaviour, for example the maximum total
size is not kept. Give each parallel run its own `--temp-folder`. The controller runs one process per pod and is the
only user of its cache directory.

## Admission Rules

The blob cache stores a blob only if its media type is one of:

- an OCI or Docker manifest or index media type;
- the OCM component config media type;
- an OCM component descriptor media type (any media type that starts with
  `application/vnd.ocm.software.component-descriptor`, which includes legacy v1 and `+tar` variants).

All other blobs are downloaded from the registry every time.

## Remote Policy

The remote policy decides whether the registry authorizes every access to cached content. Use `Always` where callers
with different registry access share the caches, such as the namespaces served by the controller. The default is
`IfNotPresent` for both the CLI and the controller.

| Policy         | Blob cache hit                                                                                                                                                 | Reference cache                                                                                       |
|----------------|----------------------------------------------------------------------------------------------------------------------------------------------------------------|-------------------------------------------------------------------------------------------------------|
| `IfNotPresent` | Returns the cached blob without contacting the registry.                                                                                                       | Returns the cached descriptor of a tag or digest without contacting the registry.                     |
| `Always`       | Checks that the blob exists and is accessible in the registry, then returns the cached blob without downloading it again. If the check fails, the fetch fails. | Off. Resolving a tag or digest is the registry's access check, so every resolve goes to the registry. |

With `IfNotPresent`, anyone who can read the cache directory can read cached manifests and component descriptors
without registry access. On macOS `$TMPDIR` is per user, but on Linux the default `/tmp` is shared by all users. On
shared machines, set `tempFolder` or `--temp-folder` to a directory that only you can read.

With `Always`, OCM asks the registry to authorize every access, but does not download cached content again.

### Moved Tags

OCM stores a component version under a tag. With `IfNotPresent`, a tag that another writer moves to a new digest, for
example by publishing the same component version again with different content, still resolves to the cached digest
until the reference cache entry expires after `reference.ttl`. Component versions are not meant to change after they
are published, so this does not matter in normal use. Tags that the same OCM process writes are updated in the cache
at once.

If you publish component versions again, read them with one of these:

- a shorter `reference.ttl`, which bounds how long a moved tag stays stale;
- `reference.enabled: false`, which resolves every tag against the registry.

## Configuration

The `oci.cache.config.ocm.software/v1alpha1` configuration type turns each cache on or off and sets the remote
policy and the limits. The `blob` and `reference` sections configure one cache each.

```yaml
type: generic.config.ocm.software/v1
configurations:
  - type: oci.cache.config.ocm.software/v1alpha1
    remotePolicy: IfNotPresent
    blob:
      ttl: 24h
      maxBlobSize: 52428800 # 50 MiB
      maxTotalSize: 5368709120 # 5 GiB
    reference:
      ttl: 1m
      maxEntries: 1000
```

All fields are optional. An omitted field keeps its default.

| Field          | Type                       | Default        | Description                                                                                                            |
|----------------|----------------------------|----------------|------------------------------------------------------------------------------------------------------------------------|
| `remotePolicy` | `IfNotPresent` or `Always` | `IfNotPresent` | [Remote policy](#remote-policy): whether the registry authorizes every access. `Always` turns off the reference cache. |
| `blob`         | object                     |                | The blob cache, see below.                                                                                             |
| `reference`    | object                     |                | The reference cache, see below.                                                                                        |

Fields of `blob`:

| Field          | Type                                                             | Default                                          | Description                                                                                                                                 |
|----------------|------------------------------------------------------------------|--------------------------------------------------|---------------------------------------------------------------------------------------------------------------------------------------------|
| `enabled`      | boolean                                                          | `true`                                           | `false` turns off the blob cache: every blob is downloaded from the registry and no cache directory is created.                             |
| `ttl`          | duration string such as `30s` or `1h30m`, or integer nanoseconds | `10m`                                            | Time to live of the entries. With `IfNotPresent`, how long a cached blob is returned without contacting the registry. Must be positive.     |
| `maxBlobSize`  | integer, bytes                                                   | `20971520` (20 MiB)                              | The largest blob the blob cache stores. Larger blobs are fetched as usual but not cached. Must be positive.                                 |
| `maxTotalSize` | integer, bytes                                                   | No limit (CLI), `1073741824` (1 GiB, controller) | The bytes the blob cache keeps on disk. When exceeded, the least recently used blobs are removed. `0` means no limit. Must not be negative. |

Fields of `reference`:

| Field        | Type                                                             | Default                            | Description                                                                                                                                              |
|--------------|------------------------------------------------------------------|------------------------------------|----------------------------------------------------------------------------------------------------------------------------------------------------------|
| `enabled`    | boolean                                                          | `true` (CLI), `false` (controller) | `false` turns off the reference cache: every tag and digest is resolved against the registry. `remotePolicy: Always` turns it off regardless.            |
| `ttl`        | duration string such as `30s` or `1h30m`, or integer nanoseconds | `10m`                              | Time to live of the entries: how long a [moved tag](#moved-tags) stays stale. Must be positive.                                                          |
| `maxEntries` | integer                                                          | `10000`                            | The tags and digests the reference cache keeps, for all repositories together. When reached, the least recently used entry is removed. Must be positive. |

An unknown `remotePolicy`, a `ttl` that is not a positive duration, a `maxBlobSize` or `maxEntries` that is not
positive, or a negative `maxTotalSize` makes the CLI and the controller fail at startup, even if a later entry
overrides it. To use a default, omit the field. If the configuration file contains more than one
`oci.cache.config.ocm.software/v1alpha1` entry, the last value set for each field wins. `ocm get config` shows the
merged entry.

The schema below defines the full structure of the `oci.cache.config.ocm.software/v1alpha1` type as specified by
[JSON Schema 2020-12](https://json-schema.org/draft/2020-12/schema).

---

{{< schema-renderer url="/schemas/bindings/go/oci/cache/v1alpha1/Config.schema.json" >}}

---

### Configuration in the CLI

Add the configuration type to the OCM configuration file.

### Configuration in the Controller

The controller does not accept `oci.cache.config.ocm.software` in `spec.ocmConfig` for individual objects. One blob
cache serves every namespace so a single object should not be able to configure it. Instead, configure the cache for
the whole controller with the `manager.ocmConfig` Helm value like this:

```yaml
manager:
  ocmConfig:
    configurations:
      - type: oci.cache.config.ocm.software/v1alpha1
        remotePolicy: Always
        blob:
          ttl: 1h
```

The chart stores this configuration in a `ConfigMap`, mounts it into the controller, and passes it with the
`--ocm-config` flag automatically. `type` defaults to `generic.config.ocm.software/v1`. The controller reads the file
once at startup. The chart sets a `checksum/ocm-config` annotation on the controller pods, which Helm computes when it
renders the chart. A `helm upgrade` that changes `manager.ocmConfig` therefore restarts the pods. Editing the `ConfigMap`
directly, for example with `kubectl edit`, restarts nothing: the controller picks up the change only on its next restart.

The controller uses only the blob cache by default. The blob cache lives in the pod's `tmp` volume and counts against
its ephemeral storage: keep `blob.maxTotalSize` below an ephemeral-storage limit set on the pod.

`reference.enabled: true` turns on the reference cache in the controller, unless `remotePolicy` is `Always`. It
saves the registry request that resolves the tag of a component version on every reconciliation, but a
[moved tag](#moved-tags) is then picked up only after `reference.ttl`.
`oci.cache.config.ocm.software` is the only type accepted here. Any other type, or an invalid field, results in a
startup error.

{{< callout context="caution" title="IfNotPresent shares cached content across namespaces" icon="outline/alert-triangle" >}}
The blob cache key is the digest, not the credentials. With `IfNotPresent`, a blob that one namespace fetched with its
credentials is, potentially, read from the cache by another with the same digest, but no credentials.
On a cluster where namespaces MUST NOT read each other's blobs, use `Always`. Every cache hit is then checked against
the registry with the credentials of the requesting object.
{{< /callout >}}

## Clearing the Cache

It is safe to delete `<tempFolder>/ocm-oci-cas` and `<tempFolder>/ocm-oci-refcache` at any time when no OCM process
is running. OCM creates the directories again on the next run. If a cached file is missing while OCM is running, OCM
treats the entry as a cache miss and downloads the content again.

## Related Documentation

- [Reference: HTTP Client Configuration]({{< relref "docs/reference/configuration/http-client.md" >}}) - timeouts and retries for requests that miss the cache
- [Reference: Resolver Configuration]({{< relref "docs/reference/configuration/resolver.md" >}}) - configuration types in the same file
- [Concept: OCM Controllers]({{< relref "docs/concepts/ocm-controllers.md" >}}) - configuration types the controller accepts
- [Reference: `ocm` CLI reference]({{< relref "docs/reference/ocm-cli/ocm.md" >}}) - global flags, including `--temp-folder`
