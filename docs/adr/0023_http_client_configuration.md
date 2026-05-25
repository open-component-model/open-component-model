# HTTP Client Configuration and Construction

* **Status**: proposed
* **Deciders**: OCM Maintainer Team
* **Date**: 2026-05-25

Technical Story: OCM makes outbound HTTP calls from many places — OCI registries, plain downloads, remote plugins.
Operators need a single place in `.ocmconfig` to tune those clients (timeouts, TLS, per-host overrides), and we need a
construction layer that turns that config into a usable `*http.Client` without leaking transport details into every
caller.

## Context and Problem Statement

Operators want one typed config entry in `.ocmconfig` that shapes outbound HTTP behaviour for every client OCM hands
out. The connection-level surface to control:

* **Timeouts** — overall request budget plus per-stage caps, so a slow or unresponsive registry can't hang the client
  indefinitely.
* **TLS verification** — let operators talk to registries with self-signed or expired certs in dev / CI without
  rebuilding the binary.
* **Per-host overrides** — different timeouts and TLS settings per destination, so a single client can be generous
  with a slow on-prem mirror and strict with `ghcr.io`.
* **Proxy** — outbound traffic must flow through a corporate / CI proxy when the environment requires one.
* **DNS overrides** *(exploratory)* — point a registry hostname at a different address so tests can hit a local
  fixture without OS-level config changes.
* **Request logging** *(deferred)* — a structured log line per request / response (optionally with headers / body)
  for debugging and audit.

## Decision Drivers

1. **Single source of truth in `.ocmconfig`** — connection-level knobs must be configurable through the same envelope
   (`generic.config.ocm.software/v1`) as every other typed config.
2. **Per-host overrides as a first-class shape** — most deployments reach more than one registry with different
   timeout budgets.
3. **Don't duplicate platform conventions** — proxy selection is already solved by env vars.
4. **Retry stays per-protocol** — each protocol config carries its own retry block (e.g.
   `oci.config.ocm.software/v1alpha1`); `http.config.ocm.software` deliberately omits retry.

## Considered Options

* **Option 1** — Versioned typed config (`http.config.ocm.software/v1alpha1`) with pointer fields, per-host overrides,
  and a dedicated `bindings/go/httpclient` construction package. Retry stays on per-protocol config types.
* **Option 2** — Single struct with non-pointer fields, using a sentinel duration (e.g. `-1`) to mean "use default".
  Construction lives next to the config type in the same package.
* **Option 3** — No typed config — feed `.ocmconfig` raw key/value pairs into each protocol stack and let it construct
  its own `http.Transport`.

## Decision Outcome

Chosen **Option 1**: versioned typed config + per-host overrides + dedicated construction package. The sub-sections
below break this into the scoped decisions made for each concern.

### Where the HTTP client factory lives

Place the construction code in `bindings/go/httpclient` and the config type in
`bindings/go/configuration/http/v1alpha1/spec`. They are separate packages, not separate files in the same package.

The split keeps the config import graph small. A CLI that only reads `.ocmconfig` (e.g. `ocm config view`) doesn't
transitively pull in oras-go, retry transports, or any protocol stack — it imports `spec` only. Anything that needs a
working client imports `httpclient`, which pulls in `spec` plus the transport composition code.

### Construction (instantiation and how clients reach calling code)

`httpclient.New(opts ...Option) *http.Client` is the canonical factory. Callers build a client once and pass the
returned `*http.Client` to the code that needs it through whatever injection the protocol stack already uses
(constructor argument, context, …) — there is no global / singleton client.

Three entry points cover the common shapes:

* `New(opts ...Option) *http.Client` — main factory. Options cover config, retry policy, and User-Agent. With no
  options, you get a working client with library defaults.
* `NewClient(*Config) *http.Client` — plain client, no retry, no UA. For callers that want less ceremony than
  functional options.
* `NewTransport(*TimeoutConfig, *TLSConfig) *http.Transport` — bare transport for callers composing their own chain.

