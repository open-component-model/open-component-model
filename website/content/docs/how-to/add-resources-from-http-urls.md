---
title: "Add Resources from HTTP URLs"
description: "Add resources served over HTTP or HTTPS to a component version with the Wget input and access types, including authentication and download tuning."
icon: "🌐"
weight: 17
toc: true
---

## Goal

Add a resource that is served over plain HTTP or HTTPS to a component version, and configure its media type,
authentication, and download behavior. Typical content is a release archive, a checksum file, or a signed binary.

OCM offers two ways to do this, and they share one transport, credential, and download implementation:

- The [`Wget/v1` input type]({{< relref "docs/reference/input-and-access-types.md#wgetv1-input" >}}) downloads the
  content while the component version is constructed and embeds it as a local blob.
- The [`Wget/v1` access type]({{< relref "docs/reference/input-and-access-types.md#wgetv1-access" >}}) stores the URL
  and leaves the bytes on the remote server.

## You'll end up with

- A component version containing a resource fetched over HTTP or HTTPS, either embedded as a local blob or referenced
  by URL
- Credentials for the origin server configured in `.ocmconfig` rather than in the resource specification
- The resource downloaded back out of the component version to verify the round trip

**Estimated time:** ~10 minutes

## Prerequisites

- [OCM CLI]({{< relref "docs/getting-started/ocm-cli-installation.md" >}}) installed
- A `component-constructor.yaml` to add the resource to
- The URL reachable from the machine running OCM, plus credentials if the server requires them

## Steps

{{< steps >}}

{{< step >}}

### Decide whether to embed or reference the content

| Use the **input type** when                                 | Use the **access type** when                                    |
|-------------------------------------------------------------|-----------------------------------------------------------------|
| The content must be reproducible and available offline      | The URL is the authoritative location and should stay so        |
| The upstream URL may disappear or change content            | The artifact is large and should not be duplicated              |
| You are building an air-gapped or self-contained repository | Consumers are expected to fetch directly from the origin server |

{{< callout context="caution" >}}
The distinction only holds for as long as the component version stays where it was built. 
To transfer a `Wget/v1` resource, the `--copy-resources true` flag must be set on `ocm transfer cv`.
Wget resources are transferred by value, so transferring a component version turns its `Wget/v1` access specifications into
[`LocalBlob/v1`]({{< relref "docs/reference/input-and-access-types.md#localblobv1" >}}) entries in the target
repository.
{{< /callout >}}

{{< /step >}}

{{< step >}}

### Add the resource to your constructor

{{< tabs "wget-spec" >}}
{{< tab "Input type" >}}

```yaml
resources:
  - name: ocm-cli
    type: blob
    version: 1.0.0
    input:
      type: Wget/v1
      url: https://github.com/open-component-model/open-component-model/releases/download/v0.12.0/cli.tar
      mediaType: application/x-tar
```

{{< /tab >}}
{{< tab "Access type" >}}

```yaml
resources:
  - name: ocm-cli
    type: blob
    version: 1.0.0
    relation: external
    access:
      type: Wget/v1
      url: https://github.com/open-component-model/open-component-model/releases/download/v0.12.0/cli.tar
      mediaType: application/x-tar
```

{{< /tab >}}
{{< /tabs >}}

Both spellings accept the same fields. For a non-`GET` request, set `verb`, `header`, and `body`. Note that `body` is
base64-encoded in YAML because the underlying field is a byte slice:

```yaml
resources:
  - name: report
    type: blob
    version: 1.0.0
    input:
      type: Wget/v1
      url: https://api.example.com/reports
      verb: POST
      mediaType: application/json
      header:
        Accept:
          - application/json
        X-Request-Source:
          - ocm
      # base64 of {"format":"json"}
      body: eyJmb3JtYXQiOiJqc29uIn0=
```

{{< callout context="caution" >}}
**Never put credentials in `url`, `header`, or `body`.** That includes userinfo (`https://user:token@host/...`) and
presigned query parameters. Anything that is part of the specification is stored, not just headers.

