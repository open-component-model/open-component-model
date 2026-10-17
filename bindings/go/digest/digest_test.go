package digest_test

import (
	"crypto"
	"testing"

	"github.com/stretchr/testify/require"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	ocmdigest "ocm.software/open-component-model/bindings/go/digest"
)

// Every spelling a binding accepts today: the spec names, the OCI names that the OCI and
// signing bindings compare without case and dashes, and the case variants signing accepts.
func TestParseHashAlgorithm(t *testing.T) {
	for _, tc := range []struct {
		name    string
		want    crypto.Hash
		wantErr error
	}{
		{name: "SHA-256", want: crypto.SHA256},
		{name: "sha-256", want: crypto.SHA256},
		{name: "Sha-256", want: crypto.SHA256},
		{name: "sha256", want: crypto.SHA256},
		{name: "SHA256", want: crypto.SHA256},
		{name: "SHA-512", want: crypto.SHA512},
		{name: "sha-512", want: crypto.SHA512},
		{name: "sha512", want: crypto.SHA512},
		{name: "SHA512", want: crypto.SHA512},
		{name: "NO-DIGEST", wantErr: ocmdigest.ErrNoDigest},
		{name: "no-digest", wantErr: ocmdigest.ErrNoDigest},
		{name: "SHA-384", wantErr: ocmdigest.ErrUnsupportedHashAlgorithm},
		{name: "MD5", wantErr: ocmdigest.ErrUnsupportedHashAlgorithm},
		{name: "", wantErr: ocmdigest.ErrUnsupportedHashAlgorithm},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := require.New(t)
			got, err := ocmdigest.ParseHashAlgorithm(tc.name)
			if tc.wantErr != nil {
				r.ErrorIs(err, tc.wantErr)
				return
			}
			r.NoError(err)
			r.Equal(tc.want, got)
		})
	}
}

func TestFormatHashAlgorithm(t *testing.T) {
	r := require.New(t)
	name, err := ocmdigest.FormatHashAlgorithm(crypto.SHA256)
	r.NoError(err)
	r.Equal(ocmdigest.HashAlgorithmSHA256, name)
	name, err = ocmdigest.FormatHashAlgorithm(crypto.SHA512)
	r.NoError(err)
	r.Equal(ocmdigest.HashAlgorithmSHA512, name)
	_, err = ocmdigest.FormatHashAlgorithm(crypto.SHA384)
	r.ErrorIs(err, ocmdigest.ErrUnsupportedHashAlgorithm)
}

func TestIsExcluded(t *testing.T) {
	for _, tc := range []struct {
		name   string
		digest *descriptor.Digest
		want   bool
	}{
		{name: "marker", digest: descriptor.NewExcludeFromSignatureDigest(), want: true},
		{name: "hash algorithm only, lower case", digest: &descriptor.Digest{HashAlgorithm: "no-digest"}, want: true},
		{name: "normalisation only", digest: &descriptor.Digest{NormalisationAlgorithm: "exclude-from-signature"}, want: true},
		{name: "content digest", digest: &descriptor.Digest{HashAlgorithm: "SHA-256", NormalisationAlgorithm: ocmdigest.GenericBlobDigestV1, Value: "0123"}},
		{name: "nil"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.New(t).Equal(tc.want, ocmdigest.IsExcluded(tc.digest))
		})
	}
}

