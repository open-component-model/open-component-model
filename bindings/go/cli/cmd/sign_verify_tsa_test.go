package cmd_test

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/asn1"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"ocm.software/open-component-model/bindings/go/blob/filesystem"
	"ocm.software/open-component-model/bindings/go/ctf"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/oci"
	ocictf "ocm.software/open-component-model/bindings/go/oci/ctf"

	"github.com/digitorus/pkcs7"
	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/cli/cmd/internal/test"
	"ocm.software/open-component-model/bindings/go/signing/tsa"
)

type mockTSTInfo struct {
	Version        int
	Policy         asn1.ObjectIdentifier
	MessageImprint tsa.MessageImprint
	SerialNumber   *big.Int
	GenTime        time.Time `asn1:"generalized"`
	Nonce          *big.Int  `asn1:"optional"`
}

type mockTSAResponse struct {
	Status         struct{ Status int }
	TimeStampToken asn1.RawValue `asn1:"optional"`
}

// newMockTSA starts a local RFC 3161 TSA that attests genTime. Its signer is a
// self-signed certificate with the critical id-kp-timeStamping EKU, valid long
// enough before now to cover backdated generation times.
func newMockTSA(t *testing.T, genTime time.Time) (*httptest.Server, *x509.Certificate) {
	t.Helper()
	r := require.New(t)
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	r.NoError(err)
	ekuVal, err := asn1.Marshal([]asn1.ObjectIdentifier{{1, 3, 6, 1, 5, 5, 7, 3, 8}})
	r.NoError(err)
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "mock TSA"},
		NotBefore:             time.Now().Add(-100 * time.Hour),
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

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		body, err := io.ReadAll(req.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		var tsReq tsa.Request
		if _, err := asn1.Unmarshal(body, &tsReq); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		info, err := asn1.Marshal(mockTSTInfo{
			Version:        1,
			Policy:         asn1.ObjectIdentifier{1, 3, 6, 1, 4, 1, 0},
			MessageImprint: tsReq.MessageImprint,
			SerialNumber:   big.NewInt(time.Now().UnixNano()),
			GenTime:        genTime.UTC().Truncate(time.Second),
			Nonce:          tsReq.Nonce,
		})
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		sd, err := pkcs7.NewSignedData(info)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		sd.SetContentType(asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 9, 16, 1, 4})
		if err := sd.AddSigner(cert, key, pkcs7.SignerInfoConfig{}); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		p7, err := sd.Finish()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		out, err := asn1.Marshal(mockTSAResponse{TimeStampToken: asn1.RawValue{FullBytes: p7}})
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/timestamp-reply")
		_, _ = w.Write(out)
	}))
	t.Cleanup(srv.Close)
	return srv, cert
}

// addTSATestComponentVersion constructs a component version in a fresh CTF and
// returns its reference.
func addTSATestComponentVersion(t *testing.T, name string) string {
	t.Helper()
	r := require.New(t)
	tmp := t.TempDir()
	constructor := filepath.Join(tmp, "component-constructor.yaml")
	r.NoError(os.WriteFile(constructor, []byte(fmt.Sprintf(`
name: %s
version: 1.0.0
provider:
  name: ocm.software
resources:
  - name: data
    type: blob
    input:
      type: utf8/v1
      text: "tsa test"
`, name)), 0o600))
	archive := filepath.Join(tmp, "transport-archive")
	_, err := test.OCM(t, test.WithArgs("add", "cv", "--constructor", constructor, "--repository", archive))
	r.NoError(err)
	return archive + "//" + name + ":1.0.0"
}

