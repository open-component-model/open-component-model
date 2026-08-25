package integration_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/log"
	"github.com/testcontainers/testcontainers-go/modules/registry"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/oci/cache"
	"ocm.software/open-component-model/bindings/go/oci/repository/provider"
	ocicredsv1 "ocm.software/open-component-model/bindings/go/oci/spec/credentials/v1"
	ocirepospecv1 "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/oci"
	"ocm.software/open-component-model/bindings/go/repository"
)

// registryCalls records the distribution-API traffic that actually leaves the
// client, classified by endpoint. It sits in a reverse proxy in front of the
// test registry so it observes exactly what the OCI bindings put on the wire,
// independent of any client-internal bookkeeping.
type registryCalls struct {
	mu    sync.Mutex
	count map[string]int
	log   []string
}

var (
	manifestsPath = regexp.MustCompile(`^/v2/.+/manifests/.+$`)
	blobsPath     = regexp.MustCompile(`^/v2/.+/blobs/[^/]+$`)
	tagsPath      = regexp.MustCompile(`^/v2/.+/tags/list$`)
)

func classifyRegistryPath(path string) string {
	switch {
	case path == "/v2/" || path == "/v2":
		return "ping"
	case strings.Contains(path, "/blobs/uploads"):
		return "uploads"
	case tagsPath.MatchString(path):
		return "tags"
	case manifestsPath.MatchString(path):
		return "manifests"
	case blobsPath.MatchString(path):
		return "blobs"
	default:
		return "other"
	}
}

func (c *registryCalls) record(r *http.Request, status int) {
	// A 401 is the auth challenge every fresh client pays once per host
	// before it knows which scheme to use. It carries no content and says
	// nothing about caching, so it is protocol overhead, not traffic.
	if status == http.StatusUnauthorized {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.count == nil {
		c.count = map[string]int{}
	}
	kind := classifyRegistryPath(r.URL.Path)
	c.count[r.Method+" "+kind]++
	c.log = append(c.log, r.Method+" "+r.URL.Path)
}

// statusRecorder captures the response status so [registryCalls.record] can
// tell served requests from auth challenges.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (w *statusRecorder) WriteHeader(status int) {
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

func (w *statusRecorder) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return w.ResponseWriter.Write(b)
}

func (c *registryCalls) reset() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.count = map[string]int{}
	c.log = nil
}

// downloads counts requests that transfer content: manifest and blob GETs.
// A working cache must drive this to zero on a warm re-read.
func (c *registryCalls) downloads() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.count["GET manifests"] + c.count["GET blobs"]
}

// revalidations counts the cheap existence checks a cache is still allowed
// to make on a warm re-read: HEADs against manifests and blobs. Resolving a
// mutable tag costs one of these and cannot be cached away.
func (c *registryCalls) revalidations() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.count["HEAD manifests"] + c.count["HEAD blobs"]
}

func (c *registryCalls) dump() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.log...)
}

// startProxiedRegistry boots the containerized distribution registry used by
// the other integration tests and puts a counting reverse proxy in front of
// it. Callers talk to the returned URL, so every request is observable.
func startProxiedRegistry(t *testing.T, ctx context.Context) (string, *ocicredsv1.OCICredentials, *registryCalls) {
	t.Helper()
	r := require.New(t)

	password := generateRandomPassword(t, passwordLength)
	htpasswd := generateHtpasswd(t, testUsername, password)

	registryContainer, err := registry.Run(ctx, distributionRegistryImage,
		registry.WithHtpasswd(htpasswd),
		testcontainers.WithEnv(map[string]string{
			"REGISTRY_VALIDATION_DISABLED": "true",
		}),
		testcontainers.WithLogger(log.TestLogger(t)),
	)
	r.NoError(err)
	t.Cleanup(func() {
		r.NoError(testcontainers.TerminateContainer(registryContainer))
	})

	address, err := registryContainer.HostAddress(ctx)
	r.NoError(err)

	target, err := url.Parse("http://" + strings.TrimPrefix(address, "http://"))
	r.NoError(err)

	calls := &registryCalls{}
	proxy := httputil.NewSingleHostReverseProxy(target)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		rec := &statusRecorder{ResponseWriter: w}
		proxy.ServeHTTP(rec, req)
		calls.record(req, rec.status)
	}))
	t.Cleanup(srv.Close)

	creds := &ocicredsv1.OCICredentials{
		Type:     ocicredsv1.OCICredentialsVersionedType,
		Username: testUsername,
		Password: password,
	}
	return srv.URL, creds, calls
}

