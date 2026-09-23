package integration_test

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/opencontainers/go-digest"
	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/log"
	"github.com/testcontainers/testcontainers-go/modules/registry"

	"ocm.software/open-component-model/bindings/go/blob"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/oci"
	ociblob "ocm.software/open-component-model/bindings/go/oci/blob"
	"ocm.software/open-component-model/bindings/go/oci/internal/pack"
	"ocm.software/open-component-model/bindings/go/oci/internal/remotestore"
	urlresolver "ocm.software/open-component-model/bindings/go/oci/resolver/url"
	ocmoci "ocm.software/open-component-model/bindings/go/oci/spec/access"
	ocmruntime "ocm.software/open-component-model/bindings/go/runtime"
)

// Test_Integration_OCIRepository_ChunkedPush verifies that RemoteStore uploads
// blobs to a real registry via the OCI chunked blob push protocol (POST/PATCH/
// PUT), for both a known-digest Push and an unknown-digest streaming push, and
// that the content round-trips.
func Test_Integration_OCIRepository_ChunkedPush(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping chunked push integration test in short mode")
	}
	t.Parallel()
	r := require.New(t)
	ctx := t.Context()

	password := generateRandomPassword(t, passwordLength)
	htpasswd := generateHtpasswd(t, testUsername, password)
	registryContainer, err := registry.Run(ctx, distributionRegistryImage,
		registry.WithHtpasswd(htpasswd),
		testcontainers.WithEnv(map[string]string{"REGISTRY_VALIDATION_DISABLED": "true"}),
		testcontainers.WithLogger(log.TestLogger(t)),
	)
	r.NoError(err)
	t.Cleanup(func() { r.NoError(testcontainers.TerminateContainer(registryContainer)) })

	address, err := registryContainer.HostAddress(ctx)
	r.NoError(err)

	// Small chunk size so a modest blob spans several PATCH requests.
	const chunkSize = 1024
	resolver, err := urlresolver.New(
		urlresolver.WithBaseURL(address),
		urlresolver.WithPlainHTTP(true),
		urlresolver.WithBaseClient(createAuthClient(address, testUsername, password)),
		urlresolver.WithChunkedPush(chunkSize, 1),
	)
	r.NoError(err)

	store, err := resolver.StoreForReference(ctx, address+"/chunked-test:latest")
	r.NoError(err)
	rs, ok := store.(*remotestore.RemoteStore)
	r.Truef(ok, "store %T must be *remotestore.RemoteStore", store)

	// ~4.5 chunks of pseudo-random-ish content.
	data := bytes.Repeat([]byte("chunked-blob-payload!"), 220)
	r.Greater(int64(len(data)), int64(chunkSize*4))

	t.Run("known-digest chunked Push round-trips", func(t *testing.T) {
		r := require.New(t)
		dig := digest.FromBytes(data)
		desc := ociImageSpecV1.Descriptor{MediaType: "application/octet-stream", Digest: dig, Size: int64(len(data))}
		r.NoError(rs.Push(ctx, desc, bytes.NewReader(data)))
		assertBlobRoundTrips(t, ctx, rs, desc, data)
	})

	t.Run("unknown-digest streaming push computes digest and round-trips", func(t *testing.T) {
		r := require.New(t)
		streamData := append([]byte("streamed-"), data...)
		got, err := rs.PushStreaming(ctx,
			ociImageSpecV1.Descriptor{MediaType: "application/octet-stream"},
			bytes.NewReader(streamData))
		r.NoError(err)
		r.Equal(digest.FromBytes(streamData), got.Digest)
		r.Equal(int64(len(streamData)), got.Size)
		assertBlobRoundTrips(t, ctx, rs, got, streamData)
	})
}

func assertBlobRoundTrips(t *testing.T, ctx context.Context, rs *remotestore.RemoteStore, desc ociImageSpecV1.Descriptor, want []byte) {
	t.Helper()
	r := require.New(t)
	exists, err := rs.Exists(ctx, desc)
	r.NoError(err)
	r.True(exists, "pushed blob must exist in registry")

	rc, err := rs.Fetch(ctx, desc)
	r.NoError(err)
	t.Cleanup(func() { _ = rc.Close() })
	got, err := io.ReadAll(rc)
	r.NoError(err)
	r.Equal(want, got)
}

