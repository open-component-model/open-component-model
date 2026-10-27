package componentversion

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/asn1"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"log/slog"
	"math/big"
	"testing"
	"time"

	"github.com/digitorus/timestamp"
	"github.com/stretchr/testify/require"

	genericv1 "ocm.software/open-component-model/bindings/go/configuration/generic/v1/spec"
	"ocm.software/open-component-model/bindings/go/credentials"
	descruntime "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/bindings/go/signing/tsa"
)

type staticResolver struct {
	creds runtime.Typed
	err   error
}

func (s staticResolver) Resolve(context.Context, runtime.Identity) (runtime.Typed, error) {
	return s.creds, s.err
}

// recordingResolver records every resolved identity and finds no credentials.
type recordingResolver struct {
	identities *[]runtime.Identity
}

func (r recordingResolver) Resolve(_ context.Context, id runtime.Identity) (runtime.Typed, error) {
	*r.identities = append(*r.identities, id)
	return nil, credentials.ErrNotFound
}

// mustTimestampToken returns a DER timestamp token over imprint, signed by a
// self-signed certificate carrying the critical id-kp-timeStamping EKU.
func mustTimestampToken(t *testing.T, imprint []byte) []byte {
	t.Helper()
	r := require.New(t)
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	r.NoError(err)
	ekuVal, err := asn1.Marshal([]asn1.ObjectIdentifier{{1, 3, 6, 1, 5, 5, 7, 3, 8}})
	r.NoError(err)
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "test TSA"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
		IsCA:                  true,
		ExtraExtensions:       []pkix.Extension{{Id: asn1.ObjectIdentifier{2, 5, 29, 37}, Critical: true, Value: ekuVal}},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	r.NoError(err)
	cert, err := x509.ParseCertificate(der)
	r.NoError(err)

	resp, err := (&timestamp.Timestamp{
		HashAlgorithm:     crypto.SHA256,
		HashedMessage:     imprint,
		Time:              time.Now().UTC().Truncate(time.Second),
		Policy:            asn1.ObjectIdentifier{1, 3, 6, 1, 4, 1, 0},
		AddTSACertificate: true,
	}).CreateResponseWithOpts(cert, key, crypto.SHA256)
	r.NoError(err)
	ts, err := timestamp.ParseResponse(resp)
	r.NoError(err)
	return ts.RawToken
}

// timestampedSignature returns a signature whose timestamp token covers the
// signature value, as produced by sign component-version --tsa.
func timestampedSignature(t *testing.T) descruntime.Signature {
	t.Helper()
	digest := sha256.Sum256([]byte("normalised descriptor"))
	value := "deadbeef"
	imprint := sha256.Sum256([]byte(value))
	return descruntime.Signature{
		Name:      "default",
		Digest:    descruntime.Digest{HashAlgorithm: "SHA-256", NormalisationAlgorithm: "jsonNormalisation/v4alpha1", Value: hex.EncodeToString(digest[:])},
		Signature: descruntime.SignatureInfo{Algorithm: "RSASSA-PSS", MediaType: "application/vnd.ocm.signature.rsa.pss", Value: value},
		Timestamp: &descruntime.TimestampSpec{Value: string(tsa.ToPEM(mustTimestampToken(t, imprint[:])))},
	}
}

func TestVerifyTSATimestamp(t *testing.T) {
	notFound := staticResolver{err: credentials.ErrNotFound}
	failure := errors.New("credential plugin crashed")
	descriptorDigest := func(t *testing.T, sig *descruntime.Signature) []byte {
		t.Helper()
		digest, err := hex.DecodeString(sig.Digest.Value)
		require.NoError(t, err)
		return digest
	}

	tests := []struct {
		name      string
		resolver  credentials.Resolver
		mutate    func(t *testing.T, sig *descruntime.Signature)
		wantErr   string
		wantErrIs error
	}{
		{name: "no TSA credentials configured", resolver: notFound},
		{name: "resolution failure is surfaced", resolver: staticResolver{err: failure}, wantErrIs: failure},
		{
			// The legacy OCM CLI stores a bare CMS SignedData over the descriptor
			// digest under PEM block type "TIMESTAMP INFO".
			name:     "legacy OCM timestamp over the descriptor digest",
			resolver: notFound,
			mutate: func(t *testing.T, sig *descruntime.Signature) {
				t.Helper()
				var contentInfo struct {
					ContentType asn1.ObjectIdentifier
					Content     asn1.RawValue `asn1:"explicit,tag:0"`
				}
				_, err := asn1.Unmarshal(mustTimestampToken(t, descriptorDigest(t, sig)), &contentInfo)
				require.NoError(t, err)
				sig.Timestamp.Value = string(pem.EncodeToMemory(&pem.Block{Type: "TIMESTAMP INFO", Bytes: contentInfo.Content.Bytes}))
			},
		},
		{
			name:     "current token not covering the signature value",
			resolver: notFound,
			mutate: func(t *testing.T, sig *descruntime.Signature) {
				t.Helper()
				sig.Timestamp.Value = string(tsa.ToPEM(mustTimestampToken(t, descriptorDigest(t, sig))))
			},
			wantErr: "message imprint does not match",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := require.New(t)
			sig := timestampedSignature(t)
			if tc.mutate != nil {
				tc.mutate(t, &sig)
			}
			_, trusted, err := verifyTSATimestamp(t.Context(), slog.Default(), tc.resolver, &descruntime.Descriptor{}, sig, false)
			switch {
			case tc.wantErrIs != nil:
				r.ErrorIs(err, tc.wantErrIs)
			case tc.wantErr != "":
				r.ErrorContains(err, tc.wantErr)
			default:
				r.NoError(err)
				r.False(trusted)
			}
		})
	}
}

