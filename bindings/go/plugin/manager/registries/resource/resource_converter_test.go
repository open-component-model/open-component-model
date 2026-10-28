package resource

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/opencontainers/go-digest"
	"github.com/stretchr/testify/require"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	v1 "ocm.software/open-component-model/bindings/go/plugin/manager/contracts/resource/v1"
	mtypes "ocm.software/open-component-model/bindings/go/plugin/manager/types"
	"ocm.software/open-component-model/bindings/go/runtime"
)

type fileResourcePlugin struct {
	v1.ReadWriteResourcePluginContract
	path string
}

func (p *fileResourcePlugin) GetGlobalResource(context.Context, *v1.GetGlobalResourceRequest, runtime.Typed) (*v1.GetGlobalResourceResponse, error) {
	return &v1.GetGlobalResourceResponse{Location: mtypes.Location{
		LocationType: mtypes.LocationTypeLocalFile,
		Value:        p.path,
	}}, nil
}

func TestResourcePluginConverter_DownloadResource_VerifiesDigest(t *testing.T) {
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

			converter := &resourcePluginConverter{
				externalPlugin: &fileResourcePlugin{path: path},
				scheme:         runtime.NewScheme(),
			}
			res := &descriptor.Resource{
				ElementMeta: descriptor.ElementMeta{ObjectMeta: descriptor.ObjectMeta{Name: "res", Version: "v1"}},
				Type:        "blob",
				Access:      &runtime.Raw{Type: dummyType, Data: []byte(`{"type":"` + dummyType.String() + `"}`)},
				Digest: &descriptor.Digest{
					HashAlgorithm:          "SHA-256",
					NormalisationAlgorithm: descriptor.GenericBlobDigestV1,
					Value:                  digest.FromString("y").Encoded(),
				},
			}

			b, err := converter.DownloadResource(t.Context(), res, nil)
			r.NoError(err)
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