An **access** specification is stored verbatim in the component descriptor: everything written there is persisted with
the component version, travels with it through every transfer, is covered by its signature, and is readable by anyone
who can read the component version. An **input** specification is resolved at construction time and never reaches the
descriptor, but it does live in your `component-constructor.yaml`, which is normally checked into version control.
Configure authentication through the credential system instead, as described in the next step.
{{< /callout >}}

{{< /step >}}

{{< step >}}

### Set the media type explicitly

The media type of the resulting blob is resolved in this order:

1. The `mediaType` field of the specification, if set.
2. The `Content-Type` header of the HTTP response.
3. `application/octet-stream`.

Set `mediaType` whenever the server does not send a useful `Content-Type`. Otherwise the resource ends up as
`application/octet-stream`. GitHub release assets are a common case: they are served as `application/octet-stream`
regardless of what they contain, which is why the example above sets `mediaType: application/x-tar` explicitly.

{{< /step >}}

{{< step >}}

### Configure credentials for the host

The release asset used above is public, so this step is not needed to follow along. It applies as soon as the URL sits
behind authentication, which includes assets in a private GitHub repository.

Credentials are resolved from the resource URL through a consumer identity of type `Wget`. The input type and the access
type derive that identity identically, so a single entry in `.ocmconfig` covers construction and later downloads:

```yaml
type: generic.config.ocm.software/v1
configurations:
  - type: credentials.config.ocm.software
    consumers:
      - identity:
          type: Wget
          hostname: downloads.example.com
          scheme: https
        credentials:
          - type: WgetCredentials/v1
            username: my-user
            password: my-password
```

HTTP Basic Auth, bearer tokens, and mutual TLS are supported. Basic Auth and a bearer token both set the
`Authorization` header and are mutually exclusive. When both are configured, the bearer token wins and a warning is
logged. A client certificate combines with either, but only takes effect during a TLS handshake.

See [Credential Consumer Identities: Wget]({{< relref "docs/reference/credential-consumer-identities.md#wget" >}}) for
the identity attributes and matching rules, and
[Credential Types: WgetCredentials/v1]({{< relref "docs/reference/credential-types.md#wgetcredentialsv1" >}}) for the
full field reference.

{{< /step >}}

{{< step >}}

### Tune the download (optional)

Only 2xx responses are accepted; any other status fails the operation. Redirects are followed by default. Set
`noRedirect: true` to assert that a URL serves content directly rather than to fetch the redirect target. The request
then stops at the first redirect response and fails, because a 3xx status is not a success status.

Response bodies are streamed to a file on disk rather than buffered, so memory use stays flat regardless of artifact
size. There is no size limit by default, so a download is bounded by free disk space. The file is created under the
`tempFolder` of the `filesystem.config.ocm.software/v1alpha1` configuration type, falling back to the operating
system's temporary directory:

```yaml
type: generic.config.ocm.software/v1
configurations:
  - type: filesystem.config.ocm.software/v1alpha1
    tempFolder: /var/tmp/ocm
```

Point `tempFolder` at a volume with enough free space when you reference large artifacts, and at an encrypted volume
when the content is sensitive.

Timeouts, retries, connection settings, and per-host overrides come from the `http.config.ocm.software/v1alpha1`
configuration type and apply to every Wget request:

```yaml
type: generic.config.ocm.software/v1
configurations:
  - type: http.config.ocm.software/v1alpha1
    timeout: 2m
    retry:
      maxRetries: 3
    hosts:
      github.com:
        timeout: 5m
```

See [HTTP Client Configuration]({{< relref "docs/reference/http-client-configuration.md" >}}) for the full schema,
defaults, and per-host merge semantics.

{{< /step >}}

{{< step >}}

### Build the component version and verify

```bash
ocm add cv --repository ./transport-archive --constructor component-constructor.yaml
```

You should see the component listed:

