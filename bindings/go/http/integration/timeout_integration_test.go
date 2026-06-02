package integration_test

import (
	"context"
	"fmt"
	"io"
	"net"
	nethttp "net/http"
	"net/http/httptrace"
	"strings"
	"sync"
	"testing"
	"time"

	toxiproxy "github.com/Shopify/toxiproxy/v2/client"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/log"
	"github.com/testcontainers/testcontainers-go/modules/registry"
	tctoxiproxy "github.com/testcontainers/testcontainers-go/modules/toxiproxy"
	"github.com/testcontainers/testcontainers-go/network"
	"github.com/testcontainers/testcontainers-go/wait"

	genericv1 "ocm.software/open-component-model/bindings/go/configuration/generic/v1/spec"
	httpv1alpha1 "ocm.software/open-component-model/bindings/go/http/spec/config/v1alpha1"
	ocmhttp "ocm.software/open-component-model/bindings/go/http"
)

const (
	registryImage  = "registry:3.0.0"
	toxiproxyImage = "ghcr.io/shopify/toxiproxy:2.12.0"

	// registryAlias is the registry container's network alias; it is reused
	// as the toxiproxy proxy name.
	registryAlias = "registry"
	// registryPort is the port the registry listens on inside its container.
	registryPort = 5000
	// firstProxiedPort is the host-exposed port the toxiproxy module assigns
	// to the first proxy registered via WithProxy.
	firstProxiedPort = 8666
)

// Test_Integration_HTTPClient is the integration suite for the HTTP client
// built by ocmhttp.New. Each top-level subtest covers one configuration
// area of http.config.ocm.software/v1alpha1. Today that is "timeouts";
// retry policy, TLS config and further areas are expected to be added as
// sibling subtests.
//
// The registry+toxiproxy fixture (see newIntegrationEnv) is started once and
// shared by every area.
func Test_Integration_HTTPClient(t *testing.T) {
	env := newIntegrationEnv(t, t.Context())

	t.Run("timeouts", func(t *testing.T) {
		// testTimeouts verifies that every timeout option of
		// http.config.ocm.software/v1alpha1 — supplied as YAML and resolved through
		// httpv1alpha1.ResolveHTTPConfig — takes effect on the client built by
		// ocmhttp.New.
		//
		// Latency injected via toxiproxy exercises the deadline-oriented options.
		// tcpDialTimeout, tlsHandshakeTimeout, idleConnTimeout and tcpKeepAlive cannot
		// be driven by latency alone and are each exercised with the technique they
		// allow (see the individual subtests).
		ctx := t.Context()
		proxy, registryURL := env.proxy, env.registryURL

		// timeout — the overall http.Client deadline for the whole request.
		t.Run("timeout", func(t *testing.T) {
			t.Run("fails when latency exceeds the overall timeout", func(t *testing.T) {
				addLatency(t, proxy, 5_000)
				err := do(ctx, configuredClient(t, "timeout: 1s"), registryURL)
				assertTimeoutError(t, err)
			})
			t.Run("succeeds when the overall timeout exceeds latency", func(t *testing.T) {
				addLatency(t, proxy, 500)
				require.NoError(t, do(ctx, configuredClient(t, "timeout: 30s"), registryURL))
			})
		})

		// responseHeaderTimeout — transport deadline for the response headers.
		t.Run("responseHeaderTimeout", func(t *testing.T) {
			t.Run("fails when headers arrive too slowly", func(t *testing.T) {
				addLatency(t, proxy, 3_000)
				err := do(ctx, configuredClient(t, "responseHeaderTimeout: 200ms"), registryURL)
				require.Error(t, err)
				require.Contains(t, err.Error(), "timeout awaiting response headers")
			})
			t.Run("succeeds when the response header timeout is generous", func(t *testing.T) {
				addLatency(t, proxy, 500)
				require.NoError(t, do(ctx, configuredClient(t, "responseHeaderTimeout: 30s"), registryURL))
			})
		})

		// tcpDialTimeout — deadline for establishing the TCP connection.
		t.Run("tcpDialTimeout", func(t *testing.T) {
			t.Run("fails when the dial timeout is unsatisfiably short", func(t *testing.T) {
				// 1ns puts the dial deadline in the past before the socket opens.
				err := do(ctx, configuredClient(t, "tcpDialTimeout: 1ns"), registryURL)
				require.Error(t, err)
				require.Contains(t, err.Error(), "i/o timeout")
			})
			t.Run("succeeds when the dial timeout is generous", func(t *testing.T) {
				require.NoError(t, do(ctx, configuredClient(t, "tcpDialTimeout: 30s"), registryURL))
			})
		})

		// tlsHandshakeTimeout — deadline for completing the TLS handshake. It is
		// exercised against an in-process listener that accepts the connection but
		// never starts the handshake, so the client's handshake stalls. (A plain
		// registry container speaks HTTP, so it cannot stall a TLS handshake.)
		t.Run("tlsHandshakeTimeout", func(t *testing.T) {
			err := do(ctx, configuredClient(t, "tlsHandshakeTimeout: 300ms"), stalledTLSServer(t))
			require.Error(t, err)
			require.Contains(t, err.Error(), "TLS handshake timeout")
		})

		// idleConnTimeout — how long an unused keep-alive connection is pooled.
		// A second request after a gap longer than the timeout must dial a fresh
		// connection; within the timeout it reuses the pooled one.
		t.Run("idleConnTimeout", func(t *testing.T) {
			const gap = 700 * time.Millisecond

			t.Run("evicts the pooled connection once the timeout elapses", func(t *testing.T) {
				reused := reusesConnection(t, ctx, configuredClient(t, "idleConnTimeout: 100ms"), registryURL, gap)
				require.False(t, reused, "connection must not be reused after idleConnTimeout elapsed")
			})
			t.Run("reuses the pooled connection within the timeout", func(t *testing.T) {
				reused := reusesConnection(t, ctx, configuredClient(t, "idleConnTimeout: 5m"), registryURL, gap)
				require.True(t, reused, "connection must be reused while within idleConnTimeout")
			})
		})

		// tcpKeepAlive — the TCP keep-alive probe interval on the dialer. Probe
		// timing is a kernel-level behaviour that is not observable from user
		// space, so this verifies end-to-end that the option is parsed, validated
		// and yields a working client — for both a positive interval and the
		// negative value that disables probes.
		t.Run("tcpKeepAlive", func(t *testing.T) {
			for _, field := range []string{"tcpKeepAlive: 1s", "tcpKeepAlive: -1s"} {
				t.Run(field, func(t *testing.T) {
					cfg := resolveConfig(t, field)
					require.NotNil(t, cfg.TCPKeepAlive, "tcpKeepAlive must be populated from YAML")
					require.NoError(t, do(ctx, ocmhttp.New(ocmhttp.WithConfig(cfg)), registryURL))
				})
			}
		})
	})

}

