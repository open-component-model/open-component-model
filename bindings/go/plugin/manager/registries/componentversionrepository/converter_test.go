package componentversionrepository

import (
	"context"
	"errors"
	"fmt"
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
	"ocm.software/open-component-model/bindings/go/repository/integrity"
	"ocm.software/open-component-model/bindings/go/runtime"
)

type fileLocalResourcePlugin struct {
	v1.ReadWriteOCMRepositoryPluginContract[runtime.Typed]
	path      string
	mediaType string
	resource  *descriptorv2.Resource
}

func (p *fileLocalResourcePlugin) GetLocalResource(context.Context, v1.GetLocalResourceRequest[runtime.Typed], runtime.Typed) (v1.GetLocalResourceResponse, error) {
	return v1.GetLocalResourceResponse{
		Location: mtypes.Location{
			LocationType: mtypes.LocationTypeLocalFile,
			Value:        p.path,
			MediaType:    p.mediaType,
		},
		Resource: p.resource,
	}, nil
}

const testArchiveMediaType = "application/vnd.test.archive"

// rootVerifier stands in for an OCI layout verifier: it accepts testArchiveMediaType
// archives and reports a mismatch at the end of the read if the requested root is not root.
type rootVerifier struct{ root digest.Digest }

func (v rootVerifier) Accepts(mediaType string) bool { return mediaType == testArchiveMediaType }

func (v rootVerifier) Verify(_ context.Context, rc io.ReadCloser, root digest.Digest, _ int64) io.ReadCloser {
	return &rootCheckingReader{ReadCloser: rc, ok: root == v.root, root: root}
}

type rootCheckingReader struct {
	io.ReadCloser
	ok   bool
	root digest.Digest
}

func (r *rootCheckingReader) Read(p []byte) (int, error) {
	n, err := r.ReadCloser.Read(p)
	if errors.Is(err, io.EOF) && !r.ok {
		return n, fmt.Errorf("archive root is not %s", r.root)
	}
	return n, err
}

func TestComponentVersionRepositoryWrapper_GetLocalResource_VerifiesOCIArtifactDigest(t *testing.T) {
	archiveRoot := digest.FromString("archive root")
	tests := []struct {
		name        string
		verifier    integrity.ArtifactVerifier
		root        digest.Digest
		wantGetErr  error
		wantReadErr string
	}{
		{name: "matching root", verifier: rootVerifier{root: archiveRoot}, root: archiveRoot},
		{name: "mismatching root", verifier: rootVerifier{root: archiveRoot}, root: digest.FromString("other"), wantReadErr: "archive root is not"},
		{name: "no artifact verifier", root: archiveRoot, wantGetErr: integrity.ErrUnverifiable},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := require.New(t)
			path := filepath.Join(t.TempDir(), "resource")
			r.NoError(os.WriteFile(path, []byte("archive"), 0o600))

			registry := NewComponentVersionRepositoryRegistry(t.Context())
			registry.SetArtifactVerifier(tt.verifier)
			wrapper := registry.externalToComponentVersionRepository(&fileLocalResourcePlugin{
				path:      path,
				mediaType: testArchiveMediaType,
				resource: &descriptorv2.Resource{
					ElementMeta: descriptorv2.ElementMeta{Name: "res", Version: "v1"},
					Type:        "ociImage",
					Access:      &runtime.Raw{Type: dummyType, Data: []byte(`{"type":"` + dummyType.String() + `"}`)},
					Digest: &descriptorv2.Digest{
						HashAlgorithm:          "SHA-256",
						NormalisationAlgorithm: descriptor.OCIArtifactDigestV1,
						Value:                  tt.root.Encoded(),
					},
				},
			}, runtime.NewScheme(), nil, nil)

			b, _, err := wrapper.GetLocalResource(t.Context(), "acme.org/component", "v1.0.0", runtime.Identity{"name": "res"})
			if tt.wantGetErr != nil {
				r.ErrorIs(err, tt.wantGetErr)
				return
			}
			r.NoError(err)
			rc, err := b.ReadCloser()
			r.NoError(err)
			_, readErr := io.Copy(io.Discard, rc)
			closeErr := rc.Close()
			if tt.wantReadErr == "" {
				r.NoError(readErr)
				r.NoError(closeErr)
				return
			}
			r.ErrorContains(errors.Join(readErr, closeErr), tt.wantReadErr)
		})
	}
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
