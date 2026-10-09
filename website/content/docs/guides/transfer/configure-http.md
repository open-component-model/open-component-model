---
title: "Configure HTTP Behaviour"
description: "Control timeouts, retry policy, TLS verification, custom CA, and HTTP proxy for OCM in constrained or restricted networks."
icon: "🌐"
weight: 110
toc: true
aliases:
  - /docs/how-to/configure-http/
  - /docs/how-to/configure-http/proxy/
  - /docs/how-to/configure-http/tls/
  - /docs/how-to/configure-http/retry/
  - /docs/how-to/configure-http/timeouts/
  - /docs/how-to/configure-http/per-host/
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

For the complete field reference — accepted fields, types, defaults, duration
syntax, and host-key matching rules — see
[HTTP Client Configuration Reference]({{< relref "docs/reference/http-client-configuration.md" >}}).

## Set HTTP timeouts

Set the time limits OCM uses for each phase of an HTTP request so slow or
unresponsive hosts are detected quickly without prematurely cutting off large
transfers. Open `$HOME/.ocmconfig` and add (or extend) an
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

Verify the settings are applied:

```bash
ocm --loglevel debug get componentversion ghcr.io/open-component-model//ocm.software/demos/podinfo:6.8.0
```

Look for a log line like:

```text
level=DEBUG msg="http config resolved" timeout=15s tlsHandshakeTimeout=10s hosts=map[]
```

## Tune the retry policy

OCM retries failed HTTP requests automatically with exponential backoff and
jitter. The defaults work for most public registries. Tune the number of
attempts and backoff timing when transient failures on slow or flaky networks
need different handling:

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

To make failures surface immediately — for a deterministic test registry, or
when you want hard failures rather than silent retries hiding transient
infrastructure problems — disable retries:

```yaml
configurations:
  - type: http.config.ocm.software/v1alpha1
    retry:
      maxRetries: -1
```

