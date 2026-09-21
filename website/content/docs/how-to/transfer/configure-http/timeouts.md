---
title: "HTTP Timeouts"
description: "Set per-request and per-phase timeouts for OCM HTTP traffic: overall deadline, TLS handshake, response headers, idle connections, and TCP dial."
icon: "⏱"
weight: 1
toc: true
aliases:
  - /docs/how-to/configure-http/timeouts/
---

## Goal

Set the time limits OCM uses for each phase of an HTTP request so that
slow or unresponsive hosts are detected quickly without prematurely cutting
off large transfers.

## Prerequisites

- [OCM CLI]({{< relref "/docs/getting-started/ocm-cli-installation.md" >}}) installed
- An OCM config file at `$HOME/.ocmconfig` (or passed with `--config`)

## Steps

{{< steps >}}

{{< step >}}
**Add timeout fields to your HTTP config**

Open `$HOME/.ocmconfig` and add (or extend) an
`http.config.ocm.software/v1alpha1` entry:

```yaml
type: generic.config.ocm.software/v1
configurations:
  - type: http.config.ocm.software/v1alpha1
    timeout: 15s               # An end-to-end deadline (default: no limit)
    tlsHandshakeTimeout: 10s   # Maximum time for the TLS handshake
    responseHeaderTimeout: 30s # Time to wait for the first response header byte
    idleConnTimeout: 90s       # How long a keep-alive connection stays pooled
    tcpDialTimeout: 5s         # TCP connection establishment deadline
```

`timeout` is the end-to-end deadline for a single HTTP request — it covers
connection, TLS handshake, sending the request body, and reading the full
response body, **including any automatic retry attempts**. All other fields
control individual phases of a single attempt.

By default, OCM does not impose an end-to-end deadline, allowing large response
bodies to stream to completion. Set `timeout` to the longest transfer you expect
on the slowest link you support. A zero value disables the limit explicitly.

{{< callout context="caution" title="No overall timeout by default" >}}
With `timeout` omitted or set to `0s` (the default), OCM does not bound response-body duration. A stalled peer can therefore leave an operation waiting indefinitely. Set a positive `timeout` when bounded completion is more important than allowing arbitrarily long transfers.
{{< /callout >}}

{{< callout context="caution" >}}
`timeout` spans the entire request **including all retry attempts and their
backoff waits** — it is not reset between retries. Raising `maxRetries`
without raising `timeout` means later retries never run. Rough sizing guide:
`timeout ≥ slowestAttempt × (maxRetries + 1) + maxWait × maxRetries`.
{{< /callout >}}

{{< callout context="tip" >}}
`timeout` and `responseHeaderTimeout` are independent. Set a generous
`timeout` to allow large body transfers while keeping `responseHeaderTimeout`
short so a hung server is detected quickly.
{{< /callout >}}
{{< /step >}}

{{< step >}}
**Verify the settings are applied**

```bash
ocm --loglevel debug get componentversion ghcr.io/open-component-model//ocm.software/demos/podinfo:6.8.0
```

Look for a log line like:

```text
level=DEBUG msg="http config resolved" timeout=15s tlsHandshakeTimeout=10s hosts=map[]
```
{{< /step >}}

{{< /steps >}}

## Troubleshooting

### `invalid http configuration: invalid value for timeout: -5s`

All timeout values must be zero (no limit) or positive. Negative values are
rejected for every field except `tcpKeepAlive`. Check all fields including
those inside `hosts` entries.

### Large downloads fail with `context deadline exceeded`

A configured `timeout` covers the complete response body and may be too short
for large artifacts or slow links. Increase it or set `timeout: 0s` to restore
the unlimited default while retaining the phase-specific timeouts.

## Reference

For the full list of accepted fields, types, and defaults see
[HTTP Client Configuration Reference — Top-Level Fields]({{< relref "docs/reference/http-client-configuration.md#top-level-fields" >}}).

Accepted duration formats: `300ms`, `10s`, `5m`, `1h30m` (Go's
[`time.ParseDuration`](https://pkg.go.dev/time#ParseDuration) syntax).

## Related

- [HTTP Retry Policy]({{< relref "retry.md" >}}) — `timeout` covers retries too; read this before raising `maxRetries`
- [Per-Host Overrides]({{< relref "per-host.md" >}}) — override timeouts for specific registries
