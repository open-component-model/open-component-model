package digest_test

import (
	"crypto"
	"testing"

	"github.com/stretchr/testify/require"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	ocmdigest "ocm.software/open-component-model/bindings/go/digest"
)

func TestIsExcluded(t *testing.T) {
	for _, tc := range []struct {
		name   string
		digest *descriptor.Digest
		want   bool
	}{
		{name: "marker", digest: descriptor.NewExcludeFromSignatureDigest(), want: true},
		{name: "hash algorithm only, lower case", digest: &descriptor.Digest{HashAlgorithm: "no-digest"}, want: true},
		{name: "normalisation only", digest: &descriptor.Digest{NormalisationAlgorithm: "exclude-from-signature"}, want: true},
		{name: "content digest", digest: &descriptor.Digest{HashAlgorithm: "SHA-256", NormalisationAlgorithm: descriptor.GenericBlobDigestV1, Value: "0123"}},
		{name: "nil"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.New(t).Equal(tc.want, ocmdigest.IsExcluded(tc.digest))
		})
	}
}

func TestCheck(t *testing.T) {
	const (
		value = "4f1c0000"
		other = "9a070000"
		blob  = descriptor.GenericBlobDigestV1
	)
	computed := ocmdigest.Computed{Hash: crypto.SHA256, Value: value}
	artifactRule := ocmdigest.Rule{Normalisation: descriptor.OCIArtifactDigestV1, Accepted: []string{blob}}
	blobRule := ocmdigest.Rule{Normalisation: blob}

	for _, tc := range []struct {
		name      string
		recorded  *descriptor.Digest
		computed  *ocmdigest.Computed
		rule      ocmdigest.Rule
		want      *descriptor.Digest
		generated bool
		wantErr   string
	}{
		{
			name:     "excluded marker is kept and nothing is computed",
			recorded: descriptor.NewExcludeFromSignatureDigest(),
			computed: &ocmdigest.Computed{},
			rule:     artifactRule,
			want:     descriptor.NewExcludeFromSignatureDigest(),
		},
		{
			name:     "excluded by normalisation only",
			recorded: &descriptor.Digest{NormalisationAlgorithm: "Exclude-From-Signature"},
			rule:     blobRule,
			want:     &descriptor.Digest{NormalisationAlgorithm: "Exclude-From-Signature"},
		},
		{
			name:      "nothing recorded is generated",
			rule:      artifactRule,
			want:      &descriptor.Digest{HashAlgorithm: "SHA-256", NormalisationAlgorithm: descriptor.OCIArtifactDigestV1, Value: value},
			generated: true,
		},
		{
			name:      "empty digest is generated in SHA-512",
			recorded:  &descriptor.Digest{},
			computed:  &ocmdigest.Computed{Hash: crypto.SHA512, Value: value},
			rule:      blobRule,
			want:      &descriptor.Digest{HashAlgorithm: "SHA-512", NormalisationAlgorithm: blob, Value: value},
			generated: true,
		},
		{
			name:     "unsupported computed hash is rejected",
			computed: &ocmdigest.Computed{Hash: crypto.SHA384, Value: value},
			rule:     blobRule,
			wantErr:  "unsupported hash algorithm",
		},
		{
			name:     "value only is rejected",
			recorded: &descriptor.Digest{Value: value},
			rule:     blobRule,
			wantErr:  "a digest must be complete or absent",
		},
		{
			name:     "accepted label without a hash is rejected, not relabelled",
			recorded: &descriptor.Digest{NormalisationAlgorithm: blob, Value: value},
			rule:     artifactRule,
			wantErr:  "a digest must be complete or absent",
		},
		{
			name:     "missing value is rejected",
			recorded: &descriptor.Digest{HashAlgorithm: "SHA-256", NormalisationAlgorithm: descriptor.OCIArtifactDigestV1},
			rule:     artifactRule,
			wantErr:  "a digest must be complete or absent",
		},
		{
			// A digest without a normalisation algorithm describes the bytes of a blob, so the
			// blob rule verifies it and keeps it unlabelled: the label is signed data too.
			name:     "unlabelled digest of a blob is kept as recorded",
			recorded: &descriptor.Digest{HashAlgorithm: "sha256", Value: value},
			rule:     blobRule,
			want:     &descriptor.Digest{HashAlgorithm: "sha256", Value: value},
		},
		{
			name:     "unlabelled digest of a blob with a mismatching value is rejected",
			recorded: &descriptor.Digest{HashAlgorithm: "SHA-256", Value: other},
			rule:     blobRule,
			wantErr:  "digest value mismatch",
		},
		{
			// The binding resolved an OCI artifact, so a digest claiming plain bytes cannot be
			// verified against what it computed.
			name:     "unlabelled digest of an OCI artifact is rejected",
			recorded: &descriptor.Digest{HashAlgorithm: "sha256", Value: value},
			rule:     artifactRule,
			wantErr:  "normalisation algorithm mismatch",
		},
		{
			name:     "complete digest is kept",
			recorded: &descriptor.Digest{HashAlgorithm: "SHA-256", NormalisationAlgorithm: descriptor.OCIArtifactDigestV1, Value: value},
			rule:     artifactRule,
			want:     &descriptor.Digest{HashAlgorithm: "SHA-256", NormalisationAlgorithm: descriptor.OCIArtifactDigestV1, Value: value},
		},
		{
			name:     "complete digest keeps an accepted label and its spelling",
			recorded: &descriptor.Digest{HashAlgorithm: "sha256", NormalisationAlgorithm: blob, Value: value},
			rule:     artifactRule,
			want:     &descriptor.Digest{HashAlgorithm: "sha256", NormalisationAlgorithm: blob, Value: value},
		},
		{
			name:     "complete digest with a mismatching value is rejected",
			recorded: &descriptor.Digest{HashAlgorithm: "SHA-256", NormalisationAlgorithm: blob, Value: other},
			rule:     blobRule,
			wantErr:  "digest value mismatch",
		},
		{
			name:     "complete digest with a mismatching hash is rejected",
			recorded: &descriptor.Digest{HashAlgorithm: "SHA-512", NormalisationAlgorithm: descriptor.OCIArtifactDigestV1, Value: value},
			rule:     artifactRule,
			wantErr:  "hash algorithm mismatch",
		},
		{
			name:     "complete digest with an unknown label is rejected",
			recorded: &descriptor.Digest{HashAlgorithm: "SHA-256", NormalisationAlgorithm: "jsonNormalisation/v1", Value: other},
			rule:     artifactRule,
			wantErr:  "normalisation algorithm mismatch",
		},
		{
			name:     "complete digest with the label of another rule is rejected",
			recorded: &descriptor.Digest{HashAlgorithm: "SHA-256", NormalisationAlgorithm: descriptor.OCIArtifactDigestV1, Value: other},
			rule:     blobRule,
			wantErr:  "normalisation algorithm mismatch",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := require.New(t)
			c := computed
			if tc.computed != nil {
				c = *tc.computed
			}
			before := tc.recorded.DeepCopy()

			got, err := ocmdigest.Check(tc.recorded, c, tc.rule)
			r.Equal(before, tc.recorded, "Check must not write the recorded digest")
			if tc.wantErr != "" {
				r.ErrorContains(err, tc.wantErr)
				return
			}
			r.NoError(err)
			r.Equal(tc.generated, got.Generated)
			r.Equal(tc.want, got.Digest)
			if tc.recorded != nil {
				r.NotSame(tc.recorded, got.Digest)
			}
		})
	}
}
