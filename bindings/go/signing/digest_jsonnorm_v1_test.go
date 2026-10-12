package signing

import (
	"encoding/json"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"

	v1 "ocm.software/open-component-model/bindings/go/descriptor/normalisation/json/v1"
	descruntime "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
)

// Combined scenario-08 descriptor from the cc-utils known-answer anchor
// (.scratch/jsonnorm-v1-anchor/fixtures/08-full-signed). digest08 is the
// cc-utils (ocm/sign.py) oracle digest over its jsonNormalisation/v1 bytes.
const (
	input08  = `{"meta":{"schemaVersion":"v2"},"component":{"name":"acme.org/full","version":"1.0.0","repositoryContexts":[],"provider":"ACME","sources":[],"componentReferences":[{"name":"child","componentName":"acme.org/child","version":"1.0.0","digest":{"hashAlgorithm":"SHA-256","normalisationAlgorithm":"jsonNormalisation/v1","value":"4518a3606aa1d257ff08d1760b84490ce2c741c1278217936e7abfe5135d2976"},"extraIdentity":{},"labels":[]}],"resources":[{"name":"oci","version":"1.0.0","type":"ociImage","access":{"type":"ociRegistry","imageReference":"registry.example/oci@sha256:aa"},"digest":{"hashAlgorithm":"SHA-256","normalisationAlgorithm":"ociArtifactDigest/v1","value":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},"extraIdentity":{},"relation":"external","labels":[{"name":"sig","value":"v","version":null,"signing":true},{"name":"nosig","value":"x","version":null,"signing":false}],"srcRefs":[]},{"name":"chart","version":"1.0.0","type":"helmChart","access":{"type":"localBlob/v1","localReference":"sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb","size":null,"mediaType":"application/vnd.oci.image.manifest.v1+tar+gzip","referenceName":null,"globalAccess":null},"digest":{"hashAlgorithm":"SHA-256","normalisationAlgorithm":"genericBlobDigest/v1","value":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"},"extraIdentity":{},"relation":"local","labels":[],"srcRefs":[]},{"name":"blob","version":"1.0.0","type":"blob","access":{"type":"s3","bucket":"b","key":"k","mediaType":null,"region":null},"digest":{"hashAlgorithm":"NO-DIGEST","normalisationAlgorithm":"EXCLUDE-FROM-SIGNATURE","value":"NO-DIGEST"},"extraIdentity":{},"relation":"external","labels":[],"srcRefs":[]},{"name":"same","version":"1.0.0","type":"ociImage","access":{"type":"ociRegistry","imageReference":"registry.example/a@sha256:d1"},"digest":{"hashAlgorithm":"SHA-256","normalisationAlgorithm":"ociArtifactDigest/v1","value":"dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"},"extraIdentity":{},"relation":"external","labels":[],"srcRefs":[]},{"name":"same","version":"1.0.0","type":"ociImage","access":{"type":"ociRegistry","imageReference":"registry.example/a@sha256:d2"},"digest":{"hashAlgorithm":"SHA-256","normalisationAlgorithm":"ociArtifactDigest/v1","value":"eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"},"extraIdentity":{},"relation":"external","labels":[],"srcRefs":[]}],"labels":[],"creationTime":"2026-06-19T07:16:30.600000+00:00"},"signatures":[],"nestedDigests":[]}`
	digest08 = "09fefed1884db76c829ba5f78ad315f26024c241121ebefd6be5aa079869a74f"
)

func descriptorFromJSON(t *testing.T, in string) *descruntime.Descriptor {
	t.Helper()
	r := require.New(t)
	var desc v2.Descriptor
	r.NoError(json.Unmarshal([]byte(in), &desc))
	rt, err := descruntime.ConvertFromV2(&desc)
	r.NoError(err)
	return rt
}

// TestVerifyDigestMatchesDescriptor_JSONNormalisationV1 exercises the real
// verify entrypoint over jsonNormalisation/v1. It does not check a cryptographic
// signature (see the plan's non-goals).
func TestVerifyDigestMatchesDescriptor_JSONNormalisationV1(t *testing.T) {
	r := require.New(t)
	desc := descriptorFromJSON(t, input08)
	sig := descruntime.Signature{
		Name: "acme",
		Digest: descruntime.Digest{
			HashAlgorithm:          "SHA-256",
			NormalisationAlgorithm: v1.Algorithm,
			Value:                  digest08,
		},
	}
	r.NoError(VerifyDigestMatchesDescriptor(t.Context(), desc, sig, slog.Default()))
}

// TestGenerateDigest_JSONNormalisationV1 confirms creation reproduces the oracle
// digest via the algorithm-agnostic signing registry.
func TestGenerateDigest_JSONNormalisationV1(t *testing.T) {
	r := require.New(t)
	desc := descriptorFromJSON(t, input08)
	d, err := GenerateDigest(t.Context(), desc, slog.Default(), v1.Algorithm, "SHA-256")
	r.NoError(err)
	r.Equal(digest08, d.Value)
	r.Equal(v1.Algorithm, d.NormalisationAlgorithm)
}

// TestSentinelAccepted_JSONNormalisationV1 confirms a descriptor carrying an
// EXCLUDE-FROM-SIGNATURE resource is not rejected by the digest
// completeness/algorithm checks.
func TestSentinelAccepted_JSONNormalisationV1(t *testing.T) {
	r := require.New(t)
	desc := descriptorFromJSON(t, input08)
	r.NoError(IsSafelyDigestible(&desc.Component))
	r.NoError(ValidateDigestHashAlgorithms(&desc.Component))
}
