---
title: "Add HTTP Resources"
description: "Add a file served over HTTP or HTTPS to a component version with the Wget input or access type, then understand media types, credentials, non-GET requests, download tuning, and v1 migration."
weight: 20
toc: true
aliases:
  - /docs/how-to/add-resources-from-http-urls/
  - /docs/tutorials/wget-http-resources/
---

## Goal

Add a file served over plain HTTP or HTTPS (a release archive, a checksum, a signed binary) to a component version
using the `Wget/v1` type.

OCM provides two types for HTTP resources. They differ in when the file is fetched and where the bytes end up:

- The [**input type**]({{< relref "docs/reference/input-and-access-types.md#wgetv1-input" >}}) downloads the file while
  the component version is built and stores it as a localBlob in the component version.
- The [**access type**]({{< relref "docs/reference/input-and-access-types.md#wgetv1-access" >}}) stores only the
  **URL** and leaves the file on the remote server.

Both types share the same download and credential code, so the rest of this guide applies to either one.

## You'll end up with

- A component version containing a resource fetched over HTTP or HTTPS, either embedded as a local blob or referenced by URL.
- That same resource downloaded back out, to confirm the round trip works

**Estimated time:** ~10 minutes

## Prerequisites

- [OCM CLI]({{< relref "docs/getting-started/ocm-cli-installation.md" >}}) installed
- A working directory for the constructor and the transport archive

