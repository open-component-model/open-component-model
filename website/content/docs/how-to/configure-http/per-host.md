---
title: "Per-Host Overrides"
description: "Give individual OCI registries their own timeout, retry, and TLS settings in OCM's HTTP client configuration."
icon: "🎯"
weight: 5
toc: true
---

## Goal

Override timeouts, retry policy, and TLS settings for specific registries
without changing the global defaults that apply to all other hosts.

## Prerequisites

- [OCM CLI]({{< relref "/docs/getting-started/ocm-cli-installation.md" >}}) installed
- An OCM config file at `$HOME/.ocmconfig` (or passed with `--config`)

## Steps

{{< steps >}}

{{< step >}}
**Add a `hosts` map to your HTTP config**

```yaml
type: generic.config.ocm.software/v1
configurations:
  - type: http.config.ocm.software/v1alpha1
    timeout: 15s               # Global default for all other hosts
    retry:
      maxRetries: 5
    hosts:
      # Internal Artifactory over a slow WAN link — longer timeout, fewer retries
      "artifactory.corp:5000":
        timeout: 5m
        retry:
          maxRetries: 2
          maxWait: 30s
      # Public GitHub Container Registry — tighten the TLS handshake window
      "ghcr.io":
        timeout: 60s
        tlsHandshakeTimeout: 5s
      # Local dev registry with self-signed cert
      "registry.kind.local:5001":
        insecureSkipVerify: true
```

Each host entry accepts the same timeout fields as the global config, plus
`retry` and `insecureSkipVerify`. Fields not specified in a host block
inherit the global value.
{{< /step >}}

{{< step >}}
**Use the correct key format**

The key is matched against `request.URL.Host` by exact string:

1. First against `hostname:port`
2. Then against the bare `hostname`

| Mistake                                        | Correct form                                          |
|------------------------------------------------|-------------------------------------------------------|
| `https://ghcr.io` — scheme included            | `ghcr.io`                                             |
| `ghcr.io/my-org` — path included               | `ghcr.io`                                             |
| `ghcr.io:443` — default HTTPS port             | `ghcr.io` (Go strips `:443` from HTTPS URLs)          |
| `artifactory.corp` — missing non-default port  | `artifactory.corp:5000`                               |
| `GHCR.IO` — wrong case                         | `ghcr.io` (Go normalises URL hostnames to lowercase)  |

{{< callout context="note" >}}
Go strips the default port (`:443` for HTTPS, `:80` for HTTP) from URLs
before matching, so always use the bare hostname for default-port registries.
Use `hostname:port` only when the registry listens on a non-standard port.
{{< /callout >}}
{{< /step >}}

{{< /steps >}}

## Troubleshooting

### Per-host override not taking effect

Check the host key against the rules in the table above. Common pitfalls:
scheme or path included in the key, default port (`:443`) included for an
HTTPS registry, wrong case, or a missing port for a non-standard port
registry.

Enable debug logging to see which host key OCM resolved for the request:

```bash
ocm --loglevel debug get componentversion <ref>
```

## Reference

[HTTP Client Configuration Reference — HostConfig]({{< relref "docs/reference/http-client-configuration.md#hostconfig" >}})

## Related

- [HTTP Timeouts]({{< relref "timeouts.md" >}}) — global timeout settings
- [HTTP Retry Policy]({{< relref "retry.md" >}}) — global retry settings
- [TLS and Custom CA]({{< relref "tls.md" >}}) — TLS settings and `insecureSkipVerify`
