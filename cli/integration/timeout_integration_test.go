package integration

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	toxiproxy "github.com/Shopify/toxiproxy/v2/client"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/registry"
	tctoxiproxy "github.com/testcontainers/testcontainers-go/modules/toxiproxy"
	"github.com/testcontainers/testcontainers-go/network"

	"ocm.software/open-component-model/cli/cmd"
	"ocm.software/open-component-model/cli/integration/internal"
)

const (
	registryImage    = "registry:3.0.0"
	toxiproxyImage   = "ghcr.io/shopify/toxiproxy:2.12.0"
	componentName    = "example.com/timeout-test"
	componentVersion = "1.0.0"
	proxyName        = "registry"
	registryPort     = 5000
)

type toxicTestEnv struct {
	proxyHost   string
	proxy       *toxiproxy.Proxy
	ctfDir      string
	cfgPath     string
	registryURL string
	sourceRef   string
}

func setupToxicTestEnv(t *testing.T) *toxicTestEnv {
	t.Helper()
	r := require.New(t)
	ctx := t.Context()

	// Create a shared Docker network so registry and toxiproxy can communicate.
	nw, err := network.New(ctx)
	r.NoError(err, "failed to create Docker network")
	t.Cleanup(func() {
		_ = nw.Remove(context.Background())
	})

	// Start registry on the shared network with alias "registry".
	user := "ocm"
	password := internal.GenerateRandomPassword(t, 20)
	htpasswd := internal.GenerateHtpasswd(t, user, password)

	registryContainer, err := registry.Run(ctx, registryImage,
		internal.WithHtpasswd(htpasswd),
		network.WithNetwork([]string{"registry"}, nw),
	)
	r.NoError(err, "failed to start registry container")
	t.Cleanup(func() {
		r.NoError(testcontainers.TerminateContainer(registryContainer))
	})

	// Start toxiproxy on the same network with a proxy forwarding to the registry.
	toxiContainer, err := tctoxiproxy.Run(ctx, toxiproxyImage,
		network.WithNetwork([]string{"toxiproxy"}, nw),
		tctoxiproxy.WithProxy(proxyName, fmt.Sprintf("registry:%d", registryPort)),
	)
	r.NoError(err, "failed to start toxiproxy container")
	t.Cleanup(func() {
		r.NoError(testcontainers.TerminateContainer(toxiContainer))
	})

	// Get the host-mapped endpoint for the proxy.
	host, port, err := toxiContainer.ProxiedEndpoint(8666)
	r.NoError(err)
	proxyHost := fmt.Sprintf("%s:%s", host, port)

	// Obtain a reference to the proxy for adding toxics.
	uri, err := toxiContainer.URI(ctx)
	r.NoError(err)
	toxiClient := toxiproxy.NewClient(uri)
	proxy, err := toxiClient.Proxy(proxyName)
	r.NoError(err, "failed to get toxiproxy proxy")

	cfgPath, err := internal.CreateOCMConfigForRegistry(t, []internal.ConfigOpts{
		{Host: host, Port: port, User: user, Password: password},
	})
	r.NoError(err)

	ctfDir := createCTF(t, componentName, componentVersion)

	return &toxicTestEnv{
		proxyHost:   proxyHost,
		proxy:       proxy,
		ctfDir:      ctfDir,
		cfgPath:     cfgPath,
		registryURL: "http://" + proxyHost,
		sourceRef:   fmt.Sprintf("ctf::%s//%s:%s", ctfDir, componentName, componentVersion),
	}
}

type timeoutConfig struct {
	timeout               string
	tcpDialTimeout        string
	tcpKeepAlive          string
	responseHeaderTimeout string
	idleConnTimeout       string
}