// cachingProvider builds a provider wired exactly the way the CLI and the
// controller wire it in cli/internal/plugin/builtin/oci/register.go and
// kubernetes/controller/cmd/main.go. cacheDir is the provider temp dir, which
// is where the on-disk blob and reference caches are rooted; reusing it across
// providers emulates a second process run.
func cachingProvider(cacheDir string, policy cache.RemotePolicy) *provider.CachingComponentVersionRepositoryProvider {
	return provider.NewComponentVersionRepositoryProvider(
		provider.WithTempDir(cacheDir),
		provider.WithBlobCacheOptions(&cache.Options{RemotePolicy: policy}),
		provider.WithReferenceCacheOptions(&cache.Options{RemotePolicy: policy}),
	)
}

func repoFor(t *testing.T, ctx context.Context, prov *provider.CachingComponentVersionRepositoryProvider, baseURL string, creds *ocicredsv1.OCICredentials) repository.ComponentVersionRepository {
	t.Helper()
	repo, err := prov.GetComponentVersionRepository(ctx, &ocirepospecv1.Repository{BaseUrl: baseURL}, creds)
	require.NoError(t, err)
	return repo
}

func cacheTestDescriptor(name, version, marker string) *descriptor.Descriptor {
	desc := &descriptor.Descriptor{}
	desc.Component.Name = name
	desc.Component.Version = version
	desc.Component.Provider.Name = "ocm.software/open-component-model/bindings/go/oci/integration/test"
	desc.Component.Labels = append(desc.Component.Labels, descriptor.Label{
		Name:  "marker",
		Value: []byte(`"` + marker + `"`),
	})
	return desc
}

func markerOf(t *testing.T, desc *descriptor.Descriptor) string {
	t.Helper()
	for _, l := range desc.Component.Labels {
		if l.Name == "marker" {
			return strings.Trim(string(l.Value), `"`)
		}
	}
	t.Fatalf("descriptor %s:%s has no marker label", desc.Component.Name, desc.Component.Version)
	return ""
}

