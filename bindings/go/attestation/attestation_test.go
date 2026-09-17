package attestation

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"testing"

	"github.com/secure-systems-lab/go-securesystemslib/dsse"
	"github.com/stretchr/testify/require"
)

func TestSignedBundle(t *testing.T) {
	r := require.New(t)
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	r.NoError(err)

	subject := Subject{Name: "ocm.software/app:1.0.0", Digest: map[string]string{"sha256": "deadbeef"}}
	predicate := json.RawMessage(`{"buildType":"https://example/bt","builder":{"id":"https://example/wf"}}`)

	bundleJSON, mediaType, err := SignedBundle(t.Context(), subject, "https://slsa.dev/provenance/v0.2", predicate, key)
	r.NoError(err)
	r.Equal(MediaTypeBundle, mediaType)

	var b struct {
		MediaType    string `json:"mediaType"`
		DSSEEnvelope struct {
			Payload     string `json:"payload"`
			PayloadType string `json:"payloadType"`
			Signatures  []struct {
				Sig string `json:"sig"`
			} `json:"signatures"`
		} `json:"dsseEnvelope"`
	}
	r.NoError(json.Unmarshal(bundleJSON, &b))
	r.Equal(MediaTypeBundle, b.MediaType)
	r.Equal(MediaTypeInTotoStatement, b.DSSEEnvelope.PayloadType)
	r.Len(b.DSSEEnvelope.Signatures, 1)

	sig, err := base64.StdEncoding.DecodeString(b.DSSEEnvelope.Signatures[0].Sig)
	r.NoError(err)
	r.NotEmpty(sig)

	payload, err := base64.StdEncoding.DecodeString(b.DSSEEnvelope.Payload)
	r.NoError(err)
	var stmt struct {
		Type          string          `json:"_type"`
		PredicateType string          `json:"predicateType"`
		Subject       []Subject       `json:"subject"`
		Predicate     json.RawMessage `json:"predicate"`
	}
	r.NoError(json.Unmarshal(payload, &stmt))
	r.Equal(StatementType, stmt.Type)
	r.Equal("https://slsa.dev/provenance/v0.2", stmt.PredicateType)
	r.Len(stmt.Subject, 1)
	r.Equal(subject.Name, stmt.Subject[0].Name)
	r.Equal(subject.Digest, stmt.Subject[0].Digest)
	r.JSONEq(string(predicate), string(stmt.Predicate))

	// The DSSE signature verifies over PAE(payloadType, payload) — the exact
	// bytes cosign checks — with the public key.
	pae := dsse.PAE(b.DSSEEnvelope.PayloadType, payload)
	digest := sha256.Sum256(pae)
	r.True(ecdsa.VerifyASN1(&key.PublicKey, digest[:], sig), "DSSE signature must verify over PAE")

	// A different key must not verify (guards against a false pass).
	other, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	r.NoError(err)
	r.False(ecdsa.VerifyASN1(&other.PublicKey, digest[:], sig))
}

func TestSignedBundleRejectsInvalidInput(t *testing.T) {
	r := require.New(t)
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	r.NoError(err)
	subject := Subject{Name: "x", Digest: map[string]string{"sha256": "y"}}

	_, _, err = SignedBundle(t.Context(), subject, "t", nil, nil)
	r.Error(err)
	_, _, err = SignedBundle(t.Context(), Subject{Digest: map[string]string{"sha256": "y"}}, "t", nil, key)
	r.Error(err)
	_, _, err = SignedBundle(t.Context(), Subject{Name: "x"}, "t", nil, key)
	r.Error(err)
}

func TestDigestFromManifestReference(t *testing.T) {
	r := require.New(t)
	d, err := DigestFromManifestReference("sha256:abc")
	r.NoError(err)
	r.Equal(map[string]string{"sha256": "abc"}, d)

	_, err = DigestFromManifestReference("abc")
	r.Error(err)
	_, err = DigestFromManifestReference("sha512:abc")
	r.Error(err)
}
