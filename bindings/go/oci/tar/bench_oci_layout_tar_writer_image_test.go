package tar

import (
	"fmt"
	"io"
	"testing"

	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
	"oras.land/oras-go/v2"
	"oras.land/oras-go/v2/registry/remote"
	"oras.land/oras-go/v2/registry/remote/auth"
	"oras.land/oras-go/v2/registry/remote/credentials"
)

// BenchmarkRealImage_CopyGraph benchmarks copying a real public OCI image
// from a registry into an OCILayoutWriter with varying concurrency levels.
//
// Run with: go test -bench=BenchmarkRealImage -benchtime=1x -timeout=5m -v ./tar/...
//
// This benchmark requires network access. It pulls alpine:3.20 (~3.6MB compressed)
// from Docker Hub, which is small enough to be fast but has enough layers to
// show concurrency effects. Note that if you run this many times concurrently, you
// might face issues due to rate-limiting without proper authentication on the running host.
func BenchmarkRealImage_CopyGraph(b *testing.B) {
	if testing.Short() {
		b.Skip("skipping remote connected benchmark in short mode")
	}
	ref := "docker.io/library/alpine:3.20"

	repo, err := remote.NewRepository(ref)
	if err != nil {
		b.Fatal(err)
	}
	store, err := credentials.NewStoreFromDocker(credentials.StoreOptions{
		DetectDefaultNativeStore: true,
	})
	if err != nil {
		b.Fatal(err)
	}
	clnt := auth.DefaultClient
	clnt.Credential = store.Get
	repo.Client = clnt

	// Resolve the manifest descriptor once (outside the benchmark loop)
	desc, err := repo.Resolve(b.Context(), "3.20")
	if err != nil {
		b.Fatalf("failed to resolve %s: %v (requires network access)", ref, err)
	}

	b.Logf("image: %s, manifest: %s, size: %d", ref, desc.Digest, desc.Size)

	benchCopy := func(b *testing.B, src oras.ReadOnlyGraphTarget, root ociImageSpecV1.Descriptor, concurrency int) {
		b.Helper()
		writer, err := NewOCILayoutWriterWithTempFile(io.Discard, b.TempDir())
		if err != nil {
			b.Fatal(err)
		}
		if err := oras.CopyGraph(b.Context(), src, writer, root, oras.CopyGraphOptions{
			Concurrency: concurrency,
		}); err != nil {
			b.Fatal(err)
		}
		if err := writer.Close(); err != nil {
			b.Fatal(err)
		}
	}

	for _, concurrency := range []int{1, 4, 8} {
		b.Run(fmt.Sprintf("concurrency_%d", concurrency), func(b *testing.B) {
			b.ResetTimer()
			for range b.N {
				benchCopy(b, repo, desc, concurrency)
			}
		})
	}
}
