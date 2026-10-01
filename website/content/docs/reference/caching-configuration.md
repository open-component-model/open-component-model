---
title: "Caching Configuration"
description: "Reference for the OCI caches used by the OCM CLI and controller: the caching configuration type, cache location, temporary folder, limits, admission rules, and remote policy."
icon: "🗄️"
weight: 8
toc: true
---

This page is the technical reference for the on-disk caches OCM uses when it reads component versions from OCI
registries.

The CLI and the controller enable caching by default. The `caching.oci.config.ocm.software/v1alpha1`
configuration type changes the cache mode, time to live, and maximum cached blob size. **Where** the cache lives is
not part of this type; see [Cache Location](#cache-location).

## Caches

OCM keeps two caches for OCI component version repositories. Both are stored on disk, so their contents survive
between runs of the CLI and restarts of the controller.

| Cache           | Key                                     | Content                                         | Scope                                                    |
|-----------------|-----------------------------------------|-------------------------------------------------|----------------------------------------------------------|
| Blob cache      | Blob digest                             | Manifests, indexes, and component descriptors   | Shared by all registries and repositories in the process |
| Reference cache | Registry, repository, and tag or digest | The descriptor that a tag or digest resolves to | One cache per repository                                 |

The blob cache stores content by digest, and a digest always identifies the same bytes. That makes it safe to share
the blob cache between repositories and credentials. OCM checks every fetched blob against its digest before it
writes the blob to disk.

The reference cache has one instance per repository location (`hostname[:port]/path`). Credentials are not part of
the key, so rotating short-lived tokens does not make the cache useless. Use the `Always` [mode](#fields)
when every access must be authorized by the registry.

Resource layers, such as the image layers of an OCI image resource, are **not** cached. Only the blobs listed in
[Admission Rules](#admission-rules) are cached.

## Configuration Type

Caching is controlled by the `caching.oci.config.ocm.software/v1alpha1` configuration type, embedded in the standard
OCM configuration file. It is a standalone entry; it is not part of an OCI repository specification, because cache
behaviour is local to the consumer.

```yaml
type: generic.config.ocm.software/v1
configurations:
  - type: caching.oci.config.ocm.software/v1alpha1
    mode: IfNotPresent
    ttl: 10m
    maxBlobSize: 4194304 # bytes
```

The unversioned type `caching.oci.config.ocm.software` is accepted as a deprecated alias.

### Fields

All fields are optional.

| Field         | Type                                   | Default                                              | Description                                                                                                                 |
|---------------|----------------------------------------|------------------------------------------------------|-----------------------------------------------------------------------------------------------------------------------------|
| `mode`        | `Always`, `IfNotPresent`, or `Never`   | `IfNotPresent`                                       | Cache activation and [remote policy](#remote-policy). `Never` disables both caches.                                          |
| `ttl`         | duration string, for example `30s`, `10m`, `1h30m` | `10m`                                    | How long cached entries remain reusable. Must be positive. Numeric values are rejected.                                     |
| `maxBlobSize` | integer, bytes                         | `4194304` (4 MiB)                                    | Largest individual blob the blob cache retains. Must be positive. Larger blobs are downloaded as usual but not cached.      |

Without a caching entry, the CLI and the controller run with `IfNotPresent` and the default `ttl` and
`maxBlobSize`.

Invalid values (an unknown `mode`, a malformed, numeric, zero, or negative `ttl`, a zero or negative `maxBlobSize`)
are rejected when the CLI starts or the controller builds its plugins. A zero value does not select a default;
omit the field instead.

### Merge Behaviour

If the configuration contains more than one caching entry, OCM merges them from first to last. The last explicitly
set value of each field wins; an omitted field keeps the value of an earlier entry, or the default. `ocm get config`
shows the merged entry, and omits it when no caching entry is configured.

### Controller

The controller accepts the caching type in the OCM configuration referenced from `spec.ocmConfig`. There are no
controller flags or Helm values for these settings.

{{< callout context="caution" title="The controller does not re-authorize cache hits by default" >}}
With the default `IfNotPresent`, the controller serves cached manifests and descriptors without contacting the
registry, so a cache hit is not authorized by the registry. The cache is shared by every object the controller
reconciles. If users of the controller have different access to registry content, set `mode: Always` so the registry
authorizes every access, or `mode: Never` to disable caching.
{{< /callout >}}

### Schema

The schema below defines the full structure of the `caching.oci.config.ocm.software/v1alpha1` type as specified by
[JSON Schema 2020-12](https://json-schema.org/draft/2020-12/schema).

---

{{< schema-renderer url="/schemas/bindings/go/oci/config/v1alpha1/Config.schema.json" >}}

---

## Cache Location

Both caches are stored under the OCM temporary folder:

| Cache           | Directory                                                                      |
|-----------------|--------------------------------------------------------------------------------|
| Blob cache      | `<tempFolder>/ocm-oci-cas/blobs/<algorithm>/<hex>`                             |
| Reference cache | `<tempFolder>/ocm-oci-refcache/<repository-key>/refs/<sha256(namespace)>.json` |

`<repository-key>` is the first 16 hex characters of the SHA-256 of `hostname[:port]/path`. `namespace` is
`<registry>/<repository>`. Each reference cache file stores the resolved descriptors of one namespace together
with the time each descriptor was resolved.

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

The same temporary folder is also used for other short-lived files, for example extracted CTF archives and
downloaded resource content.

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
writable volume. If the cache directory cannot be created, the controller logs a warning and continues without caching.

## Limits

Each cache applies these limits:

| Limit             | Default | Configurable             | Applies to                       | Behaviour                                                            |
|-------------------|---------|--------------------------|----------------------------------|----------------------------------------------------------------------|
| Maximum entries   | `256`   | No                       | Blob cache, each reference cache | When the limit is reached, the least recently used entry is removed. |
| Time to live      | `10m`   | `ttl`                    | Blob cache, each reference cache | Entries older than this are removed.                                 |
| Maximum blob size | `4 MiB` | `maxBlobSize`            | Blob cache                       | Larger blobs are downloaded as usual but not cached.                 |

The 256-entry limit is an implementation default and is deliberately not configurable. It counts entries of one
cache, so it has a different scope for the shared blob cache and for each per-repository reference cache.

When the blob cache removes an entry, it also deletes the file. When a reference cache is loaded from disk,
entries resolved more than the time to live ago are not loaded. When the blob cache is loaded from disk, every valid file is
loaded and gets a new time to live, because content addressed by digest cannot become stale.

## Admission Rules

The blob cache stores a blob only if its media type is one of:

- an OCI or Docker manifest or index media type;
- the OCM component config media type;
- an OCM component descriptor media type (any media type that starts with
  `application/vnd.ocm.software.component-descriptor`, which includes legacy v1 and `+tar` variants).

All other blobs are downloaded from the registry every time.

## Remote Policy

The remote policy decides whether OCM contacts the registry when it finds an entry in the cache. It is set by
`mode`; when `mode` is omitted, the CLI and the controller use `IfNotPresent`.

`mode: Never` is not a remote policy: it bypasses both caches entirely, and OCM creates neither cache directory.

| Policy         | Blob cache hit                                                                                                                                                 | Reference cache                                                                              |
|----------------|----------------------------------------------------------------------------------------------------------------------------------------------------------------|----------------------------------------------------------------------------------------------|
| `IfNotPresent` | Returns the cached blob without contacting the registry.                                                                                                       | Returns cached results for digest references. Tags are always resolved against the registry. |
| `Always`       | Checks that the blob exists and is accessible in the registry, then returns the cached blob without downloading it again. If the check fails, the fetch fails. | Always resolves against the registry and updates the cache with the result.                  |

With `IfNotPresent`, anyone who can read the cache directory can read cached manifests and component descriptors
without registry access. On macOS `$TMPDIR` is per user, but on Linux the default `/tmp` is shared by all users. On
shared machines, set `tempFolder` or `--temp-folder` to a directory that only you can read.

With `Always`, OCM asks the registry to authorize every access, but does not download cached content again.

A tag that has been moved to a new digest is never served from the cache under either policy, because tags are always
resolved against the registry.

## Clearing the Cache

It is safe to delete `<tempFolder>/ocm-oci-cas` and `<tempFolder>/ocm-oci-refcache` at any time when no OCM process
is running. OCM creates the directories again on the next run. If a cached file is missing while OCM is running, OCM
treats the entry as a cache miss and downloads the content again.

## Related Documentation

- [HTTP Client Configuration]({{< relref "docs/reference/http-client-configuration.md" >}}) — timeouts and retries for requests that miss the cache
- [Resolver Configuration]({{< relref "docs/reference/resolver-configuration.md" >}}) — configuration types in the same file
- [OCM Controllers]({{< relref "docs/concepts/ocm-controllers.md" >}}) — configuration types the controller accepts
- [`ocm` CLI reference]({{< relref "docs/reference/ocm-cli/ocm.md" >}}) — global flags, including `--temp-folder`