// integrationEnv is the shared fixture for the HTTP client integration suite:
// a registry container reachable through a toxiproxy container whose network
// behaviour can be manipulated per (sub)test.
type integrationEnv struct {
	// registryURL is the registry's /v2/ endpoint, reached through the proxy.
	registryURL string
	// proxy is the toxiproxy proxy in front of the registry; subtests attach
	// toxics (latency, ...) to it to shape the connection.
	proxy *toxiproxy.Proxy
}

// newIntegrationEnv starts the registry and toxiproxy containers on a shared
// docker network and returns the fixture. Every container and the network are
// torn down when the test finishes.
func newIntegrationEnv(t *testing.T, ctx context.Context) *integrationEnv {
	t.Helper()
	r := require.New(t)

	nw, err := network.New(ctx)
	r.NoError(err, "create docker network")
	t.Cleanup(func() { _ = nw.Remove(ctx) })

	t.Log("starting registry container")
	registryContainer, err := registry.Run(ctx, registryImage,
		network.WithNetwork([]string{registryAlias}, nw),
		testcontainers.WithLogger(log.TestLogger(t)),
		testcontainers.WithWaitStrategy(wait.ForHTTP("/v2/").WithPort("5000/tcp")),
	)
	r.NoError(err, "start registry container")
	t.Cleanup(func() { _ = testcontainers.TerminateContainer(registryContainer) })

	t.Log("starting toxiproxy container")
	toxiContainer, err := tctoxiproxy.Run(ctx, toxiproxyImage,
		network.WithNetwork([]string{"toxiproxy"}, nw),
		tctoxiproxy.WithProxy(registryAlias, fmt.Sprintf("%s:%d", registryAlias, registryPort)),
		testcontainers.WithLogger(log.TestLogger(t)),
	)
	r.NoError(err, "start toxiproxy container")
	t.Cleanup(func() { _ = testcontainers.TerminateContainer(toxiContainer) })

	host, port, err := toxiContainer.ProxiedEndpoint(firstProxiedPort)
	r.NoError(err, "resolve proxied registry endpoint")

	controlURI, err := toxiContainer.URI(ctx)
	r.NoError(err, "resolve toxiproxy control URI")
	proxy, err := toxiproxy.NewClient(controlURI).Proxy(registryAlias)
	r.NoError(err, "look up toxiproxy proxy")

	return &integrationEnv{
		registryURL: "http://" + net.JoinHostPort(host, port) + "/v2/",
		proxy:       proxy,
	}
}

