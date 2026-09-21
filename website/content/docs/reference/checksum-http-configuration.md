---
title: "HTTP Checksum Configuration"
description: "Complete reference for OCM HTTP checksum verification: schema, source strategies, precedence rules, and the access-side fast path."
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

## Design Rationale

**Verification is a deployment concern, not a descriptor concern.** The wget
input spec has no `checksumPolicy` field, and the `Wget/v1` access carries no
verification hints. Instead, operators steer verification centrally via the
`checksum.http.config.ocm.software/v1alpha1` configuration:

- **The same descriptor behaves identically wherever it is constructed.** Two
  operators pointing at two mirrors trust each mirror independently without
  editing the constructor.
- **How a mirror is trusted is up to the operator.** RFC 9530 `Content-Digest`,
  a `x-checksum-*` header, a Maven-style `<url>.<ext>` sidecar, a hosted-mirror
  sidecar at a different URL — all coexist in the same configuration.
- **The transport identifier is stable.** The config type is named for the
  transport (`checksum.http.config.ocm.software`), not for one input plugin, so
  a future rename of the wget package leaves the on-the-wire identifier and any
  operator configuration unchanged.

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
    defaultChecksumPolicy:
      onMissing: compute
      sources:
        - type: httpHeader
        - type: externalUrl
          algorithms: [sha256, sha1]
    hosts:
      "repo.example.com":
        checksumPolicy:
          onMissing: fail
          sources: [{type: httpHeader}]
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

## Source Strategies

Each entry in `sources` has a `type`. Sources are tried in order; the first
that yields an expected checksum wins.

### `httpHeader` — "Remote Included"

