package resolution

import (
	"context"
	"crypto"
	"crypto/fips140"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"log/slog"
	"testing"

	"github.com/opencontainers/go-digest"
	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/credentials"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/kubernetes/controller/internal/verification"
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/signinghandler"
	"ocm.software/open-component-model/bindings/go/repository"
	rsahandler "ocm.software/open-component-model/bindings/go/rsa/signing/handler"
	rsasigningv1alpha1 "ocm.software/open-component-model/bindings/go/rsa/signing/v1alpha1"
	rsacredentialsv1 "ocm.software/open-component-model/bindings/go/rsa/spec/credentials/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
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

// signedOptions builds resolveOptions for desc with a real RSA signing registry and a credential graph
// carrying the matching public key for each requested signature. It signs the descriptor's canonical
// digest with an ephemeral RSA key using RSASSA-PSS Plain encoding.
func signedOptions(t *testing.T, desc *descriptor.Descriptor, signatureNames ...string) resolveOptions {
	t.Helper()
	r := require.New(t)

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	r.NoError(err)
	pubDER, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	r.NoError(err)
	pubPEM := string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: pubDER}))
	privPEM := string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}))

	scheme := runtime.NewScheme()
	r.NoError(scheme.RegisterScheme(rsasigningv1alpha1.Scheme))
	handler, err := rsahandler.New(scheme, false)
	r.NoError(err)

	registry := signinghandler.NewSigningRegistry(t.Context())
	r.NoError(registry.RegisterInternalComponentSignatureHandler(handler))

	cfg := &rsasigningv1alpha1.Config{
		SignatureAlgorithm:      rsasigningv1alpha1.AlgorithmRSASSAPSS,
		SignatureEncodingPolicy: rsasigningv1alpha1.SignatureEncodingPolicyPlain,
	}
	signCreds := &rsacredentialsv1.RSACredentials{
		Type:          rsacredentialsv1.VersionedType,
		PublicKeyPEM:  pubPEM,
		PrivateKeyPEM: privPEM,
	}

	dig, err := signing.GenerateDigest(t.Context(), desc, slog.New(slog.DiscardHandler),
		signing.DefaultNormalisationAlgorithm, crypto.SHA256.String())
	r.NoError(err)

	credMap := map[string]runtime.Typed{}
	for _, name := range signatureNames {
		info, err := handler.Sign(t.Context(), *dig, cfg, signCreds)
		r.NoError(err)
		sig := descriptor.Signature{Name: name, Digest: *dig, Signature: info}
		desc.Signatures = append(desc.Signatures, sig)

		id, err := handler.GetVerifyingCredentialConsumerIdentity(t.Context(), sig, nil)
		r.NoError(err)
		credMap[id.String()] = &rsacredentialsv1.RSACredentials{
			Type:         rsacredentialsv1.VersionedType,
			PublicKeyPEM: pubPEM,
		}
	}

	return resolveOptions{
		component:       desc.Component.Name,
		version:         desc.Component.Version,
		repository:      &fakeRepository{desc: desc},
		credentialGraph: credentials.NewStaticTypedCredentialsResolver(credMap),
		signingRegistry: registry,
	}
}

// baseSignatureDescriptor returns a complete, signable descriptor. Variants remove or weaken the
// resource/reference digest before signing.
func baseSignatureDescriptor() *descriptor.Descriptor {
	return &descriptor.Descriptor{
		Meta: descriptor.Meta{Version: "v2"},
		Component: descriptor.Component{
			ComponentMeta: descriptor.ComponentMeta{
				ObjectMeta: descriptor.ObjectMeta{Name: "ocm.software/signature-verification", Version: "1.0.0"},
			},
			Provider: descriptor.Provider{Name: "ocm.software"},
			Resources: []descriptor.Resource{{
				ElementMeta: descriptor.ElementMeta{ObjectMeta: descriptor.ObjectMeta{Name: "data", Version: "1.0.0"}},
				Type:        "blob",
				Relation:    descriptor.ExternalRelation,
				Access: &runtime.Raw{
					Type: runtime.NewUnversionedType("wget/v1"),
					Data: []byte(`{"type":"wget/v1","url":"https://example.invalid/data"}`),
				},
				Digest: &descriptor.Digest{
					HashAlgorithm:          "SHA-256",
					NormalisationAlgorithm: descriptor.GenericBlobDigestV1,
					Value:                  digest.FromString("content").Encoded(),
				},
			}},
		},
	}
}

func requestVerifications(t *testing.T, names ...string) []verification.Verification {
	t.Helper()
	spec := &rsasigningv1alpha1.Config{}
	_, err := rsasigningv1alpha1.Scheme.DefaultType(spec)
	require.NoError(t, err)
	out := make([]verification.Verification, 0, len(names))
	for _, name := range names {
		out = append(out, verification.Verification{Signature: name, Verifier: spec})
	}
	return out
}

