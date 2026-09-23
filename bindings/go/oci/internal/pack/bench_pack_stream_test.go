package pack_test

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"testing"

	"github.com/opencontainers/go-digest"
	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
	"oras.land/oras-go/v2/content"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	resourceblob "ocm.software/open-component-model/bindings/go/oci/blob"
	. "ocm.software/open-component-model/bindings/go/oci/internal/pack"
	oci "ocm.software/open-component-model/bindings/go/oci/spec/access"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// discardStreamingStore implements content.Storage plus the streaming pusher
// contract; it hashes streamed bytes on the fly and never buffers the whole
// blob, so a benchmark measures the pack layer's own allocations.
type discardStreamingStore struct{}

func (discardStreamingStore) Push(_ context.Context, _ ociImageSpecV1.Descriptor, r io.Reader) error {
	_, err := io.Copy(io.Discard, r)
	return err
}

func (discardStreamingStore) Fetch(_ context.Context, _ ociImageSpecV1.Descriptor) (io.ReadCloser, error) {
	return nil, fmt.Errorf("not implemented")
}

func (discardStreamingStore) Exists(_ context.Context, _ ociImageSpecV1.Descriptor) (bool, error) {
	return false, nil
}

func (discardStreamingStore) PushStreaming(_ context.Context, partial ociImageSpecV1.Descriptor, r io.Reader) (ociImageSpecV1.Descriptor, error) {
	digester := digest.Canonical.Digester()
	n, err := io.Copy(digester.Hash(), r)
	if err != nil {
		return ociImageSpecV1.Descriptor{}, err
	}
	return ociImageSpecV1.Descriptor{MediaType: partial.MediaType, Digest: digester.Digest(), Size: n}, nil
}

// discardBufferingStore implements only content.Storage (no PushStreaming), so
// the pack layer must buffer an unknown-digest/size blob to compute its
// descriptor before the monolithic push.
type discardBufferingStore struct{}

func (discardBufferingStore) Push(_ context.Context, _ ociImageSpecV1.Descriptor, r io.Reader) error {
	_, err := io.Copy(io.Discard, r)
	return err
}

func (discardBufferingStore) Fetch(_ context.Context, _ ociImageSpecV1.Descriptor) (io.ReadCloser, error) {
	return nil, fmt.Errorf("not implemented")
}

func (discardBufferingStore) Exists(_ context.Context, _ ociImageSpecV1.Descriptor) (bool, error) {
	return false, nil
}

// benchLazyBlob is a ReadOnlyBlob with unknown size and digest (not SizeAware or
// DigestAware) that hands out a fresh reader over a shared payload.
type benchLazyBlob struct {
	content   []byte
	mediaType string
}

func (b *benchLazyBlob) ReadCloser() (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader(b.content)), nil
}
func (b *benchLazyBlob) MediaType() (string, bool) { return b.mediaType, true }

func benchmarkResourceLayer(b *testing.B, storage content.Storage, size int) {
	b.Helper()
	payload := bytes.Repeat([]byte("x"), size)
	scheme := runtime.NewScheme()
	v2.MustAddToScheme(scheme)
	oci.MustAddToScheme(scheme)

	b.SetBytes(int64(size))
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		src := &benchLazyBlob{content: payload, mediaType: "application/octet-stream"}
		artifactBlob, err := resourceblob.NewArtifactBlob(&descriptor.Resource{}, src)
		if err != nil {
			b.Fatal(err)
		}
		access := &v2.LocalBlob{MediaType: "application/octet-stream"}
		if _, err := ResourceLocalBlobOCILayer(b.Context(), storage, artifactBlob, access, Options{AccessScheme: scheme}); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkResourceLayer_StreamingVsBuffering contrasts the memory profile of
// packing an unknown-digest blob when the storage supports streaming (no
// buffering) versus when it does not (the blob is buffered into memory to
// compute its descriptor before the monolithic push). The streaming variant
// should allocate roughly a constant amount regardless of blob size, while the
// buffering variant allocates on the order of the blob size.
//
// Run with:
//
//	go test -bench=BenchmarkResourceLayer_StreamingVsBuffering -benchmem ./oci/internal/pack/...
func BenchmarkResourceLayer_StreamingVsBuffering(b *testing.B) {
	for _, size := range []int{1 << 20, 16 << 20, 64 << 20} {
		name := fmt.Sprintf("%dMiB", size>>20)
		b.Run(name+"/streaming", func(b *testing.B) {
			benchmarkResourceLayer(b, discardStreamingStore{}, size)
		})
		b.Run(name+"/buffering", func(b *testing.B) {
			benchmarkResourceLayer(b, discardBufferingStore{}, size)
		})
	}
}
