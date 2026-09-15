---
title: "HTTP Client Configuration"
description: "Complete reference for OCM HTTP client configuration: schema, field descriptions, defaults, and per-host merge semantics."
icon: "🌐"
weight: 6
toc: true
---

This page is the technical reference for OCM HTTP client configuration. For a
task-oriented walkthrough, see the
[Configure HTTP Client Behaviour]({{< relref "docs/how-to/configure-http/_index.md" >}}) how-to guides.

## Configuration Type

HTTP client behaviour is controlled by the `http.config.ocm.software/v1alpha1`
configuration type, embedded in the standard OCM configuration file:

```yaml
type: generic.config.ocm.software/v1
configurations:
  - type: http.config.ocm.software/v1alpha1
    timeout: 30s
    retry:
      maxRetries: 5
```

By default the CLI looks for configuration in `$HOME/.ocmconfig`. Pass
`--config <file>` to use a different file.

## Schema

The schema below defines the full structure of the `http.config.ocm.software/v1alpha1`
type as specified by [JSON Schema 2020-12](https://json-schema.org/draft/2020-12/schema).

---

{{< schema-renderer url="/schemas/bindings/go/http/Config.schema.json" >}}

---

## Notes

### Default Values

All fields are optional. When omitted, OCM applies these defaults:

| Field                    | Default                              |
|--------------------------|--------------------------------------|
| `timeout`                | `0s` (no overall deadline)           |
| `retry.maxRetries`       | `5`                                  |
| `retry.minWait`          | `200ms`                              |
| `retry.maxWait`          | `3s`                                 |
| All other timeout fields | No limit (OS default for TCP fields) |
| `insecureSkipVerify`     | `false`                              |

{{< callout context="caution" title="No overall timeout by default" >}}
The default does not bound response-body duration. A stalled peer can therefore
leave an operation waiting indefinitely. Set a positive `timeout` when bounded
completion is more important than allowing arbitrarily long transfers.
Connection-phase timeouts do not limit an active response-body read.
{{< /callout >}}

### Duration Format

All duration fields accept Go's
[`time.ParseDuration`](https://pkg.go.dev/time#ParseDuration) syntax:
`300ms`, `10s`, `5m`, `1h30m`. Zero (`0s`) disables the limit. Negative
values are only accepted for `tcpKeepAlive` (disables keep-alive probes);
all other fields reject negative values.

### `timeout` and Retries

`timeout` covers the **entire** HTTP request, including retry attempts, their
backoff waits, and the response body. It defaults to `0s`, allowing large
response bodies to stream to completion regardless of duration. When configured
with a positive value, it is not reset between retries. Pick a value large
enough to cover the worst-case retry chain:
`timeout ≥ slowestAttempt × (maxRetries + 1) + maxWait × maxRetries`.

### `maxRetries` Semantics

`maxRetries` counts attempts **after** the initial request:

| Value           | Meaning                                     |
|-----------------|---------------------------------------------|
| `nil` / omitted | Library default (5 retries)                 |
| `0`             | Infinite retries                            |
| `-1`            | Disable retries entirely                    |
| positive        | That many retries after the initial attempt |

### Per-Host Merge Semantics

When multiple `http.config.ocm.software/v1alpha1` blocks appear in the same
or layered config files, they are merged field by field — the **last non-nil
value wins**. Host entries are merged map-key by map-key; the last entry for
a given key wins. Per-host fields are then merged on top of the resolved
global values using the same rule.

### Host Key Matching

The `hosts` map key is matched against `request.URL.Host` by exact string:
first `hostname:port`, then bare `hostname`. Go strips the default port
(`:443` for HTTPS, `:80` for HTTP) from URLs before matching, so use bare
hostnames for default-port registries and `hostname:port` only for
non-standard ports. Keys must be lowercase — Go normalises URL hostnames.

### Proxy

Proxy configuration is not a field in this type. OCM inherits Go's standard
`http.ProxyFromEnvironment` — set `HTTPS_PROXY`, `HTTP_PROXY`, and `NO_PROXY`
environment variables. See
[Route Traffic Through a Proxy]({{< relref "docs/how-to/configure-http/proxy.md" >}}).

### TLS Trust: SSL_CERT_FILE and SSL_CERT_DIR

`insecureSkipVerify` is the only TLS field in this config type. To trust a
private CA without disabling verification, use the `SSL_CERT_FILE` /
`SSL_CERT_DIR` environment variables — they are **replacements** for the
built-in system CA path lists in Go's `crypto/x509` loader, not additions.
See [TLS and Custom CA]({{< relref "docs/how-to/configure-http/tls.md" >}}).

## Related Documentation

- [Configure HTTP Client Behaviour]({{< relref "docs/how-to/configure-http/_index.md" >}}) — task-oriented how-to guides
- [Configure Credentials for Multiple Registries]({{< relref "docs/how-to/configure-multiple-credentials.md" >}}) — pairing HTTP config with credential setup
- [Resolver Configuration]({{< relref "docs/reference/resolver-configuration.md" >}}) — reference for resolver config in the same file
