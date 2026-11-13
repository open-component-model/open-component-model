---
title: "Migrate from http:// Prefixes"
icon: "arrow-autofit-right"
slug: "migrate-from-http-prefixes"
description: "Replace the deprecated http:// and https:// prefixes on OCI repository base URLs and references with plainHTTP in the HTTP configuration."
weight: 70
toc: true
---

## Goal

Drop the scheme prefix (`http://` or `https://`) from OCI repository base URLs
and references, and mark hosts without TLS as `plainHTTP` in the HTTP
configuration.

{{< callout context="caution" >}}
A scheme prefix is deprecated, and OCM logs a warning whenever it sees one. The
prefix no longer selects the connection: only `plainHTTP` in the HTTP
configuration does. An `http://` prefix on a host that is not `plainHTTP` is an
error, because the request would silently go over HTTPS when the URL asked for
cleartext.
{{< /callout >}}

## Why Migrate?

The prefix was deprecated for security. A scheme in a descriptor, a reference,
or a CLI argument could switch a host to cleartext and send credentials
unencrypted without the operator's consent. Now only the operator's HTTP
configuration can turn on plain HTTP.

## Prerequisites

- [OCM CLI]({{< docref "/docs/getting-started/ocm-cli-installation.md" >}}) installed
- A repository spec, a reference, or a configuration that uses an `http://` or
  `https://` prefix for an OCI registry

## Steps

### 1. Mark the host as plain HTTP

Skip this step if the registry has TLS. Otherwise add the host to the HTTP
configuration:

```yaml
type: generic.config.ocm.software/v1
configurations:
  - type: http.config.ocm.software/v1alpha1
    hosts:
      "registry.local:5000":
        plainHTTP: true
```

See
[Use plain HTTP for a local registry]({{< docref "docs/guides/transfer/configure-http.md#use-plain-http-for-a-local-registry" >}})
for what the setting covers.

### 2. Drop the prefix from repository base URLs

{{< tabs "migrate-http-prefix-base-url" >}}
{{< tab "Before" >}}

```yaml
type: OCIRepository/v1
baseUrl: http://registry.local:5000/ocm
```

{{< /tab >}}
{{< tab "After" >}}

```yaml
type: OCIRepository/v1
baseUrl: registry.local:5000/ocm
```

{{< /tab >}}
{{< /tabs >}}

### 3. Drop the prefix from references

Do the same for references, for example on the command line.

{{< tabs "migrate-http-prefix-reference" >}}
{{< tab "Before" >}}

```text
http://registry.local:5000/ocm
```

{{< /tab >}}
{{< tab "After" >}}

```text
registry.local:5000/ocm
```

{{< /tab >}}
{{< /tabs >}}

## Related documentation

- [Guides: Configure HTTP Behaviour]({{< docref "docs/guides/transfer/configure-http.md" >}}) - Timeouts, retries, TLS, and plain HTTP per host
- [Reference: HTTP Client]({{< docref "docs/reference/configuration/http-client.md" >}}) - Full schema of `http.config.ocm.software/v1alpha1`
