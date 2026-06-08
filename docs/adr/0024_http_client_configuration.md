# HTTP Client Configuration and Construction

* **Status**: proposed
* **Deciders**: OCM Maintainer Team
* **Date**: 2026-05-25

## Context and Problem Statement

OCM makes outbound HTTP calls from many places — OCI registries, plain downloads, remote plugins.
Operators need one place in `.ocmconfig` to tune those clients (timeouts, per-host overrides), and a
construction layer that turns that config into a `*http.Client` without leaking transport details into callers.

## Decision Drivers

1. **Single source of truth** — connection-level knobs via the same `generic.config.ocm.software/v1` envelope as every other typed config.
2. **Per-host overrides as first-class** — most deployments reach registries with different timeout budgets.
3. **Proxy via env vars** — `HTTP_PROXY` / `HTTPS_PROXY` / `NO_PROXY` are already the platform convention; no YAML surface needed.
4. **Retry stays per-protocol** — `http.config.ocm.software` deliberately omits retry; each protocol config (e.g. `oci.config.ocm.software/v1alpha1`) owns its own retry block.

## Considered Options

* **Option 1** — Versioned typed config (`http.config.ocm.software/v1alpha1`) with pointer fields, per-host overrides, and a dedicated `bindings/go/http` construction package.
* **Option 2** — Single struct with sentinel durations (e.g. `-1` = use default). Construction co-located with config type.
* **Option 3** — No typed config; raw key/value pairs fed directly into each protocol stack.

Chosen **Option 1**.

## Decision Outcome

### Package layout

Config type lives in `bindings/go/http/spec/config/v1alpha1` (package `v1alpha1`).
Construction code lives in `bindings/go/http` (package `http`).

The split keeps the config import graph lean: a tool that only reads `.ocmconfig` imports `v1alpha1` only and doesn't pull in oras-go or retry transports. Anything that needs a working client imports `bindings/go/http`, which brings in `v1alpha1` plus the transport composition.

### Construction

Three entry points:

* `New(opts ...Option) *http.Client` — wraps `NewTransport` in oras-go's retry transport. Options: `WithConfig`, `WithUserAgent`.
* `NewClient(cfg *Config) *http.Client` — plain client, no retry.
* `NewTransport(cfg *TimeoutConfig) *http.Transport` — bare transport for callers composing their own chain.

`NewTransport` clones `http.DefaultTransport` and overrides only non-nil fields. A fresh `net.Dialer` is installed only when TCP fields are set (`Transport.Clone` cannot expose the default dialer for partial override).

### Timeouts

Six pointer-typed duration fields. Pointers preserve the *unset / zero / positive* distinction that `net/http` already encodes — without them an omitted YAML field is indistinguishable from `0s`.

| Field                   | Default | Meaning                                                                     |
|-------------------------|---------|-----------------------------------------------------------------------------|
| `timeout`               | `30s`   | Total request budget (connect + headers + body read). Zero = no timeout.    |
| `tcpDialTimeout`        | `30s`   | Max time for TCP connect.                                                   |
| `tcpKeepAlive`          | `30s`   | Keep-alive probe interval. Negative disables probes.                        |
| `tlsHandshakeTimeout`   | `10s`   | Max time for TLS handshake. Zero = no timeout.                              |
| `responseHeaderTimeout` | `0s`    | Time waiting for response headers after request sent. Excludes body read.   |
| `idleConnTimeout`       | `90s`   | Max idle keep-alive connection lifetime. Zero = no limit.                   |

Negative values are rejected by `Validate` except `tcpKeepAlive` (negative disables probes, consistent with `net.Dialer.KeepAlive`).
The `30s` default for `timeout` is injected by `ResolveHTTPConfig`, not `Scheme.Convert`, so tests can inspect the literal YAML value.

### Per-host overrides

`Hosts` is a `map[string]*HostConfig` keyed by hostname or `hostname:port`. Each entry embeds `TimeoutConfig`; non-nil fields override the global for that host. Nil entries are ignored.

Routing is handled by an internal `hostRouter` `RoundTripper` that is only installed when `Hosts` has at least one non-nil entry. When all entries are nil or `Hosts` is empty, `hostRouter` is skipped entirely.

**Why a context deadline instead of `http.Client.Timeout`**: setting `http.Client.Timeout` globally would cap every request before the router runs, preventing a per-host timeout from exceeding the global. Instead, `http.Client.Timeout` is left zero when `hostRouter` is active and the deadline is applied per-request inside `hostRouter.RoundTrip` via `context.WithTimeout`. Note: this deadline covers the round-trip call (until response headers are received); it does not extend over the response body read.

Transport chain when per-host routing is active:

```text
http.Client → [userAgentTransport] → hostRouter → retry.Transport (per host) → http.Transport (per host)
```

When no per-host routing is needed the chain collapses and `http.Client.Timeout` is used normally.

### Deferred concerns

* **TLS** — `tls.insecure` (`*bool`) is planned for a follow-up revision. Custom CAs and client certs are deferred until shapes are settled.
* **DNS overrides** — `dnsOverrides` (`map[string]string`) is reserved in the schema but not stabilised. Open questions around `Dialer.Resolver` scoping and TLS SNI interaction block it.
* **Logging / monitoring** — deferred. The `logging` key is reserved in the schema.

### Contract

```go
// bindings/go/http/spec/config/v1alpha1
package v1alpha1

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

type HostConfig struct {
    TimeoutConfig `json:",inline"`
}

type Config struct {
    Type  runtime.Type              `json:"type"`
    TimeoutConfig `json:",inline"`
    Hosts map[string]*HostConfig    `json:"hosts,omitempty"`
}

func (c *Config) Validate() error
func Merge(configs ...*Config) *Config
func LookupConfig(cfg *genericv1.Config) (*Config, error)
func ResolveHTTPConfig(cfg *genericv1.Config) (*Config, error)
```

```go
// bindings/go/http
package http

func WithConfig(cfg *v1alpha1.Config) Option
func WithUserAgent(userAgent string) Option   // always overwrites; for library-level identification

func New(opts ...Option) *http.Client         // retry-enabled; honours cfg.Hosts via hostRouter
func NewClient(cfg *v1alpha1.Config) *http.Client // no retry; also honours cfg.Hosts
func NewTransport(cfg *v1alpha1.TimeoutConfig) *http.Transport
```

### Example

```yaml
type: generic.config.ocm.software/v1
configurations:
  - type: http.config.ocm.software/v1alpha1
    timeout: "30s"
    tcpDialTimeout: "30s"
    tlsHandshakeTimeout: "10s"
    idleConnTimeout: "90s"
    hosts:
      ghcr.io:
        timeout: "60s"
      localhost:5000:
        tlsHandshakeTimeout: "2s"
```

```go
cfg, err := v1alpha1.ResolveHTTPConfig(genericConfig)
if err != nil {
    return err
}
client := ocmhttp.New(
    ocmhttp.WithConfig(cfg),
    ocmhttp.WithUserAgent("my-component/1.0"),
)
```
