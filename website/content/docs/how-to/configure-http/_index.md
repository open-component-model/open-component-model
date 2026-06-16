---
title: "Configure HTTP Behaviour"
description: "Control timeouts, retry policy, TLS verification, custom CA, and HTTP proxy for OCM in constrained or restricted networks."
icon: "🌐"
weight: 15
toc: true
sidebar:
  collapsed: true
---

Control the HTTP behaviour OCM uses when talking to OCI registries and Helm
repositories — globally and per-host — so that slow, flaky, or restricted
networks do not cause silent hangs, premature failures, or surprise certificate
errors.

All settings are configured through the `http.config.ocm.software/v1alpha1`
type in your OCM config file (`$HOME/.ocmconfig`):

```yaml
type: generic.config.ocm.software/v1
configurations:
  - type: http.config.ocm.software/v1alpha1
    # settings go here
```

For the complete field reference, see
[HTTP Client Configuration Reference]({{< relref "docs/reference/http-client-configuration.md" >}}).

## Guides in This Section

- **[HTTP Timeouts]({{< relref "timeouts.md" >}})** — Set request, TLS handshake, response header, and idle connection timeouts
- **[HTTP Retry Policy]({{< relref "retry.md" >}})** — Tune retry attempts and backoff, or disable retries entirely
- **[Route Traffic Through a Proxy]({{< relref "proxy.md" >}})** — Use `HTTPS_PROXY` / `NO_PROXY` to route OCM traffic through a corporate proxy
- **[TLS and Custom CA]({{< relref "tls.md" >}})** — Trust a private CA without disabling verification, or opt out for local registries
- **[Per-Host Overrides]({{< relref "per-host.md" >}})** — Give individual registries their own timeout, retry, and TLS settings

## Complete Example

A full config combining all topics:

```yaml
type: generic.config.ocm.software/v1
configurations:
  - type: http.config.ocm.software/v1alpha1
    timeout: 15s
    tlsHandshakeTimeout: 10s
    responseHeaderTimeout: 30s
    idleConnTimeout: 90s
    retry:
      maxRetries: 3
      minWait: 500ms
      maxWait: 10s
    hosts:
      # Slow internal registry — generous timeout, fewer retries
      "artifactory.corp:5000":
        timeout: 5m
        retry:
          maxRetries: 2
          maxWait: 30s
      # Air-gapped mirror with known-good latency
      "mirror.airgap.local":
        timeout: 2m
        tlsHandshakeTimeout: 5s
      # Local dev registry with self-signed certificate
      "registry.kind.local:5001":
        insecureSkipVerify: true

  # Credentials (can coexist in the same config file)
  - type: credentials.config.ocm.software
    consumers:
      - identities:
          - type: OCIRegistry
            hostname: artifactory.corp
        credentials:
          - type: Credentials/v1
            properties:
              username: ocm-user
              password: s3cr3t
```

## Troubleshooting

### Requests hang for 30 seconds before failing

**Cause:** No HTTP config in `.ocmconfig`; the built-in 30-second default applies.

**Fix:** Add a `http.config.ocm.software/v1alpha1` block with a `timeout` appropriate for your network.

### `invalid http configuration: invalid value for timeout: -5s`

**Cause:** A negative duration was written in the config file.

**Fix:** All timeout values must be zero (no timeout) or positive. Check all fields including those in the `hosts` map.

For retry, proxy, TLS, and per-host troubleshooting see the individual guide pages linked above.

## Related Documentation

- [HTTP Client Configuration Reference]({{< relref "docs/reference/http-client-configuration.md" >}}) — complete field reference and schema
- [How-To: Transfer Components across an Air Gap]({{< relref "docs/how-to/air-gap-transfer.md" >}}) — move component versions into isolated networks
- [How-To: Configure Credentials for Multiple Registries]({{< relref "docs/how-to/configure-multiple-credentials.md" >}}) — pair HTTP config with credential setup in the same config file