func writeConfigWithTimeouts(t *testing.T, baseCfgPath string, cfg timeoutConfig) string {
	t.Helper()
	data, err := os.ReadFile(baseCfgPath)
	require.NoError(t, err)

	cfgPath := filepath.Join(t.TempDir(), "ocmconfig-timeouts.yaml")
	configContent := string(data) + "\n- type: http.config.ocm.software/v1alpha1\n"

	if cfg.timeout != "" {
		configContent += fmt.Sprintf("  timeout: %q\n", cfg.timeout)
	}
	if cfg.tcpDialTimeout != "" {
		configContent += fmt.Sprintf("  tcpDialTimeout: %q\n", cfg.tcpDialTimeout)
	}
	if cfg.tcpKeepAlive != "" {
		configContent += fmt.Sprintf("  tcpKeepAlive: %q\n", cfg.tcpKeepAlive)
	}
	if cfg.responseHeaderTimeout != "" {
		configContent += fmt.Sprintf("  responseHeaderTimeout: %q\n", cfg.responseHeaderTimeout)
	}
	if cfg.idleConnTimeout != "" {
		configContent += fmt.Sprintf("  idleConnTimeout: %q\n", cfg.idleConnTimeout)
	}

	require.NoError(t, os.WriteFile(cfgPath, []byte(configContent), os.ModePerm))
	return cfgPath
}

