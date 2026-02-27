package internal

import (
	"fmt"
	"testing"

	toxiproxy "github.com/Shopify/toxiproxy/v2/client"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	tctoxiproxy "github.com/testcontainers/testcontainers-go/modules/toxiproxy"
	"github.com/testcontainers/testcontainers-go/network"
)

const (
	toxiproxyImage = "ghcr.io/shopify/toxiproxy:2.12.0"
)

// ToxiproxyEnv holds the toxiproxy container environment for testing.
type ToxiproxyEnv struct {
	Host  string
	Port  string
	Proxy *toxiproxy.Proxy
}

// SetupToxiproxy starts a toxiproxy container on the given network with a proxy
// forwarding to the specified upstream (e.g. "registry:5000"). Returns the proxy
// environment with host-mapped endpoint and a reference to the proxy for adding toxics.
func SetupToxiproxy(t *testing.T, nw *testcontainers.DockerNetwork, proxyName, upstream string) *ToxiproxyEnv {
	t.Helper()
	r := require.New(t)
	ctx := t.Context()

	toxiContainer, err := tctoxiproxy.Run(ctx, toxiproxyImage,
		network.WithNetwork([]string{"toxiproxy"}, nw),
		tctoxiproxy.WithProxy(proxyName, upstream),
	)
	r.NoError(err, "failed to start toxiproxy container")
	t.Cleanup(func() {
		r.NoError(testcontainers.TerminateContainer(toxiContainer))
	})

	host, port, err := toxiContainer.ProxiedEndpoint(8666)
	r.NoError(err)

	uri, err := toxiContainer.URI(ctx)
	r.NoError(err)
	toxiClient := toxiproxy.NewClient(uri)
	proxy, err := toxiClient.Proxy(proxyName)
	r.NoError(err, "failed to get toxiproxy proxy")

	return &ToxiproxyEnv{
		Host:  host,
		Port:  port,
		Proxy: proxy,
	}
}

// ProxyHost returns the host:port string for the proxy endpoint.
func (e *ToxiproxyEnv) ProxyHost() string {
	return fmt.Sprintf("%s:%s", e.Host, e.Port)
}

// AddLatency adds a latency toxic to the proxy on the given stream direction
// ("upstream" = client→server, "downstream" = server→client) and removes it on test cleanup.
func AddLatency(t *testing.T, proxy *toxiproxy.Proxy, latencyMs int, stream string) {
	t.Helper()
	_, err := proxy.AddToxic("latency", "latency", stream, 1.0, toxiproxy.Attributes{
		"latency": latencyMs,
	})
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, proxy.RemoveToxic("latency"))
	})
}

// AddTimeoutToxic adds a timeout toxic to the proxy and removes it on test cleanup.
func AddTimeoutToxic(t *testing.T, proxy *toxiproxy.Proxy, timeoutMs int) {
	t.Helper()
	_, err := proxy.AddToxic("timeout", "timeout", "upstream", 1.0, toxiproxy.Attributes{
		"timeout": timeoutMs,
	})
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, proxy.RemoveToxic("timeout"))
	})
}

// AddLimitDataToxic adds a limit_data toxic to the proxy and removes it on test cleanup.
func AddLimitDataToxic(t *testing.T, proxy *toxiproxy.Proxy, bytes int) {
	t.Helper()
	_, err := proxy.AddToxic("limit_data", "limit_data", "upstream", 1.0, toxiproxy.Attributes{
		"bytes": bytes,
	})
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, proxy.RemoveToxic("limit_data"))
	})
}

// AddResetPeerToxic adds a reset_peer toxic that closes the client connection after
// the given timeout (ms). The toxicity parameter controls the probability (0.0–1.0)
// that the toxic is applied to a given connection.
func AddResetPeerToxic(t *testing.T, proxy *toxiproxy.Proxy, timeoutMs int, toxicity float32) {
	t.Helper()
	_, err := proxy.AddToxic("reset_peer", "reset_peer", "downstream", toxicity, toxiproxy.Attributes{
		"timeout": timeoutMs,
	})
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, proxy.RemoveToxic("reset_peer"))
	})
}

// AddBandwidthToxic adds a bandwidth toxic to the proxy on the given stream
// direction ("upstream" = client→server, "downstream" = server→client)
// and removes it on test cleanup.
func AddBandwidthToxic(t *testing.T, proxy *toxiproxy.Proxy, rateKBps int, stream string) {
	t.Helper()
	_, err := proxy.AddToxic("bandwidth", "bandwidth", stream, 1.0, toxiproxy.Attributes{
		"rate": rateKBps,
	})
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, proxy.RemoveToxic("bandwidth"))
	})
}
