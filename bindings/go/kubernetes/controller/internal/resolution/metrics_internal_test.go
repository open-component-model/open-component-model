package resolution

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/ctf"
	"ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/oci"
	"ocm.software/open-component-model/bindings/go/runtime"
)

func TestRepositoryLabel(t *testing.T) {
	ociRepo := func(typ, baseURL string) runtime.Typed {
		return &oci.Repository{Type: runtime.NewUnversionedType(typ), BaseUrl: baseURL}
	}
	raw := &runtime.Raw{}
	require.NoError(t, raw.UnmarshalJSON([]byte(`{"type":"OCIRegistry","baseUrl":"https://ghcr.io/org/components"}`)))

	tests := []struct {
		name string
		spec runtime.Typed
		want string
	}{
		{"host without scheme or path", ociRepo(oci.Type, "ghcr.io/org/repo"), "ghcr.io"},
		{"scheme and port are reduced to host and port", ociRepo(oci.LegacyRegistryType, "http://registry:5000/sub"), "registry:5000"},
		{"raw spec is converted", raw, "ghcr.io"},
		{"ctf path is never used", &ctf.Repository{Type: runtime.NewUnversionedType(ctf.ShortType), FilePath: "/tmp/a"}, ctf.ShortType2},
		{"unknown type falls back to its name", &runtime.Raw{Type: runtime.NewUnversionedType("PluginRepo")}, "PluginRepo"},
		{"nil spec", nil, "unknown"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := require.New(t)
			r.Equal(tc.want, repositoryLabel(tc.spec))
		})
	}
}

func TestResultLabel(t *testing.T) {
	r := require.New(t)
	r.Equal("success", resultLabel(nil))
	r.Equal("success", resultLabel(fmt.Errorf("%w: %w", ErrNotSafelyDigestible, errors.New("legacy digest"))))
	r.Equal("error", resultLabel(errors.New("unauthorized")))
}
