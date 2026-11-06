package nexustest

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/log"
	"github.com/testcontainers/testcontainers-go/wait"
)

const (
	// Image is the Nexus Repository 3 image [Start] runs.
	Image = "sonatype/nexus3:3.96.3"

	apiPort      = "8081/tcp"
	startTimeout = 5 * time.Minute
)

// Nexus is a running Nexus server with the EULA accepted. BaseURL is the HTTP address of its
// API port and Password the admin password. Container reaches the ports exposed with
// [WithExposedPorts].
type Nexus struct {
	BaseURL   string
	Password  string
	Container *testcontainers.DockerContainer
}

type config struct {
	ports []string
}

// Option configures [Start].
type Option func(*config)

// WithExposedPorts exposes extra container ports next to the API port, such as the
// connectors of Docker or OCI hosted repositories.
func WithExposedPorts(ports ...string) Option {
	return func(c *config) {
		c.ports = append(c.ports, ports...)
	}
}

// Start starts Nexus, waits until it accepts writes, reads the generated admin password and
// accepts the Community Edition EULA, which otherwise blocks uploads. The container is
// terminated when the test ends.
func Start(t *testing.T, opts ...Option) Nexus {
	t.Helper()
	r := require.New(t)
	ctx := t.Context()

	cfg := config{ports: []string{apiPort}}
	for _, opt := range opts {
		opt(&cfg)
	}

	container, err := testcontainers.Run(ctx, Image,
		testcontainers.WithExposedPorts(cfg.ports...),
		testcontainers.WithEnv(map[string]string{
			"INSTALL4J_ADD_VM_PARAMS": "-Xms1g -Xmx1g -XX:MaxDirectMemorySize=1g -Djava.util.prefs.userRoot=/nexus-data/javaprefs",
		}),
		// WithWaitStrategy caps the wait at a 60s deadline regardless of the strategy's
		// own timeout; the Nexus JVM routinely needs longer under CI load, so both the
		// deadline and the strategy timeout must allow 5m.
		testcontainers.WithWaitStrategyAndDeadline(startTimeout,
			wait.ForHTTP("/service/rest/v1/status/writable").WithPort(apiPort).WithStartupTimeout(startTimeout),
		),
		testcontainers.WithLogger(log.TestLogger(t)),
	)
	testcontainers.CleanupContainer(t, container)
	r.NoError(err, "should start nexus container")

	endpoint, err := container.PortEndpoint(ctx, apiPort, "")
	r.NoError(err)
	nexus := Nexus{BaseURL: "http://" + endpoint, Container: container}

	rc, err := container.CopyFileFromContainer(ctx, "/nexus-data/admin.password")
	r.NoError(err)
	password, err := io.ReadAll(rc)
	r.NoError(err)
	r.NoError(rc.Close())
	nexus.Password = string(bytes.TrimSpace(password))

	acceptEULA(t, nexus)
	return nexus
}

func acceptEULA(t *testing.T, nexus Nexus) {
	t.Helper()
	r := require.New(t)

	eulaURL := nexus.BaseURL + "/service/rest/v1/system/eula"
	status, body := Request(t, http.MethodGet, eulaURL, nexus.Password, nil)
	if status == http.StatusNotFound {
		return
	}
	r.Equal(http.StatusOK, status, string(body))
	var eula map[string]any
	r.NoError(json.Unmarshal(body, &eula))
	eula["accepted"] = true
	accepted, err := json.Marshal(eula)
	r.NoError(err)
	status, body = Request(t, http.MethodPost, eulaURL, nexus.Password, accepted)
	r.Equal(http.StatusNoContent, status, string(body))
}
