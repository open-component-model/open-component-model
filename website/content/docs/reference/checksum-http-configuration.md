---
title: "HTTP Checksum Configuration"
description: "Complete reference for OCM HTTP checksum verification: schema, checksum modes, precedence rules, and the access-side fast path."
icon: "🔒"
weight: 7
toc: true
---

This page is the technical reference for OCM HTTP checksum verification — the
mechanism that ties a downloaded HTTP artifact to a source-side digest without
putting the checksum policy into the component descriptor. Both the
[`Wget/v1` input]({{< relref "input-and-access-types.md" >}}#wgetv1-input) and
the [`Wget/v1` access]({{< relref "input-and-access-types.md" >}}#wgetv1-access)
digest processor consult the same configuration, so a mirror trusted for the
input side is also trusted when the descriptor is refreshed.

For a task-oriented walkthrough, see
[Working with HTTP Resources]({{< relref "docs/tutorials/wget-http-resources.md" >}}#checksum-verification).

## Configuration Type

Checksum policy is controlled by the
`checksum.http.config.ocm.software/v1alpha1` configuration type, embedded in
the standard OCM configuration file alongside
[`http.config.ocm.software/v1alpha1`]({{< relref "http-client-configuration.md" >}}):

```yaml
type: generic.config.ocm.software/v1
configurations:
  # transport-level knobs — timeouts, TLS, retries
  - type: http.config.ocm.software/v1alpha1
    timeout: 30s

  # checksum-verification knobs — how downloaded HTTP bytes are verified
  - type: checksum.http.config.ocm.software/v1alpha1
    mode: Prefer   # default when omitted
    hosts:
      "repo.example.com":
        mode: Require
```

By default the CLI looks for configuration in `$HOME/.ocmconfig`. Pass
`--config <file>` to use a different file.

## Schema

The schema below defines the full structure of the
`checksum.http.config.ocm.software/v1alpha1` type as specified by
[JSON Schema 2020-12](https://json-schema.org/draft/2020-12/schema).

---

{{< schema-renderer url="/schemas/bindings/go/configuration/checksum/http/v1alpha1/Config.schema.json" >}}

---

## Checksum Modes

The `mode` field selects how checksum verification behaves. It can be set at
the top level (applying to all hosts) and overridden per host in the `hosts`
map.

Verification is header-only: OCM understands the IETF-standard
[RFC 9530](https://www.rfc-editor.org/rfc/rfc9530) `Content-Digest` field and
the non-standard `x-checksum-sha256`/`x-checksum-sha1`/`x-checksum-md5` family
(plus `x-goog-meta-*` and `x-amz-meta-*` variants).

### `Require`

- **Access side:** issues a single HEAD to the artifact URL and pins the
  digest from the source-advertised response headers. Aborts if no checksum
  header is advertised.
- **Input side:** downloads the body, verifies the bytes against the
  advertised header. Fails if no checksum header is advertised.

Use this mode when every mirror is expected to advertise a checksum.

### `Prefer` (default) {#mode-default}

This is the default when `mode` is not set.

- **Access side:** issues a single HEAD and pins the digest from the
  source-advertised response headers. If nothing is advertised, falls back to
  downloading the body and hashing it as SHA-256.
- **Input side:** downloads the body, verifies against the advertised header
  when present. If no checksum header is advertised, records SHA-256
  unverified.

### `Skip`

- **Access side:** never consults source-advertised checksums; always
  downloads the body and hashes it as SHA-256.
- **Input side:** downloads and hashes as SHA-256 without verification.

Use this mode when the source is trusted transport-wise but exposes no
digest headers.

## Precedence

For a given wget URL, the effective mode is resolved as (tightest wins):

1. A `hosts.<host>.mode` whose key matches the URL's host.
   Entries keyed `host:port` win over bare-hostname entries (case-insensitive
   host matching).
2. The top-level `mode`.
3. No configuration — the default mode `Prefer` applies.

A pinned `digest` on the resource itself is checked against the same authority
as the mode — the downloaded bytes on the input path, the source-advertised
checksum on the access fast path. Pinned and mode are each verified
independently, never against each other.

## Input Side — Always Downloads, Always SHA-256

The wget input embeds the download as a local blob, so the resource identity
*is* those bytes. The input method therefore:

- Always streams the body to disk.
- Always records `SHA-256` with `genericBlobDigest/v1`.
- (When the mode enables verification) also verifies the bytes against
  whatever algorithm the source advertises in response headers.

{{< callout context="note" >}}
Verification and storage are decoupled: the mode may verify against any
advertised algorithm (Maven repos often ship SHA-1/MD5), but the recorded
digest is **always SHA-256** with `genericBlobDigest/v1`, so no weak algorithm
leaks into the descriptor, OCI storage, or signing. A mismatch fails
construction before anything is stored.
{{< /callout >}}

## Access Side — Pin From Source, No Body Download {#access-digest}

A `Wget/v1` **access** references bytes on a remote server that any consumer
will re-fetch on demand. Whenever the mode enables it, the access-side digest
processor pins the resource digest from what the source advertises in response
headers via a single HEAD — **the artifact body is never fetched** (unless the
mode falls back to a download-and-hash).

```yaml
type: generic.config.ocm.software/v1
configurations:
  - type: checksum.http.config.ocm.software/v1alpha1
    mode: Require
    hosts:
      "legacy-mirror.example.com":
        mode: Skip
```

Semantics:

- The digest processor issues **one HEAD** to the artifact URL to harvest
  response headers. It **never fetches the artifact body** unless the mode
  falls through to the download-and-hash path.
- The processor pins only a source-advertised **SHA-256** or **SHA-512** digest
  (SHA-256 preferred) — the algorithms OCM/OCI storage and signing accept on a
  resource. Recording a weaker algorithm would make the component
  un-transferable by value, so SHA-1/MD5 offers are ignored on this fast path.
  The recorded digest carries `normalisationAlgorithm: genericBlobDigest/v1`;
  it is a legitimate pin because any downstream consumer re-fetches from the
  same source and re-verifies against the same authority.
- If no SHA-256/SHA-512 digest is advertised and the mode is `Require`, the
  processor aborts without downloading. `Prefer` falls through to a
  download-and-hash path (SHA-256), so a source that ships only SHA-1/MD5 still
  yields a self-describing SHA-256 pin.
- When the resource already carries a pinned `digest`, its algorithm and value
  MUST agree with the source-advertised digest for the same algorithm; a
  mismatch is a hard error.

{{< callout context="caution" >}}
The fast path applies to the access side only. Transferring a `Wget/v1` access
**by value** (`--copy-resources`) promotes it to a `LocalBlob/v1` and re-runs
the input-side rules, re-digesting the streamed bytes as SHA-256 regardless of
this configuration — every local blob stays self-describing.
{{< /callout >}}

## Credential Scoping

HEAD requests reuse the artifact's OCM credentials only when the resolved URL
matches the artifact URL's exact origin (scheme + host + port) and uses HTTPS.
Redirects on the credentialed client are hard-disabled: a 3xx cannot leak the
header cross-origin.

## Related Documentation

- [Working with HTTP Resources]({{< relref "docs/tutorials/wget-http-resources.md" >}}) — task-oriented walkthrough of the wget input and access types
- [Reference: Input and Access Types]({{< relref "input-and-access-types.md" >}}#wgetv1-input) — field reference for the `Wget/v1` input and access types
- [Reference: HTTP Client Configuration]({{< relref "http-client-configuration.md" >}}) — the transport-level `http.config.ocm.software/v1alpha1` configuration
- [Reference: Credential Consumer Identities: Wget]({{< relref "credential-consumer-identities.md" >}}#wget) — identity attributes and matching rules for `Wget` consumers
