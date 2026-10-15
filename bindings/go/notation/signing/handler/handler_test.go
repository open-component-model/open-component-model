package handler

import (
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"testing"

	"github.com/notaryproject/notation-core-go/testhelper"
	"github.com/stretchr/testify/require"

	descruntime "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/notation/signing/v1alpha1"
	notationcredsv1 "ocm.software/open-component-model/bindings/go/notation/spec/credentials/v1"
)

// certTuple bundles a self-signed signing certificate, its private key, and the
// PEM encodings the handler consumes via credentials.
type certTuple struct {
	cert       *x509.Certificate
	privateKey string // PKCS#8 PEM
	certChain  string // CERTIFICATE PEM (leaf == self-signed root)
}

func newSigningCert(t *testing.T, cn string) certTuple {
	t.Helper()
	r := require.New(t)
	tuple := testhelper.GetRSASelfSignedSigningCertTuple(cn)

	keyDER, err := x509.MarshalPKCS8PrivateKey(tuple.PrivateKey)
	r.NoError(err)
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: tuple.Cert.Raw})

	return certTuple{
		cert:       tuple.Cert,
		privateKey: string(keyPEM),
		certChain:  string(certPEM),
	}
}

func (c certTuple) caPEM() string { return c.certChain }

func digestOf(data []byte) descruntime.Digest {
	sum := sha256.Sum256(data)
	return descruntime.Digest{
		HashAlgorithm:          "SHA-256",
		NormalisationAlgorithm: "jsonNormalisation/v4alpha1",
		Value:                  hex.EncodeToString(sum[:]),
	}
}

