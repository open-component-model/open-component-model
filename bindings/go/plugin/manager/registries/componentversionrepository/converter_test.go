package componentversionrepository

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/opencontainers/go-digest"
	"github.com/stretchr/testify/require"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	descriptorv2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	v1 "ocm.software/open-component-model/bindings/go/plugin/manager/contracts/ocmrepository/v1"
	mtypes "ocm.software/open-component-model/bindings/go/plugin/manager/types"
	"ocm.software/open-component-model/bindings/go/runtime"
)

type fileLocalResourcePlugin struct {
	v1.ReadWriteOCMRepositoryPluginContract[runtime.Typed]
	path     string
	resource *descriptorv2.Resource
}

func (p *fileLocalResourcePlugin) GetLocalResource(context.Context, v1.GetLocalResourceRequest[runtime.Typed], runtime.Typed) (v1.GetLocalResourceResponse, error) {
	return v1.GetLocalResourceResponse{
		Location: mtypes.Location{
			LocationType: mtypes.LocationTypeLocalFile,
			Value:        p.path,
		},
		Resource: p.resource,
	}, nil
}

func TestComponentVersionRepositoryWrapper_GetLocalResource_VerifiesDigest(t *testing.T) {
	tests := []struct {
		name    string
		content string
		wantErr string
	}{
		{name: "matching content", content: "y"},
		{name: "mismatching content", content: "x", wantErr: "digest mismatch"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := require.New(t)
			path := filepath.Join(t.TempDir(), "resource")
			r.NoError(os.WriteFile(path, []byte(tt.content), 0o600))

			wrapper := &componentVersionRepositoryWrapper{
				externalPlugin: &fileLocalResourcePlugin{
					path: path,
					resource: &descriptorv2.Resource{
						ElementMeta: descriptorv2.ElementMeta{Name: "res", Version: "v1"},
						Type:        "blob",
						Access:      &runtime.Raw{Type: dummyType, Data: []byte(`{"type":"` + dummyType.String() + `"}`)},
						Digest: &descriptorv2.Digest{
							HashAlgorithm:          "SHA-256",
							NormalisationAlgorithm: descriptor.GenericBlobDigestV1,
							Value:                  digest.FromString("y").Encoded(),
						},
					},
				},
				scheme: runtime.NewScheme(),
			}

			b, res, err := wrapper.GetLocalResource(t.Context(), "acme.org/component", "v1.0.0", runtime.Identity{"name": "res"})
			r.NoError(err)
			r.Equal("res", res.Name)
			rc, err := b.ReadCloser()
			r.NoError(err)
			data, readErr := io.ReadAll(rc)
			closeErr := rc.Close()
			if tt.wantErr == "" {
				r.NoError(readErr)
				r.NoError(closeErr)
				r.Equal(tt.content, string(data))
				return
			}
			r.ErrorContains(readErr, tt.wantErr)
		})
	}
}