```text
 COMPONENT                 │ VERSION │ PROVIDER
───────────────────────────┼─────────┼──────────
 github.com/acme.org/myapp │ 1.0.0   │ acme.org
```

Check that the resource landed with the media type you expect:

```bash
ocm get cv ./transport-archive//github.com/acme.org/myapp:1.0.0 -o yaml
```

An input-type resource resolves to a `LocalBlob/v1` access carrying the resolved media type:

{{< details "Expected output" >}}

```yaml
    resources:
      - access:
          localReference: sha256:6e3205bbad194f902ee8bdc5a712c47f7a5443fabfd066ef1e95088c837fe0ae
          mediaType: application/x-tar
          type: LocalBlob/v1
        digest:
          hashAlgorithm: SHA-256
          normalisationAlgorithm: genericBlobDigest/v1
          value: 6e3205bbad194f902ee8bdc5a712c47f7a5443fabfd066ef1e95088c837fe0ae
        name: ocm-cli
        relation: local
        type: blob
        version: 1.0.0
```

{{< /details >}}

An access-type resource keeps the `Wget/v1` specification verbatim, with the digest computed over the fetched bytes:

{{< details "Expected output" >}}

```yaml
    resources:
      - access:
          mediaType: application/x-tar
          type: Wget/v1
          url: https://github.com/open-component-model/open-component-model/releases/download/v0.12.0/cli.tar
        digest:
          hashAlgorithm: SHA-256
          normalisationAlgorithm: genericBlobDigest/v1
          value: 6e3205bbad194f902ee8bdc5a712c47f7a5443fabfd066ef1e95088c837fe0ae
        name: ocm-cli
        relation: external
        type: blob
        version: 1.0.0
```

{{< /details >}}

Downloading the resource proves the transport and credentials work end to end:

```bash
ocm download resource ./transport-archive//github.com/acme.org/myapp:1.0.0 \
  --identity name=ocm-cli \
  --output ./ocm-cli
```

You should see: `level=INFO msg="resource downloaded successfully" output=./ocm-cli`.

{{< callout context="note" >}}
When the media type is an archive format the CLI can unpack, `--output` is a **directory** holding the extracted
contents. Other media types are written to a file at that path.
{{< /callout >}}

{{< /step >}}

{{< /steps >}}

## Pinning an expected digest

Wget content comes from a server you do not control, so vet it before it enters the component version. Download the
artifact and run whatever your process requires: a virus scan, a check against the vendor's published checksum, a
manual review. Then hash the copy you trust and pin that value with `digest`. On the next `ocm add cv`, OCM hashes what
it fetches and refuses to add the resource if the two values differ:

```yaml
components:
  - name: github.com/acme.org/myapp
    version: 1.0.0
    provider:
      name: acme.org
    resources:
      - name: ocm-cli
        type: blob
        version: 1.0.0
        digest:
          hashAlgorithm: SHA-256
          normalisationAlgorithm: genericBlobDigest/v1
          value: 6e3205bbad194f902ee8bdc5a712c47f7a5443fabfd066ef1e95088c837fe0ae
        access:
          type: Wget/v1
          url: https://github.com/open-component-model/open-component-model/releases/download/v0.12.0/cli.tar
          mediaType: application/x-tar
```

The field is optional and applies to both the input and the access type.

Hash the copy you trust:

```bash
sha256sum cli.tar     # Linux
shasum -a 256 cli.tar # macOS
```

{{< callout context="caution" >}}
Write all three fields:

- `hashAlgorithm` must be `SHA-256`. It is the only algorithm the Wget resource repository computes.
- `normalisationAlgorithm` must be `genericBlobDigest/v1`, meaning the raw bytes are hashed with no normalisation.
- `value` is the bare lowercase hex digest, **without** a `sha256:` prefix.

On an access-type resource the constructor schema requires all three and each is matched exactly. On an input-type
resource the schema does not validate the block at all, and only `hashAlgorithm` and `value` are checked, so a wrong
`normalisationAlgorithm` is accepted and written to the descriptor unchanged.
{{< /callout >}}

