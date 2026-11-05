package pack

import (
	"bytes"
	"log/slog"
	"testing"

	"github.com/opencontainers/go-digest"
	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/require"
	"oras.land/oras-go/v2/content"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/runtime"
)

func TestVerifyLayoutRootDigest_WarnsAboutExcludedDigestWithValue(t *testing.T) {
	var logged bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logged, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(previous) })

	root := digest.FromString("root")
	value := digest.FromString("other").Encoded()
	for _, tc := range []struct {
		name     string
		digest   *descriptor.Digest
		wantWarn bool
	}{
		{name: "marker alone", digest: descriptor.NewExcludeFromSignatureDigest()},
		{name: "excluded with a value", digest: &descriptor.Digest{HashAlgorithm: "SHA-256", NormalisationAlgorithm: descriptor.ExcludeFromSignature, Value: value}, wantWarn: true},
		{name: "excluded in lower case with a value", digest: &descriptor.Digest{HashAlgorithm: "SHA-256", NormalisationAlgorithm: "exclude-from-signature", Value: value}, wantWarn: true},
		{name: "no digest with a value", digest: &descriptor.Digest{HashAlgorithm: descriptor.NoDigest, NormalisationAlgorithm: "ociArtifactDigest/v1", Value: value}, wantWarn: true},
		{name: "no digest with an unknown normalisation and a value", digest: &descriptor.Digest{HashAlgorithm: descriptor.NoDigest, NormalisationAlgorithm: "other/v1", Value: value}, wantWarn: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := require.New(t)
			logged.Reset()
			resource := &descriptor.Resource{
				ElementMeta: descriptor.ElementMeta{ObjectMeta: descriptor.ObjectMeta{Name: "artifact"}},
				Digest:      tc.digest,
			}

			r.NoError(verifyLayoutRootDigest(t.Context(), resource, root))

			if tc.wantWarn {
				r.Contains(logged.String(), "level=WARN")
				r.Contains(logged.String(), "artifact", "the warning must name the resource")
			} else {
				r.NotContains(logged.String(), "level=WARN")
			}
		})
	}
}

func TestVerifyLayoutRootDigest_PreservesOtherNormalisations(t *testing.T) {
	root := digest.FromString("root")
	value := digest.FromString("other").Encoded()
	for _, tc := range []struct {
		name      string
		digest    descriptor.Digest
		wantError string
	}{
		{name: "unknown normalisation", digest: descriptor.Digest{HashAlgorithm: "SHA-256", NormalisationAlgorithm: "other/v1", Value: value}},
		{name: "unknown normalisation, value not hex", digest: descriptor.Digest{HashAlgorithm: "SHA-256", NormalisationAlgorithm: "other/v1", Value: "not-hex"}},
		{name: "unknown normalisation, unsupported hash", digest: descriptor.Digest{HashAlgorithm: "MD5", NormalisationAlgorithm: "other/v1", Value: "x"}},
		{name: "unknown normalisation, value of another hash", digest: descriptor.Digest{HashAlgorithm: "SHA-512", NormalisationAlgorithm: "other/v1", Value: value}},
		{name: "known normalisation, value not hex", digest: descriptor.Digest{HashAlgorithm: "SHA-256", NormalisationAlgorithm: "ociArtifactDigest/v1", Value: "not-hex"}, wantError: "invalid digest"},
		{name: "known normalisation, unsupported hash", digest: descriptor.Digest{HashAlgorithm: "MD5", NormalisationAlgorithm: "ociArtifactDigest/v1", Value: "x"}, wantError: "unsupported hash algorithm"},
		{name: "known normalisation, other root", digest: descriptor.Digest{HashAlgorithm: "SHA-256", NormalisationAlgorithm: "ociArtifactDigest/v1", Value: value}, wantError: "OCI artifact digest mismatch"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := require.New(t)
			err := verifyLayoutRootDigest(t.Context(), &descriptor.Resource{Digest: tc.digest.DeepCopy()}, root)
			if tc.wantError != "" {
				r.ErrorContains(err, tc.wantError)
				return
			}
			r.NoError(err)
		})
	}
}

