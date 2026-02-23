package integration

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go/network"

	"ocm.software/open-component-model/cli/cmd"
	"ocm.software/open-component-model/cli/integration/internal"
)

const (
	componentName    = "example.com/timeout-test"
	componentVersion = "1.0.0"
	proxyName        = "registry"
	registryPort     = 5000
)

type toxicTestEnv struct {
	toxiproxy   *internal.ToxiproxyEnv
	ctfDir      string
	cfgPath     string
	registryURL string
	sourceRef   string
}

func setupToxicTestEnv(t *testing.T) *toxicTestEnv {
	t.Helper()
	r := require.New(t)

	// Create a shared Docker network so registry and toxiproxy can communicate.
	nw, err := network.New(t.Context())
	r.NoError(err, "failed to create Docker network")
	t.Cleanup(func() {
		_ = nw.Remove(context.Background())
	})

	// Start registry on the shared network with alias "registry".
	user := "ocm"
	password := internal.GenerateRandomPassword(t, 20)
	htpasswd := internal.GenerateHtpasswd(t, user, password)
	internal.StartDockerContainerRegistry(t, "", htpasswd, network.WithNetwork([]string{"registry"}, nw))

	// Start toxiproxy on the same network with a proxy forwarding to the registry.
	toxiEnv := internal.SetupToxiproxy(t, nw, proxyName, fmt.Sprintf("registry:%d", registryPort))

	cfgPath, err := internal.CreateOCMConfigForRegistry(t, []internal.ConfigOpts{
		{Host: toxiEnv.Host, Port: toxiEnv.Port, User: user, Password: password},
	})
	r.NoError(err)

	ctfDir := createCTF(t, componentName, componentVersion)
	proxyHost := toxiEnv.ProxyHost()

	return &toxicTestEnv{
		toxiproxy:   toxiEnv,
		ctfDir:      ctfDir,
		cfgPath:     cfgPath,
		registryURL: "http://" + proxyHost,
		sourceRef:   fmt.Sprintf("ctf::%s//%s:%s", ctfDir, componentName, componentVersion),
	}
}