The two types report a mismatch differently, because the check happens in a different place. The access type verifies
while computing the digest; the input type verifies while storing the blob, and prefixes the computed value:

```text
digest mismatch: expected 6e3205bb…, got 3b1f0c72…                          # access type
resource blob digest mismatch: resource 6e3205bb… vs blob sha256:3b1f0c72…  # input type
```

For the access type the check is not confined to construction. The bytes are fetched again whenever a component version
referencing the URL is transferred, and verified against the same digest, so content that changes after publication
makes the transfer fail rather than substituting different bytes. That transfer-time check is the input-type one, since
the content is stored as a local blob in the target.

## Migrating from OCM v1

### Constructor changes

| Area         | OCM v1       | OCM v2                    | What to do                                              |
|--------------|--------------|---------------------------|---------------------------------------------------------|
| Input `body` | Plain string | Base64-encoded byte slice | Base64-encode the body in `component-constructor.yaml`. |

The OCM v1 access specification typed `body` as an `io.Reader`, which has no YAML representation, so this affects
constructor files using the input type.

### Behavior changes

| Area        | OCM v1                                                                             | OCM v2                                                    |
|-------------|------------------------------------------------------------------------------------|-----------------------------------------------------------|
| Media type  | `mediaType` → `Content-Type` → **URL file extension** → `application/octet-stream` | `mediaType` → `Content-Type` → `application/octet-stream` |
| Minimum TLS | TLS 1.3                                                                            | TLS 1.2                                                   |

Set `mediaType` explicitly wherever OCM v1 relied on the URL's file extension. A `.tar.gz` URL that previously
resolved to `application/x-gzip` now falls back to the server's `Content-Type`, or to `application/octet-stream`.

### Credential changes

Two changes are breaking. A carried-over entry does not match, no credentials are resolved, and the request goes out
unauthenticated, so the symptom is a `401` from the server rather than a configuration error.

| Area              | OCM v1                             | OCM v2             | What to do                                                                                                                                         |
|-------------------|------------------------------------|--------------------|----------------------------------------------------------------------------------------------------------------------------------------------------|
| Consumer identity | `type: wget`                       | `type: Wget`       | Update `.ocmconfig`. Identity types match by exact string, so the lowercase spelling silently resolves no credentials.                             |
| Identity path     | `pathprefix`, longest-prefix match | `path`, glob match | Rename the attribute. `*` matches one path segment, so a prefix spanning nested paths has no glob equivalent: omit `path` to match the whole host. |

The following change what a matching entry does, without any change to the entry itself:

| Area            | OCM v1                                          | OCM v2                                                          |
|-----------------|-------------------------------------------------|-----------------------------------------------------------------|
| Auth precedence | Basic Auth wins; the bearer token is a fallback | Bearer token wins; Basic Auth is used only when no token is set |
| Basic Auth      | Requires both `username` and `password`         | Sent as soon as `username` is set                               |
| Custom CA       | Root CAs from credentials always applied        | `certificateAuthority` applied only alongside `certificate`     |

The precedence flip only becomes visible when a single entry carries `identityToken` **and** both `username` and
`password`: OCM v1 sends Basic Auth, OCM v2 sends the bearer token and logs a warning. With only a token, or only a
username/password pair, both versions behave identically. Split such entries if the server distinguishes the two
mechanisms.

## Troubleshooting

### Symptom: `401 Unauthorized` from the origin server

**Cause:** Either the credentials that were sent are wrong, or no consumer entry matched the identity OCM derives from
the resource URL and the request went out unauthenticated. The second case is the harder one to spot: a non-matching
entry is not an error, so nothing is logged and the server's response is the first sign of it.

**Fix:** Check the credentials themselves first, then check that the entry matches. `hostname` has to match the URL
host; `scheme`, `port`, and `path` narrow the match further whenever they are set, so the entry that covers every
download from a host is the one that omits all three:

```yaml
- identity:
    type: Wget
    hostname: downloads.example.com
  credentials:
    - type: WgetCredentials/v1
      username: my-user
      password: my-password
```

