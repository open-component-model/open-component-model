package remotestore

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/opencontainers/go-digest"
	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
	"oras.land/oras-go/v2/registry/remote"
)

// sinkRegistry is a minimal in-process registry that accepts both the
// monolithic (POST/PUT) and chunked (POST/PATCH/PUT) blob upload protocols and
// discards all bytes. It performs no per-request recording so benchmark
// allocation numbers reflect the client push path, not the fake server.
func sinkRegistryHandler() http.HandlerFunc {
	var session int
	return func(w http.ResponseWriter, r *http.Request) {
		// Drain and discard the body without buffering it.
		_, _ = io.Copy(io.Discard, r.Body)
		switch {
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/blobs/uploads/"):
			session++
			w.Header().Set("Location", fmt.Sprintf("/v2/test-repo/blobs/uploads/%d", session))
			w.WriteHeader(http.StatusAccepted)
		case r.Method == http.MethodPatch:
			w.Header().Set("Location", r.URL.Path)
			w.WriteHeader(http.StatusAccepted)
		case r.Method == http.MethodPut:
			w.Header().Set("Location", r.URL.Path)
			w.WriteHeader(http.StatusCreated)
		default:
			w.WriteHeader(http.StatusOK)
		}
	}
}

func benchStore(b *testing.B, srv *httptest.Server, chunkSize, threshold int64) *RemoteStore {
	b.Helper()
	repo, err := remote.NewRepository(srv.Listener.Addr().String() + "/test-repo")
	if err != nil {
		b.Fatal(err)
	}
	repo.PlainHTTP = true
	repo.Client = &http.Client{}
	return &RemoteStore{Repository: repo, ChunkSize: chunkSize, ChunkThreshold: threshold}
}

// blobSizes exercised by the push benchmarks.
var benchBlobSizes = []struct {
	name string
	size int
}{
	{"1MiB", 1 << 20},
	{"16MiB", 16 << 20},
	{"64MiB", 64 << 20},
}

// BenchmarkRemoteStore_Push compares the monolithic push (chunking disabled)
// against chunked push at several chunk sizes, for known-digest blobs. The
// server discards bytes, so this isolates the client-side request/loop/hashing
// overhead of the chunked protocol versus a single PUT.
//
// Run with:
//
//	go test -bench=BenchmarkRemoteStore_Push -benchmem ./oci/internal/remotestore/...
func BenchmarkRemoteStore_Push(b *testing.B) {
	srv := httptest.NewServer(sinkRegistryHandler())
	b.Cleanup(srv.Close)

	for _, bs := range benchBlobSizes {
		data := bytes.Repeat([]byte("x"), bs.size)
		dig := digest.FromBytes(data)
		desc := ociImageSpecV1.Descriptor{MediaType: "application/octet-stream", Digest: dig, Size: int64(len(data))}

		// Monolithic: chunking disabled -> single POST/PUT via oras.
		b.Run(bs.name+"/monolithic", func(b *testing.B) {
			store := benchStore(b, srv, 0, 0)
			b.SetBytes(int64(len(data)))
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				if err := store.Push(b.Context(), desc, bytes.NewReader(data)); err != nil {
					b.Fatal(err)
				}
			}
		})

		for _, chunk := range []int64{1 << 20, 4 << 20, 16 << 20} {
			b.Run(fmt.Sprintf("%s/chunked_%dMiB", bs.name, chunk>>20), func(b *testing.B) {
				store := benchStore(b, srv, chunk, 1)
				b.SetBytes(int64(len(data)))
				b.ReportAllocs()
				b.ResetTimer()
				for range b.N {
					if err := store.Push(b.Context(), desc, bytes.NewReader(data)); err != nil {
						b.Fatal(err)
					}
				}
			})
		}
	}
}

// BenchmarkRemoteStore_PushStreaming benchmarks the streaming push (digest and
// size computed during upload) at several chunk sizes. This is the path taken
// for blobs whose digest/size are not known in advance and would otherwise be
// buffered into memory before a monolithic push.
//
// Run with:
//
//	go test -bench=BenchmarkRemoteStore_PushStreaming -benchmem ./oci/internal/remotestore/...
func BenchmarkRemoteStore_PushStreaming(b *testing.B) {
	srv := httptest.NewServer(sinkRegistryHandler())
	b.Cleanup(srv.Close)

	for _, bs := range benchBlobSizes {
		data := bytes.Repeat([]byte("x"), bs.size)
		for _, chunk := range []int64{1 << 20, 4 << 20, 16 << 20} {
			b.Run(fmt.Sprintf("%s/chunked_%dMiB", bs.name, chunk>>20), func(b *testing.B) {
				store := benchStore(b, srv, chunk, 1)
				partial := ociImageSpecV1.Descriptor{MediaType: "application/octet-stream"}
				b.SetBytes(int64(len(data)))
				b.ReportAllocs()
				b.ResetTimer()
				for range b.N {
					if _, err := store.PushStreaming(b.Context(), partial, bytes.NewReader(data)); err != nil {
						b.Fatal(err)
					}
				}
			})
		}
	}
}
