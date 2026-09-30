package integration_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/opencontainers/go-digest"
	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/log"
	"github.com/testcontainers/testcontainers-go/network"
	"github.com/testcontainers/testcontainers-go/wait"
	"oras.land/oras-go/v2/registry/remote/auth"

	"ocm.software/open-component-model/bindings/go/oci/internal/remotestore"
	urlresolver "ocm.software/open-component-model/bindings/go/oci/resolver/url"
)

const (
	quayImage          = "quay.io/projectquay/quay:3.14.9"
	quayPostgresImage  = "docker.io/library/postgres:13.22-alpine"
	quayRedisImage     = "docker.io/library/redis:7.2.12-alpine"
	quayLogicalHost    = "quay:8080"
	quayRepositoryPath = "/v2/ocm/chunked-limit/blobs/uploads/"
	quayLayerLimit     = "1M"
	quayChunkSize      = int64(256 << 10)
)

func Test_Integration_OCIRepository_ChunkedPush_QuayLayerLimit(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping Quay layer-limit integration test in short mode")
	}

	r := require.New(t)
	ctx := t.Context()
	env := startQuayLayerLimitEnvironment(t, ctx)
	recorder := &uploadRecorder{}
	client := newQuayAuthClient(env.mappedAddress, env.username, env.password, recorder)

	t.Logf("Quay images: quay=%s postgres=%s redis=%s", quayImage, quayPostgresImage, quayRedisImage)
	t.Logf("Quay logical hostname=%s mapped endpoint=%s maximum layer size=%s", quayLogicalHost, env.mappedEndpoint, quayLayerLimit)

	resolver, err := urlresolver.New(
		urlresolver.WithBaseURL(quayLogicalHost),
		urlresolver.WithPlainHTTP(true),
		urlresolver.WithBaseClient(client),
		urlresolver.WithChunkedPush(quayChunkSize, 1),
	)
	r.NoError(err)

	store, err := resolver.StoreForReference(ctx, quayLogicalHost+"/ocm/chunked-limit:latest")
	r.NoError(err)
	rs, ok := store.(*remotestore.RemoteStore)
	r.Truef(ok, "store %T must be *remotestore.RemoteStore", store)

	below := deterministicPayload(768<<10, "below-limit-")
	belowDesc := ociImageSpecV1.Descriptor{
		MediaType: "application/octet-stream",
		Digest:    digest.FromBytes(below),
		Size:      int64(len(below)),
	}
	r.NoError(rs.Push(ctx, belowDesc, bytes.NewReader(below)))
	assertBlobRoundTrips(t, ctx, rs, belowDesc, below)
	belowEvents := recorder.Events()
	t.Logf("below limit upload events: %+v", belowEvents)
	assertSuccessfulChunkedUpload(t, belowEvents)

	recorder.Reset()
	above := deterministicPayload(1536<<10, "above-limit-")
	aboveDesc := ociImageSpecV1.Descriptor{
		MediaType: "application/octet-stream",
		Digest:    digest.FromBytes(above),
		Size:      int64(len(above)),
	}
	err = rs.Push(ctx, aboveDesc, bytes.NewReader(above))
	r.Error(err)
	r.ErrorContains(err, "chunked blob push")
	r.ErrorContains(err, "PATCH")

	aboveEvents := recorder.Events()
	t.Logf("above limit upload events: %+v", aboveEvents)
	assertCumulativeLimitRejection(t, aboveEvents)

	exists, err := rs.Exists(ctx, aboveDesc)
	r.NoError(err)
	r.False(exists, "oversized blob must not be committed")
}

type quayLayerLimitEnvironment struct {
	mappedAddress  string
	mappedEndpoint string
	username       string
	password       string
}