// patchSignalTransport wraps an http.RoundTripper and closes `seen` the first
// time it observes a PATCH request, letting a test detect that a chunk was
// uploaded to the registry.
type patchSignalTransport struct {
	base http.RoundTripper
	once sync.Once
	seen chan struct{}
}

func (t *patchSignalTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.Method == http.MethodPatch {
		t.once.Do(func() { close(t.seen) })
	}
	return t.base.RoundTrip(req)
}

// interleavingReader yields firstChunk, then blocks until `patched` is closed
// (or a timeout), then yields the remainder. If the upload buffered the whole
// blob before contacting the registry, no PATCH would fire while the reader is
// blocked, so the read times out — proving buffering. Streaming uploads the
// first chunk (PATCH) while the reader is still blocked on the remainder, so it
// unblocks and the push completes.
type interleavingReader struct {
	firstChunk []byte
	rest       []byte
	patched    <-chan struct{}
	t          *testing.T

	pos      int
	waited   bool
	sawPatch bool
}

func (r *interleavingReader) Read(p []byte) (int, error) {
	if r.pos < len(r.firstChunk) {
		n := copy(p, r.firstChunk[r.pos:])
		r.pos += n
		return n, nil
	}
	if !r.waited {
		r.waited = true
		select {
		case <-r.patched:
			r.sawPatch = true
		case <-time.After(30 * time.Second):
			r.t.Fatal("no PATCH observed before the blob was fully read: upload buffered instead of streaming")
		}
	}
	restPos := r.pos - len(r.firstChunk)
	if restPos >= len(r.rest) {
		return 0, io.EOF
	}
	n := copy(p, r.rest[restPos:])
	r.pos += n
	return n, nil
}

// streamOnlyBlob is a ReadOnlyBlob with neither a known size nor digest (not
// SizeAware/DigestAware), forcing the pack layer's unknown-digest path: it must
// either stream the blob or buffer it to learn the digest.
type streamOnlyBlob struct {
	reader    io.Reader
	mediaType string
}

func (b *streamOnlyBlob) ReadCloser() (io.ReadCloser, error) { return io.NopCloser(b.reader), nil }
func (b *streamOnlyBlob) MediaType() (string, bool)          { return b.mediaType, b.mediaType != "" }

