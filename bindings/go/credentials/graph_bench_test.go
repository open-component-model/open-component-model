package credentials_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/runtime"
)

func Benchmark_Perf_Resolve_Direct(b *testing.B) {
	r := require.New(b)
	graph, err := GetGraph(b, testYAML)
	r.NoError(err)
	var creds runtime.Typed

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		creds, err = graph.Resolve(b.Context(), runtime.Identity{
			"type":     "OCIRegistry",
			"hostname": "docker.io",
		})
		r.NoError(err)
		r.NotEmpty(creds)
	}

}

func Benchmark_Perf_Resolve_Repository(b *testing.B) {
	r := require.New(b)
	graph, err := GetGraph(b, testYAML)
	r.NoError(err)
	var creds runtime.Typed

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		creds, err = graph.Resolve(b.Context(), runtime.Identity{
			"type":     "OCIRegistry",
			"hostname": "quay.io",
		})
		r.NoError(err)
		r.NotEmpty(creds)
	}

}

func Benchmark_Perf_Resolve_Indirect_CatchAll(b *testing.B) {
	r := require.New(b)
	graph, err := GetGraph(b, testYAML)
	r.NoError(err)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		creds, err := graph.Resolve(b.Context(), runtime.Identity{
			"type":     "SomeCatchAllType",
			"hostname": "some-hostname.com",
		})
		r.NoError(err)
		r.NotEmpty(creds)
	}

}

func Benchmark_Perf_Resolve_Indirect_PartialPath(b *testing.B) {
	r := require.New(b)
	graph, err := GetGraph(b, testYAML)
	r.NoError(err)
	var creds runtime.Typed

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		creds, err = graph.Resolve(b.Context(), runtime.Identity{
			"type":     "OCIRegistry",
			"hostname": "quay.io",
			"path":     "some-owner/some-repo",
		})
		r.NoError(err)
		r.NotEmpty(creds)
	}

}