// configuredClient resolves the given YAML timeout fields and returns the
// retry-enabled client ocmhttp.New builds from them.
func configuredClient(t *testing.T, fields ...string) *nethttp.Client {
	t.Helper()
	return ocmhttp.New(ocmhttp.WithConfig(resolveConfig(t, fields...)))
}

// resolveConfig renders a generic OCM config document carrying a single
// http.config.ocm.software/v1alpha1 entry with the given timeout fields
// (each a YAML "key: value" line), decodes it, and resolves it to a typed
// *httpv1alpha1.Config — the same path a caller loading OCM config takes.
func resolveConfig(t *testing.T, fields ...string) *httpv1alpha1.Config {
	t.Helper()

	var b strings.Builder
	b.WriteString("type: generic.config.ocm.software/v1\n")
	b.WriteString("configurations:\n")
	b.WriteString("  - type: http.config.ocm.software/v1alpha1\n")
	for _, f := range fields {
		b.WriteString("    " + f + "\n")
	}

	var generic genericv1.Config
	require.NoError(t, genericv1.Scheme.Decode(strings.NewReader(b.String()), &generic),
		"decode http config YAML")
	cfg, err := httpv1alpha1.ResolveHTTPConfig(&generic)
	require.NoError(t, err, "resolve http config")
	return cfg
}

// do issues a GET against url, draining and closing the response body so the
// connection can return to the idle pool. It returns the request error, if any.
func do(ctx context.Context, client *nethttp.Client, url string) error {
	req, err := nethttp.NewRequestWithContext(ctx, nethttp.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	_, err = io.Copy(io.Discard, resp.Body)
	return err
}

// addLatency installs a downstream latency toxic on the proxy and removes it
// when the (sub)test finishes. Each test gets a unique toxic name derived from
// t.Name() so that concurrent or nested calls on the same proxy don't collide.
func addLatency(t *testing.T, proxy *toxiproxy.Proxy, latencyMs int) {
	t.Helper()
	toxicName := "latency-" + strings.ReplaceAll(t.Name(), "/", "-")
	_, err := proxy.AddToxic(toxicName, "latency", "downstream", 1.0, toxiproxy.Attributes{
		"latency": latencyMs,
	})
	require.NoError(t, err, "add latency toxic")
	t.Cleanup(func() {
		if err := proxy.RemoveToxic(toxicName); err != nil {
			t.Logf("remove latency toxic: %v", err)
		}
	})
}

// assertTimeoutError fails the test unless err is an overall-deadline timeout.
// The exact wording varies by Go runtime depending on which layer observes the
// cancelled context first, so both spellings are accepted.
func assertTimeoutError(t *testing.T, err error) {
	t.Helper()
	require.Error(t, err)
	msg := err.Error()
	require.Truef(t,
		strings.Contains(msg, "Client.Timeout") || strings.Contains(msg, "deadline exceeded"),
		"expected an overall-timeout error, got: %v", err)
}

// reusesConnection performs a request, waits gap, performs a second request,
// and reports whether the second request reused a pooled connection.
func reusesConnection(t *testing.T, ctx context.Context, client *nethttp.Client, url string, gap time.Duration) bool {
	t.Helper()
	require.NoError(t, do(ctx, client, url), "prime the connection pool")
	time.Sleep(gap)

	var reused bool
	traced := httptrace.WithClientTrace(ctx, &httptrace.ClientTrace{
		GotConn: func(info httptrace.GotConnInfo) { reused = info.Reused },
	})
	require.NoError(t, do(traced, client, url), "second request")
	return reused
}

// stalledTLSServer starts a TCP listener that accepts connections but never
// performs the TLS handshake, and returns an https URL pointing at it.
func stalledTLSServer(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err, "listen for stalled TLS server")

	var mu sync.Mutex
	var conns []net.Conn
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			// Hold the connection open without responding.
			mu.Lock()
			conns = append(conns, conn)
			mu.Unlock()
		}
	}()
	t.Cleanup(func() {
		_ = ln.Close()
		mu.Lock()
		defer mu.Unlock()
		for _, c := range conns {
			_ = c.Close()
		}
	})
	return "https://" + ln.Addr().String() + "/"
}