See [Credential Consumer Identities: Wget]({{< relref "docs/reference/credential-consumer-identities.md#wget" >}}) for
how the identity is derived from a URL, and for the matching rules of each attribute.

### Symptom: `401 Unauthorized` after migrating a consumer entry from OCM v1

**Cause:** Two identity attributes were renamed, so an entry carried over unchanged matches nothing:

- The type is `Wget`, matched by exact string and unversioned. The lowercase `wget` of OCM v1 does not match, and
  neither does `Wget/v1`.
- `pathprefix` no longer exists. OCM v2 uses `path`, matched with `path.Match`, whose `*` does not cross `/`, so it
  cannot express a prefix spanning nested paths.

**Fix:** Rename the type and drop `pathprefix`. Omitting `path` matches every path on the host:

```yaml
- identity:
    type: Wget            # not "wget", not "Wget/v1"
    hostname: downloads.example.com
    # was: pathprefix: my-org
```

Set `path` only to pin one exact object (`path: my-org/app/1.0/app.tgz`), or a single segment (`path: my-org/*`, which
matches `my-org/app.tgz` but not `my-org/app/1.0.tgz`). See [Credential changes](#credential-changes) for the rest of
the migration.

### Symptom: the resource has media type `application/octet-stream`

**Cause:** The specification set no `mediaType` and the server sent no useful `Content-Type`. OCM v2 does not fall back
to the URL's file extension.

**Fix:** Set `mediaType` on the input or access specification.

### Symptom: `digest mismatch` when adding the component version

**Cause:** The resource pins a `digest` and the fetched bytes hash to something else. The content behind the URL
changed, the download was truncated or corrupted, or the pinned value was taken from a different artifact. The input
type reports the same condition as `resource blob digest mismatch`.

**Fix:** Re-download the URL and recompute the digest with `sha256sum`. If the new value is the one you trust, update
`value` in the constructor; if it is not, the content behind the URL changed and the pin did its job.

### Symptom: `unsupported hash algorithm`, `unsupported normalisation algorithm`, or `invalid hash algorithm`

**Cause:** The pinned `digest` names something other than the pair the Wget resource repository computes. Matching is
exact, so `sha256` or `SHA256` do not resolve to `SHA-256`. The access type reports `unsupported …`; the input type
reports `invalid hash algorithm` and never checks the normalisation algorithm at all.

**Fix:** Use the exact spellings:

```yaml
digest:
  hashAlgorithm: SHA-256                       # not "sha256", not "SHA256"
  normalisationAlgorithm: genericBlobDigest/v1
  value: 6e3205bb…                             # bare hex, no "sha256:" prefix
```

### Symptom: the operation fails reporting a 3xx status

**Cause:** `noRedirect: true` stops the request at the first redirect response, and a 3xx status is not a success
status.

**Fix:** Remove `noRedirect`, or point `url` at the final location the redirect resolves to.

## Next Steps

- [How-To: Download Resources from Component Versions]({{< relref "
  docs/how-to/download-resources-from-component-versions.md" >}}) -
  Fetch the resource you just added
- [How-To: Air-Gap Transfer]({{< relref "docs/how-to/air-gap-transfer.md" >}}) - Move component versions into
  disconnected environments

## Related Documentation

- [Reference: Input and Access Types]({{< relref "docs/reference/input-and-access-types.md" >}}) - Field reference for
  the `Wget/v1` input and access types
- [Reference: Resource Repositories]({{< relref "docs/reference/resource-repositories.md" >}}) - Capabilities,
  credential resolution, and digest processing of the Wget resource repository
- [Reference: Credential Consumer Identities]({{< relref "docs/reference/credential-consumer-identities.md#wget" >}}) -
  Identity attributes and matching rules for `Wget` consumers
- [Reference: HTTP Client Configuration]({{< relref "docs/reference/http-client-configuration.md" >}}) - Timeouts,
  retries, and per-host settings