func TestGetComponentVersion_Signatures(t *testing.T) {
	t.Run("complete descriptor verifies and retains digests", func(t *testing.T) {
		r := require.New(t)
		desc := baseSignatureDescriptor()
		opts := signedOptions(t, desc, "required")
		opts.verifications = requestVerifications(t, "required")

		got, err := getComponentVersion(t.Context(), opts)
		r.NoError(err)
		r.NotNil(got)
		r.Equal("SHA-256", got.Component.Resources[0].Digest.HashAlgorithm)
		r.Equal(digest.FromString("content").Encoded(), got.Component.Resources[0].Digest.Value)
	})

	t.Run("missing resource digest still verifies the signature", func(t *testing.T) {
		r := require.New(t)
		desc := baseSignatureDescriptor()
		desc.Component.Resources[0].Digest = nil
		opts := signedOptions(t, desc, "required")
		opts.verifications = requestVerifications(t, "required")

		got, err := getComponentVersion(t.Context(), opts)
		r.NoError(err)
		r.NotNil(got)
		r.Nil(got.Component.Resources[0].Digest)
	})

	t.Run("missing resource digest does not bypass a missing signature", func(t *testing.T) {
		r := require.New(t)
		desc := baseSignatureDescriptor()
		desc.Component.Resources[0].Digest = nil
		opts := signedOptions(t, desc, "required")
		desc.Signatures = nil
		opts.verifications = requestVerifications(t, "required")

		got, err := getComponentVersion(t.Context(), opts)
		r.ErrorContains(err, "signature required not found")
		r.Nil(got)
	})

	t.Run("missing resource digest does not bypass an invalid signature", func(t *testing.T) {
		r := require.New(t)
		desc := baseSignatureDescriptor()
		desc.Component.Resources[0].Digest = nil
		opts := signedOptions(t, desc, "required")
		opts.verifications = requestVerifications(t, "required")

		// Confirm the fixture's descriptor digest still matches, so the failure is cryptographic.
		r.NoError(signing.VerifyDigestMatchesDescriptor(t.Context(), desc, desc.Signatures[0], slog.New(slog.DiscardHandler)))
		value := desc.Signatures[0].Signature.Value
		raw, err := hex.DecodeString(value)
		r.NoError(err)
		raw[0] ^= 0xFF
		desc.Signatures[0].Signature.Value = hex.EncodeToString(raw)

		got, err := getComponentVersion(t.Context(), opts)
		r.ErrorContains(err, "signature verification failed for signature required")
		r.Nil(got)
	})

	t.Run("missing reference digest still verifies the signature", func(t *testing.T) {
		r := require.New(t)
		desc := baseSignatureDescriptor()
		desc.Component.Resources = nil
		desc.Component.References = []descriptor.Reference{{
			ElementMeta: descriptor.ElementMeta{ObjectMeta: descriptor.ObjectMeta{Name: "child", Version: "1.0.0"}},
			Component:   "ocm.software/child",
		}}
		opts := signedOptions(t, desc, "required")
		opts.verifications = requestVerifications(t, "required")

		got, err := getComponentVersion(t.Context(), opts)
		r.NoError(err)
		r.NotNil(got)
		r.Empty(got.Component.References[0].Digest.Value)
	})

	t.Run("missing reference digest with no requested signature fails", func(t *testing.T) {
		r := require.New(t)
		desc := baseSignatureDescriptor()
		desc.Component.Resources = nil
		desc.Component.References = []descriptor.Reference{{
			ElementMeta: descriptor.ElementMeta{ObjectMeta: descriptor.ObjectMeta{Name: "child", Version: "1.0.0"}},
			Component:   "ocm.software/child",
		}}
		opts := signedOptions(t, desc, "required")
		desc.Signatures = nil
		opts.verifications = requestVerifications(t, "required")

		got, err := getComponentVersion(t.Context(), opts)
		r.ErrorContains(err, "signature required not found")
		r.Nil(got)
	})

	t.Run("verifying one signature does not satisfy a second requested one", func(t *testing.T) {
		r := require.New(t)
		desc := baseSignatureDescriptor()
		desc.Component.Resources[0].Digest = nil
		opts := signedOptions(t, desc, "required")
		opts.verifications = requestVerifications(t, "required", "secondary")

		got, err := getComponentVersion(t.Context(), opts)
		r.ErrorContains(err, "signature secondary not found")
		r.Nil(got)
	})

	t.Run("no requested signatures and no digest returns the descriptor", func(t *testing.T) {
		r := require.New(t)
		desc := baseSignatureDescriptor()
		desc.Component.Resources[0].Digest = nil
		opts := signedOptions(t, desc)

		got, err := getComponentVersion(t.Context(), opts)
		r.NoError(err)
		r.NotNil(got)
		r.Nil(got.Component.Resources[0].Digest)
	})

	t.Run("weak resource hash algorithm", func(t *testing.T) {
		r := require.New(t)
		desc := baseSignatureDescriptor()
		desc.Component.Resources[0].Digest = &descriptor.Digest{
			HashAlgorithm:          "MD5",
			NormalisationAlgorithm: descriptor.GenericBlobDigestV1,
			Value:                  "00000000000000000000000000000000",
		}
		opts := signedOptions(t, desc, "required")
		opts.verifications = requestVerifications(t, "required")

		got, err := getComponentVersion(t.Context(), opts)
		if fips140.Enforced() {
			r.ErrorContains(err, "refusing to verify component version")
			r.ErrorIs(err, signing.ErrUnsupportedDigestHash)
			r.Nil(got)
			return
		}
		r.NoError(err)
		r.NotNil(got)
	})
}