func Test_Integration_Timeout(t *testing.T) {
	t.Parallel()
	env := setupToxicTestEnv(t)

	t.Run("fails when configured timeout is shorter than proxy latency", func(t *testing.T) {
		r := require.New(t)

		// Toxic: "latency" with latency=5000ms, direction=upstream.
		// Effect: Every packet from the registry is delayed by 5 seconds before
		// reaching the client. The data arrives intact but late. This adds 5s to
		// every round-trip (auth handshake, manifest fetch, blob fetch, etc.),
		// so the cumulative delay far exceeds the 1s configured timeout.
		// Configured timeout: 1s (via config file).
		// Expected: fail — total request time exceeds timeout.
		addLatency(t, env.proxy, 5_000)

		cfgPath := writeConfigWithTimeouts(t, env.cfgPath, timeoutConfig{
			timeout: "1s",
		})

		ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
		defer cancel()

		transferCMD := cmd.New()
		transferCMD.SetArgs([]string{
			"transfer",
			"component-version",
			env.sourceRef,
			env.registryURL,
			"--config", cfgPath,
		})
		err := transferCMD.ExecuteContext(ctx)
		r.Error(err, "transfer should fail due to HTTP timeout")
	})

	t.Run("fails with default 30s timeout when latency exceeds it", func(t *testing.T) {
		r := require.New(t)

		// Toxic: "latency" with latency=10000ms, direction=upstream.
		// Effect: Every packet from the registry is delayed by 10 seconds. Since
		// a transfer involves multiple HTTP round-trips (auth token request,
		// manifest HEAD/GET, blob uploads), each one adds 10s of latency.
		// The cumulative delay exceeds the default 30s timeout.
		// Configured timeout: default (30s).
		// Expected: fail — cumulative latency across requests exceeds 30s.
		addLatency(t, env.proxy, 10_000)

		ctx, cancel := context.WithTimeout(t.Context(), 40*time.Second)
		defer cancel()

		transferCMD := cmd.New()
		transferCMD.SetArgs([]string{
			"transfer",
			"component-version",
			env.sourceRef,
			env.registryURL,
			"--config", env.cfgPath,
		})
		err := transferCMD.ExecuteContext(ctx)
		r.Error(err, "transfer should fail due to HTTP timeout")
	})

	t.Run("succeeds when configured timeout exceeds proxy latency", func(t *testing.T) {
		r := require.New(t)

		// Toxic: "latency" with latency=500ms, direction=upstream.
		// Effect: Every packet from the registry is delayed by 500ms. This adds
		// a small overhead to each round-trip but the total transfer time stays
		// well within the 10s configured timeout.
		// Configured timeout: 10s (via config file).
		// Expected: succeed — latency is tolerable within the timeout budget.
		addLatency(t, env.proxy, 500)

		cfgPath := writeConfigWithTimeouts(t, env.cfgPath, timeoutConfig{
			timeout: "10s",
		})

		ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
		defer cancel()

		transferCMD := cmd.New()
		transferCMD.SetArgs([]string{
			"transfer",
			"component-version",
			env.sourceRef,
			env.registryURL,
			"--config", cfgPath,
		})
		r.NoError(transferCMD.ExecuteContext(ctx))
	})

	t.Run("fails when server goes silent mid-connection (timeout toxic)", func(t *testing.T) {
		r := require.New(t)

		// Toxic: "timeout" with timeout=0ms, direction=upstream.
		// Effect: The TCP connection to the proxy succeeds (SYN-ACK), but
		// toxiproxy never forwards any data from the upstream registry back
		// to the client. With timeout=0 the proxy holds the connection open
		// indefinitely without delivering a single byte. The client's HTTP
		// request is sent but no response (not even headers) ever comes back.
		// Simulates: server process hung after accept(), dead backend behind
		// a load balancer, or a firewall silently dropping response packets.
		// Configured timeout: 5s (via config file).
		// Expected: fail — no data arrives within 5s.
		addTimeoutToxic(t, env.proxy, 0)

		cfgPath := writeConfigWithTimeouts(t, env.cfgPath, timeoutConfig{
			timeout: "5s",
		})

		ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
		defer cancel()

		transferCMD := cmd.New()
		transferCMD.SetArgs([]string{
			"transfer",
			"component-version",
			env.sourceRef,
			env.registryURL,
			"--config", cfgPath,
		})
		err := transferCMD.ExecuteContext(ctx)
		r.Error(err, "should fail because server stopped forwarding data")
	})

	t.Run("fails when connection drops after partial transfer (limit_data toxic)", func(t *testing.T) {
		r := require.New(t)

		// Toxic: "limit_data" with bytes=512, direction=upstream.
		// Effect: Toxiproxy allows exactly 512 bytes to pass from the registry
		// to the client, then forcibly closes the connection (TCP RST). The
		// client receives a partial HTTP response — possibly truncated headers
		// or an incomplete body — and then gets a connection-reset error.
		// Simulates: server crash mid-response, network equipment killing long
		// connections, or a flaky link that drops after a few packets.
		// Configured timeout: 5s (via config file).
		// Expected: fail — connection is cut before the full response is read.
		addLimitDataToxic(t, env.proxy, 512)

		cfgPath := writeConfigWithTimeouts(t, env.cfgPath, timeoutConfig{
			timeout: "5s",
		})

		ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
		defer cancel()

		transferCMD := cmd.New()
		transferCMD.SetArgs([]string{
			"transfer",
			"component-version",
			env.sourceRef,
			env.registryURL,
			"--config", cfgPath,
		})
		err := transferCMD.ExecuteContext(ctx)
		r.Error(err, "should fail because connection was cut after 512 bytes")
	})

	t.Run("fails when connection stalls with zero throughput (bandwidth toxic)", func(t *testing.T) {
		r := require.New(t)

		// Toxic: "bandwidth" with rate=0 KB/s, direction=upstream.
		// Effect: Toxiproxy throttles the upstream data rate to 0 KB/s. The
		// TCP connection stays alive (keepalive probes are ACKed), but zero
		// application-level bytes are delivered to the client. The HTTP client
		// is stuck in a Read() call on the response body that never returns.
		// Simulates: completely saturated network link, NIC failure on the
		// server side where TCP stays up but no data flows, or a misbehaving
		// reverse proxy that buffers indefinitely.
		// Configured timeout: 5s (via config file).
		// Expected: fail — no data arrives within 5s.
		addBandwidthToxic(t, env.proxy, 0)

		cfgPath := writeConfigWithTimeouts(t, env.cfgPath, timeoutConfig{
			timeout: "5s",
		})

		ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
		defer cancel()

		transferCMD := cmd.New()
		transferCMD.SetArgs([]string{
			"transfer",
			"component-version",
			env.sourceRef,
			env.registryURL,
			"--config", cfgPath,
		})
		err := transferCMD.ExecuteContext(ctx)
		r.Error(err, "should fail because bandwidth is zero — no data flows")
	})

	t.Run("fails when registry is completely unreachable (no TCP connection)", func(t *testing.T) {
		r := require.New(t)

		// Setup: proxy.Disable() (not a toxic — disables the entire proxy).
		// Effect: Toxiproxy stops listening on the proxy port. All new TCP
		// connections are refused with a TCP RST — the three-way handshake
		// never completes. The HTTP client's Dial() call fails immediately
		// with "connection refused".
		// Simulates: registry server is down, port not listening, container
		// crashed, or DNS resolves but no process is bound to the port.
		// Configured timeout: default (30s) — but irrelevant since the
		// connection is refused instantly, no timeout needed.
		// Expected: fail — immediate "connection refused" error.
		r.NoError(env.proxy.Disable())
		t.Cleanup(func() {
			r.NoError(env.proxy.Enable())
		})

		ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
		defer cancel()

		transferCMD := cmd.New()
		transferCMD.SetArgs([]string{
			"transfer",
			"component-version",
			env.sourceRef,
			env.registryURL,
			"--config", env.cfgPath,
		})
		err := transferCMD.ExecuteContext(ctx)
		r.Error(err, "should fail because registry is unreachable — no TCP connection possible")
	})

	t.Run("succeeds with slow body transfer when timeout is long enough", func(t *testing.T) {
		r := require.New(t)

		// Toxic: "bandwidth" with rate=1 KB/s, direction=upstream.
		// Effect: Toxiproxy throttles the upstream data rate to 1 KB/s. HTTP
		// response headers (typically <1 KB) arrive within ~1 second, but the
		// response body trickles in at 1 KB/s. Data IS actively flowing — the
		// connection is not stalled — it is just very slow.
		// Simulates: heavily congested network, bandwidth-limited satellite
		// link, or a server under extreme load that can only serve at minimal
		// throughput.
		// Configured timeout: 60s (via config file).
		// Expected: succeed — data keeps flowing within the generous timeout.
		addBandwidthToxic(t, env.proxy, 1)

		cfgPath := writeConfigWithTimeouts(t, env.cfgPath, timeoutConfig{
			timeout: "60s",
		})

		ctx, cancel := context.WithTimeout(t.Context(), 120*time.Second)
		defer cancel()

		transferCMD := cmd.New()
		transferCMD.SetArgs([]string{
			"transfer",
			"component-version",
			env.sourceRef,
			env.registryURL,
			"--config", cfgPath,
		})
		r.NoError(transferCMD.ExecuteContext(ctx), "should succeed because data is still flowing, just slowly")
	})

	t.Run("fails with slow body transfer when timeout is too short", func(t *testing.T) {
		r := require.New(t)

		// Toxic: "bandwidth" with rate=1 KB/s, direction=upstream.
		// Effect: Data trickles at 1 KB/s. Even small HTTP requests (~200+ bytes
		// of headers + body) take >200ms at 1 KB/s, exceeding the 100ms timeout.
		// Simulates: heavily congested network with a tight timeout.
		// Configured timeout: 100ms (via config file).
		// Expected: fail — even small requests too slow for the 100ms deadline.
		addBandwidthToxic(t, env.proxy, 1)

		cfgPath := writeConfigWithTimeouts(t, env.cfgPath, timeoutConfig{
			timeout: "100ms",
		})

		ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
		defer cancel()

		transferCMD := cmd.New()
		transferCMD.SetArgs([]string{
			"transfer",
			"component-version",
			env.sourceRef,
			env.registryURL,
			"--config", cfgPath,
		})
		err := transferCMD.ExecuteContext(ctx)
		r.Error(err, "should fail because body transfer is too slow for the configured timeout")
	})

	// NOTE: tcpDialTimeout cannot be integration-tested with toxiproxy because
	// the TCP dial target is the proxy itself (localhost), which always connects
	// instantly. Toxiproxy latency only affects data after the TCP connection is
	// established, not the TCP SYN/SYN-ACK handshake. tcpDialTimeout is verified
	// via unit tests on the HTTP client transport configuration.

	t.Run("responseHeaderTimeout: fails when response headers don't arrive within configured time", func(t *testing.T) {
		r := require.New(t)

		// Toxic: "latency" with latency=3000ms.
		// Effect: Response headers delayed by 3s.
		// Configured responseHeaderTimeout: 1s (via config file).
		// Expected: fail — headers don't arrive within 1s.
		addLatency(t, env.proxy, 3_000)

		cfgPath := writeConfigWithTimeouts(t, env.cfgPath, timeoutConfig{
			responseHeaderTimeout: "1s",
		})

		ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
		defer cancel()

		transferCMD := cmd.New()
		transferCMD.SetArgs([]string{
			"transfer",
			"component-version",
			env.sourceRef,
			env.registryURL,
			"--config", cfgPath,
		})
		err := transferCMD.ExecuteContext(ctx)
		r.Error(err, "transfer should fail due to response header timeout")
	})

	t.Run("all timeout options can be configured together", func(t *testing.T) {
		r := require.New(t)

		// Toxic: "latency" with latency=500ms.
		// All timeouts configured generously to allow operation to succeed.
		// Expected: succeed — all timeouts are sufficient.
		addLatency(t, env.proxy, 500)

		cfgPath := writeConfigWithTimeouts(t, env.cfgPath, timeoutConfig{
			timeout:               "30s",
			tcpDialTimeout:        "10s",
			tcpKeepAlive:          "10s",
			responseHeaderTimeout: "10s",
			idleConnTimeout:       "60s",
		})

		ctx, cancel := context.WithTimeout(t.Context(), 40*time.Second)
		defer cancel()

		transferCMD := cmd.New()
		transferCMD.SetArgs([]string{
			"transfer",
			"component-version",
			env.sourceRef,
			env.registryURL,
			"--config", cfgPath,
		})
		r.NoError(transferCMD.ExecuteContext(ctx))
	})
}

