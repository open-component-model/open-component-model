package repository_test

import (
	"bytes"
	"context"
	"io"
	"testing"

	"github.com/opencontainers/go-digest"
	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/blob/verified"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/repository"
	"ocm.software/open-component-model/bindings/go/repository/integrity"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// localRepository serves a local resource that records digest, as a component version
// repository does that reads the component version again by a tag that may have moved.
// Like an OCI repository, it binds the content to the digest of the resource it returns.
type localRepository struct {
	repository.ComponentVersionRepository
	digest *descriptor.Digest
	served blob.ReadOnlyBlob
}

func (l *localRepository) GetLocalResource(ctx context.Context, _, _ string, _ runtime.Identity) (blob.ReadOnlyBlob, *descriptor.Resource, error) {
	b, err := integrity.Verify(ctx, l.digest, l.served)
	return b, &descriptor.Resource{Digest: l.digest}, err
}

// trackedBlob records whether it was opened and closed.
type trackedBlob struct {
	content string
	opened  bool
	closed  bool
}

func (b *trackedBlob) ReadCloser() (io.ReadCloser, error) {
	b.opened = true
	return io.NopCloser(bytes.NewReader([]byte(b.content))), nil
}

func (b *trackedBlob) Close() error {
	b.closed = true
	return nil
}

func genericDigest(d digest.Digest) *descriptor.Digest {
	return &descriptor.Digest{HashAlgorithm: "SHA-256", NormalisationAlgorithm: descriptor.GenericBlobDigestV1, Value: d.Encoded()}
}

func dataResource(d *descriptor.Digest) *descriptor.Resource {
	return &descriptor.Resource{ElementMeta: descriptor.ElementMeta{ObjectMeta: descriptor.ObjectMeta{Name: "data"}}, Digest: d}
}

func TestGetLocalResource_MustRecordResolvedDigest(t *testing.T) {
	recorded := genericDigest(digest.FromString("content"))
	other := genericDigest(digest.FromString("other"))
	sameValue := *recorded

	tests := []struct {
		name             string
		resolved, served *descriptor.Digest
		wantErr          string
	}{
		{name: "same digest", resolved: recorded, served: &sameValue},
		{name: "neither has a digest"},
		{
			name: "another digest", resolved: recorded, served: other,
			wantErr: "records digest SHA-256:" + other.Value + " (genericBlobDigest/v1), but the resolved component version records SHA-256:" + recorded.Value + " (genericBlobDigest/v1)",
		},
		{name: "digest dropped", resolved: recorded, wantErr: "records digest <none>"},
		{name: "digest added", served: recorded, wantErr: "the resolved component version records <none>"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := require.New(t)
			served := &trackedBlob{content: "content"}
			repo := &localRepository{digest: tt.served, served: served}

			data, err := repository.GetLocalResource(t.Context(), repo, "ocm.software/test", "1.0.0", dataResource(tt.resolved))
			if tt.wantErr == "" {
				r.NoError(err)
				r.NotNil(data)
				return
			}
			r.ErrorContains(err, tt.wantErr)
			r.True(served.closed, "content of a substituted resource must be released")
		})
	}
}

// VerifyLocalResource only reads content the repository cannot check by its address.
func TestVerifyLocalResource(t *testing.T) {
	content := "content"
	want := genericDigest(digest.FromString(content))
	for _, tc := range []struct {
		name     string
		served   func(*trackedBlob) blob.ReadOnlyBlob
		wantRead bool
		wantErr  string
	}{
		{
			name: "stored under the recorded digest",
			served: func(b *trackedBlob) blob.ReadOnlyBlob {
				return verified.New(b, verified.Metadata{Digest: digest.FromString(content), Size: blob.SizeUnknown})
			},
		},
		{
			name: "stored under another digest",
			served: func(b *trackedBlob) blob.ReadOnlyBlob {
				return verified.New(b, verified.Metadata{Digest: digest.FromString("other"), Size: blob.SizeUnknown})
			},
			wantErr: "digest mismatch",
		},
		{
			name:     "not addressed by a digest",
			served:   func(b *trackedBlob) blob.ReadOnlyBlob { return b },
			wantRead: true,
		},
		{
			name: "not addressed by a digest, with other content",
			served: func(b *trackedBlob) blob.ReadOnlyBlob {
				b.content = "tampered"
				return b
			},
			wantRead: true,
			wantErr:  "digest mismatch",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := require.New(t)
			tracked := &trackedBlob{content: content}
			repo := &localRepository{digest: want, served: tc.served(tracked)}

			err := repository.VerifyLocalResource(t.Context(), repo, "ocm.software/test", "1.0.0", dataResource(want))
			if tc.wantErr != "" {
				r.ErrorContains(err, tc.wantErr)
			} else {
				r.NoError(err)
			}
			r.Equal(tc.wantRead, tracked.opened, "content is read only if it cannot be checked otherwise")
		})
	}
}
