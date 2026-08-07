---
title: "HTTP Retry Policy"
description: "Tune automatic retry attempts and exponential backoff for OCM HTTP requests, or disable retries entirely."
icon: "🔄"
weight: 2
toc: true
---

## Goal

Control how OCM retries failed HTTP requests — the number of attempts,
backoff timing, and whether to retry at all — so transient failures on
slow or flaky networks are handled gracefully without masking real errors.

## Prerequisites

- [OCM CLI]({{< relref "/docs/getting-started/ocm-cli-installation.md" >}}) installed
- An OCM config file at `$HOME/.ocmconfig` (or passed with `--config`)

## Background

OCM retries failed HTTP requests automatically with exponential backoff and
jitter. The defaults — 5 retries, 200ms minimum wait, 3s maximum wait — work
for most public registries.

`timeout` covers the entire request including all retry attempts and backoff
waits — see [HTTP Timeouts]({{< relref "timeouts.md" >}}) for sizing guidance.

## Steps

{{< steps >}}

{{< step >}}
**Tune the retry policy**

```yaml
type: generic.config.ocm.software/v1
configurations:
  - type: http.config.ocm.software/v1alpha1
    timeout: 30s
    retry:
      maxRetries: 3      # 0 = infinite, -1 = disable retries, nil = default (5)
      minWait: 500ms     # Lower bound on backoff between attempts
      maxWait: 10s       # Upper bound on backoff between attempts
```

`maxRetries` counts attempts **after** the initial request, so `maxRetries: 3`
makes up to four total attempts. Each retry waits for a duration drawn from
`[minWait, maxWait]` with exponential growth and jitter. Set both bounds to
the same value for a fixed delay.
{{< /step >}}

{{< step >}}
**Disable retries when you need failures to surface immediately**

```yaml
configurations:
  - type: http.config.ocm.software/v1alpha1
    retry:
      maxRetries: -1
```

Use `maxRetries: -1` when running against a deterministic test registry or
when you want hard failures rather than silent retries hiding transient
infrastructure problems.
{{< /step >}}

{{< /steps >}}

## Troubleshooting

### Slow registry returns `context deadline exceeded` before retries finish

`timeout` is shared across all retry attempts. If `timeout` is shorter than
the total worst-case retry chain, the deadline fires before the last retries
run. Raise `timeout`, lower `maxRetries`/`maxWait`, or both.

### `invalid retry config: invalid value for maxRetries: -2`

`maxRetries` accepts only `-1` (disable), `0` (infinite retries), or a
positive integer. Any other negative value is rejected.

### `minWait (5s) must not exceed maxWait (3s)`

The backoff bounds were inverted. Ensure `minWait` ≤ `maxWait`.

## Reference

[HTTP Client Configuration Reference — RetryConfig]({{< relref "docs/reference/http-client-configuration.md#retryconfig" >}})

## Related

- [HTTP Timeouts]({{< relref "timeouts.md" >}}) — set `timeout` to cover the full retry chain
- [Per-Host Overrides]({{< relref "per-host.md" >}}) — override retry policy for specific registries