`NewTransport` clones `http.DefaultTransport` and overrides only the fields the caller set. It swaps in a fresh
`net.Dialer` only when TCP fields are set — `Transport.Clone` doesn't expose the default dialer for partial override.
The replacement starts from `net/http`'s documented defaults, so unset fields stay predictable.

Functional options keep the no-arg, config-only, and config + retry + UA cases ergonomic at the call site; future
concerns (hooks, metrics, custom transports) can accrete as new options without breaking existing callers.

### Per-host overrides (logic and config shape)

`hosts` is a `map[string]*HostConfig` keyed by hostname or `hostname:port`. Each entry embeds a `TimeoutConfig` whose
non-nil fields override the matching global value for requests to that host; unset fields inherit from the global —
so "same as global but with a longer timeout for this slow mirror" stays concise.

| Field        | Type                  | Default | Meaning                                                                         |
|--------------|-----------------------|---------|---------------------------------------------------------------------------------|
| `hosts`      | map\<string, object\> | `nil`   | Maps a host key to per-host TimeoutConfig overrides.                            |
| `hosts[*]`   | TimeoutConfig         | inherit | Any field from Timeouts may appear here and overrides the global for that host. |

Host-key format: a bare hostname (`registry.example.com`) matches any port; `hostname:port` (`localhost:5000`) matches
that specific port. A flat string key is sufficient for the deployments operators actually run today and keeps the
YAML flat — a `{host, port, scheme}` triple would add ceremony for no concrete need yet.

#### How the http client honours per-host config (custom transport)

Per-host routing lives in an internal `http.RoundTripper` called `hostRouter`. It fronts the rest of the transport
chain whenever `cfg.Hosts` is non-empty and is omitted entirely when it isn't, so callers that don't use per-host pay
nothing for it.

**At construction time** (inside `New` / `NewClient`):

1. For every entry in `cfg.Hosts`, `MergeTimeoutConfig(global, hostOverride)` produces the effective `TimeoutConfig`
   for that host.
2. A per-host inner chain is built from each merged config — a bare `*http.Transport` for `NewClient`, the same
   wrapped in `retry.Transport` for `New`.
3. The router stores `hostKey → innerChain` in a map alongside the global inner chain (used as fallback) and the
   per-host overall `Timeout` values.
4. The router is installed as `http.Client.Transport`.

**At request time** (inside `hostRouter.RoundTrip`):

1. Look up the request URL's host. The full `Host` (`host` or `host:port`) is checked first; on miss, the bare
   hostname is tried in case the URL carried an explicit port that doesn't appear as a key.
2. Pick the matching host's inner chain, or fall back to the global chain.
3. If the matched entry has an overall `Timeout`, wrap the request context with
   `context.WithTimeout(req.Context(), hostTimeout)` and clone the request onto that context.
4. Call `innerChain.RoundTrip(req)` and return the result.

**Why a context deadline instead of `http.Client.Timeout`**: `http.Client.Timeout` caps the entire request before the
router runs, which would prevent any per-host timeout from exceeding the global. With `cfg.Hosts` non-empty,
`http.Client.Timeout` is left zero and the overall timeout (global or per-host) is applied per request inside the
router. That way a host can be configured with a longer (or shorter) deadline than the global value, and the deadline
covers the whole request including any retries inside the inner chain.

Transport chain when per-host is active (outermost first):

```text
http.Client → [userAgentTransport] → hostRouter → retry.Transport (per host) → http.Transport (per host)
```

When `cfg.Hosts` is empty the chain collapses to the previous shape (no router) and the global `Timeout` is applied
as `http.Client.Timeout`.

### Timeouts

Adopt six pointer-typed duration fields that mirror `net/http`'s transport fields one-for-one. Pointers preserve the
*unset / zero / value* distinction `net/http` already encodes — without them an omitted YAML key is indistinguishable
from `timeout: 0s`.

