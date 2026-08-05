---
title: "Add Resources from HTTP URLs"
description: "Add a file served over HTTP or HTTPS to a component version with the Wget input or access type."
icon: "🌐"
weight: 17
toc: true
---

## Goal

Add a file served over plain HTTP or HTTPS (a release archive, a checksum, a signed binary) to a component version
using the `Wget/v1` type.

OCM provides two types for HTTP resources. They differ in when the file is fetched and where the bytes end up:

- The [**input type**]({{< relref "docs/reference/input-and-access-types.md#wgetv1-input" >}}) downloads the file while
  the component version is built and stores it as a localBlob in the component version.
- The [**access type**]({{< relref "docs/reference/input-and-access-types.md#wgetv1-access" >}}) stores only the
  **URL** and leaves the file on the remote server.

Both types share the same download and credential code, so the rest of this guide applies to either one. For the full set of options and the reasoning behind them, see the tutorial 
[Work with HTTP Resources]({{< relref "docs/tutorials/wget-http-resources.md" >}}).

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
[Set the media type]({{< relref "docs/tutorials/wget-http-resources.md#set-the-media-type" >}}).

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
GitHub asset), [configure credentials]({{< relref "docs/how-to/configure-multiple-credentials.md" >}}) of type `WgetCredentials` instead of putting them in the specification. OCM matches them to the request
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
methods interact, see the [Authentication]({{< relref "docs/tutorials/wget-http-resources.md#authentication" >}}) section in the
tutorial.

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
re-fetched at transfer time (see [How transfer works]({{< relref "docs/tutorials/wget-http-resources.md" >}})). So, a file that changes *after* you publish
the component version makes the next transfer fail, rather than silently swapping in different content.

```text
digest mismatch: expected 6e3205bb…, got 3b1f0c72…                          # access type
resource blob digest mismatch: resource 6e3205bb… vs blob sha256:3b1f0c72…  # input type
```

## Next Steps

- [How-To: Download Resources from Component Versions]({{< relref "docs/how-to/download-resources-from-component-versions.md" >}}) -
  Fetch the resource you just added
- [How-To: Air-Gap Transfer]({{< relref "docs/how-to/air-gap-transfer.md" >}}) - Move component versions into
  disconnected environments
- [Tutorial: Work with HTTP Resources]({{< relref "docs/tutorials/wget-http-resources.md" >}}) - Digest pinning,
  non-GET requests, download tuning, and migrating from OCM v1

## Related Documentation

- [Reference: Input and Access Types]({{< relref "docs/reference/input-and-access-types.md" >}}) - Field reference for
  the `Wget/v1` input and access types
- [Reference: Credential Consumer Identities]({{< relref "docs/reference/credential-consumer-identities.md#wget" >}}) -
  Identity attributes and matching rules for `Wget` consumers
- [Reference: Credential Types]({{< relref "docs/reference/credential-types.md#wgetcredentialsv1" >}}) - Full field
  reference for `WgetCredentials/v1`
