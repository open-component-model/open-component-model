---
title: "Configure HTTP Client Behaviour for Constrained Environments"
description: "Set global and per-host HTTP timeouts for OCM operations in corporate networks, air-gapped environments, and high-latency registries."
icon: "🌐"
weight: 15
toc: true
---

## Goal

Control the HTTP timeouts OCM uses when talking to OCI registries and Helm
repositories — globally and per-host — so that slow or restricted networks
do not cause silent hangs or premature failures.

## You'll end up with

- An OCM config file that sets a sensible global request timeout
- Per-host overrides for registries with different latency profiles
- All `ocm` commands using these settings automatically

**Estimated time:** ~5 minutes

## Prerequisites

- [OCM CLI]({{< relref "/docs/getting-started/ocm-cli-installation.md" >}}) installed
- `$HOME/.ocmconfig` created (or another config file referenced with `--config`)

## Background

By default OCM uses a 30-second request timeout for all HTTP traffic. In constrained
environments this is often wrong in both directions:

- **Too long** — a corporate firewall that silently drops connections wastes 30 s per
  attempt before returning an error.
- **Too short** — a large Helm chart or container layer served over a slow WAN link may
  need several minutes to download.

OCM exposes full control over the HTTP client through the
`http.config.ocm.software/v1alpha1` configuration type, which you embed in the
same `generic.config.ocm.software/v1` file used for credentials and resolvers.

## Steps

{{< steps >}}

{{< step >}}
**Set a global timeout**

Add an `http.config.ocm.software/v1alpha1` block to `$HOME/.ocmconfig`:

```yaml
type: generic.config.ocm.software/v1
configurations:
  - type: http.config.ocm.software/v1alpha1
    timeout: 15s
```

`timeout` is the end-to-end deadline for a single HTTP request — it covers
connection, TLS handshake, sending the request body, and reading the response
body. Setting it to `15s` means any request that takes longer returns an error
immediately instead of hanging.

Accepted duration formats: `300ms`, `10s`, `5m`, `1h30m` (Go's
[`time.ParseDuration`](https://pkg.go.dev/time#ParseDuration) syntax).
{{< /step >}}

{{< step >}}
**Tune individual timeout phases**

For finer control you can set phase-level timeouts independently:

```yaml
type: generic.config.ocm.software/v1
configurations:
  - type: http.config.ocm.software/v1alpha1
    timeout: 10m               # Total request deadline (entire round-trip)
    tlsHandshakeTimeout: 10s   # Maximum time for the TLS handshake
    responseHeaderTimeout: 30s # Time to wait for the first response header byte
    idleConnTimeout: 90s       # How long a keep-alive connection stays pooled
    tcpDialTimeout: 5s         # TCP connection establishment deadline
```

{{< callout context="tip" >}}
`timeout` and `responseHeaderTimeout` are independent. You can set a generous
`timeout` to allow large body transfers while keeping `responseHeaderTimeout`
short so a hung server is detected quickly.
{{< /callout >}}
{{< /step >}}

{{< step >}}
**Override timeouts for specific registries**

Use the `hosts` map to give individual registries their own timeout budget.
The key is `hostname` or `hostname:port` — the port is required when it
differs from the default for the scheme.

```yaml
type: generic.config.ocm.software/v1
configurations:
  - type: http.config.ocm.software/v1alpha1
    timeout: 15s               # Global default for all other hosts
    hosts:
      # Internal Artifactory over a slow WAN link
      "artifactory.corp:5000":
        timeout: 5m
      # Public GitHub Container Registry — tighten TLS check
      "ghcr.io:443":
        timeout: 60s
        tlsHandshakeTimeout: 5s
```

Per-host values override the corresponding global field for that host only.
Fields not specified in a host block inherit the global value.
{{< /step >}}

{{< step >}}
**Verify the configuration is picked up**

Run any OCM command with debug logging to confirm the settings are applied:

```bash
ocm --loglevel debug get componentversion ghcr.io/my-org/my-component:1.0.0
```

{{< details "Expected output (excerpt)" >}}
```text
DEBUG  http config resolved  timeout=15s tlsHandshakeTimeout=5s hosts=map[ghcr.io:443:{...}]
```
{{< /details >}}

If the HTTP config block is missing or invalid (e.g. a negative timeout), OCM
reports the error at startup before making any requests.
{{< /step >}}

{{< /steps >}}

## Complete Example Configuration

```yaml
type: generic.config.ocm.software/v1
configurations:
  # HTTP client behaviour
  - type: http.config.ocm.software/v1alpha1
    timeout: 15s
    tlsHandshakeTimeout: 10s
    responseHeaderTimeout: 30s
    idleConnTimeout: 90s
    hosts:
      # Slow internal registry — generous timeout
      "artifactory.corp:5000":
        timeout: 5m
      # Air-gapped mirror with known-good latency
      "mirror.airgap.local:443":
        timeout: 2m
        tlsHandshakeTimeout: 5s

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

## Layered configuration

You can split settings across multiple files and merge them with `--config`:

```bash
# Org-wide defaults
ocm --config /etc/ocm/base.yaml \
    --config ~/.ocmconfig \
    get componentversion ghcr.io/my-org/my-component:1.0.0
```

When the same field appears in multiple `http.config.ocm.software/v1alpha1`
blocks (whether in one file or across several `--config` flags), the **last
non-nil value wins**. Host entries from all blocks are merged: if two blocks
define different host keys they are both kept; if they define the same key the
later block's value replaces the earlier one.

## Troubleshooting

### Symptom: requests hang for 30 seconds before failing

**Cause:** No HTTP config in `.ocmconfig`; the built-in 30-second default
applies.

**Fix:** Add a `http.config.ocm.software/v1alpha1` block with a `timeout`
appropriate for your network.

### Symptom: `invalid http configuration: invalid value for timeout: -5s`

**Cause:** A negative duration was written in the config file.

**Fix:** All timeout values must be zero (no timeout) or positive. Check all
fields including those in the `hosts` map.

### Symptom: per-host override not taking effect

**Cause:** The host key does not include the port, but the registry listens on
a non-default port (e.g. `artifactory.corp` instead of `artifactory.corp:5000`).

**Fix:** Always include the port in the `hosts` key when the registry is not on
the standard HTTPS port (443).

## Related Documentation

- [How-To: Transfer Components across an Air Gap]({{< relref "air-gap-transfer.md" >}}) — move component versions into isolated networks
- [How-To: Configure Credentials for Multiple Registries]({{< relref "configure-multiple-credentials.md" >}}) — pair HTTP config with credential setup in the same config file
- `bindings/go/examples/08_http_config_test.go` in the repository — runnable Go test that shows the programmatic API