// writeExpiredSigner writes a key and a self-signed certificate that expired a
// day ago but was valid two days ago.
func writeExpiredSigner(t *testing.T, dir string) (keyPath, chainPath string) {
	t.Helper()
	r := require.New(t)
	key := mustKey(t)
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(time.Now().UnixNano()),
		Subject:               pkix.Name{CommonName: "expired signer"},
		NotBefore:             time.Now().Add(-72 * time.Hour),
		NotAfter:              time.Now().Add(-24 * time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	r.NoError(err)
	cert, err := x509.ParseCertificate(der)
	r.NoError(err)
	return writeKeyAndChain(t, dir, key, cert)
}

func rsaConsumerYAML(signature, chainPath, keyPath, extraProperties string) string {
	return fmt.Sprintf(`
  - identity:
      type: RSA/v1alpha1
      algorithm: RSASSA-PSS
      signature: %s
    credentials:
    - type: Credentials/v1
      properties:
        public_key_pem_file: %s
        private_key_pem_file: %s%s`, signature, chainPath, keyPath, extraProperties)
}

func pemSigningConfigYAML(consumers string) string {
	return `type: generic.config.ocm.software/v1
configurations:
- type: signing.config.ocm.software/v1alpha1
  signer:
    type: RSASigningConfiguration/v1alpha1
    signatureEncodingPolicy: PEM
- type: credentials.config.ocm.software
  consumers:` + consumers + "\n"
}

func tsaConsumerYAML(identityAttributes, rootPath string) string {
	return `
  - identity:
      type: TSA/v1alpha1` + identityAttributes + `
    credentials:
    - type: TSACredentials/v1alpha1
      rootCertsPEMFile: ` + rootPath
}

func Test_Verify_TSA_Trusted_Timestamp_Validates_Expired_Certificate(t *testing.T) {
	// The TSA attests a signing time at which the now expired certificate was valid.
	tsaSrv, tsaCert := newMockTSA(t, time.Now().Add(-48*time.Hour))
	tsaURL, err := url.Parse(tsaSrv.URL)
	require.NoError(t, err)

	dir := t.TempDir()
	keyPath, chainPath := writeExpiredSigner(t, dir)
	tsaRoot := writeCertsPEM(t, dir, "tsa-root.pem", tsaCert)
	rsaConsumer := rsaConsumerYAML("default", chainPath, keyPath, "")

	writeConfig := func(name, consumers string) string {
		p := filepath.Join(dir, name)
		require.NoError(t, os.WriteFile(p, []byte(pemSigningConfigYAML(consumers)), 0o600))
		return p
	}
	withoutTSA := writeConfig("no-tsa.yaml", rsaConsumer)

	tests := []struct {
		name   string
		config string
	}{
		{
			name:   "URL-specific TSA identity",
			config: writeConfig("url.yaml", rsaConsumer+tsaConsumerYAML(fmt.Sprintf("\n      hostname: %q\n      port: %q\n      scheme: http", tsaURL.Hostname(), tsaURL.Port()), tsaRoot)),
		},
		{
			name:   "generic TSA identity",
			config: writeConfig("generic.yaml", rsaConsumer+tsaConsumerYAML("", tsaRoot)),
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := require.New(t)
			ref := addTSATestComponentVersion(t, "ocm.software/tsa-expired-cert")
			_, err := test.OCM(t, test.WithArgs("sign", "cv", ref, "--config", withoutTSA, "--tsa-url", tsaSrv.URL))
			r.NoError(err)

			_, err = test.OCM(t, test.WithArgs("verify", "cv", ref, "--config", withoutTSA))
			r.Error(err, "without TSA roots the expired certificate must be rejected")

			_, err = test.OCM(t, test.WithArgs("verify", "cv", ref, "--config", tc.config))
			r.NoError(err, "a trusted timestamp must validate the certificate as of the signing time")
		})
	}
}

func Test_Verify_Ignores_Configured_TSA_Verified_Time(t *testing.T) {
	r := require.New(t)
	dir := t.TempDir()
	keyPath, chainPath := writeExpiredSigner(t, dir)
	injected := filepath.Join(dir, "injected.yaml")
	signedWhileValid := time.Now().Add(-48 * time.Hour).UTC().Format(time.RFC3339)
	r.NoError(os.WriteFile(injected, []byte(pemSigningConfigYAML(rsaConsumerYAML("default", chainPath, keyPath,
		fmt.Sprintf("\n        %s: %q", tsa.VerifiedTimeKey, signedWhileValid)))), 0o600))

	ref := addTSATestComponentVersion(t, "ocm.software/tsa-injected-time")
	_, err := test.OCM(t, test.WithArgs("sign", "cv", ref, "--config", injected))
	r.NoError(err)

	// The signature has no timestamp; a configured verified time must not rescue the expired certificate.
	_, err = test.OCM(t, test.WithArgs("verify", "cv", ref, "--config", injected))
	r.ErrorContains(err, "certificate has expired")
}