// Test_Integration_OCIRepository_StreamingDoesNotBuffer proves that packing a
// blob with an unknown digest streams it (chunked PATCHes with the digest
// computed on the fly) instead of buffering it to precompute the digest. The
// reader blocks mid-stream until it observes a PATCH reach the registry; a
// buffering implementation would deadlock (and time out) because it reads the
// entire blob before issuing any network request.
func Test_Integration_OCIRepository_StreamingDoesNotBuffer(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping streaming no-buffer integration test in short mode")
	}
	t.Parallel()
	r := require.New(t)
	ctx := t.Context()

	password := generateRandomPassword(t, passwordLength)
	htpasswd := generateHtpasswd(t, testUsername, password)
	registryContainer, err := registry.Run(ctx, distributionRegistryImage,
		registry.WithHtpasswd(htpasswd),
		testcontainers.WithEnv(map[string]string{"REGISTRY_VALIDATION_DISABLED": "true"}),
		testcontainers.WithLogger(log.TestLogger(t)),
	)
	r.NoError(err)
	t.Cleanup(func() { r.NoError(testcontainers.TerminateContainer(registryContainer)) })

	address, err := registryContainer.HostAddress(ctx)
	r.NoError(err)

	// Auth client whose transport signals the first PATCH.
	authClient := createAuthClient(address, testUsername, password)
	base := http.DefaultTransport
	if authClient.Client != nil && authClient.Client.Transport != nil {
		base = authClient.Client.Transport
	}
	signal := &patchSignalTransport{base: base, seen: make(chan struct{})}
	clientCopy := *authClient.Client
	clientCopy.Transport = signal
	authClient.Client = &clientCopy

	const chunkSize = 1024
	resolver, err := urlresolver.New(
		urlresolver.WithBaseURL(address),
		urlresolver.WithPlainHTTP(true),
		urlresolver.WithBaseClient(authClient),
		urlresolver.WithChunkedPush(chunkSize, 1),
	)
	r.NoError(err)

	store, err := resolver.StoreForReference(ctx, address+"/stream-no-buffer:latest")
	r.NoError(err)

	first := bytes.Repeat([]byte("A"), chunkSize)
	rest := bytes.Repeat([]byte("B"), chunkSize*3)
	whole := append(append([]byte{}, first...), rest...)
	wantDigest := digest.FromBytes(whole)

	reader := &interleavingReader{firstChunk: first, rest: rest, patched: signal.seen, t: t}
	src := &streamOnlyBlob{reader: reader, mediaType: "application/octet-stream"}

	// Precondition: the blob is neither SizeAware nor DigestAware.
	_, sizeKnown := blob.ReadOnlyBlob(src).(blob.SizeAware)
	r.False(sizeKnown, "precondition: blob must not be SizeAware")
	_, digestKnown := blob.ReadOnlyBlob(src).(blob.DigestAware)
	r.False(digestKnown, "precondition: blob must not be DigestAware")

	scheme := ocmruntime.NewScheme()
	v2.MustAddToScheme(scheme)
	ocmoci.MustAddToScheme(scheme)

	resource := &descriptor.Resource{
		ElementMeta: descriptor.ElementMeta{ObjectMeta: descriptor.ObjectMeta{Name: "stream", Version: "v1.0.0"}},
		Type:        "blob",
		Relation:    descriptor.LocalRelation,
	}
	artifactBlob, err := ociblob.NewArtifactBlobWithMediaType(resource, src, "application/octet-stream")
	r.NoError(err)

	// LocalReference is empty so no digest is supplied via the access, forcing
	// the unknown-digest streaming path in ResourceLocalBlobOCILayer.
	access := &v2.LocalBlob{MediaType: "application/octet-stream"}
	layer, err := pack.ResourceLocalBlob(ctx, store, artifactBlob, access, pack.Options{
		AccessScheme:  scheme,
		BaseReference: address + "/stream-no-buffer",
	})
	r.NoError(err)

	// The reader observed a PATCH before it finished producing bytes: streaming.
	r.True(reader.sawPatch, "reader must observe a PATCH mid-stream (proves no buffering)")
	r.Equal(wantDigest, layer.Digest)
	r.Equal(int64(len(whole)), layer.Size)
	r.NotNil(resource.Digest)
	r.Equal(wantDigest.Encoded(), resource.Digest.Value)

	rs, ok := store.(*remotestore.RemoteStore)
	r.Truef(ok, "store %T must be *remotestore.RemoteStore", store)
	assertBlobRoundTrips(t, ctx, rs, layer, whole)
}

