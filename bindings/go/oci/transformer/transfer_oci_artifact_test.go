package transformer

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	ociaccess "ocm.software/open-component-model/bindings/go/oci/spec/access"
	accessv1 "ocm.software/open-component-model/bindings/go/oci/spec/access/v1"
	"ocm.software/open-component-model/bindings/go/oci/spec/transformation/v1alpha1"
	ocistream "ocm.software/open-component-model/bindings/go/oci/stream"
	"ocm.software/open-component-model/bindings/go/repository"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// streamingRepository is a minimal [ocistream.ResourceRepository] fake that
// supports allowing missing subjects and records into the shared seen sink
// whether the repository a stream was started from allows them.
type streamingRepository struct {
	repository.ResourceRepository

	allow bool
	seen  *bool
}

func (m *streamingRepository) WithAllowMissingSubjects(allow bool) ocistream.ResourceRepository {
	c := *m
	c.allow = allow
	return &c
}

func (m *streamingRepository) DownloadResourceStream(ctx context.Context, resource *descriptor.Resource, credentials runtime.Typed) (ocistream.ResourceStream, error) {
	*m.seen = m.allow
	return &ocistream.OCIResourceStream{}, nil
}

func (m *streamingRepository) UploadResourceStream(ctx context.Context, resource *descriptor.Resource, stream ocistream.ResourceStream, credentials runtime.Typed) (*descriptor.Resource, error) {
	resource = resource.DeepCopy()
	resource.Access = &accessv1.OCIImage{ImageReference: "example.com/image:1.0.0"}
	return resource, nil
}

// unconfigurableStreamingRepository supports streaming but has no
// WithAllowMissingSubjects derivation method.
type unconfigurableStreamingRepository struct {
	repository.ResourceRepository
}

func (m *unconfigurableStreamingRepository) DownloadResourceStream(ctx context.Context, resource *descriptor.Resource, credentials runtime.Typed) (ocistream.ResourceStream, error) {
	return &ocistream.OCIResourceStream{}, nil
}

func (m *unconfigurableStreamingRepository) UploadResourceStream(ctx context.Context, resource *descriptor.Resource, stream ocistream.ResourceStream, credentials runtime.Typed) (*descriptor.Resource, error) {
	resource = resource.DeepCopy()
	resource.Access = &accessv1.OCIImage{ImageReference: "example.com/image:1.0.0"}
	return resource, nil
}

func transferArtifactSpec(allowMissingSubjects bool) *v1alpha1.TransferOCIArtifact {
	return &v1alpha1.TransferOCIArtifact{
		Type: runtime.NewVersionedType(v1alpha1.TransferOCIArtifactType, v1alpha1.Version),
		ID:   "test-transfer",
		Spec: &v1alpha1.TransferOCIArtifactSpec{
			Resource: &v2.Resource{
				ElementMeta: v2.ElementMeta{ObjectMeta: v2.ObjectMeta{Name: "image", Version: "1.0.0"}},
				Type:        "ociImage",
				Relation:    "external",
			},
			TargetResource: &v2.Resource{
				ElementMeta: v2.ElementMeta{ObjectMeta: v2.ObjectMeta{Name: "image", Version: "1.0.0"}},
				Type:        "ociImage",
				Relation:    "external",
			},
			AllowMissingSubjects: allowMissingSubjects,
		},
	}
}

func TestTransferOCIArtifact_AllowMissingSubjects(t *testing.T) {
	scheme := runtime.NewScheme()
	v2.MustAddToScheme(scheme)
	ociaccess.MustAddToScheme(scheme)
	scheme.MustRegisterWithAlias(&v1alpha1.TransferOCIArtifact{}, v1alpha1.TransferOCIArtifactV1alpha1)

	tests := []struct {
		name            string
		allow           bool
		unconfigurable  bool
		wantErrContains string
	}{
		{name: "unset leaves repository untouched"},
		{name: "allowMissingSubjects is applied to the streaming repository", allow: true},
		{name: "unset works without repository support", unconfigurable: true},
		{
			name:            "allowMissingSubjects without repository support fails",
			allow:           true,
			unconfigurable:  true,
			wantErrContains: "does not support allowing missing subjects",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := require.New(t)

			var seen bool
			var repo repository.ResourceRepository = &streamingRepository{seen: &seen}
			if tc.unconfigurable {
				repo = &unconfigurableStreamingRepository{}
			}

			transformer := &TransferOCIArtifact{
				Scheme:     scheme,
				Repository: repo,
			}

			_, err := transformer.Transform(t.Context(), transferArtifactSpec(tc.allow))
			if tc.wantErrContains != "" {
				r.ErrorContains(err, tc.wantErrContains)
				return
			}
			r.NoError(err)
			r.Equal(tc.allow, seen)
		})
	}
}