The checksum travels in the download response headers. Both the IETF-standard
[RFC 9530](https://www.rfc-editor.org/rfc/rfc9530) `Content-Digest` field and
the non-standard `x-checksum-sha256`/`x-checksum-sha1`/`x-checksum-md5` family
(plus the `x-goog-meta-*` and `x-amz-meta-*` variants) are understood.

Add extra header names with `headers: [x-my-sha256]`. The algorithm is
inferred from a trailing token (e.g. `x-my-sha256`) unless the header is
already known.

Restrict which algorithms this source considers with
`algorithms: [sha256, sha1, sha512, md5]`. Empty means every supported
algorithm is accepted, strongest-preferred first.

### `externalUrl` — "Remote External"

A sibling resource fetched from a separate URL. By default, the URL is
constructed by appending the algorithm's file extension to the artifact URL
(e.g. `<url>.sha256`, Maven's convention). Set `url` to an absolute URL to
point the checksum request at a mirror instead.

One URL per source; use multiple `externalUrl` sources for multiple algorithms
or hosts. Empty `algorithms` falls back to the default preference list.

The file may be a bare hex digest or GNU coreutils format (`<hex>  <name>`).

### `stream`

No expected checksum; the digest is computed from the downloaded stream.
Placing this in the list stops the search and disables verification from that
point on. Useful when a source is trusted transport-wise but exposes no
digest.

## Precedence

For a given wget URL, the effective policy is resolved as (tightest wins):

1. A `hosts.<host>.checksumPolicy` whose key matches the URL's host.
   Entries keyed `host:port` win over bare-hostname entries (case-insensitive
   host matching).
2. `defaultChecksumPolicy` at the top level.
3. No policy — the input path computes the storage digest from the stream
   without external verification; the access path downloads and hashes.

A pinned `digest` on the resource itself is checked against the same authority
as the policy — the downloaded bytes on the input path, the source-advertised
checksum on the access fast path. Pinned and policy are each verified
independently, never against each other.

## `onMissing`

Controls what happens when no source in the policy yields a checksum:

| Value              | Behaviour                                                                                       |
|--------------------|-------------------------------------------------------------------------------------------------|
| `fail` (default)   | Abort the input or digest processor with an error.                                              |
| `compute`          | Fall back to computing the digest from the stream without external verification.                |

## Input Side — Always Downloads, Always SHA-256

The wget input embeds the download as a local blob, so the resource identity
*is* those bytes. The input method therefore:

- Always streams the body to disk.
- Always records `SHA-256` with `genericBlobDigest/v1`.
- (If a policy applies) also verifies the bytes against whatever algorithm the
  source advertises.

{{< callout context="note" >}}
Verification and storage are decoupled on the input side. A policy may verify
the transferred bytes against any supported algorithm — Maven repositories
commonly ship SHA-1 or MD5 — but the digest recorded on the resource is
**always SHA-256** with the `genericBlobDigest/v1` normalisation, so a
non-SHA-256 transport checksum never leaks a weak algorithm into the component
descriptor, OCI storage, or signing. A mismatch fails construction before
anything is stored.
{{< /callout >}}

## Access Side — Pin From Source, No Body Download {#access-digest}

A `Wget/v1` **access** references bytes on a remote server that any consumer
will re-fetch on demand. When you opt in via `accessDigest`, the digest
processor pins the resource digest from the source-advertised checksum and
skips the body download entirely — a single HEAD (and, for `externalUrl`,
a tiny sidecar GET) is enough to establish the digest.

```yaml
type: generic.config.ocm.software/v1
configurations:
  - type: checksum.http.config.ocm.software/v1alpha1
    defaultChecksumPolicy:
      onMissing: fail
      sources:
        - type: httpHeader
        - type: externalUrl
          algorithms: [sha256, sha1]
      # Access-side fast path: pin from what the source advertises, do not
      # download the body just to hash it. SHA-256 is preferred; SHA-1 is a
      # legitimate fallback for legacy mirrors that don't offer it.
      accessDigest:
        algorithms: [sha256, sha1]
```

Semantics:

- The digest processor issues **one HEAD** to the artifact URL to harvest
  response headers, plus one GET per `externalUrl` source (fetching the small
  sidecar file). It **never fetches the artifact body**.
- The strongest algorithm from `accessDigest.algorithms` that any source
  advertises wins. Weaker algorithms are used only as a fallback.
- The recorded digest carries the algorithm the source actually offered
  (`SHA-256`, `SHA-1`, `MD5`, `SHA-512`), still with
  `normalisationAlgorithm: genericBlobDigest/v1`. It is a legitimate pin
  because any downstream consumer re-fetches from the same source and
  re-verifies against the same authority.
- When `accessDigest.algorithms` is empty, the default preference list
  `[sha256, sha512, sha1, md5]` is used.
- If no source advertises an acceptable digest and `onMissing` is `fail`, the
  processor aborts without downloading. `onMissing: compute` falls through to
  the download-and-hash path (SHA-256).
- When the resource already carries a pinned `digest`, its algorithm and value
  MUST agree with the source-advertised digest for the same algorithm; a
  mismatch is a hard error.

{{< callout context="caution" >}}
`accessDigest` applies to the digest processor only. Transferring a `Wget/v1`
access resource **by value** (`--copy-resources`) promotes it to a
`LocalBlob/v1` and re-runs the input-side rules — the bytes are streamed
into the target and re-digested as SHA-256, regardless of `accessDigest`. This
preserves the OCM invariant that every local blob is self-describing.
{{< /callout >}}

## Credential Scoping

Sibling checksum URLs (`externalUrl`) and HEAD requests reuse the artifact's
OCM credentials only when the resolved URL matches the artifact URL's exact
origin (scheme + host + port) and uses HTTPS. Cross-origin or plain-HTTP
checksum URLs get an undecorated client and no `Authorization` header, so the
credential never leaves the trust boundary the operator configured. Redirects
on the credentialed client are hard-disabled: a 3xx cannot leak the header
cross-origin.

## Related Documentation

- [Working with HTTP Resources]({{< relref "docs/tutorials/wget-http-resources.md" >}}) — task-oriented walkthrough of the wget input and access types
- [Reference: Input and Access Types]({{< relref "input-and-access-types.md" >}}#wgetv1-input) — field reference for the `Wget/v1` input and access types
- [Reference: HTTP Client Configuration]({{< relref "http-client-configuration.md" >}}) — the transport-level `http.config.ocm.software/v1alpha1` configuration
- [Reference: Credential Consumer Identities: Wget]({{< relref "credential-consumer-identities.md" >}}#wget) — identity attributes and matching rules for `Wget` consumers
