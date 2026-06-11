---
title: "Configure HTTP Client Behaviour for Constrained Environments"
description: "Set global and per-host HTTP timeouts for OCM operations in corporate networks, air-gapped, and high-latency registries."
icon: "🌐"
weight: 15
toc: true
---

## Goal

Control the HTTP timeouts OCM uses when talking to OCI registries and Helm
repositories — globally and per-host — so that slow or restricted networks
do not cause silent hangs or premature failures.

## You'll end up with

- An OCM config file that sets a global request timeout
- Per-host overrides for registries with different latency profiles
- All `ocm` commands using these settings automatically

**Estimated time:** ~5 minutes

## Prerequisites

- [OCM CLI]({{< relref "/docs/getting-started/ocm-cli-installation.md" >}}) installed

## Background

By default OCM uses a 30-second request timeout for all HTTP traffic. In constrained
environments this is often wrong in both directions:

- **Too long** — a corporate firewall that silently drops connections wastes 30s per
  attempt before returning an error.
- **Too short** — a large Helm chart or container layer served over a slow WAN link may
  need several minutes to download.

OCM exposes full control over the HTTP client through the
`http.config.ocm.software/v1alpha1` configuration type, which you embed in the
same `generic.config.ocm.software/v1` file used for credentials and resolvers.

## Steps

{{< steps >}}

{{< step >}}
**Configure HTTP timeouts**

Add an `http.config.ocm.software/v1alpha1` block to `$HOME/.ocmconfig` (or another
config file you pass with `--config`):

```yaml
type: generic.config.ocm.software/v1
configurations:
  - type: http.config.ocm.software/v1alpha1
    timeout: 15s               # Total request deadline (entire round-trip)
    tlsHandshakeTimeout: 10s   # Maximum time for the TLS handshake
    responseHeaderTimeout: 30s # Time to wait for the first response header byte
    idleConnTimeout: 90s       # How long a keep-alive connection stays pooled
    tcpDialTimeout: 5s         # TCP connection establishment deadline
```

`timeout` is the end-to-end deadline for a single HTTP request — it covers
connection, TLS handshake, sending the request body, and reading the response
body. All other fields control individual phases of that request.

Accepted duration formats: `300ms`, `10s`, `5m`, `1h30m` (Go's
[`time.ParseDuration`](https://pkg.go.dev/time#ParseDuration) syntax).

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
ocm --loglevel debug get componentversion ghcr.io/open-component-model//ocm.software/demos/podinfo:6.8.0
```

{{< details "Expected output (excerpt)" >}}
```text
time=2026-06-10T17:25:11.071+02:00 level=DEBUG msg="http config resolved" timeout=15s tlsHandshakeTimeout=10s hosts=map[]
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

**Cause:** The `hosts` key does not match the URL's host part exactly. Matching
is a plain string lookup against `request.URL.Host` — first against
`hostname:port`, then falling back to the bare `hostname`. Common pitfalls:

- Typo in the hostname or port
- Including the scheme in the key (e.g. `https://ghcr.io` instead of `ghcr.io`)
- Including a path (e.g. `ghcr.io/my-org` instead of `ghcr.io`)
- Missing port when the registry listens on a non-default port
  (e.g. `artifactory.corp` instead of `artifactory.corp:5000`)
- Casing mismatch (Go normalises URL hostnames to lowercase)

**Fix:** Set the key to exactly what appears as the host part of the registry
URL — hostname only when the port is implicit (`443` for HTTPS), or
`hostname:port` when it is not. No scheme, no path, lowercase.

## Related Documentation

- [How-To: Transfer Components across an Air Gap]({{< relref "air-gap-transfer.md" >}}) — move component versions into isolated networks
- [How-To: Configure Credentials for Multiple Registries]({{< relref "configure-multiple-credentials.md" >}}) — pair HTTP config with credential setup in the same config file
- [bindings/go/examples/](https://github.com/open-component-model/open-component-model/tree/main/bindings/go/examples/) — runnable Go tests that show the programmatic API
