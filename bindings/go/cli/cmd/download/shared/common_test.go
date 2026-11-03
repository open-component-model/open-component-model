package shared_test

import (
	"bytes"
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/blob/inmemory"
	"ocm.software/open-component-model/bindings/go/cli/cmd/download/shared"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/repository"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// localRepository serves a local resource that records digest, as a component version
// repository does that reads the component version again by a tag that may have moved.
type localRepository struct {
	repository.ComponentVersionRepository
	digest *descriptor.Digest
	served *closableBlob
}

type closableBlob struct {
	*inmemory.Blob
	closed bool
}

func (c *closableBlob) Close() error {
	c.closed = true
	return nil
}

func (l *localRepository) GetLocalResource(context.Context, string, string, runtime.Identity) (blob.ReadOnlyBlob, *descriptor.Resource, error) {
	return l.served, &descriptor.Resource{Digest: l.digest}, nil
}

func TestDownloadResourceData_LocalResourceMustRecordResolvedDigest(t *testing.T) {
	recorded := &descriptor.Digest{HashAlgorithm: "SHA-256", NormalisationAlgorithm: descriptor.GenericBlobDigestV1, Value: "aa"}
	other := &descriptor.Digest{HashAlgorithm: "SHA-256", NormalisationAlgorithm: descriptor.GenericBlobDigestV1, Value: "bb"}
	sameValue := *recorded

	tests := []struct {
		name             string
		resolved, served *descriptor.Digest
		wantErr          string
	}{
		{name: "same digest", resolved: recorded, served: &sameValue},
		{name: "neither has a digest"},
		{name: "another digest", resolved: recorded, served: other, wantErr: "records digest SHA-256:bb (genericBlobDigest/v1), but the resolved component version records SHA-256:aa (genericBlobDigest/v1)"},
		{name: "digest dropped", resolved: recorded, wantErr: "records digest <none>"},
		{name: "digest added", served: recorded, wantErr: "the resolved component version records <none>"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := require.New(t)
			served := &closableBlob{Blob: inmemory.New(bytes.NewReader([]byte("content")))}
			repo := &localRepository{digest: tt.served, served: served}
			desc := &descriptor.Descriptor{Component: descriptor.Component{
				ComponentMeta: descriptor.ComponentMeta{ObjectMeta: descriptor.ObjectMeta{Name: "ocm.software/test", Version: "1.0.0"}},
			}}
			res := &descriptor.Resource{
				ElementMeta: descriptor.ElementMeta{ObjectMeta: descriptor.ObjectMeta{Name: "data"}},
				Access:      &v2.LocalBlob{Type: runtime.NewVersionedType(v2.LocalBlobAccessType, v2.LocalBlobAccessTypeVersion)},
				Digest:      tt.resolved,
			}

			data, err := shared.DownloadResourceData(t.Context(), nil, nil, desc, repo, res, runtime.Identity{"name": "data"})
			if tt.wantErr == "" {
				r.NoError(err)
				r.Same(served, data)
				return
			}
			r.ErrorContains(err, tt.wantErr)
			r.True(served.closed, "content of a substituted resource must be released")
		})
	}
}