`timeout` covers the entire request including all retry attempts and backoff
waits, so raise it alongside `maxRetries` — see [Set HTTP timeouts](#set-http-timeouts)
for sizing guidance.

## Route traffic through a proxy

The OCM CLI inherits Go's standard proxy resolution
([`http.ProxyFromEnvironment`](https://pkg.go.dev/net/http#ProxyFromEnvironment)).
No OCM config file field is needed — the proxy is controlled entirely through
environment variables.

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

`NO_PROXY` matches by **suffix** — `.corp` matches `registry.corp` and
`internal.corp`, while a bare hostname matches only that exact host. Always
include loopback addresses explicitly — Go's proxy resolver does not
auto-exclude them:

```bash
export NO_PROXY=localhost,127.0.0.1,::1${NO_PROXY:+,$NO_PROXY}
```

Add corporate suffixes (`.corp`, `.svc.cluster.local`) when you want them to
connect directly.

{{< callout context="tip" >}}
Blob downloads for `ghcr.io` content are served from a separate CDN host
(`pkg-containers.githubusercontent.com`). Allow both through your proxy
ACLs, or include neither in `NO_PROXY` — otherwise component fetches succeed
on the manifest step but fail on the blob step.
{{< /callout >}}

## Trust a private CA

The OCM CLI uses Go's standard `crypto/tls` stack; the system root CA pool is
consulted automatically. To trust an additional internal or self-signed CA,
point `SSL_CERT_FILE` and/or `SSL_CERT_DIR` at your bundle. TLS verification
stays on — the CLI just learns about an extra root.

```bash
# Single PEM bundle — covers most internal-CA setups.
export SSL_CERT_FILE=/etc/ocm/corp-ca.pem
ocm get cv registry.corp/my-org//ocm.software/demos/podinfo:6.8.0
```

```bash
# Multiple bundles — point at a directory of *.pem / *.crt files.
export SSL_CERT_DIR=/etc/ocm/ca.d
ocm get cv registry.corp/my-org//ocm.software/demos/podinfo:6.8.0
```

{{< callout context="caution" title="Replacement, not merge" >}}
Each variable **replaces** the corresponding
built-in default list inside Go's loader (`crypto/x509`'s `loadOnDiskRoots`).
`SSL_CERT_FILE` replaces the built-in list of fallback bundle paths
(`/etc/ssl/certs/ca-certificates.crt`, `/etc/pki/tls/certs/ca-bundle.crt`, …).
`SSL_CERT_DIR` replaces the built-in list of fallback directories
(`/etc/ssl/certs`, `/etc/pki/tls/certs`).

Because the file and directory channels are independent, **setting only one**
of the two leaves the other channel pulling in system roots — on a typical
Debian/Ubuntu host `SSL_CERT_FILE=/etc/ocm/corp-ca.pem` alone still trusts
public registries. **Setting both** disables all system-root fallbacks; only
your custom CAs are trusted. Public registries then fail with
`x509: certificate signed by unknown authority`.
{{< /callout >}}

To keep both your private CA **and** the system roots, concatenate them:

```bash
cat /etc/ssl/certs/ca-certificates.crt /etc/ocm/corp-ca.pem \
  > /etc/ocm/combined-ca.pem
export SSL_CERT_FILE=/etc/ocm/combined-ca.pem
```

Or drop both into a directory:

```bash
mkdir -p /etc/ocm/ca.d
cp /etc/ssl/certs/ca-certificates.crt /etc/ocm/ca.d/system.pem
cp /etc/ocm/corp-ca.pem               /etc/ocm/ca.d/corp.pem
export SSL_CERT_DIR=/etc/ocm/ca.d
```

{{< callout context="caution" >}}
**Platform differences.** On macOS and Windows, Go normally delegates
verification to the platform trust store (Keychain / CryptoAPI). Setting
**either** `SSL_CERT_FILE` or `SSL_CERT_DIR` switches Go to its pure-Go
on-disk loader and bypasses the platform store entirely. To re-enable the
platform store and ignore the env vars, set
`GODEBUG=x509sslcertoverrideplatform=0` and install the corp CA into the
platform store instead.
{{< /callout >}}

## Disable TLS verification for a local registry

When a local registry uses a self-signed certificate that you cannot or do not
want to trust via a CA bundle, use `insecureSkipVerify`. Always scope it to
the specific host:

```yaml
type: generic.config.ocm.software/v1
configurations:
  - type: http.config.ocm.software/v1alpha1
    hosts:
      "registry.kind.local:5001":
        insecureSkipVerify: true
```

{{< callout context="danger" >}}
`insecureSkipVerify: true` disables certificate chain and hostname checks —
connections become vulnerable to active man-in-the-middle attacks. **Never
enable this against a public registry or in production.** OCM logs a warning
at startup and on the first request to each new host whenever this is active.
{{< /callout >}}

{{< callout context="tip" >}}
Prefer `SSL_CERT_FILE` / `SSL_CERT_DIR` over `insecureSkipVerify` whenever
the registry has a real certificate — even a self-signed one — because TLS
verification stays active and the CLI will still detect tampered or expired
certs. Reach for `insecureSkipVerify` only when you have no certificate to
pin against (e.g. a bare `kind` cluster with no cert at all).
{{< /callout >}}

## Override settings per host

Override timeouts, retry policy, and TLS settings for specific registries
without changing the global defaults that apply to all other hosts. Add a
`hosts` map to your HTTP config:

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

The host key is matched against `request.URL.Host` by exact string — first
against `hostname:port`, then against the bare `hostname`. Use the bare
hostname for default-port registries (Go strips `:443`/`:80`), and
`hostname:port` only for non-standard ports. The
[HTTP Client Configuration Reference — HostConfig]({{< relref "docs/reference/http-client-configuration.md#hostconfig" >}})
has the full key-matching rules.

## Complete example

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

## Verify your configuration loads

Changes to `$HOME/.ocmconfig` take effect on the next `ocm` invocation. There is
no apply step. To confirm the HTTP settings parsed and resolved as you intended,
run any command that initialises the CLI with debug logging and inspect the
resolved config line:

```bash
ocm --loglevel debug get cv ghcr.io/open-component-model//ocm.software/demos/podinfo:6.8.0
```

Early in the output, before any registry request, OCM logs the resolved HTTP
config. The `hosts` field reflects your per-host map, so a non-empty value proves
the overrides were picked up:

```text
level=DEBUG msg="http config resolved" timeout=15s tlsHandshakeTimeout=10s hosts=map[artifactory.corp:5000:{...} ghcr.io:{...}]
```

If `hosts=map[]` while you expected entries, the `hosts` block was not parsed,
check the indentation and the host-key format. If the command exits with
`invalid http configuration: ...`, a field value is out of range, see
[Troubleshooting](#troubleshooting). A malformed config fails fast at startup
rather than silently falling back to defaults.

## Troubleshooting

### Large downloads fail with `context deadline exceeded`

**Cause:** A configured end-to-end `timeout` also limits reading response
bodies and may be too short for large artifacts.

**Fix:** Increase `timeout` or set it to `0s` to use the unlimited default.
Connection-phase limits remain independently configurable.

### `invalid http configuration: invalid value for timeout: -5s`

**Cause:** A negative duration was written in the config file.

**Fix:** All timeout values must be zero (no timeout) or positive. Check all fields including those in the `hosts` map.

### Slow registry returns `context deadline exceeded` before retries finish

**Cause:** `timeout` is shared across all retry attempts. If `timeout` is
shorter than the total worst-case retry chain, the deadline fires before the
last retries run.

**Fix:** Raise `timeout`, lower `maxRetries`/`maxWait`, or both.

### `invalid retry config: invalid value for maxRetries: -2`

**Cause:** `maxRetries` accepts only `-1` (disable), `0` (infinite retries), or
a positive integer.

**Fix:** Use one of the accepted values.

### `minWait (5s) must not exceed maxWait (3s)`

**Cause:** The backoff bounds were inverted.

**Fix:** Ensure `minWait` ≤ `maxWait`.

### `x509: certificate signed by unknown authority`

**Cause:** The registry's certificate is signed by a CA not in the system
trust store, and no env var override has been set.

**Fix,** in order of preference:

1. Point `SSL_CERT_FILE` at a PEM bundle containing the issuing CA — TLS
   verification stays enabled.
2. Install the CA into the system trust store so all tools on the machine
   accept it.
3. Set `insecureSkipVerify: true` for that host only as a last resort.

### `SSL_CERT_FILE` is set but the registry still fails verification

Common causes:

- The variable was set in a different shell or after the OCM process started.
- The PEM bundle contains the **server** certificate rather than its issuing CA.
- `SSL_CERT_DIR` points at files without `.pem`, `.crt`, or `.cer` extensions.
- The hostname in the URL does not match a Subject Alternative Name on the cert — trusting the CA does not bypass hostname verification.

Verify the chain:

```bash
openssl s_client -connect host:port -showcerts
```

Run OCM with `--loglevel debug` — a TLS error reports the leaf cert subject
(hostname mismatch) versus the issuer (chain validation failure).

### Setting `SSL_CERT_FILE` breaks public registries

**Cause:** Both env vars were set and the custom bundle contains only the corp
CA, so public CAs disappeared from the trust pool.

**Fix:** Concatenate the system bundle into your custom one. On macOS / Windows,
install the corp CA into the platform store and set
`GODEBUG=x509sslcertoverrideplatform=0` to re-enable the platform verifier.

### `proxyconnect tcp: … connection refused` (or `i/o timeout`)

**Cause:** `HTTPS_PROXY` is set but the proxy address is unreachable.

**Fix:** Verify the proxy URL with a direct probe (a `200`, or `401` from the
registry, means the proxy is reachable):

```bash
curl -sx "$HTTPS_PROXY" -o /dev/null -w "HTTP %{http_code}\n" https://ghcr.io/v2/
```

Unset the variable for a quick direct comparison:

```bash
unset HTTPS_PROXY https_proxy
```

### Manifest fetch succeeds via proxy but blob download fails

**Cause:** The blob CDN host is missing from the proxy ACLs or is incorrectly
listed in `NO_PROXY`.

**Fix:** Inspect the failing URL in the error message — the host that errors is
the one with the wrong policy. Either allow both hosts through the proxy, or
include both in `NO_PROXY`.

### Per-host override not taking effect

**Cause:** The host key does not match `request.URL.Host`. Common pitfalls:
scheme or path included in the key, default port (`:443`) included for an HTTPS
registry, wrong case, or a missing port for a non-standard-port registry.

**Fix:** Enable debug logging to see which host key OCM resolved for the
request:

```bash
ocm --loglevel debug get componentversion <ref>
```

## Related Documentation

- [HTTP Client Configuration Reference]({{< relref "docs/reference/http-client-configuration.md" >}}) — complete field reference and schema
- [Transfer Components across an Air Gap]({{< relref "docs/guides/transfer/air-gap-transfer.md" >}}) — move component versions into isolated networks
- [Configure Registry Credentials]({{< relref "docs/guides/transfer/configure-registry-credentials.md" >}}) — pair HTTP config with credential setup in the same config file