| Field                   | Type     | Default | Meaning                                                                    |
|-------------------------|----------|---------|----------------------------------------------------------------------------|
| `timeout`               | duration | `30s`   | Total request budget (connect + redirects + body read). Zero = no timeout. |
| `tcpDialTimeout`        | duration | `30s`   | Max time for TCP connect. OS may cap earlier.                              |
| `tcpKeepAlive`          | duration | `30s`   | Interval between keep-alive probes. Negative disables probes.              |
| `tlsHandshakeTimeout`   | duration | `10s`   | Max time for TLS handshake. Zero = no timeout.                             |
| `responseHeaderTimeout` | duration | `0s`    | Time waiting for response headers after request sent. Excludes body read.  |
| `idleConnTimeout`       | duration | `90s`   | Max time an idle keep-alive connection stays in the pool. Zero = no limit. |

Negative values are rejected by `Validate`, except `tcpKeepAlive` where a negative value disables probes (consistent
with `net.Dialer.KeepAlive`). The 30s default for `timeout` is injected by `ResolveHTTPConfig`, not by
`Scheme.Convert` — so tests can inspect what the YAML literally said.

### TLS

Ship a single `*bool` toggle (`tls.insecure`) in `v1alpha1`. Custom CAs, client certificates, and minimum TLS versions
are deferred until their shapes are settled.

| Field          | Type | Default | Meaning                                                                         |
|----------------|------|---------|---------------------------------------------------------------------------------|
| `tls.insecure` | bool | `false` | When `true`, the transport skips TLS cert verification. Dev / self-signed only. |

`insecure` is a `*bool` for the same three-state reason as timeouts — an explicit `false` is distinguishable from
unset, which keeps a future per-host TLS override able to opt back into verification when the global has disabled it.

### Retry (per-protocol, supplied at construction time)

`http.config.ocm.software/v1alpha1` carries no retry. Each protocol stack owns its own retry block on its own typed
config — e.g. `oci.config.ocm.software/v1alpha1` carries `retry.{minWait, maxWait, maxRetry}` starting from oras-go's
default policy — and supplies the resulting policy to the factory.

The hand-off mechanism is `WithRetryPolicy()`. The caller (the protocol stack) builds a retry policy from its own YAML
and passes it as a construction option:

```go
client := httpclient.New(
    httpclient.WithConfig(httpCfg),
    httpclient.WithRetryPolicy(ociRetryPolicy), // OCI plugin supplies this
)
```

The factory then wraps the timeout/TLS transport with a retry transport built from that policy. `httpclient.New`
itself assumes no retry policy — callers that don't pass one get a client with no retry layer.

Why per-protocol: OCI traffic benefits from oras-go's policy with its own `minWait` / `maxWait` / `maxRetry` knobs; a
plain download or a remote-plugin call may want no retry at all. Putting retry on `http.config.ocm.software` would
either force every caller onto one policy or push policy selection into the construction package — neither matches
how operators reason about retry.

### Proxy

