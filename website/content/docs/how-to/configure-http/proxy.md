---
title: "Route Traffic Through a Proxy"
description: "Route OCM HTTP traffic through a corporate proxy using HTTPS_PROXY and NO_PROXY environment variables."
icon: "🔀"
weight: 3
toc: true
---

## Goal

Route OCM registry traffic through a corporate HTTP/HTTPS proxy and exclude
loopback or internal hosts that should connect directly.

## Prerequisites

- [OCM CLI]({{< relref "/docs/getting-started/ocm-cli-installation.md" >}}) installed
- The proxy URL and any bypass rules from your network team

## Background

The OCM CLI inherits Go's standard proxy resolution
([`http.ProxyFromEnvironment`](https://pkg.go.dev/net/http#ProxyFromEnvironment)).
No OCM config file field is needed — the proxy is controlled entirely through
environment variables.

## Steps

{{< steps >}}

{{< step >}}
**Set the proxy environment variables**

```bash
export HTTPS_PROXY=http://proxy.corp:3128
export NO_PROXY=localhost,127.0.0.1,.corp,.svc.cluster.local
ocm get cv ghcr.io/open-component-model//ocm.software/demos/podinfo:6.8.0
```

| Variable                      | Purpose                                                        |
|-------------------------------|----------------------------------------------------------------|
| `HTTPS_PROXY` / `https_proxy` | Proxy URL for `https://` requests (almost all OCI traffic)     |
| `HTTP_PROXY` / `http_proxy`   | Proxy URL for plain-`http://` requests                         |
| `NO_PROXY` / `no_proxy`       | Comma-separated list of hosts or CIDRs that bypass the proxy   |

Both upper- and lowercase variable names are honoured; uppercase wins when
both are set. Authenticated proxies use the standard URL form
`http://user:pass@proxy.corp:3128`.
{{< /step >}}

{{< step >}}
**Configure `NO_PROXY` correctly**

`NO_PROXY` matches by **suffix** — `.corp` matches `registry.corp` and
`internal.corp`, while a bare hostname matches only that exact host.

Always include loopback addresses explicitly — Go's proxy resolver does not
auto-exclude them:

```bash
export NO_PROXY=localhost,127.0.0.1,::1${NO_PROXY:+,$NO_PROXY}
```

Add corporate suffixes (`.corp`, `.svc.cluster.local`) when you want them
to connect directly.

{{< callout context="tip" >}}
Blob downloads for `ghcr.io` content are served from a separate CDN host
(`pkg-containers.githubusercontent.com`). Allow both through your proxy
ACLs, or include neither in `NO_PROXY` — otherwise component fetches succeed
on the manifest step but fail on the blob step.
{{< /callout >}}
{{< /step >}}

{{< /steps >}}

## Troubleshooting

### `proxyconnect tcp: … connection refused` (or `i/o timeout`)

`HTTPS_PROXY` is set but the proxy address is unreachable. Verify the proxy
URL with a direct probe:

```bash
curl -sx "$HTTPS_PROXY" -o /dev/null -w "HTTP %{http_code}\n" https://ghcr.io/v2/
```

A `200` (or `401` from the registry) means the proxy is reachable. Unset
the variable for a quick direct comparison:

```bash
unset HTTPS_PROXY https_proxy
```

### Manifest fetch succeeds via proxy but blob download fails

The blob CDN host is missing from the proxy ACLs or is incorrectly listed in
`NO_PROXY`. Inspect the failing URL in the error message — the host that
errors is the one with the wrong policy. Either allow both hosts through the
proxy, or include both in `NO_PROXY`.

### Traffic to a loopback registry is being sent through the proxy

`NO_PROXY` does not list loopback addresses. Add `localhost,127.0.0.1,::1`
explicitly:

```bash
export NO_PROXY=localhost,127.0.0.1,::1${NO_PROXY:+,$NO_PROXY}
```

## Reference

[HTTP Client Configuration Reference — Proxy Environment Variables]({{< relref "docs/reference/http-client-configuration.md#proxy-environment-variables" >}})

## Related

- [TLS and Custom CA]({{< relref "tls.md" >}}) — trust a private CA on the connection after it passes through the proxy
- [Per-Host Overrides]({{< relref "per-host.md" >}}) — timeout and TLS settings per registry