func startQuayLayerLimitEnvironment(t *testing.T, ctx context.Context) *quayLayerLimitEnvironment {
	t.Helper()
	r := require.New(t)

	nw, err := network.New(ctx)
	testcontainers.CleanupNetwork(t, nw)
	r.NoError(err, "create Quay docker network")

	runQuayFixtureContainer(t, ctx, "postgres", quayPostgresImage,
		network.WithNetwork([]string{"quay-postgres"}, nw),
		testcontainers.WithEnv(map[string]string{
			"POSTGRES_DB":       "quay",
			"POSTGRES_USER":     "quay",
			"POSTGRES_PASSWORD": "quay",
		}),
		testcontainers.WithFiles(testcontainers.ContainerFile{
			Reader:            strings.NewReader("CREATE EXTENSION IF NOT EXISTS pg_trgm;\n"),
			ContainerFilePath: "/docker-entrypoint-initdb.d/01-pg-trgm.sql",
			FileMode:          0o644,
		}),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(time.Minute),
		),
	)

	runQuayFixtureContainer(t, ctx, "redis", quayRedisImage,
		network.WithNetwork([]string{"quay-redis"}, nw),
		testcontainers.WithCmd("redis-server", "--requirepass", "quay"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("Ready to accept connections").WithStartupTimeout(time.Minute),
		),
	)

	quay := runQuayFixtureContainer(t, ctx, "quay", quayImage,
		network.WithNetwork([]string{"quay"}, nw),
		// Start only the request-path services and cap CPU-derived worker counts so
		// the fixture stays fast and within Docker Desktop's memory limit.
		testcontainers.WithEnv(map[string]string{
			"QUAY_SERVICES":                    "gunicorn-registry,gunicorn-web,memcache,nginx",
			"WORKER_COUNT":                     "1",
			"WORKER_COUNT_UNSUPPORTED_MINIMUM": "1",
		}),
		testcontainers.WithExposedPorts("8080/tcp"),
		testcontainers.WithTmpfs(map[string]string{"/datastorage": "rw,mode=0777"}),
		testcontainers.WithFiles(testcontainers.ContainerFile{
			Reader:            strings.NewReader(quayLayerLimitConfig),
			ContainerFilePath: "/conf/stack/config.yaml",
			FileMode:          0o644,
		}),
		testcontainers.WithWaitStrategy(
			wait.ForHTTP("/health/instance").
				WithPort("8080/tcp").
				WithPollInterval(time.Second).
				WithStartupTimeout(5*time.Minute),
		),
	)

	host, err := quay.Host(ctx)
	r.NoError(err, "resolve Quay host")
	port, err := quay.MappedPort(ctx, "8080/tcp")
	r.NoError(err, "resolve Quay mapped port")
	mappedAddress := net.JoinHostPort(host, port.Port())
	mappedEndpoint := "http://" + mappedAddress

	password := generateRandomPassword(t, passwordLength)
	initializeQuayUser(t, ctx, mappedEndpoint, testUsername, password)

	return &quayLayerLimitEnvironment{
		mappedAddress:  mappedAddress,
		mappedEndpoint: mappedEndpoint,
		username:       testUsername,
		password:       password,
	}
}

func runQuayFixtureContainer(
	t *testing.T,
	ctx context.Context,
	phase string,
	image string,
	opts ...testcontainers.ContainerCustomizer,
) *testcontainers.DockerContainer {
	t.Helper()
	opts = append(opts, testcontainers.WithLogger(log.TestLogger(t)))
	container, err := testcontainers.Run(ctx, image, opts...)
	testcontainers.CleanupContainer(t, container, testcontainers.StopTimeout(time.Second))
	if container != nil {
		t.Cleanup(func() {
			if !t.Failed() {
				return
			}
			ctx, cancel := context.WithTimeout(context.WithoutCancel(t.Context()), 30*time.Second)
			defer cancel()
			logContainerOutput(t, ctx, phase, container)
		})
	}
	require.NoErrorf(t, err, "start %s container (%s)", phase, image)
	return container
}

func logContainerOutput(t *testing.T, ctx context.Context, phase string, container testcontainers.Container) {
	t.Helper()
	logs, err := container.Logs(ctx)
	if err != nil {
		t.Logf("read %s container logs: %v", phase, err)
		return
	}
	defer func() { _ = logs.Close() }()
	output, err := io.ReadAll(logs)
	if err != nil {
		t.Logf("read %s container log stream: %v", phase, err)
		return
	}
	t.Logf("%s container logs:\n%s", phase, output)
}

func initializeQuayUser(t *testing.T, ctx context.Context, endpoint, username, password string) {
	t.Helper()
	body, err := json.Marshal(map[string]any{
		"username":     username,
		"password":     password,
		"email":        "ocm@example.invalid",
		"access_token": false,
	})
	require.NoError(t, err, "marshal Quay user initialization request")

	initCtx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	var lastErr error
	for {
		req, err := http.NewRequestWithContext(initCtx, http.MethodPost, endpoint+"/api/v1/user/initialize", bytes.NewReader(body))
		if err != nil {
			require.NoError(t, err, "build Quay user initialization request")
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err == nil {
			_, _ = io.Copy(io.Discard, resp.Body)
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return
			}
			lastErr = fmt.Errorf("unexpected status %d", resp.StatusCode)
			if resp.StatusCode < http.StatusInternalServerError {
				require.NoError(t, lastErr, "initialize first Quay user")
			}
		} else {
			lastErr = err
		}

		select {
		case <-initCtx.Done():
			require.NoErrorf(t, lastErr, "initialize first Quay user before timeout: %v", initCtx.Err())
		case <-ticker.C:
		}
	}
}

