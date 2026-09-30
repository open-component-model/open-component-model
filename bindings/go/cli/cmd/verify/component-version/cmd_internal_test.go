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

	"github.com/digitorus/pkcs7"
	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/credentials"
	credconfigv1 "ocm.software/open-component-model/bindings/go/credentials/spec/config/v1"
	descruntime "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/bindings/go/signing/tsa"
)

func mustRaw(t *testing.T, jsonStr string) *runtime.Raw {
	t.Helper()
	raw := &runtime.Raw{}
	require.NoError(t, json.Unmarshal([]byte(jsonStr), raw))
	return raw
}

func TestCredentialProperties_DirectCredentials(t *testing.T) {
	r := require.New(t)
	dc := &credconfigv1.DirectCredentials{
		Type:       runtime.NewVersionedType(credconfigv1.CredentialsType, credconfigv1.Version),
		Properties: map[string]string{"public_key_pem": "KEY", "extra": "x"},
	}
	props := credentialProperties(dc)
	r.Equal("KEY", props["public_key_pem"])
	r.Equal("x", props["extra"])
}

func TestCredentialProperties_RawWithNestedProperties(t *testing.T) {
	r := require.New(t)
	// A plugin-resolved credential arriving as *runtime.Raw that wraps
	// DirectCredentials: the material lives under a nested "properties" object.
	raw := mustRaw(t, `{"type":"Credentials/v1","properties":{"public_key_pem":"PUBKEY","private_key_pem":"PRIVKEY"}}`)
	props := credentialProperties(raw)
	r.Equal("PUBKEY", props["public_key_pem"], "nested properties must be preserved, not dropped")
	r.Equal("PRIVKEY", props["private_key_pem"])
}

func TestCredentialProperties_RawWithFlatFields(t *testing.T) {
	r := require.New(t)
	// A typed credential (flat string fields) arriving as *runtime.Raw.
	raw := mustRaw(t, `{"type":"RSA/v1alpha1","publicKeyPEM":"PUBKEY"}`)
	props := credentialProperties(raw)
	r.Equal("PUBKEY", props["publicKeyPEM"])
	r.NotContains(props, "type")
}

func TestWithVerifiedTime_PreservesMaterialAndAddsTime(t *testing.T) {
	r := require.New(t)
	raw := mustRaw(t, `{"type":"Credentials/v1","properties":{"public_key_pem":"PUBKEY"}}`)
	ts := time.Date(2020, 1, 2, 3, 4, 5, 0, time.UTC)

	out := withVerifiedTime(raw, ts)
	dc, ok := out.(*credconfigv1.DirectCredentials)
	r.True(ok)
	r.Equal("PUBKEY", dc.Properties["public_key_pem"], "credential material must survive re-wrapping")
	r.Equal(ts.Format(time.RFC3339), dc.Properties[tsa.VerifiedTimeKey])
}

type staticResolver struct {
	creds runtime.Typed
	err   error
}

func (s staticResolver) Resolve(context.Context, runtime.Identity) (runtime.Typed, error) {
	return s.creds, s.err
}

type testTSTInfo struct {
	Version        int
	Policy         asn1.ObjectIdentifier
	MessageImprint tsa.MessageImprint
	SerialNumber   *big.Int
	GenTime        time.Time `asn1:"generalized"`
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

	mi, err := tsa.NewMessageImprint(crypto.SHA256, imprint)
	r.NoError(err)
	info, err := asn1.Marshal(testTSTInfo{
		Version:        1,
		Policy:         asn1.ObjectIdentifier{1, 3, 6, 1, 4, 1, 0},
		MessageImprint: mi,
		SerialNumber:   big.NewInt(42),
		GenTime:        time.Now().UTC().Truncate(time.Second),
	})
	r.NoError(err)
	sd, err := pkcs7.NewSignedData(info)
	r.NoError(err)
	sd.SetContentType(asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 9, 16, 1, 4})
	r.NoError(sd.AddSigner(cert, key, pkcs7.SignerInfoConfig{}))
	p7, err := sd.Finish()
	r.NoError(err)
	return p7
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