This guide fetches a public OCM release asset, so it runs end-to-end with no registry and no credentials. To point it
at a server that needs authentication, see [Authenticate against a protected server](#authenticate-against-a-protected-server).

## Steps

{{< steps >}}

{{< step >}}

### Create the component constructor

Both variants below describe the same resource with the same fields. Write one of them to `component-constructor.yaml`:

{{< tabs "wget-spec" >}}
{{< tab "Input type" >}}

```bash
cat > component-constructor.yaml << 'EOF'
components:
  - name: github.com/acme.org/myapp
    version: 1.0.0
    provider:
      name: acme.org
    resources:
      - name: ocm-cli
        type: blob
        version: 1.0.0
        input:
          type: Wget/v1
          url: https://github.com/open-component-model/open-component-model/releases/download/v0.12.0/cli.tar
          mediaType: application/x-tar
EOF
```

{{< /tab >}}
{{< tab "Access type" >}}

```bash
cat > component-constructor.yaml << 'EOF'
components:
  - name: github.com/acme.org/myapp
    version: 1.0.0
    provider:
      name: acme.org
    resources:
      - name: ocm-cli
        type: blob
        version: 1.0.0
        relation: external
        access:
          type: Wget/v1
          url: https://github.com/open-component-model/open-component-model/releases/download/v0.12.0/cli.tar
          mediaType: application/x-tar
EOF
```

{{< /tab >}}
{{< /tabs >}}

Set `mediaType` explicitly here. Otherwise, OCM will default to the `Content-Type` served by GitHub, which for release assets is always `application/octet-stream`. The full `mediaType` defaulting mechanism of `Wget` is described in
[Set the media type](#set-the-media-type).

{{< callout context="caution" >}}
**Never put credentials in `url`, `header`, or `body`.** That includes user info (`https://user:token@host/...`) and
presigned query parameters. An access specification is stored in the component descriptor then signed and finally included in
the transfer. An input specification lives in your constructor file, which is usually checked into version control.
Either way the secret leaks. Use the [credential system]({{< relref "docs/concepts/credential-system.md" >}}) instead, as shown in
[Authenticate against a protected server](#authenticate-against-a-protected-server).
{{< /callout >}}

{{< /step >}}

{{< step >}}

### Build the component version

```bash
ocm add cv
```

The asset is ~25 MB, so this takes a moment. You should see the component listed:

```text
 COMPONENT                 │ VERSION │ PROVIDER
───────────────────────────┼─────────┼──────────
 github.com/acme.org/myapp │ 1.0.0   │ acme.org
```

{{< /step >}}

{{< step >}}

### Check what ended up in the descriptor

```bash
ocm get cv ./transport-archive//github.com/acme.org/myapp:1.0.0 -o yaml
```

The **input type** stores the bytes locally, so the access becomes `LocalBlob/v1`:

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

The **access type** keeps the `Wget/v1` specification as written:

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

In both cases, the resource has the same digest, computed over the bytes that were fetched.

{{< /step >}}

{{< step >}}

### Download the resource back

This proves the transport (and, on a protected server, the credentials) work end-to-end:

```bash
ocm download resource ./transport-archive//github.com/acme.org/myapp:1.0.0 \
  --identity name=ocm-cli \
  --output ./ocm-cli
```

You should see: `level=INFO msg="resource downloaded successfully" output=./ocm-cli`.

{{< callout context="note" >}}
When the media type is an archive the CLI can unpack, `--output` is a **directory** with the extracted contents.
Other media types are written to a file at that path. `cli.tar` is a `.tar`, so `./ocm-cli` is a directory here.
{{< /callout >}}

{{< /step >}}

{{< /steps >}}

## Authenticate against a protected server

The release asset above is public. When accessing the URL requires authentication (an artifact repository, or a private
GitHub asset), [configure credentials]({{< relref "docs/guides/transfer/configure-registry-credentials.md" >}}) of type `WgetCredentials` instead of putting them in the specification. OCM matches them to the request
by a consumer identity of type `Wget` derived from the URL.
Another silent cause: an entry that sets only certificateAuthority without certificate. In OCM v2, certificateAuthority is only evaluated alongside a client certificate. It is not applied for server-side certificate verification on its own.

Write the credentials to `.ocmconfig`:

```bash
cat > .ocmconfig << 'EOF'
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
EOF
```

Then pass `--config` when you build the component (or drop the file in one of the
[well-known locations]({{< relref "docs/reference/ocm-cli/ocm.md" >}}) the CLI reads automatically):

```bash
ocm --config .ocmconfig add cv
```

HTTP Basic Auth, bearer tokens, and mutual TLS are all supported. For how the identity is matched and how the auth
methods interact, see the [Authentication](#authentication) section below.

## Pin a digest {#pin-a-digest}

A Wget resource comes from a server you don't control, so it's good practice to check the content before you trust it
and then pin what you checked. Download the file, run whatever your process requires and hash the copy after:

```bash
sha256sum cli.tar     # Linux
shasum -a 256 cli.tar # macOS
```

Add that value as a `digest` on the resource. From then on, OCM hashes whatever it fetches and refuses to add the
resource if the hashes differ:

```yaml
resources:
  - name: ocm-cli
    type: blob
    version: 1.0.0
    relation: external
    digest:
      hashAlgorithm: SHA-256
      normalisationAlgorithm: genericBlobDigest/v1
      value: 6e3205bbad194f902ee8bdc5a712c47f7a5443fabfd066ef1e95088c837fe0ae
    access:
      type: Wget/v1
      url: https://github.com/open-component-model/open-component-model/releases/download/v0.12.0/cli.tar
      mediaType: application/x-tar
```

The `digest` block is optional and works on both the input and the access type. If set, these are the exact fields:

- `hashAlgorithm` MUST be `SHA-256`. It is the only algorithm the Wget resource repository computes. `sha256` and
  `SHA256` do not match.
- `normalisationAlgorithm` MUST be `genericBlobDigest/v1`, which means "hash the raw bytes, no normalisation".
- `value` is the bare lowercase hex digest, with **no** `sha256:` prefix.

### Why access-type pinning {#why-access-type-pinning}

For the access type, the digest is checked again every time the component version is transferred, because the bytes are
re-fetched at transfer time (see [How transfer works](#how-transfer-works)). So, a file that changes *after* you publish
the component version makes the next transfer fail, rather than silently swapping in different content.

```text
digest mismatch: expected 6e3205bb…, got 3b1f0c72…                          # access type
resource blob digest mismatch: resource 6e3205bb… vs blob sha256:3b1f0c72…  # input type
```

## Input or access: where the bytes live {#choosing-input-or-access}

Both types describe the same resource with the same fields. The only difference is *when* the file is fetched and
*where* it ends up.

- With the **input type**, OCM downloads the file the moment you run `ocm add cv` and stores it inside the component
  version as a local blob. The original URL is not kept. The component version is now self-contained.

- With the **access type**, OCM stores the URL in the component descriptor and fetches the bytes only when someone asks
  for them. The URL stays the source of truth, which is what you want for a large file, or when consumers are meant to
  pull directly from the origin server.

The rule of thumb is: use **input type** when you want a reproducible copy, and the **access type** when the URL
is the authoritative location and should be used for accessing the resource.

## How transfer works {#how-transfer-works}

By default, a Wget resource stays by reference: without a matching uploader, `ocm transfer cv` keeps the
`Wget/v1` access unchanged in the target, and the file stays on the remote server.

With a matching local blob uploader configuration (in your OCM configuration, for example `.ocmconfig` in the working directory), OCM fetches the bytes and writes them into the
target as a [`LocalBlob/v1`]({{< relref "docs/reference/input-and-access-types.md#localblobv1" >}}). This means:

1. To embed Wget resources, add a `localblob.uploader.transfer.config.ocm.software/v1alpha1` entry to your OCM
   configuration. Without it, the `Wget/v1` access remains by reference.
2. When the local blob uploader copies the resource, the bytes are fetched *again at transfer time* and checked against
   the resource's digest. If the file behind the URL changed since the component version was built, the transfer fails
   instead of copying different content.

To keep a resource behind a URL instead of embedding it — streaming it to a custom HTTP target and rewriting the access to a new `Wget/v1` URL — configure an uploader; see [Upload to a Custom Target]({{< relref "docs/guides/transfer/upload-to-custom-target.md" >}}).

### Forward a digest header to the upload target {#uploader-digest-header}

An uploader can template its `header` values as `${…}` CEL expressions over the source resource, so
the upload `PUT` can carry a checksum the source already advertised, read from
`resource.digest`. To emit a standards-compliant
[`Content-Digest`](https://developer.mozilla.org/en-US/docs/Web/HTTP/Reference/Headers/Content-Digest)
(RFC 9530), map the OCM algorithm name to the RFC 9530 key with `contentDigestAlgorithm()`
and convert the hex digest to base64 with `base64.encode(hex.decode(...))`:

```yaml
type: generic.config.ocm.software/v1
configurations:
  - type: http.uploader.transfer.config.ocm.software/v1alpha1
    match: resource.access.isType("Wget/v1")
    targetURL: '${"https://mytarget.example.com/uploads" + url(resource.access.url).path}'
    method: PUT
    header:
      # RFC 9530 Content-Digest: sha-256=:<base64>:
      Content-Digest: ['${contentDigestAlgorithm(resource.digest.hashAlgorithm) + "=:" + base64.encode(hex.decode(resource.digest.value)) + ":"}']
```

{{< callout context="caution" >}}
Use the `Content-Digest` expression only when `resource.digest` was produced by a
byte-preserving normalization such as `genericBlobDigest/v1`. The uploader streams the
source bytes unchanged, so a digest built from a non-byte-preserving normalization does
not describe the transmitted content and a target that validates `Content-Digest` may
reject the upload.
{{< /callout >}}

`resource.digest` is only present when the source resource carries a digest (for
example when it is pinned from the source via the checksum-http configuration). See
[Templating Headers]({{< relref "docs/reference/transfer-configuration/http-uploader.md" >}}#templating-headers)
for the full field reference.

For a target such as JFrog Artifactory, which verifies uploads against a **hex**
`X-Checksum-*` header, `resource.digest.value` maps on directly — no base64
conversion:

```yaml
  - type: http.uploader.transfer.config.ocm.software/v1alpha1
    match: resource.access.isType("Wget/v1")
    targetURL: '${"https://myorg.jfrog.io/artifactory/my-repo" + url(resource.access.url).path}'
    method: PUT
    header:
      X-Checksum-Sha256: ['${resource.digest.value}']
```

## Set the media type {#set-the-media-type}

For a Wget resource, OCM picks the media type in the following order and stops at the first one it finds:

1. The `mediaType` field in your specification, if you set it.
2. The `Content-Type` header the server returns.
3. `application/octet-stream`, as a last resort.

**Practical advice**: set `mediaType` yourself whenever the server doesn't return a useful `Content-Type`.
The most common examples are GitHub release assets. GitHub serves all of them as `application/octet-stream`, no matter what
they actually are, so a `.tar` file would end up with a meaningless media type unless you set `mediaType:
application/x-tar` explicitly.

{{< callout context="note" >}}
OCM v2 does **not** guess the media type from the file extension in the URL. OCM v1 did. See
[Migrate from OCM v1](#migrating-from-ocm-v1) if you are moving old constructor files across.
{{< /callout >}}

## Authentication {#authentication}

Credentials are resolved through OCM's normal [credential system]({{< relref "docs/concepts/credential-system.md" >}}). Never add authentication to a URL directly. OCM
builds a [consumer identity]({{< relref "docs/reference/credential-consumer-identities.md#wget" >}}) of type `Wget` from
the URL and uses the matching consumer entry's credentials for the request. Because the input type and the access type
build that identity the same way, one entry will work for both, building the component version, and downloading the resource.
For how identities are matched and resolved, see [Understand Credential Resolution]({{< relref "docs/concepts/credential-resolution.md" >}}).

Wget's credential type is: `WgetCredentials/v1`.

### Authentication methods {#auth-methods}

`WgetCredentials/v1` supports three methods:

| Method                   | Fields                                                           | Sets                      |
|--------------------------|------------------------------------------------------------------|---------------------------|
| HTTP Basic Auth          | `username`, `password`                                           | `Authorization: Basic …`  |
| Bearer token             | `identityToken`                                                  | `Authorization: Bearer …` |
| Mutual TLS (client cert) | `certificate`, `privateKey`, and optional `certificateAuthority` | The TLS handshake         |

- Basic Auth and a bearer token both set the `Authorization` header, **so they are mutually exclusive.** If you
  configure both, the bearer token wins and OCM logs a warning.
- **The client certificate is separate.** It is applied during the TLS handshake, not in a header, so it works independent 
  of the other two.

{{< callout context="caution" >}}
Put secrets in your credential configuration, never in the specification. Anything you write into `url`, `header`, or
`body` (including `https://user:token@host/...` and presigned query parameters) is stored with the component version
(access type) or lives in your constructor file (input type). Neither is a safe place for a secret.
{{< /callout >}}

For the full field reference, see
[Credential Types: WgetCredentials/v1]({{< relref "docs/reference/credential-types.md#wgetcredentialsv1" >}}).

## Send a non-GET request {#non-get-requests}

By default, OCM sends a `GET` request. To send something else, set `verb`, and optionally `header` and `body`. `body` is
base64-encoded in YAML, because the underlying field is a byte slice:

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

## Fine-tuning the download {#fine-tuning-the-download}

**Only successful responses are accepted.** OCM accepts a 2xx status and fails on anything else. Redirects are followed by
default. If you set `noRedirect: true`, the request fails.

**Downloads are streamed to disk, not held in memory.** The response body is written straight to a temporary file, so
memory use stays flat no matter how big the file is. There is no size limit by default. That temporary file is created
under the `tempFolder` of the `filesystem.config.ocm.software/v1alpha1` attribute in the .ocmconfig configuration file, falling back to the operating
system's temp directory if no configuration is provided.

**Timeouts, retries, and per-host settings** come from the `http.config.ocm.software/v1alpha1` configuration and apply
to every Wget request:

```yaml
type: generic.config.ocm.software/v1
configurations:
  - type: filesystem.config.ocm.software/v1alpha1
    tempFolder: /var/tmp/ocm
  - type: http.config.ocm.software/v1alpha1
    timeout: 2m
    retry:
      maxRetries: 3
    hosts:
      github.com:
        timeout: 5m
```

See [HTTP Client Configuration]({{< relref "docs/reference/http-client-configuration.md" >}}) for the full schema,
defaults, and how per-host settings are merged.

## Verifying downloads against the source {#checksum-verification}

A pinned resource `digest` protects consumers after the build, but the first
`ocm add cv` still trusts whatever the server sends. A **source-side checksum**
closes that gap: OCM compares the downloaded bytes against a digest the source
advertises in response headers (RFC 9530 `Content-Digest`, `x-checksum-*`) and
fails on a mismatch. It is configured centrally via
`checksum.http.config.ocm.software/v1alpha1`, not in the constructor. See
[HTTP Checksum Configuration]({{< relref "docs/reference/checksum-http-configuration.md" >}})
for the schema, checksum modes, precedence, and the access-side fast path.

## Migrate from OCM v1 {#migrating-from-ocm-v1}

Three things changed between OCM v1 and v2: credential matching, constructor syntax, and behavior. The credential changes are the most likely to break existing configurations.

### Credential changes {#credential-changes}

Identity matching has been updated.

| Field             | OCM v1                             | OCM v2             | What to do                                                                               |
|-------------------|------------------------------------|--------------------|------------------------------------------------------------------------------------------|
| Consumer identity | `type: wget`                       | `type: Wget`       | Rename the type.                                                                         |
| Identity path     | `pathprefix`, longest-prefix match | `path`, glob match | Rename the attribute. `*` matches one path segment; omit `path` to match the whole host. |

Further matching changes:

| Area            | OCM v1                                          | OCM v2                                                          |
|-----------------|-------------------------------------------------|-----------------------------------------------------------------|
| Auth precedence | Basic Auth wins; the bearer token is a fallback | Bearer token wins; Basic Auth is used only when no token is set |
| Basic Auth      | Needs both `username` and `password`            | `username` is enough                                            |
| Custom CA       | Root CAs from credentials always applied        | `certificateAuthority` applied only alongside `certificate`     |

### Constructor changes {#constructor-changes}

| Field        | OCM v1       | OCM v2                    | What to do                                 |
|--------------|--------------|---------------------------|--------------------------------------------|
| `input.body` | Plain string | Base64-encoded byte slice | Base64-encode the body in the constructor. |

In OCM v1 the body was an `io.Reader`, which has no YAML form. In OCM v2 it is a byte slice, therefore it needs to be base64.

### Behavior changes{#behavior-changes}

| Area        | OCM v1                                                                             | OCM v2                                                    |
|-------------|------------------------------------------------------------------------------------|-----------------------------------------------------------|
| Media type  | `mediaType` → `Content-Type` → **URL file extension** → `application/octet-stream` | `mediaType` → `Content-Type` → `application/octet-stream` |
| Minimum TLS | TLS 1.3                                                                            | TLS 1.2                                                   |

OCM v2 **dropped** the file-extension guess, so a `.tar.gz` URL that used to resolve to `application/x-gzip` on its own, now
needs an explicit `mediaType`.

## Troubleshooting {#troubleshooting}

### `401 Unauthorized` from the server

**Why:** Either the credentials are wrong, or no consumer entry matched the identity OCM built from the URL.  The second case is hard to spot: when nothing matches, OCM sends the request without credentials and the server returns the same 401 either way.

**Fix:** Check the credentials first, then check that the entry matches. `hostname` must equal the URL host; `scheme`,
`port`, and `path` narrow the match only when set, so the broadest entry is the one that sets only `hostname`. See
[Credential Consumer Identities: Wget]({{< relref "docs/reference/credential-consumer-identities.md#wget" >}}).

### `401 Unauthorized` right after migrating from OCM v1

**Why:** Two identity attributes were renamed. `type: wget` MUST become `type: Wget`, and `pathprefix` no longer exists,
because OCM v2 uses `path`.

**Fix:** Rename the type and drop `pathprefix`. Omitting `path` matches every path on the host. See
[Credential changes](#credential-changes).

### The resource has media type `application/octet-stream`

**Why:** No `mediaType` was set and the server sent no useful `Content-Type`. OCM v2 does not fall back to the file
extension.

**Fix:** Set `mediaType` on the input or access specification.

### `digest mismatch` when adding the component version

**Why:** The resource pins a `digest` and the fetched bytes hash to something else: the content behind the URL changed,
the download was truncated, or the pinned value came from a different file. The error to watch for is `resource blob
digest mismatch`.

**Fix:** Re-download the URL and recompute the digest. If the new value is the one you have, update `value`. If it
isn't, the content changed.

### The operation fails reporting a 3xx status

**Why:** `noRedirect: true` is set.

**Fix:** Remove `noRedirect`, or point `url` at the final location the redirect resolves to.

## Next Steps

- [Download Resources]({{< relref "docs/guides/transfer/download-resources.md" >}}) -
  Fetch the resource you just added
- [Transfer Components across an Air Gap]({{< relref "docs/guides/transfer/air-gap-transfer.md" >}}) - Move component versions into
  disconnected environments

## Related Documentation

- [Reference: Input and Access Types]({{< relref "docs/reference/input-and-access-types.md" >}}) - Field reference for
  the `Wget/v1` input and access types
- [Reference: Resource Repositories]({{< relref "docs/reference/resource-repositories.md" >}}) - Capabilities,
  credential resolution, and digest processing of the Wget resource repository
- [Reference: Credential Consumer Identities]({{< relref "docs/reference/credential-consumer-identities.md#wget" >}}) -
  Identity attributes and matching rules for `Wget` consumers
- [Reference: Credential Types]({{< relref "docs/reference/credential-types.md#wgetcredentialsv1" >}}) - Full field
  reference for `WgetCredentials/v1`
- [Reference: HTTP Client Configuration]({{< relref "docs/reference/http-client-configuration.md" >}}) - Timeouts,
  retries, and per-host settings