func Test_Integration_Timeout(t *testing.T) {
	t.Parallel()
	env := setupToxicTestEnv(t)

	t.Run("--timeout", func(t *testing.T) {
		t.Run("fails when configured timeout is shorter than proxy latency", func(t *testing.T) {
			r := require.New(t)

			// 5s latency per packet, 1s timeout → cumulative delay exceeds timeout.
			internal.AddLatency(t, env.toxiproxy.Proxy, 5_000, "upstream")

			ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
			defer cancel()

			transferCMD := cmd.New()
			transferCMD.SetArgs([]string{
				"transfer",
				"component-version",
				env.sourceRef,
				env.registryURL,
				"--config", env.cfgPath,
				"--timeout", "1s",
			})
			err := transferCMD.ExecuteContext(ctx)
			r.Error(err, "transfer should fail due to HTTP timeout")
		})

		t.Run("succeeds when configured timeout exceeds proxy latency", func(t *testing.T) {
			r := require.New(t)

			// 500ms latency per packet, 10s timeout → latency is tolerable.
			internal.AddLatency(t, env.toxiproxy.Proxy, 500, "upstream")

			ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
			defer cancel()

			transferCMD := cmd.New()
			transferCMD.SetArgs([]string{
				"transfer",
				"component-version",
				env.sourceRef,
				env.registryURL,
				"--config", env.cfgPath,
				"--timeout", "10s",
			})
			r.NoError(transferCMD.ExecuteContext(ctx))
		})

		t.Run("fails with default 30s when latency exceeds it", func(t *testing.T) {
			r := require.New(t)

			// 10s latency per packet, default 30s timeout → cumulative delay exceeds timeout.
			internal.AddLatency(t, env.toxiproxy.Proxy, 10_000, "upstream")

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
	})

	t.Run("--response-header-timeout", func(t *testing.T) {
		t.Run("fails when response headers can't arrive within configured time", func(t *testing.T) {
			r := require.New(t)

			// 1 KB/s downstream bandwidth + 3s downstream latency → headers can't arrive within 1s.
			internal.AddBandwidthToxic(t, env.toxiproxy.Proxy, 1, "downstream")
			internal.AddLatency(t, env.toxiproxy.Proxy, 3_000, "downstream")

			ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
			defer cancel()

			transferCMD := cmd.New()
			transferCMD.SetArgs([]string{
				"transfer",
				"component-version",
				env.sourceRef,
				env.registryURL,
				"--config", env.cfgPath,
				"--response-header-timeout", "1s",
			})
			err := transferCMD.ExecuteContext(ctx)
			r.Error(err, "transfer should fail due to response header timeout")
		})

		t.Run("succeeds when response header timeout is generous enough", func(t *testing.T) {
			r := require.New(t)

			// 1 KB/s downstream bandwidth → slow but 30s timeout is enough for headers to arrive.
			internal.AddBandwidthToxic(t, env.toxiproxy.Proxy, 1, "downstream")

			ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
			defer cancel()

			transferCMD := cmd.New()
			transferCMD.SetArgs([]string{
				"transfer",
				"component-version",
				env.sourceRef,
				env.registryURL,
				"--config", env.cfgPath,
				"--response-header-timeout", "30s",
			})
			r.NoError(transferCMD.ExecuteContext(ctx))
		})
	})

	t.Run("--retry-max-retry", func(t *testing.T) {
		t.Run("fails when retries are exhausted on broken connection", func(t *testing.T) {
			r := require.New(t)

			// Reset every connection immediately (toxicity 1.0 = 100% of connections).
			// With only 1 retry and fast wait times, all attempts fail.
			internal.AddResetPeerToxic(t, env.toxiproxy.Proxy, 0, 1.0)

			ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
			defer cancel()

			transferCMD := cmd.New()
			transferCMD.SetArgs([]string{
				"transfer",
				"component-version",
				env.sourceRef,
				env.registryURL,
				"--config", env.cfgPath,
				"--retry-max-retry", "1",
				"--retry-min-wait", "10ms",
				"--retry-max-wait", "10ms",
			})
			err := transferCMD.ExecuteContext(ctx)
			r.Error(err, "transfer should fail because all retries are exhausted")
		})

		t.Run("succeeds when retries overcome flaky connection", func(t *testing.T) {
			r := require.New(t)

			// Reset 30% of connections (toxicity 0.3). With 10 retries the probability
			// of all 11 attempts failing is 0.3^11 ≈ 0.000002, so this is effectively
			// deterministic.
			internal.AddResetPeerToxic(t, env.toxiproxy.Proxy, 0, 0.3)

			ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
			defer cancel()

			transferCMD := cmd.New()
			transferCMD.SetArgs([]string{
				"transfer",
				"component-version",
				env.sourceRef,
				env.registryURL,
				"--config", env.cfgPath,
				"--retry-max-retry", "10",
				"--retry-min-wait", "100ms",
				"--retry-max-wait", "500ms",
			})
			r.NoError(transferCMD.ExecuteContext(ctx))
		})
	})

	t.Run("--tcp-dial-timeout", func(t *testing.T) {
		t.Run("fails when dial timeout is too short to establish connection", func(t *testing.T) {
			r := require.New(t)

			// Toxiproxy toxics only affect data on established connections, not the TCP
			// handshake itself. A true dial timeout (silently dropped SYN) can't be
			// simulated with toxiproxy. Instead, we use an impossibly short dial deadline
			// to verify the flag is wired up correctly.
			ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
			defer cancel()

			transferCMD := cmd.New()
			transferCMD.SetArgs([]string{
				"transfer",
				"component-version",
				env.sourceRef,
				env.registryURL,
				"--config", env.cfgPath,
				"--tcp-dial-timeout", "1ns",
			})
			err := transferCMD.ExecuteContext(ctx)
			r.Error(err, "transfer should fail because TCP dial cannot complete in 1ns")
		})

		t.Run("succeeds with generous dial timeout", func(t *testing.T) {
			r := require.New(t)

			internal.AddLatency(t, env.toxiproxy.Proxy, 500, "upstream")

			ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
			defer cancel()

			transferCMD := cmd.New()
			transferCMD.SetArgs([]string{
				"transfer",
				"component-version",
				env.sourceRef,
				env.registryURL,
				"--config", env.cfgPath,
				"--tcp-dial-timeout", "10s",
			})
			r.NoError(transferCMD.ExecuteContext(ctx))
		})
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