// The legacy OCM CLI timestamps the descriptor digest and stores a bare CMS
// SignedData under PEM block "TIMESTAMP INFO". It validates an expired signing
// certificate as of that TSA time, and so must this CLI when reading v1 data.
func Test_Verify_Legacy_OCM_Timestamp_Validates_Expired_Certificate(t *testing.T) {
	r := require.New(t)
	// The TSA attests a time at which the now expired certificate was valid.
	tsaSrv, tsaCert := newMockTSA(t, time.Now().Add(-48*time.Hour))

	dir := t.TempDir()
	keyPath, chainPath := writeExpiredSigner(t, dir)
	rsaConsumer := rsaConsumerYAML("default", chainPath, keyPath, "")
	withRoots := filepath.Join(dir, "with-tsa-roots.yaml")
	r.NoError(os.WriteFile(withRoots, []byte(pemSigningConfigYAML(rsaConsumer+tsaConsumerYAML("", writeCertsPEM(t, dir, "tsa-root.pem", tsaCert)))), 0o600))

	name := "ocm.software/v1-timestamped"
	ref := addTSATestComponentVersion(t, name)
	_, err := test.OCM(t, test.WithArgs("sign", "cv", ref, "--config", withRoots))
	r.NoError(err)

	// Control: without a timestamp the expired certificate is rejected.
	_, err = test.OCM(t, test.WithArgs("verify", "cv", ref, "--config", withRoots))
	r.ErrorContains(err, "certificate has expired")

	// Attach a legacy timestamp: token over the raw descriptor digest, stored as
	// a bare SignedData under "TIMESTAMP INFO".
	archive := strings.SplitN(ref, "//", 2)[0]
	fs, err := filesystem.NewFS(archive, os.O_RDWR)
	r.NoError(err)
	repo, err := oci.NewRepository(ocictf.WithCTF(ocictf.NewFromCTF(ctf.NewFileSystemCTF(fs))))
	r.NoError(err)
	desc, err := repo.GetComponentVersion(t.Context(), name, "1.0.0")
	r.NoError(err)
	r.Len(desc.Signatures, 1)
	digest, err := hex.DecodeString(desc.Signatures[0].Digest.Value)
	r.NoError(err)
	token, err := tsa.RequestTimestamp(t.Context(), nil, tsaSrv.URL, crypto.SHA256, digest)
	r.NoError(err)
	var contentInfo struct {
		ContentType asn1.ObjectIdentifier
		Content     asn1.RawValue `asn1:"explicit,tag:0"`
	}
	_, err = asn1.Unmarshal(token.Raw, &contentInfo)
	r.NoError(err)
	desc.Signatures[0].Timestamp = &descriptor.TimestampSpec{
		Value: string(pem.EncodeToMemory(&pem.Block{Type: "TIMESTAMP INFO", Bytes: contentInfo.Content.Bytes})),
		Time:  descriptor.CreationTime(token.Time),
	}
	r.NoError(repo.AddComponentVersion(t.Context(), desc))

	_, err = test.OCM(t, test.WithArgs("verify", "cv", ref, "--config", withRoots))
	r.NoError(err, "a trusted legacy timestamp must validate the certificate as of the TSA time")
}

// Sigstore timestamps its bundles with TSAs from its own signing config, so
// --tsa must be rejected for a Sigstore signer before anything is contacted.
func Test_Sign_TSA_Rejected_For_Sigstore_Signer(t *testing.T) {
	t.Setenv("SIGSTORE_ID_TOKEN", "")
	t.Setenv("ACTIONS_ID_TOKEN_REQUEST_TOKEN", "")
	r := require.New(t)

	var tsaRequests atomic.Int64
	tsaSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		tsaRequests.Add(1)
		http.Error(w, "unexpected", http.StatusTeapot)
	}))
	t.Cleanup(tsaSrv.Close)

	config := filepath.Join(t.TempDir(), "sigstore.yaml")
	r.NoError(os.WriteFile(config, []byte(`type: generic.config.ocm.software/v1
configurations:
- type: signing.config.ocm.software/v1alpha1
  signer:
    type: SigstoreSigningConfiguration/v1alpha1
`), 0o600))

	ref := addTSATestComponentVersion(t, "ocm.software/sigstore-tsa")
	_, err := test.OCM(t, test.WithArgs("sign", "cv", ref, "--config", config, "--tsa-url", tsaSrv.URL))
	r.ErrorContains(err, "cannot be used with a Sigstore signer")
	r.Zero(tsaRequests.Load())
}
