package transformer

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/oci"
	ociaccess "ocm.software/open-component-model/bindings/go/oci/spec/access"
	accessv1 "ocm.software/open-component-model/bindings/go/oci/spec/access/v1"
	"ocm.software/open-component-model/bindings/go/oci/spec/transformation/v1alpha1"
	ocistream "ocm.software/open-component-model/bindings/go/oci/stream"
	"ocm.software/open-component-model/bindings/go/repository"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// streamingRepository is a minimal [ocistream.ResourceRepository] fake that
// supports weak edge failure policy derivation and records the policy of the
// repository a stream was started from into the shared seenPolicy sink.
type streamingRepository struct {
	repository.ResourceRepository

	policy     *oci.WeakEdgeFailurePolicy
	seenPolicy **oci.WeakEdgeFailurePolicy
}

func (m *streamingRepository) WithWeakEdgeFailurePolicy(policy oci.WeakEdgeFailurePolicy) ocistream.ResourceRepository {
	c := *m
	c.policy = &policy
	return &c
}

func (m *streamingRepository) DownloadResourceStream(ctx context.Context, resource *descriptor.Resource, credentials runtime.Typed) (ocistream.ResourceStream, error) {
	*m.seenPolicy = m.policy
	return &ocistream.OCIResourceStream{}, nil
}

func (m *streamingRepository) UploadResourceStream(ctx context.Context, resource *descriptor.Resource, stream ocistream.ResourceStream, credentials runtime.Typed) (*descriptor.Resource, error) {
	resource = resource.DeepCopy()
	resource.Access = &accessv1.OCIImage{ImageReference: "example.com/image:1.0.0"}
	return resource, nil
}

// unconfigurableStreamingRepository supports streaming but has no
// WithWeakEdgeFailurePolicy derivation method.
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

func transferArtifactSpec(policy v1alpha1.WeakEdgeFailurePolicy) *v1alpha1.TransferOCIArtifact {
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
			WeakEdgeFailurePolicy: policy,
		},
	}
}

func TestTransferOCIArtifact_WeakEdgeFailurePolicy(t *testing.T) {
	scheme := runtime.NewScheme()
	v2.MustAddToScheme(scheme)
	ociaccess.MustAddToScheme(scheme)
	scheme.MustRegisterWithAlias(&v1alpha1.TransferOCIArtifact{}, v1alpha1.TransferOCIArtifactV1alpha1)

	skip := oci.WeakEdgeFailurePolicySkip
	abort := oci.WeakEdgeFailurePolicyAbort

	tests := []struct {
		name            string
		specPolicy      v1alpha1.WeakEdgeFailurePolicy
		unconfigurable  bool
		wantPolicy      *oci.WeakEdgeFailurePolicy
		wantErrContains string
	}{
		{
			name:       "no policy leaves repository untouched",
			specPolicy: "",
		},
		{
			name:       "skip policy is applied to the streaming repository",
			specPolicy: v1alpha1.WeakEdgeFailurePolicySkip,
			wantPolicy: &skip,
		},
		{
			name:       "abort policy is applied explicitly",
			specPolicy: v1alpha1.WeakEdgeFailurePolicyAbort,
			wantPolicy: &abort,
		},
		{
			name:            "invalid policy is rejected",
			specPolicy:      "bogus",
			wantErrContains: "unsupported weakEdgeFailurePolicy",
		},
		{
			name:            "policy without repository support fails",
			specPolicy:      v1alpha1.WeakEdgeFailurePolicySkip,
			unconfigurable:  true,
			wantErrContains: "does not support configuring a weak edge failure policy",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := require.New(t)

			var seen *oci.WeakEdgeFailurePolicy
			var repo repository.ResourceRepository = &streamingRepository{seenPolicy: &seen}
			if tc.unconfigurable {
				repo = &unconfigurableStreamingRepository{}
			}

			transformer := &TransferOCIArtifact{
				Scheme:     scheme,
				Repository: repo,
			}

			_, err := transformer.Transform(t.Context(), transferArtifactSpec(tc.specPolicy))
			if tc.wantErrContains != "" {
				r.ErrorContains(err, tc.wantErrContains)
				return
			}
			r.NoError(err)
			if tc.wantPolicy == nil {
				r.Nil(seen, "no policy must have been derived")
			} else {
				r.NotNil(seen)
				r.Equal(*tc.wantPolicy, *seen)
			}
		})
	}
}