func TestCheck(t *testing.T) {
	const (
		value    = "4f1c0000"
		other    = "9a070000"
		artifact = "ociArtifactDigest/v1"
	)
	computed := ocmdigest.Computed{Hash: crypto.SHA256, Value: value}
	artifactRule := ocmdigest.Rule{Normalisation: artifact, Accepted: []string{ocmdigest.GenericBlobDigestV1}}
	blobRule := ocmdigest.Rule{Normalisation: ocmdigest.GenericBlobDigestV1}

	for _, tc := range []struct {
		name     string
		recorded *descriptor.Digest
		computed *ocmdigest.Computed
		rule     ocmdigest.Rule
		mode     ocmdigest.Mode
		want     *descriptor.Digest
		decision ocmdigest.Decision
		wantErr  string
	}{
		{
			name:     "excluded marker is kept and nothing is computed",
			recorded: descriptor.NewExcludeFromSignatureDigest(),
			computed: &ocmdigest.Computed{},
			rule:     artifactRule,
			want:     descriptor.NewExcludeFromSignatureDigest(),
			decision: ocmdigest.Excluded,
		},
		{
			name:     "excluded by normalisation only",
			recorded: &descriptor.Digest{NormalisationAlgorithm: "Exclude-From-Signature"},
			rule:     blobRule,
			want:     &descriptor.Digest{NormalisationAlgorithm: "Exclude-From-Signature"},
			decision: ocmdigest.Excluded,
		},
		{
			name:     "nothing recorded is generated",
			rule:     artifactRule,
			want:     &descriptor.Digest{HashAlgorithm: "SHA-256", NormalisationAlgorithm: artifact, Value: value},
			decision: ocmdigest.Generate,
		},
		{
			name:     "empty digest is generated in SHA-512",
			recorded: &descriptor.Digest{},
			computed: &ocmdigest.Computed{Hash: crypto.SHA512, Value: value},
			rule:     blobRule,
			want:     &descriptor.Digest{HashAlgorithm: "SHA-512", NormalisationAlgorithm: ocmdigest.GenericBlobDigestV1, Value: value},
			decision: ocmdigest.Generate,
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
			mode:     ocmdigest.Lenient,
			wantErr:  ocmdigest.ErrIncomplete.Error(),
		},
		{
			name:     "OCI hash spelling without a label is rejected",
			recorded: &descriptor.Digest{HashAlgorithm: "sha256", Value: value},
			rule:     artifactRule,
			wantErr:  ocmdigest.ErrIncomplete.Error(),
		},
		{
			name:     "accepted label without a hash is rejected, not relabelled",
			recorded: &descriptor.Digest{NormalisationAlgorithm: ocmdigest.GenericBlobDigestV1, Value: value},
			rule:     artifactRule,
			wantErr:  ocmdigest.ErrIncomplete.Error(),
		},
		{
			name:     "missing value is rejected",
			recorded: &descriptor.Digest{HashAlgorithm: "SHA-256", NormalisationAlgorithm: artifact},
			rule:     artifactRule,
			wantErr:  ocmdigest.ErrIncomplete.Error(),
		},
		{
			name:     "complete digest is kept",
			recorded: &descriptor.Digest{HashAlgorithm: "SHA-256", NormalisationAlgorithm: artifact, Value: value},
			rule:     artifactRule,
			want:     &descriptor.Digest{HashAlgorithm: "SHA-256", NormalisationAlgorithm: artifact, Value: value},
			decision: ocmdigest.Keep,
		},
		{
			name:     "complete digest keeps an accepted label and its spelling",
			recorded: &descriptor.Digest{HashAlgorithm: "sha256", NormalisationAlgorithm: ocmdigest.GenericBlobDigestV1, Value: value},
			rule:     artifactRule,
			want:     &descriptor.Digest{HashAlgorithm: "sha256", NormalisationAlgorithm: ocmdigest.GenericBlobDigestV1, Value: value},
			decision: ocmdigest.Keep,
		},
		{
			name:     "complete digest with a mismatching value is rejected",
			recorded: &descriptor.Digest{HashAlgorithm: "SHA-256", NormalisationAlgorithm: ocmdigest.GenericBlobDigestV1, Value: other},
			rule:     blobRule,
			mode:     ocmdigest.Lenient,
			wantErr:  "digest value mismatch",
		},
		{
			name:     "complete digest with a mismatching hash is rejected",
			recorded: &descriptor.Digest{HashAlgorithm: "SHA-512", NormalisationAlgorithm: artifact, Value: value},
			rule:     artifactRule,
			wantErr:  "hash algorithm mismatch",
		},
		{
			name:     "complete digest with a foreign label is rejected in strict mode",
			recorded: &descriptor.Digest{HashAlgorithm: "SHA-256", NormalisationAlgorithm: "jsonNormalisation/v1", Value: other},
			rule:     artifactRule,
			mode:     ocmdigest.Strict,
			wantErr:  "normalisation algorithm mismatch",
		},
		{
			name:     "complete digest with a foreign label is kept unverified in lenient mode",
			recorded: &descriptor.Digest{HashAlgorithm: "SHA-256", NormalisationAlgorithm: artifact, Value: other},
			rule:     blobRule,
			mode:     ocmdigest.Lenient,
			want:     &descriptor.Digest{HashAlgorithm: "SHA-256", NormalisationAlgorithm: artifact, Value: other},
			decision: ocmdigest.KeepUnverified,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := require.New(t)
			c := computed
			if tc.computed != nil {
				c = *tc.computed
			}
			before := tc.recorded.DeepCopy()

			got, err := ocmdigest.Check(tc.recorded, c, tc.rule, tc.mode)
			r.Equal(before, tc.recorded, "Check must not write the recorded digest")
			if tc.wantErr != "" {
				r.ErrorContains(err, tc.wantErr)
				return
			}
			r.NoError(err)
			r.Equal(tc.decision, got.Decision)
			r.Equal(tc.want, got.Digest)
			if tc.recorded != nil {
				r.NotSame(tc.recorded, got.Digest)
			}
		})
	}
}