func TestVerifyTSATimestamp_CredentialResolution(t *testing.T) {
	failure := errors.New("credential plugin crashed")
	tests := []struct {
		name    string
		err     error
		wantErr error
	}{
		{name: "no TSA credentials configured", err: credentials.ErrNotFound},
		{name: "resolution failure is surfaced", err: failure, wantErr: failure},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := require.New(t)
			_, trusted, err := verifyTSATimestamp(t.Context(), slog.Default(), staticResolver{err: tc.err}, &descruntime.Descriptor{}, timestampedSignature(t))
			if tc.wantErr != nil {
				r.ErrorIs(err, tc.wantErr)
				return
			}
			r.NoError(err)
			r.False(trusted)
		})
	}
}

func TestWithoutVerifiedTime(t *testing.T) {
	tests := []struct {
		name         string
		creds        runtime.Typed
		wantStripped bool
		wantProps    map[string]string
	}{
		{name: "nil credentials"},
		{
			name: "direct credentials carrying a verified time",
			creds: &credconfigv1.DirectCredentials{
				Type:       runtime.NewVersionedType(credconfigv1.CredentialsType, credconfigv1.Version),
				Properties: map[string]string{"public_key_pem": "PUBKEY", tsa.VerifiedTimeKey: "2020-01-02T03:04:05Z"},
			},
			wantStripped: true,
			wantProps:    map[string]string{"public_key_pem": "PUBKEY"},
		},
		{
			name:         "plugin credentials carrying a verified time",
			creds:        mustRaw(t, `{"type":"Credentials/v1","properties":{"public_key_pem":"PUBKEY","tsa_verified_time":"2020-01-02T03:04:05Z"}}`),
			wantStripped: true,
			wantProps:    map[string]string{"public_key_pem": "PUBKEY"},
		},
		{
			name:  "typed credentials without a verified time",
			creds: mustRaw(t, `{"type":"RSA/v1alpha1","publicKeyPEM":"PUBKEY"}`),
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := require.New(t)
			out, stripped := withoutVerifiedTime(tc.creds)
			r.Equal(tc.wantStripped, stripped)
			if !tc.wantStripped {
				r.Equal(tc.creds, out)
				return
			}
			dc, ok := out.(*credconfigv1.DirectCredentials)
			r.True(ok)
			r.Equal(tc.wantProps, dc.Properties)
		})
	}
}

func TestVerifyTSATimestamp_LegacyAndMismatchedTokens(t *testing.T) {
	r := require.New(t)
	resolver := staticResolver{err: credentials.ErrNotFound}

	// The legacy OCM CLI stores a bare CMS SignedData over the descriptor digest
	// under PEM block type "TIMESTAMP INFO".
	legacy := timestampedSignature(t)
	digest, err := hex.DecodeString(legacy.Digest.Value)
	r.NoError(err)
	var contentInfo struct {
		ContentType asn1.ObjectIdentifier
		Content     asn1.RawValue `asn1:"explicit,tag:0"`
	}
	_, err = asn1.Unmarshal(mustTimestampToken(t, digest), &contentInfo)
	r.NoError(err)
	legacy.Timestamp.Value = string(pem.EncodeToMemory(&pem.Block{Type: "TIMESTAMP INFO", Bytes: contentInfo.Content.Bytes}))

	_, trusted, err := verifyTSATimestamp(t.Context(), slog.Default(), resolver, &descruntime.Descriptor{}, legacy)
	r.NoError(err, "a legacy OCM timestamp must not fail an otherwise valid signature")
	r.False(trusted)

	// A current-format token that does not cover the signature value is still rejected.
	mismatched := timestampedSignature(t)
	mismatched.Timestamp.Value = string(tsa.ToPEM(mustTimestampToken(t, digest)))
	_, _, err = verifyTSATimestamp(t.Context(), slog.Default(), resolver, &descruntime.Descriptor{}, mismatched)
	r.ErrorContains(err, "message imprint does not match")
}