func newQuayAuthClient(mappedAddress, username, password string, recorder *uploadRecorder) *auth.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	dialer := &net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second}
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		if address == quayLogicalHost {
			address = mappedAddress
		}
		return dialer.DialContext(ctx, network, address)
	}
	recorder.base = transport

	return &auth.Client{
		Client: &http.Client{Transport: recorder},
		Header: http.Header{"User-Agent": []string{userAgent}},
		Credential: auth.StaticCredential(quayLogicalHost, auth.Credential{
			Username: username,
			Password: password,
		}),
		Cache: auth.NewCache(),
	}
}

type uploadEvent struct {
	Method        string
	ContentLength int64
	Path          string
	Status        int
}

type uploadRecorder struct {
	mu     sync.Mutex
	base   http.RoundTripper
	events []uploadEvent
}

func (r *uploadRecorder) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := r.base.RoundTrip(req)
	if strings.HasPrefix(req.URL.Path, quayRepositoryPath) {
		status := 0
		if resp != nil {
			status = resp.StatusCode
		}
		r.mu.Lock()
		r.events = append(r.events, uploadEvent{
			Method:        req.Method,
			ContentLength: req.ContentLength,
			Path:          req.URL.Path,
			Status:        status,
		})
		r.mu.Unlock()
	}
	return resp, err
}

func (r *uploadRecorder) Events() []uploadEvent {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]uploadEvent(nil), r.events...)
}

func (r *uploadRecorder) Reset() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = nil
}

func assertSuccessfulChunkedUpload(t *testing.T, events []uploadEvent) {
	t.Helper()
	r := require.New(t)
	var successfulPosts, successfulPatches, successfulPuts int
	sessionPaths := map[string]struct{}{}
	for _, event := range events {
		if event.Method == http.MethodPost && event.Status == http.StatusAccepted {
			successfulPosts++
		}
		if event.Method == http.MethodPatch {
			r.Positive(event.ContentLength)
			r.LessOrEqual(event.ContentLength, quayChunkSize)
			if event.Status == http.StatusAccepted {
				successfulPatches++
				sessionPaths[event.Path] = struct{}{}
			}
		}
		if event.Method == http.MethodPut {
			r.GreaterOrEqual(event.ContentLength, int64(0))
			r.LessOrEqual(event.ContentLength, quayChunkSize)
			if event.Status == http.StatusCreated {
				successfulPuts++
				sessionPaths[event.Path] = struct{}{}
			}
		}
	}
	r.Equal(1, successfulPosts)
	r.GreaterOrEqual(successfulPatches, 2)
	r.Equal(1, successfulPuts)
	r.Len(sessionPaths, 1, "PATCH and PUT requests must use one upload session")
}

func assertCumulativeLimitRejection(t *testing.T, events []uploadEvent) {
	t.Helper()
	r := require.New(t)
	acceptedBeforeRejection := 0
	rejectionSeen := false
	rejectedSessionPath := ""
	deleteSeen := false
	for _, event := range events {
		switch event.Method {
		case http.MethodPatch:
			r.Positive(event.ContentLength)
			r.LessOrEqual(event.ContentLength, quayChunkSize)
			if event.Status == http.StatusAccepted && !rejectionSeen {
				acceptedBeforeRejection++
			} else if event.Status != 0 && event.Status != http.StatusAccepted && event.Status != http.StatusUnauthorized {
				rejectionSeen = true
				rejectedSessionPath = event.Path
			}
		case http.MethodPut:
			r.NotEqual(http.StatusCreated, event.Status, "oversized upload must not be finalized")
		case http.MethodDelete:
			if event.Path == rejectedSessionPath && event.Status == http.StatusNoContent {
				deleteSeen = true
			}
		}
	}
	r.True(rejectionSeen, "expected Quay to reject a PATCH after cumulative upload exceeded %s", quayLayerLimit)
	r.GreaterOrEqual(acceptedBeforeRejection, 2, "expected multiple accepted PATCH requests before rejection")
	r.True(deleteSeen, "expected the failed upload session to be cancelled")
}

func deterministicPayload(size int, pattern string) []byte {
	return bytes.Repeat([]byte(pattern), size/len(pattern)+1)[:size]
}

const quayLayerLimitConfig = `AUTHENTICATION_TYPE: Database
DB_URI: postgresql://quay:quay@quay-postgres:5432/quay
DATABASE_SECRET_KEY: integration-test-database-secret
SECRET_KEY: integration-test-secret
BUILDLOGS_REDIS:
  host: quay-redis
  port: 6379
  password: quay
USER_EVENTS_REDIS:
  host: quay-redis
  port: 6379
  password: quay
DISTRIBUTED_STORAGE_CONFIG:
  default:
    - LocalStorage
    - storage_path: /datastorage/registry
DISTRIBUTED_STORAGE_PREFERENCE:
  - default
FEATURE_USER_INITIALIZE: true
MAXIMUM_LAYER_SIZE: 1M
PREFERRED_URL_SCHEME: http
SERVER_HOSTNAME: quay:8080
SETUP_COMPLETE: true
`
