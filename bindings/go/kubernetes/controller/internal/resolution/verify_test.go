package resolution

import (
	"context"
	"crypto"
	"errors"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/kubernetes/controller/internal/verification"
	"ocm.software/open-component-model/bindings/go/repository"
	"ocm.software/open-component-model/bindings/go/signing"
)

type fakeRepository struct {
	repository.ComponentVersionRepository
	desc *descriptor.Descriptor
	err  error
}

func (f *fakeRepository) GetComponentVersion(context.Context, string, string) (*descriptor.Descriptor, error) {
	return f.desc, f.err
}

func TestGetComponentVersion(t *testing.T) {
	desc := &descriptor.Descriptor{
		Component: descriptor.Component{
			ComponentMeta: descriptor.ComponentMeta{
				ObjectMeta: descriptor.ObjectMeta{Name: "ocm.software/test", Version: "1.0.0"},
			},
			Provider: descriptor.Provider{Name: "ocm.software"},
		},
	}

	digest, err := signing.GenerateDigest(t.Context(), desc, slog.New(slog.DiscardHandler),
		signing.LegacyNormalisationAlgo, crypto.SHA256.String())
	require.NoError(t, err)

	matching := &v2.Digest{
		HashAlgorithm:          digest.HashAlgorithm,
		NormalisationAlgorithm: digest.NormalisationAlgorithm,
		Value:                  digest.Value,
	}
	mismatching := &v2.Digest{
		HashAlgorithm:          digest.HashAlgorithm,
		NormalisationAlgorithm: digest.NormalisationAlgorithm,
		Value:                  "deadbeef",
	}
	repoErr := errors.New("registry unavailable")

	tests := []struct {
		name    string
		repo    *fakeRepository
		opts    resolveOptions
		wantErr string
	}{
		{
			name: "no verification returns the descriptor",
			repo: &fakeRepository{desc: desc},
		},
		{
			name:    "repository error is returned",
			repo:    &fakeRepository{err: repoErr},
			wantErr: repoErr.Error(),
		},
		{
			name: "matching digest",
			repo: &fakeRepository{desc: desc},
			opts: resolveOptions{digest: matching},
		},
		{
			name:    "mismatching digest",
			repo:    &fakeRepository{desc: desc},
			opts:    resolveOptions{digest: mismatching},
			wantErr: "digest mismatch",
		},
		{
			name: "digest and verifications are mutually exclusive",
			repo: &fakeRepository{desc: desc},
			opts: resolveOptions{
				digest:        matching,
				verifications: []verification.Verification{{Signature: "sig"}},
			},
			wantErr: "mutually exclusive",
		},
		{
			name:    "verifications require a signing registry",
			repo:    &fakeRepository{desc: desc},
			opts:    resolveOptions{verifications: []verification.Verification{{Signature: "sig"}}},
			wantErr: "signing registry is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := require.New(t)

			tt.opts.component = desc.Component.Name
			tt.opts.version = desc.Component.Version
			tt.opts.repository = tt.repo

			got, err := getComponentVersion(t.Context(), tt.opts)
			if tt.wantErr != "" {
				r.ErrorContains(err, tt.wantErr)
				r.Nil(got)

				return
			}

			r.NoError(err)
			r.Equal(desc, got)
		})
	}
}