No YAML surface. Proxy selection is delegated to Go's `net/http` environment-variable convention: the constructed
transport uses [`http.ProxyFromEnvironment`](https://pkg.go.dev/net/http#ProxyFromEnvironment), which honours
`HTTP_PROXY`, `HTTPS_PROXY`, and `NO_PROXY` (and their lowercase variants).

Operators already set these env vars once per shell, pod, or CI runner and every Go HTTP client picks them up. A YAML
mirror would only fragment a stable platform convention. If a future use case demonstrates a need for per-config proxy
that env vars can't express, this decision will be revisited in a follow-up ADR.

### DNS overrides (TBD)

Reserve `dnsOverrides` as a schema placeholder for hostname → IP rewrites, primarily for local testing against
fixtures bound to `127.0.0.1`. The field is documented but **not stabilised**.

| Field          | Type                 | Default | Meaning                                                               |
|----------------|----------------------|---------|-----------------------------------------------------------------------|
| `dnsOverrides` | map\<string,string\> | `nil`   | Hostname → IP rewrites at the dialer level. Mostly for local testing. |

Open questions (block stabilisation; require a PoC):

* **Where the rewrite happens** — a custom `Dialer.Resolver` keeps the rewrite scoped to clients built by
  `httpclient`; a process-wide hook is global but easier for plugins to honour transparently.
* **TLS SNI interaction** — the ServerName must track the original hostname for SNI / cert verification.

Decision deferred until the PoC settles both questions. The field may rename or move before `v1alpha1` stabilises.

### Logging and monitoring (TBD)

No fields in `v1alpha1`. The `logging` key is reserved in the schema so existing configs don't need reshaping when it
lands. Implementations should not silently accept unknown `logging` fields today.

Open questions for the eventual design:

* Structured log line per request / response — which fields are mandatory, which are opt-in (headers, body)?
* Interaction with retry — one log line per attempt, or one per logical request?
* Whether monitoring counters (latency, errors, retry rate) belong on this config type or on a sibling.

Decision deferred until the shape is designed.

### Contract

```go
package spec // .../configuration/http/v1alpha1/spec

const ConfigType = "http.config.ocm.software"
const DefaultTimeout = Timeout(30 * time.Second)

type Timeout time.Duration // marshals as "30s", "5m", ...

type TimeoutConfig struct {
    Timeout               *Timeout `json:"timeout,omitempty"`
    TCPDialTimeout        *Timeout `json:"tcpDialTimeout,omitempty"`
    TCPKeepAlive          *Timeout `json:"tcpKeepAlive,omitempty"`
    TLSHandshakeTimeout   *Timeout `json:"tlsHandshakeTimeout,omitempty"`
    ResponseHeaderTimeout *Timeout `json:"responseHeaderTimeout,omitempty"`
    IdleConnTimeout       *Timeout `json:"idleConnTimeout,omitempty"`
}

type TLSConfig struct {
    Insecure *bool `json:"insecure,omitempty"`
}

// HostConfig is the per-host override entry. It embeds TimeoutConfig so any
// timeout field may appear under a host key and override the global value.
type HostConfig struct {
    TimeoutConfig `json:",inline"`
}

type Config struct {
    Type runtime.Type `json:"type"`
    TimeoutConfig `json:",inline"`

    // Hosts is keyed by hostname or hostname:port. Non-nil entries override
    // matching global timeout fields for requests to that host.
    Hosts map[string]*HostConfig `json:"hosts,omitempty"`

    // Planned for later v1alpha1 revisions; documented in the TLS and
    // DNS overrides sub-sections.
    // TLS          *TLSConfig        `json:"tls,omitempty"`
    // DNSOverrides map[string]string `json:"dnsOverrides,omitempty"`
}

func (c *Config) Validate() error
func Merge(configs ...*Config) *Config
func LookupConfig(cfg *genericv1.Config) (*Config, error)
func ResolveHTTPConfig(cfg *genericv1.Config) (*Config, error)
```

```go
package httpclient // .../bindings/go/httpclient

type Option func(*Options)

func WithConfig(cfg *httpv1alpha1.Config) Option
func WithUserAgent(userAgent string) Option
func WithRetryPolicy(policy RetryPolicy) Option

func New(opts ...Option) *http.Client       // wraps NewTransport in retry; honours cfg.Hosts via hostRouter
func NewClient(cfg *httpv1alpha1.Config) *http.Client // no retry; also honours cfg.Hosts via hostRouter
func NewTransport(timeouts *httpv1alpha1.TimeoutConfig) *http.Transport
```

### Example

```yaml
type: generic.config.ocm.software/v1
configurations:
  - type: oci.config.ocm.software/v1alpha1
    retry:
      minWait: "200ms"
      maxWait: "3s"
      maxRetry: 5

  - type: http.config.ocm.software/v1alpha1
    timeout: "30s"
    tcpDialTimeout: "30s"
    tcpKeepAlive: "30s"
    tlsHandshakeTimeout: "10s"
    responseHeaderTimeout: "10s"
    idleConnTimeout: "90s"

    hosts:
      # bare hostname — matches any port on this host
      ghcr.io:
        timeout: "60s"
      # host:port — matches only requests on this exact port
      localhost:5000:
        tlsHandshakeTimeout: "2s"
```

Putting it together:

```go
cfg, err := httpv1alpha1.ResolveHTTPConfig(genericConfig)
if err != nil {
    return err
}
client := httpclient.New(
    httpclient.WithConfig(cfg),
    httpclient.WithUserAgent("my-component/1.0"),
    // OCI plugin adds: httpclient.WithRetryPolicy(ociRetryPolicy),
)
```

