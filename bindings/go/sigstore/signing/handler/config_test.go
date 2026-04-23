package handler

import (
	"testing"

	"github.com/stretchr/testify/require"

	descruntime "ocm.software/open-component-model/bindings/go/descriptor/runtime"

	"ocm.software/open-component-model/bindings/go/sigstore/signing/v1alpha1"
)

// --- Identity tests ---
// Intentionally separate: signing and verifying identity tests use different
// input types (digest+config vs. Signature) and call different handler methods,
// so a shared table would add complexity without improving clarity.

func TestGetSigningCredentialConsumerIdentity(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	h := NewWithExecutor(&mockExecutor{})
	cfg := testSignConfig()

	id, err := h.GetSigningCredentialConsumerIdentity(t.Context(), "my-sig", testDigest(), cfg)
	r.NoError(err)
	r.Equal(v1alpha1.AlgorithmSigstore, id[IdentityAttributeAlgorithm])
	r.Equal("my-sig", id[IdentityAttributeSignature])
	r.Equal(v1alpha1.IdentityTypeOIDCIdentityToken, id.GetType())
	r.Len(id, 3) // algorithm + signature + type
}

func TestGetVerifyingCredentialConsumerIdentity(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	h := NewWithExecutor(&mockExecutor{})
	signed := descruntime.Signature{
		Name: "my-sig",
		Signature: descruntime.SignatureInfo{
			MediaType: v1alpha1.MediaTypeSigstoreBundle,
		},
	}

	id, err := h.GetVerifyingCredentialConsumerIdentity(t.Context(), signed, nil)
	r.NoError(err)
	r.Equal(v1alpha1.AlgorithmSigstore, id[IdentityAttributeAlgorithm])
	r.Equal("my-sig", id[IdentityAttributeSignature])
	r.Equal(v1alpha1.IdentityTypeTrustedRoot, id.GetType())
	r.Len(id, 3) // algorithm + signature + type
}

func TestGetVerifyingCredentialConsumerIdentity_WrongMediaType(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	h := NewWithExecutor(&mockExecutor{})
	signed := descruntime.Signature{
		Name: "my-sig",
		Signature: descruntime.SignatureInfo{
			MediaType: "application/pgp-signature",
		},
	}

	_, err := h.GetVerifyingCredentialConsumerIdentity(t.Context(), signed, nil)
	r.Error(err)
	r.Contains(err.Error(), "unsupported media type")
}

// --- extractIssuerFromBundleJSON tests ---

func TestExtractIssuerFromBundleJSON(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		input     func(t *testing.T) []byte
		expected  string
		expectErr bool
	}{
		{
			name:      "empty bundle",
			input:     func(_ *testing.T) []byte { return []byte(`{}`) },
			expectErr: true,
		},
		{
			name: "no cert in bundle",
			input: func(_ *testing.T) []byte {
				return []byte(`{
					"mediaType": "application/vnd.dev.sigstore.bundle.v0.3+json",
					"verificationMaterial": {"certificate": {"rawBytes": ""}},
					"messageSignature": {}
				}`)
			},
			expectErr: true,
		},
		{
			name:     "valid v1 issuer OID",
			input:    func(t *testing.T) []byte { return fakeBundleJSONWithCert(t, "https://issuer.example.com") },
			expected: "https://issuer.example.com",
		},
		{
			name:      "invalid JSON",
			input:     func(_ *testing.T) []byte { return []byte("not json") },
			expectErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			r := require.New(t)
			got, err := extractIssuerFromBundleJSON(tc.input(t))
			if tc.expectErr {
				r.Error(err)
				return
			}
			r.NoError(err)
			r.Equal(tc.expected, got)
		})
	}
}