func addLatency(t *testing.T, proxy *toxiproxy.Proxy, latencyMs int) {
	t.Helper()
	_, err := proxy.AddToxic("latency", "latency", "upstream", 1.0, toxiproxy.Attributes{
		"latency": latencyMs,
	})
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, proxy.RemoveToxic("latency"))
	})
}

func addTimeoutToxic(t *testing.T, proxy *toxiproxy.Proxy, timeoutMs int) {
	t.Helper()
	_, err := proxy.AddToxic("timeout", "timeout", "upstream", 1.0, toxiproxy.Attributes{
		"timeout": timeoutMs,
	})
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, proxy.RemoveToxic("timeout"))
	})
}

func addLimitDataToxic(t *testing.T, proxy *toxiproxy.Proxy, bytes int) {
	t.Helper()
	_, err := proxy.AddToxic("limit_data", "limit_data", "upstream", 1.0, toxiproxy.Attributes{
		"bytes": bytes,
	})
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, proxy.RemoveToxic("limit_data"))
	})
}

func addBandwidthToxic(t *testing.T, proxy *toxiproxy.Proxy, rateKBps int) {
	t.Helper()
	_, err := proxy.AddToxic("bandwidth", "bandwidth", "upstream", 1.0, toxiproxy.Attributes{
		"rate": rateKBps,
	})
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, proxy.RemoveToxic("bandwidth"))
	})
}

func createCTF(t *testing.T, component, version string) string {
	t.Helper()
	r := require.New(t)

	ctfDir := filepath.Join(t.TempDir(), "ctf")
	constructorContent := fmt.Sprintf(`
components:
- name: %s
  version: %s
  provider:
    name: test
`, component, version)

	constructorPath := filepath.Join(t.TempDir(), "constructor.yaml")
	r.NoError(os.WriteFile(constructorPath, []byte(constructorContent), os.ModePerm))

	addCMD := cmd.New()
	addCMD.SetArgs([]string{
		"add",
		"component-version",
		"--repository", fmt.Sprintf("ctf::%s", ctfDir),
		"--constructor", constructorPath,
	})
	r.NoError(addCMD.ExecuteContext(t.Context()), "creation of source CTF should succeed")

	return ctfDir
}