// Test_Integration_OCICache_ReReadAvoidsRegistry is the test the cache wiring
// was missing: it asserts on observed registry traffic rather than on the
// cache's own internals.
func Test_Integration_OCICache_ReReadAvoidsRegistry(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	r := require.New(t)

	baseURL, creds, calls := startProxiedRegistry(t, ctx)

	const component, version = "ocm.software/cache-test", "v1.0.0"

	// Seed the registry through an uncached provider so the cached providers
	// below start cold.
	seedProv := provider.NewComponentVersionRepositoryProvider(provider.WithTempDir(t.TempDir()))
	seedRepo := repoFor(t, ctx, seedProv, baseURL, creds)
	r.NoError(seedRepo.AddComponentVersion(ctx, cacheTestDescriptor(component, version, "original")))

	t.Run("baseline: without the cache a re-read hits the registry again", func(t *testing.T) {
		r := require.New(t)
		prov := provider.NewComponentVersionRepositoryProvider(provider.WithTempDir(t.TempDir()))
		repo := repoFor(t, ctx, prov, baseURL, creds)

		calls.reset()
		_, err := repo.GetComponentVersion(ctx, component, version)
		r.NoError(err)
		first := calls.downloads()
		r.Positive(first, "cold read must download content")

		calls.reset()
		_, err = repo.GetComponentVersion(ctx, component, version)
		r.NoError(err)
		second := calls.downloads()
		t.Logf("uncached: first read %d downloads, second read %d", first, second)
		r.Positive(second, "without a cache the second read must download again")
	})

	cacheDir := t.TempDir()

	t.Run("IfNotPresent: a re-read on the same provider downloads nothing", func(t *testing.T) {
		r := require.New(t)
		prov := cachingProvider(cacheDir, cache.RemotePolicyIfNotPresent)
		repo := repoFor(t, ctx, prov, baseURL, creds)

		calls.reset()
		got, err := repo.GetComponentVersion(ctx, component, version)
		r.NoError(err)
		r.Equal("original", markerOf(t, got))
		first := calls.downloads()
		r.Positive(first, "cold read must download content")

		calls.reset()
		got, err = repo.GetComponentVersion(ctx, component, version)
		r.NoError(err)
		r.Equal("original", markerOf(t, got))
		t.Logf("cached: first read %d downloads, second read %d downloads / %d revalidations (%v)",
			first, calls.downloads(), calls.revalidations(), calls.dump())
		r.Zero(calls.downloads(), "a warm cache must not re-download content")
		r.LessOrEqual(calls.revalidations(), 1, "a warm cache should cost at most one tag revalidation")
	})

	t.Run("IfNotPresent: a fresh provider reuses the on-disk cache", func(t *testing.T) {
		r := require.New(t)
		// A new provider over the same directory is what a second `ocm`
		// invocation looks like: in-memory state is gone, only the disk
		// cache survives.
		prov := cachingProvider(cacheDir, cache.RemotePolicyIfNotPresent)
		repo := repoFor(t, ctx, prov, baseURL, creds)

		calls.reset()
		got, err := repo.GetComponentVersion(ctx, component, version)
		r.NoError(err)
		r.Equal("original", markerOf(t, got))
		t.Logf("fresh provider, warm disk cache: %d downloads / %d revalidations (%v)",
			calls.downloads(), calls.revalidations(), calls.dump())
		r.Zero(calls.downloads(), "a warm on-disk cache must survive process restart")
		r.LessOrEqual(calls.revalidations(), 1, "a warm on-disk cache should cost at most one tag revalidation")
	})

	t.Run("IfNotPresent: a moved tag is not served stale", func(t *testing.T) {
		r := require.New(t)
		prov := cachingProvider(cacheDir, cache.RemotePolicyIfNotPresent)
		repo := repoFor(t, ctx, prov, baseURL, creds)

		// Warm the cache.
		got, err := repo.GetComponentVersion(ctx, component, version)
		r.NoError(err)
		r.Equal("original", markerOf(t, got))

		// Someone else republishes the same component version with different
		// content, moving the tag to a new manifest digest.
		r.NoError(seedRepo.AddComponentVersion(ctx, cacheTestDescriptor(component, version, "republished")))

		calls.reset()
		got, err = repo.GetComponentVersion(ctx, component, version)
		r.NoError(err)
		t.Logf("after retag: %d downloads / %d revalidations (%v)",
			calls.downloads(), calls.revalidations(), calls.dump())
		r.Equal("republished", markerOf(t, got), "cache served content from before the tag moved")
	})
}

// Test_Integration_OCICache_RemotePolicyAlways covers the policy the
// controller runs with: the registry must be contacted on every hit so
// authorization is re-checked, but cached bytes should not be re-downloaded.
func Test_Integration_OCICache_RemotePolicyAlways(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	r := require.New(t)

	baseURL, creds, calls := startProxiedRegistry(t, ctx)

	const component, version = "ocm.software/cache-test-always", "v1.0.0"

	seedProv := provider.NewComponentVersionRepositoryProvider(provider.WithTempDir(t.TempDir()))
	seedRepo := repoFor(t, ctx, seedProv, baseURL, creds)
	r.NoError(seedRepo.AddComponentVersion(ctx, cacheTestDescriptor(component, version, "original")))

	prov := cachingProvider(t.TempDir(), cache.RemotePolicyAlways)
	repo := repoFor(t, ctx, prov, baseURL, creds)

	calls.reset()
	_, err := repo.GetComponentVersion(ctx, component, version)
	r.NoError(err)
	r.Positive(calls.downloads(), "cold read must download content")

	calls.reset()
	_, err = repo.GetComponentVersion(ctx, component, version)
	r.NoError(err)
	t.Logf("Always: second read %d downloads / %d revalidations (%v)",
		calls.downloads(), calls.revalidations(), calls.dump())

	r.Zero(calls.downloads(), "RemotePolicyAlways must not re-download cached content")
	r.Positive(calls.revalidations(), "RemotePolicyAlways must re-check the registry on a cache hit")

	t.Run("a moved tag is not served stale", func(t *testing.T) {
		r := require.New(t)
		r.NoError(seedRepo.AddComponentVersion(ctx, cacheTestDescriptor(component, version, "republished")))

		calls.reset()
		got, err := repo.GetComponentVersion(ctx, component, version)
		r.NoError(err)
		t.Logf("Always after retag: %d downloads / %d revalidations (%v)",
			calls.downloads(), calls.revalidations(), calls.dump())
		r.Equal("republished", markerOf(t, got), "cache served content from before the tag moved")
	})
}
