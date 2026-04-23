package handler

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	descruntime "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/runtime"

	"ocm.software/open-component-model/bindings/go/sigstore/signing/v1alpha1"
)

// mockExecutor records calls and returns configurable responses.
type mockExecutor struct {
	signCalled   bool
	verifyCalled bool

	signData []byte
	signOpts SignOpts

	verifyData       []byte
	verifyBundlePath string
	verifyOpts       VerifyOpts

	signBundleJSON []byte
	signErr        error
	verifyErr      error
}

func (m *mockExecutor) SignData(_ context.Context, data []byte, opts SignOpts) ([]byte, error) {
	m.signCalled = true
	m.signData = data
	m.signOpts = opts

	if m.signErr != nil {
		return nil, m.signErr
	}

	return m.signBundleJSON, nil
}

func (m *mockExecutor) VerifyData(_ context.Context, data []byte, bundlePath string, opts VerifyOpts) error {
	m.verifyCalled = true
	m.verifyData = data
	m.verifyBundlePath = bundlePath
	m.verifyOpts = opts
	return m.verifyErr
}

// --- Test helpers ---

func testDigest() descruntime.Digest {
	return descruntime.Digest{
		HashAlgorithm:          "SHA-256",
		NormalisationAlgorithm: "jsonNormalisation/v2",
		Value:                  "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
	}
}

func testSignConfig() *v1alpha1.SignConfig {
	cfg := &v1alpha1.SignConfig{
		FulcioURL:          "https://fulcio.example.com",
		RekorURL:           "https://rekor.example.com",
		TimestampServerURL: "https://tsa.example.com/api/v1/timestamp",
	}
	cfg.SetType(runtime.NewVersionedType(v1alpha1.SignConfigType, v1alpha1.Version))
	return cfg
}

func testVerifyConfig() *v1alpha1.VerifyConfig {
	cfg := &v1alpha1.VerifyConfig{
		CertificateOIDCIssuer: "https://accounts.google.com",
		CertificateIdentity:   "user@example.com",
	}
	cfg.SetType(runtime.NewVersionedType(v1alpha1.VerifyConfigType, v1alpha1.Version))
	return cfg
}

func fakeBundleJSON(t *testing.T) []byte {
	t.Helper()
	return fakeBundleJSONWithCert(t, "https://accounts.google.com")
}

func fakeBundleJSONWithCert(t *testing.T, issuer string) []byte {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	template := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "test"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		BasicConstraintsValid: true,
	}

	issuerExtValue := []byte(issuer)
	template.ExtraExtensions = []pkix.Extension{
		{
			Id:    sigstoreIssuerV1OID,
			Value: issuerExtValue,
		},
	}

	certDER, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	require.NoError(t, err)

	certB64 := base64.StdEncoding.EncodeToString(certDER)
	bundle := map[string]interface{}{
		"mediaType": "application/vnd.dev.sigstore.bundle.v0.3+json",
		"verificationMaterial": map[string]interface{}{
			"certificate": map[string]string{"rawBytes": certB64},
			"tlogEntries": []interface{}{},
		},
		"messageSignature": map[string]interface{}{
			"messageDigest": map[string]string{"algorithm": "SHA2_256", "digest": ""},
			"signature":     "",
		},
	}
	data, err := json.Marshal(bundle)
	require.NoError(t, err)
	return data
}
