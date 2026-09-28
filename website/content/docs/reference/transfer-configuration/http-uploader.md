---
title: "HTTP Uploader"
description: "Reference for http.uploader.transfer.config.ocm.software/v1alpha1: stream matched resources to an HTTP endpoint and publish a Wget/v1 access."
weight: 2
toc: true
---

Streams a matched resource's content directly to an HTTP endpoint (typically a
`PUT` upload) and rewrites the resource to a `Wget/v1` access pointing at the
uploaded location. The **source** may be any access type (wget, OCI, S3, GitHub,
…) — its content is fetched through the access-type-specific downloader; only the
**target** is always an HTTP endpoint. The source content is piped straight into
the request body, so it is never buffered in memory or on disk. The digest is
computed during the stream, or — when the source resource already carries one —
verified as the bytes pass through.

The upload request and the published access are kept separate. `method`, `header`,
`body` and `noRedirect` describe the **upload request** only. The **published**
`Wget/v1` access — the download access recorded on the transferred resource —
carries just the resolved `url` and `mediaType`, never the write verb, body or
request headers. This ensures a later `ocm download` issues a plain read (GET) and
cannot re-send the write request that would overwrite the uploaded object.

## Schema

{{< schema-renderer url="/schemas/bindings/go/transfer/HTTPUploaderConfig.schema.json" >}}

## Fields

`targetURL` and `mediaType` also become the published `Wget/v1` download access;
`method`, `header`, `body` and `noRedirect` apply to the upload request only:

| Field                 | Type                  | Applies to                        | Description                                                                                                                                        |
|-----------------------|-----------------------|-----------------------------------|----------------------------------------------------------------------------------------------------------------------------------------------------|
| `match.accessType`    | `runtime.Type`        | —                                 | Access type this uploader applies to; any alias matches, omitted version = any.                                                                    |
| `match.name`          | string (optional)     | —                                 | Restrict the match to resources with this exact name.                                                                                              |
| `match.version`       | string (optional)     | —                                 | Restrict the match to resources with this exact version.                                                                                           |
| `match.extraIdentity` | `map[string]string`   | —                                 | Restrict the match to resources whose identity contains these key/value pairs.                                                                     |
| `targetURL`           | CEL expression        | request + published (`url`)       | The upload URL; also the published download URL. See [CEL Expressions]({{< relref "docs/reference/transfer-configuration/cel-expressions.md" >}}). |
| `method`              | string                | request (`verb`)                  | HTTP method for the upload request. Defaults to PUT. Not on the published access.                                                                  |
| `header`              | `map[string][]string` | request                           | HTTP headers sent with the upload request. May be CEL-templated. Request only.                                                                     |
| `noRedirect`          | bool                  | request                           | Disable following HTTP redirects on the upload. Not on the published access.                                                                       |
| `mediaType`           | string                | request + published (`mediaType`) | Media type recorded on the resource. Defaults to the source's.                                                                                     |

## Templating Headers

`header` values are templated with the same `${…}` CEL expressions. This is how
you forward a checksum the source already advertised on the upload request. Header
expressions read `resource.digest`, which is only present when the source resource
carries a digest (e.g. pinned from the source via the checksum-http configuration),
so scope the rule with `match` so every matched resource has one. A value without
`${…}` is sent as a literal.

`resource.digest.value` is the **hex** digest and `resource.digest.hashAlgorithm`
is the OCM algorithm name (e.g. `SHA-256`). Two inbuilt CEL functions build a
strictly conformant
[`Content-Digest`](https://developer.mozilla.org/en-US/docs/Web/HTTP/Reference/Headers/Content-Digest)
/ `Repr-Digest` field (RFC 9530):

- `contentDigestAlgorithm(<name>)` maps the OCM algorithm name to the RFC 9530 key,
  lower-cased and canonicalized (`SHA-256` → `sha-256`, `SHA-512` → `sha-512`,
  `MD5` → `md5`, and `SHA-1` → the registered key `sha`). Matching is case- and
  separator-insensitive; an unknown algorithm fails the transfer.
- RFC 9530 carries the digest **value** as base64, while `resource.digest.value` is
  hex, so convert it with `base64.encode(hex.decode(resource.digest.value))`. The
  `base64.encode`/`base64.decode` functions come from the CEL
  [encoders extension](https://github.com/google/cel-go/blob/master/ext/README.md);
  `hex.encode`/`hex.decode` are inbuilt companions.

```yaml
  - type: http.uploader.transfer.config.ocm.software/v1alpha1
    match:
      accessType: Wget/v1
    targetURL: '${"https://mytarget.example.com/uploads" + url(resource.access.url).path}'
    method: PUT
    header:
      # RFC 9530 Content-Digest: sha-256=:<base64>:
      Content-Digest: ['${contentDigestAlgorithm(resource.digest.hashAlgorithm) + "=:" + base64.encode(hex.decode(resource.digest.value)) + ":"}']
      # A simple non-standard checksum header carrying the raw hex value.
      X-Checksum-Sha256: ['${resource.digest.value}']
      # A static literal header is sent verbatim (no ${...}).
      X-Uploaded-By: ['ocm-transfer']
```

To upload into JFrog Artifactory or Sonatype Nexus repositories, use the vendor
uploaders
[`artifactory.uploader.transfer.config.ocm.software/v1alpha1`]({{< relref "docs/reference/transfer-configuration/artifactory-uploader.md" >}})
or [`nexus.uploader.transfer.config.ocm.software/v1alpha1`]({{< relref "docs/reference/transfer-configuration/nexus-uploader.md" >}})
instead.

## Credentials

The uploader resolves credentials for the **target** URL independently from the
source resource, using the target host's
[consumer identity]({{< relref "docs/reference/credential-consumer-identities.md" >}})
(the same `Wget` identity used for downloads). Configure target-side credentials
the same way you would for any HTTP endpoint; see
[Credential Types]({{< relref "docs/reference/credential-types.md" >}}).
