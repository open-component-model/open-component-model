---
title: "Working with HTTP Resources"
description: "Understand the Wget input and access types in depth: the input/access model, credentials, media types, digest pinning, download tuning, and migrating from OCM v1."
icon: "🌐"
weight: 62
toc: true
---

## Overview

OCM can add a file served over HTTP or HTTPS to a component version using the `Wget/v1` type. There is one download
engine underneath, with two front doors:

- The **input type** downloads the file while the component version is built and stores the bytes inside it.
- The **access type** stores only the URL and leaves the bytes on the remote server.

They share the same download, credential, and configuration code, so the choice between them is about *where the bytes
live*, not about what you can configure.

If you just want the commands to add a file and download it back, start with the How-To guide 
[Add Resources from HTTP URLs]({{< relref "docs/how-to/add-resources-from-http-urls.md" >}}). This tutorial explains
the parts that guide leaves out: why you'd pick one type over the other, how credentials are matched, how the media
type is decided, how digest pinning protects you, and what changed since OCM v1.

## What You'll Learn

- When to embed a file (input type) and when to reference it by URL (access type)
- What happens to a Wget resource when you transfer the component version
- How OCM resolves a resource's media type
- How credentials are matched to a request, and how the three authentication methods interact
- How to pin a resource by digest so a changed file is rejected instead of silently accepted
- How to send a non-GET request and tune timeouts, retries, and the download directory
- What changed between OCM v1 and OCM v2, and how to migrate

## Prerequisites

- [OCM CLI]({{< relref "docs/getting-started/ocm-cli-installation.md" >}}) installed
- Comfortable adding a component version with `ocm add cv`; the
  [how-to]({{< relref "docs/how-to/add-resources-from-http-urls.md" >}}) is a good warm-up

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

A Wget resource is always transferred by value.

When you run `ocm transfer cv`, an access-type resource does not stay a `Wget/v1` access in the target. OCM fetches the
bytes and writes them into the target as a [`LocalBlob/v1`]({{< relref "docs/reference/input-and-access-types.md#localblobv1" >}}). After a transfer
both will end up as local blobs.

This means:

1. You MUST pass the `--copy-resources` flag to `ocm transfer cv`. Without it, the resource is skipped, because there is no
   way to transfer it without copying the bytes.
2. The bytes are fetched *again at transfer time* and checked against the resource's digest. If the file behind the URL
   changed since the component version was built, the transfer fails instead of copying different content.

The file stays on the remote server only if the component version is never transferred. Transfer converts the `Wget/v1` reference into a `LocalBlob/v1`.

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
For how identities are matched and resolved, see [Understand Credential Resolution]({{< relref "docs/tutorials/credential-resolution.md" >}}).

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

## Migrate from OCM v1 {#migrating-from-ocm-v1}

Three things changed between OCM v1 and v2: credential matching, constructor syntax, and behavior. The credential changes are the most likely to break existing configurations.

### Credential changes {#credential-changes}

Identity matching has been updated.

| Field             | OCM v1                             | OCM v2             | What to do                                                                                 |
|-------------------|------------------------------------|--------------------|--------------------------------------------------------------------------------------------|
| Consumer identity | `type: wget`                       | `type: Wget`       | Rename the type.                                                                           |
| Identity path     | `pathprefix`, longest-prefix match | `path`, glob match | Rename the attribute. `*` matches one path segment; omit `path` to match the whole host.   |

Further matching changes:

| Area            | OCM v1                                          | OCM v2                                                          |
|-----------------|-------------------------------------------------|-----------------------------------------------------------------|
| Auth precedence | Basic Auth wins; the bearer token is a fallback | Bearer token wins; Basic Auth is used only when no token is set |
| Basic Auth      | Needs both `username` and `password`            | `username` is enough                                            |
| Custom CA       | Root CAs from credentials always applied        | `certificateAuthority` applied only alongside `certificate`     |

### Constructor changes {#constructor-changes}

| Field        | OCM v1       | OCM v2                    | What to do                                    |
|--------------|--------------|---------------------------|-----------------------------------------------|
| `input.body` | Plain string | Base64-encoded byte slice | Base64-encode the body in the constructor.    |

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

## Related Documentation

- [How-To: Add Resources from HTTP URLs]({{< relref "docs/how-to/add-resources-from-http-urls.md" >}}) - The task
  recipe this tutorial expands on
- [Reference: Input and Access Types]({{< relref "docs/reference/input-and-access-types.md" >}}) - Field reference for
  the `Wget/v1` input and access types
- [Reference: Resource Repositories]({{< relref "docs/reference/resource-repositories.md" >}}) - Capabilities,
  credential resolution, and digest processing of the Wget resource repository
- [Reference: Credential Consumer Identities]({{< relref "docs/reference/credential-consumer-identities.md#wget" >}}) -
  Identity attributes and matching rules for `Wget` consumers
- [Reference: HTTP Client Configuration]({{< relref "docs/reference/http-client-configuration.md" >}}) - Timeouts,
  retries, and per-host settings