func TestUpdateArtifactAccess_CompletesPartialDigest(t *testing.T) {
	root := content.NewDescriptorFromBytes(ociImageSpecV1.MediaTypeImageManifest, []byte("test content"))
	for _, normalization := range []string{"genericBlobDigest/v1", "ociArtifactDigest/v1"} {
		for _, tc := range []struct {
			name      string
			digest    descriptor.Digest
			wantError string
		}{
			{name: "missing normalization", digest: descriptor.Digest{HashAlgorithm: "SHA-256", Value: root.Digest.Encoded()}},
			{name: "missing hash", digest: descriptor.Digest{NormalisationAlgorithm: normalization, Value: root.Digest.Encoded()}},
			{name: "missing value", digest: descriptor.Digest{HashAlgorithm: "SHA-256", NormalisationAlgorithm: normalization}},
			{name: "normalisation only", digest: descriptor.Digest{NormalisationAlgorithm: normalization}},
			{name: "mismatching value", digest: descriptor.Digest{HashAlgorithm: "SHA-256", Value: "previous"}, wantError: "digest value mismatch"},
			{name: "mismatching hash", digest: descriptor.Digest{HashAlgorithm: "SHA-512"}, wantError: "hash algorithm mismatch"},
			{name: "unknown normalisation", digest: descriptor.Digest{NormalisationAlgorithm: "other/v1"}, wantError: "normalisation algorithm mismatch"},
		} {
			t.Run(normalization+"/"+tc.name, func(t *testing.T) {
				r := require.New(t)
				scheme := runtime.NewScheme()
				v2.MustAddToScheme(scheme)
				resource := &descriptor.Resource{Digest: tc.digest.DeepCopy()}

				err := updateArtifactAccess(resource, &v2.LocalBlob{}, root, updateAccessOptions{
					Options: Options{AccessScheme: scheme}, NormalisationAlgorithm: normalization,
				})
				if tc.wantError != "" {
					r.ErrorContains(err, tc.wantError)
					r.Equal(&tc.digest, resource.Digest, "a rejected digest must not be rewritten")
					return
				}
				r.NoError(err)
				r.Equal(&descriptor.Digest{
					HashAlgorithm:          "SHA-256",
					NormalisationAlgorithm: normalization,
					Value:                  root.Digest.Encoded(),
				}, resource.Digest)
			})
		}
	}
}

func TestUpdateArtifactAccess_PartialLegacyLabel(t *testing.T) {
	root := content.NewDescriptorFromBytes(ociImageSpecV1.MediaTypeImageManifest, []byte("test content"))
	for _, tc := range []struct {
		name          string
		set           string
		normalization string
		wantError     string
	}{
		{name: "generic label on OCI artifact is corrected", set: "genericBlobDigest/v1", normalization: "ociArtifactDigest/v1"},
		{name: "OCI label on blob is rejected", set: "ociArtifactDigest/v1", normalization: "genericBlobDigest/v1", wantError: "normalisation algorithm mismatch"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := require.New(t)
			scheme := runtime.NewScheme()
			v2.MustAddToScheme(scheme)
			resource := &descriptor.Resource{Digest: &descriptor.Digest{NormalisationAlgorithm: tc.set, Value: root.Digest.Encoded()}}

			err := updateArtifactAccess(resource, &v2.LocalBlob{}, root, updateAccessOptions{
				Options: Options{AccessScheme: scheme}, NormalisationAlgorithm: tc.normalization,
			})
			if tc.wantError != "" {
				r.ErrorContains(err, tc.wantError)
				return
			}
			r.NoError(err)
			r.Equal(&descriptor.Digest{
				HashAlgorithm:          "SHA-256",
				NormalisationAlgorithm: tc.normalization,
				Value:                  root.Digest.Encoded(),
			}, resource.Digest)
		})
	}
}