func TestVerifyTSATimestamp_ResolvesRootsByTSAURLLabel(t *testing.T) {
	tests := []struct {
		name          string
		labelValue    string
		wantHostnames []string
	}{
		{name: "URL-specific identity first, then generic", labelValue: `"https://tsa.example/ts"`, wantHostnames: []string{"tsa.example", ""}},
		{name: "generic identity without label", wantHostnames: []string{""}},
		{name: "unparsable URL falls back to generic", labelValue: `"://invalid"`, wantHostnames: []string{""}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := require.New(t)
			sig := timestampedSignature(t)
			desc := &descruntime.Descriptor{}
			if tc.labelValue != "" {
				desc.Component.Labels = []descruntime.Label{{Name: tsa.TSAURLLabelPrefix + sig.Name, Value: []byte(tc.labelValue)}}
			}

			var identities []runtime.Identity
			_, trusted, err := verifyTSATimestamp(t.Context(), slog.Default(), recordingResolver{identities: &identities}, desc, sig, false)
			r.NoError(err)
			r.False(trusted)
			hostnames := make([]string, 0, len(identities))
			for _, id := range identities {
				hostnames = append(hostnames, id["hostname"])
			}
			r.Equal(tc.wantHostnames, hostnames)
		})
	}
}

func TestLoadVerifierConfig(t *testing.T) {
	sigstoreVerifierConfig := `{"type":"generic.config.ocm.software/v1","configurations":[{
		"type":"signing.config.ocm.software/v1alpha1",
		"verifier":{"type":"SigstoreVerificationConfiguration/v1alpha1","certificateOIDCIssuer":"https://issuer.example","certificateIdentity":"signer@example.com"}}]}`
	signature := func(algorithm, mediaType string) descruntime.Signature {
		return descruntime.Signature{Name: "default", Signature: descruntime.SignatureInfo{Algorithm: algorithm, MediaType: mediaType}}
	}

	tests := []struct {
		name      string
		config    string
		signature descruntime.Signature
		wantType  string
	}{
		{name: "RSASSA-PSS without config uses RSA verifier", signature: signature("RSASSA-PSS", "application/vnd.ocm.signature.rsa.pss"), wantType: "RSASigningConfiguration"},
		{name: "RSASSA-PKCS1-V1_5 without config uses RSA verifier", signature: signature("RSASSA-PKCS1-V1_5", "application/vnd.ocm.signature.rsa"), wantType: "RSASigningConfiguration"},
		{name: "Sigstore without config uses Sigstore verifier", signature: signature("Sigstore/v1alpha1", "application/vnd.dev.sigstore.bundle.v0.3+json"), wantType: "SigstoreVerificationConfiguration/v1alpha1"},
		{name: "legacy sigstore without config uses Sigstore verifier", signature: signature("sigstore", "application/vnd.dev.sigstore.bundle.v0.3+json"), wantType: "SigstoreVerificationConfiguration/v1alpha1"},
		{name: "configured verifier wins over the signature algorithm", config: sigstoreVerifierConfig, signature: signature("RSASSA-PSS", "application/vnd.ocm.signature.rsa.pss"), wantType: "SigstoreVerificationConfiguration/v1alpha1"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := require.New(t)
			var cfg *genericv1.Config
			if tc.config != "" {
				cfg = &genericv1.Config{}
				r.NoError(json.Unmarshal([]byte(tc.config), cfg))
			}

			spec, err := loadVerifierConfig(cfg, tc.signature, slog.Default())
			r.NoError(err)
			r.Equal(tc.wantType, spec.GetType().String())
		})
	}
}