func Test_Notation_Handler(t *testing.T) {
	ctx := t.Context()
	h := New()
	signer := newSigningCert(t, "notation-roundtrip")
	d := digestOf([]byte("hello notation"))

	sign := func(t *testing.T, cfg *v1alpha1.SignConfig) descruntime.SignatureInfo {
		t.Helper()
		si, err := h.Sign(ctx, d, cfg, &notationcredsv1.NotationCredentials{
			Type:                notationcredsv1.VersionedType,
			PrivateKeyPEM:       signer.privateKey,
			CertificateChainPEM: signer.certChain,
		})
		require.NoError(t, err)
		return si
	}

	for _, tc := range []struct {
		name      string
		envelope  string
		wantMedia string
	}{
		{"JWS default", "", v1alpha1.MediaTypeJWSEnvelope},
		{"JWS explicit", v1alpha1.MediaTypeJWSEnvelope, v1alpha1.MediaTypeJWSEnvelope},
		{"COSE", v1alpha1.MediaTypeCOSEEnvelope, v1alpha1.MediaTypeCOSEEnvelope},
	} {
		t.Run("roundtrip/"+tc.name, func(t *testing.T) {
			r := require.New(t)
			si := sign(t, &v1alpha1.SignConfig{EnvelopeMediaType: tc.envelope})

			r.Equal(string(v1alpha1.AlgorithmNotationV1Alpha1), si.Algorithm)
			r.Equal(tc.wantMedia, si.MediaType)
			r.NotEmpty(si.Value)

			err := h.Verify(ctx, descruntime.Signature{Name: "default", Digest: d, Signature: si},
				&v1alpha1.VerifyConfig{},
				&notationcredsv1.NotationCredentials{
					Type:                     notationcredsv1.VersionedType,
					TrustedCACertificatesPEM: signer.caPEM(),
				})
			r.NoError(err)
		})
	}

	t.Run("tamper: mutated envelope fails", func(t *testing.T) {
		r := require.New(t)
		si := sign(t, &v1alpha1.SignConfig{})
		// Corrupt one character of the base64 envelope.
		mutated := si
		b := []byte(si.Value)
		if b[0] == 'A' {
			b[0] = 'B'
		} else {
			b[0] = 'A'
		}
		mutated.Value = string(b)
		err := h.Verify(ctx, descruntime.Signature{Name: "default", Digest: d, Signature: mutated},
			&v1alpha1.VerifyConfig{},
			&notationcredsv1.NotationCredentials{Type: notationcredsv1.VersionedType, TrustedCACertificatesPEM: signer.caPEM()})
		r.Error(err)
	})

	t.Run("tamper: mismatched digest fails", func(t *testing.T) {
		r := require.New(t)
		si := sign(t, &v1alpha1.SignConfig{})
		other := digestOf([]byte("a different payload"))
		err := h.Verify(ctx, descruntime.Signature{Name: "default", Digest: other, Signature: si},
			&v1alpha1.VerifyConfig{},
			&notationcredsv1.NotationCredentials{Type: notationcredsv1.VersionedType, TrustedCACertificatesPEM: signer.caPEM()})
		r.Error(err)
	})

	t.Run("untrusted CA fails", func(t *testing.T) {
		r := require.New(t)
		si := sign(t, &v1alpha1.SignConfig{})
		other := newSigningCert(t, "someone-else")
		err := h.Verify(ctx, descruntime.Signature{Name: "default", Digest: d, Signature: si},
			&v1alpha1.VerifyConfig{},
			&notationcredsv1.NotationCredentials{Type: notationcredsv1.VersionedType, TrustedCACertificatesPEM: other.caPEM()})
		r.Error(err)
	})

	t.Run("trusted identity pinning", func(t *testing.T) {
		r := require.New(t)
		si := sign(t, &v1alpha1.SignConfig{})
		matching := "x509.subject: " + signer.cert.Subject.String()

		r.NoError(h.Verify(ctx, descruntime.Signature{Name: "default", Digest: d, Signature: si},
			&v1alpha1.VerifyConfig{TrustedIdentities: []string{matching}},
			&notationcredsv1.NotationCredentials{Type: notationcredsv1.VersionedType, TrustedCACertificatesPEM: signer.caPEM()}))

		nonMatching := "x509.subject: CN=nobody,O=Notary,ST=WA,C=US"
		r.Error(h.Verify(ctx, descruntime.Signature{Name: "default", Digest: d, Signature: si},
			&v1alpha1.VerifyConfig{TrustedIdentities: []string{nonMatching}},
			&notationcredsv1.NotationCredentials{Type: notationcredsv1.VersionedType, TrustedCACertificatesPEM: signer.caPEM()}))
	})

	t.Run("missing private key on sign", func(t *testing.T) {
		r := require.New(t)
		_, err := h.Sign(ctx, d, &v1alpha1.SignConfig{},
			&notationcredsv1.NotationCredentials{Type: notationcredsv1.VersionedType, CertificateChainPEM: signer.certChain})
		r.ErrorIs(err, ErrMissingPrivateKey)
	})

	t.Run("missing certificate chain on sign", func(t *testing.T) {
		r := require.New(t)
		_, err := h.Sign(ctx, d, &v1alpha1.SignConfig{},
			&notationcredsv1.NotationCredentials{Type: notationcredsv1.VersionedType, PrivateKeyPEM: signer.privateKey})
		r.ErrorIs(err, ErrMissingCertificateChain)
	})

	t.Run("missing trusted CA on verify", func(t *testing.T) {
		r := require.New(t)
		si := sign(t, &v1alpha1.SignConfig{})
		err := h.Verify(ctx, descruntime.Signature{Name: "default", Digest: d, Signature: si},
			&v1alpha1.VerifyConfig{}, nil)
		r.ErrorIs(err, ErrMissingTrustedCACertificates)
	})

	t.Run("envelope validation", func(t *testing.T) {
		si := sign(t, &v1alpha1.SignConfig{})
		creds := &notationcredsv1.NotationCredentials{Type: notationcredsv1.VersionedType, TrustedCACertificatesPEM: signer.caPEM()}

		cases := []struct {
			name string
			sig  descruntime.SignatureInfo
		}{
			{"empty algorithm", descruntime.SignatureInfo{Algorithm: "", MediaType: si.MediaType, Value: si.Value}},
			{"wrong algorithm", descruntime.SignatureInfo{Algorithm: "RSASSA-PSS", MediaType: si.MediaType, Value: si.Value}},
			{"unsupported media type", descruntime.SignatureInfo{Algorithm: si.Algorithm, MediaType: "application/octet-stream", Value: si.Value}},
		}
		for _, c := range cases {
			t.Run(c.name, func(t *testing.T) {
				err := h.Verify(ctx, descruntime.Signature{Name: "default", Digest: d, Signature: c.sig},
					&v1alpha1.VerifyConfig{}, creds)
				require.Error(t, err)
			})
		}
	})
}