// Test_Integration_OCIRepository_AddComponentVersion_Streaming adds a component
// version whose single resource is backed by a lazily-loaded blob with unknown
// size and digest, then reads it back. This exercises the full add-cv transfer
// path (AddLocalResource -> pack.ArtifactBlob -> ResourceLocalBlobOCILayer ->
// RemoteStore.PushStreaming), proving the resource blob is streamed (not
// buffered) and the component version round-trips.
//
// Behavior of the transfer as a function of the input blob (remote registry
// store with chunked push enabled via WithChunkedPush):
//
//	blob size | blob digest | transfer behavior
//	----------+-------------+--------------------------------------------------
//	known     | known       | monolithic POST/PUT if size < threshold, else
//	          |             | chunked PATCH upload (RemoteStore.Push, threshold-
//	          |             | gated). No buffering; digest already present.
//	known     | unknown     | streamed: PushStreaming computes the digest during
//	          |             | upload. No buffering.
//	unknown   | known       | streamed: PushStreaming discovers the size during
//	          |             | upload and verifies the declared digest. No
//	          |             | buffering (previously this case buffered to learn
//	          |             | the size).
//	unknown   | unknown     | streamed: PushStreaming computes both size and
//	          |             | digest during upload. No buffering.
//
//	Non-streaming stores (CTF, memory, or remote with chunking disabled) do not
//	implement StreamingPusher, so an unknown size or digest falls back to
//	buffering the whole blob in memory to build the descriptor before a
//	monolithic push. Manifests are never chunked; they use the manifests
//	endpoint. A chunked-protocol failure before the first byte is consumed falls
//	back to the monolithic push so no registry regresses.
func Test_Integration_OCIRepository_AddComponentVersion_Streaming(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping add-cv streaming integration test in short mode")
	}
	t.Parallel()
	r := require.New(t)
	ctx := t.Context()

	password := generateRandomPassword(t, passwordLength)
	htpasswd := generateHtpasswd(t, testUsername, password)
	registryContainer, err := registry.Run(ctx, distributionRegistryImage,
		registry.WithHtpasswd(htpasswd),
		testcontainers.WithEnv(map[string]string{"REGISTRY_VALIDATION_DISABLED": "true"}),
		testcontainers.WithLogger(log.TestLogger(t)),
	)
	r.NoError(err)
	t.Cleanup(func() { r.NoError(testcontainers.TerminateContainer(registryContainer)) })

	address, err := registryContainer.HostAddress(ctx)
	r.NoError(err)

	scheme := ocmruntime.NewScheme()
	ocmoci.MustAddToScheme(scheme)
	v2.MustAddToScheme(scheme)

	resolver, err := urlresolver.New(
		urlresolver.WithBaseURL(address),
		urlresolver.WithPlainHTTP(true),
		urlresolver.WithBaseClient(createAuthClient(address, testUsername, password)),
		urlresolver.WithChunkedPush(1024 /*1 KiB chunks*/, 1 /*threshold*/),
	)
	r.NoError(err)

	repo, err := oci.NewRepository(oci.WithResolver(resolver), oci.WithScheme(scheme), oci.WithTempDir(t.TempDir()))
	r.NoError(err)

	const (
		component = "ocm.software/test/streamed-cv"
		version   = "v1.0.0"
	)
	cd := &descriptor.Descriptor{
		Meta: descriptor.Meta{Version: "v2"},
		Component: descriptor.Component{
			Provider:      descriptor.Provider{Name: "ocm.software/test"},
			ComponentMeta: descriptor.ComponentMeta{ObjectMeta: descriptor.ObjectMeta{Name: component, Version: version}},
		},
	}

	// Resource payload spanning several chunks; wrapped in a lazy blob that
	// exposes neither size nor digest, forcing the streaming transfer path.
	payload := bytes.Repeat([]byte("component-version-layer-"), 300)
	wantDigest := digest.FromBytes(payload)
	resource := &descriptor.Resource{
		ElementMeta: descriptor.ElementMeta{ObjectMeta: descriptor.ObjectMeta{Name: "streamed", Version: version}},
		Type:        "blob",
		Relation:    descriptor.LocalRelation,
		Access:      &v2.LocalBlob{MediaType: "application/octet-stream"},
	}

	newRes, err := repo.AddLocalResource(ctx, component, version, resource,
		&streamOnlyBlob{reader: bytes.NewReader(payload), mediaType: "application/octet-stream"})
	r.NoError(err)
	r.NotNil(newRes)
	cd.Component.Resources = []descriptor.Resource{*newRes}

	r.NoError(repo.AddComponentVersion(ctx, cd))

	// The component version round-trips and the resource blob is retrievable
	// with the digest computed during the streaming upload.
	fetched, err := repo.GetComponentVersion(ctx, component, version)
	r.NoError(err)
	r.Equal(component, fetched.Component.Name)

	blobRC, gotRes, err := repo.GetLocalResource(ctx, component, version, resource.ElementMeta.ToIdentity())
	r.NoError(err)
	r.NotNil(gotRes)
	rc, err := blobRC.ReadCloser()
	r.NoError(err)
	t.Cleanup(func() { _ = rc.Close() })
	data, err := io.ReadAll(rc)
	r.NoError(err)
	r.Equal(payload, data)
	r.Equal(wantDigest, digest.FromBytes(data))
}
