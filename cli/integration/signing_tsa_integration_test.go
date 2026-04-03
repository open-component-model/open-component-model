package integration

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/asn1"
	"encoding/pem"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/digitorus/pkcs7"
	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/blob/direct"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/oci"
	urlresolver "ocm.software/open-component-model/bindings/go/oci/resolver/url"
	"ocm.software/open-component-model/cli/cmd"
	"ocm.software/open-component-model/cli/integration/internal"
)

// Test_Integration_Signing_TSA verifies the full TSA signing and verification
// flow using the credential graph for TSA configuration.
func Test_Integration_Signing_TSA(t *testing.T) {
	r := require.New(t)
	t.Parallel()

	// --- TSA mock server ---
	tsaKey := mustRSAKey(t)
	tsaCert := issueTSACert(t, tsaKey)
	tsaRootPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: tsaCert.Raw})

	tsaServer := httptest.NewServer(newMockTSAHandler(t, tsaCert, tsaKey))
	t.Cleanup(tsaServer.Close)
	tsaServerURL, err := url.Parse(tsaServer.URL)
	r.NoError(err)

	// --- OCI registry ---
	registry, err := internal.CreateOCIRegistry(t)
	r.NoError(err)

	// --- RSA signing keys ---
	k := mustRSAKey(t)
	privPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(k),
	})
	pubPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PUBLIC KEY",
		Bytes: x509.MarshalPKCS1PublicKey(&k.PublicKey),
	})

	dir := t.TempDir()

	// Write TSA root cert to file for credential graph
	tsaRootPath := filepath.Join(dir, "tsa-root.pem")
	r.NoError(os.WriteFile(tsaRootPath, tsaRootPEM, 0o600))

	// --- ocmconfig with TSA credential graph entry ---
	cfg := fmt.Sprintf(`
type: generic.config.ocm.software/v1
configurations:
- type: credentials.config.ocm.software
  consumers:
  - identity:
      type: OCIRegistry
      hostname: %[1]q
      port: %[2]q
      scheme: http
    credentials:
    - type: Credentials/v1
      properties:
        username: %[3]q
        password: %[4]q
  - identity:
      type: RSA/v1alpha1
      algorithm: RSASSA-PSS
      signature: default
    credentials:
    - type: Credentials/v1
      properties:
        public_key_pem: %[5]q
        private_key_pem: %[6]q
  - identity:
      type: TSA/v1alpha1
      hostname: %[8]q
      port: %[9]q
      scheme: %[10]q
    credentials:
    - type: Credentials/v1
      properties:
        root_certs_pem_file: %[7]q
`, registry.Host, registry.Port, registry.User, registry.Password, pubPEM, privPEM, tsaRootPath, tsaServerURL.Hostname(), tsaServerURL.Port(), tsaServerURL.Scheme)
	cfgPath := filepath.Join(dir, "ocmconfig.yaml")
	r.NoError(os.WriteFile(cfgPath, []byte(cfg), os.ModePerm))

	// --- ocmconfig WITHOUT TSA entry (for flag-only sign tests) ---
	cfgNoTSA := fmt.Sprintf(`
type: generic.config.ocm.software/v1
configurations:
- type: credentials.config.ocm.software
  consumers:
  - identity:
      type: OCIRegistry
      hostname: %[1]q
      port: %[2]q
      scheme: http
    credentials:
    - type: Credentials/v1
      properties:
        username: %[3]q
        password: %[4]q
  - identity:
      type: RSA/v1alpha1
      algorithm: RSASSA-PSS
      signature: default
    credentials:
    - type: Credentials/v1
      properties:
        public_key_pem: %[5]q
        private_key_pem: %[6]q
`, registry.Host, registry.Port, registry.User, registry.Password, pubPEM, privPEM)
	cfgNoTSAPath := filepath.Join(dir, "ocmconfig-no-tsa.yaml")
	r.NoError(os.WriteFile(cfgNoTSAPath, []byte(cfgNoTSA), os.ModePerm))

	// --- ocmconfig with URL-specific TSA root certs (no tsa_url) for verify-side credential graph ---
	// Uses the mock TSA server's hostname/port for URL-specific credential matching.
	cfgTSARootsOnly := fmt.Sprintf(`
type: generic.config.ocm.software/v1
configurations:
- type: credentials.config.ocm.software
  consumers:
  - identity:
      type: OCIRegistry
      hostname: %[1]q
      port: %[2]q
      scheme: http
    credentials:
    - type: Credentials/v1
      properties:
        username: %[3]q
        password: %[4]q
  - identity:
      type: RSA/v1alpha1
      algorithm: RSASSA-PSS
      signature: default
    credentials:
    - type: Credentials/v1
      properties:
        public_key_pem: %[5]q
        private_key_pem: %[6]q
  - identity:
      type: TSA/v1alpha1
      hostname: %[8]q
      port: %[9]q
      scheme: %[10]q
    credentials:
    - type: Credentials/v1
      properties:
        root_certs_pem_file: %[7]q
`, registry.Host, registry.Port, registry.User, registry.Password, pubPEM, privPEM, tsaRootPath, tsaServerURL.Hostname(), tsaServerURL.Port(), tsaServerURL.Scheme)
	cfgTSARootsOnlyPath := filepath.Join(dir, "ocmconfig-tsa-roots-only.yaml")
	r.NoError(os.WriteFile(cfgTSARootsOnlyPath, []byte(cfgTSARootsOnly), os.ModePerm))

	client := internal.CreateAuthClient(registry.RegistryAddress, registry.User, registry.Password)
	resolver, err := urlresolver.New(
		urlresolver.WithBaseURL(registry.RegistryAddress),
		urlresolver.WithPlainHTTP(true),
		urlresolver.WithBaseClient(client),
	)
	r.NoError(err)
	repo, err := oci.NewRepository(oci.WithResolver(resolver), oci.WithTempDir(t.TempDir()))
	r.NoError(err)

	t.Run("sign with --tsa-url flag and verify with generic TSA root certs from credential graph", func(t *testing.T) {
		r := require.New(t)

		localResource := resource{
			Resource: &descriptor.Resource{
				ElementMeta: descriptor.ElementMeta{
					ObjectMeta: descriptor.ObjectMeta{
						Name:    "raw-data",
						Version: "v1.0.0",
					},
				},
				Type:         "plainText",
				Access:       &v2.LocalBlob{},
				CreationTime: descriptor.CreationTime(time.Now()),
			},
			ReadOnlyBlob: direct.NewFromBytes([]byte("hello tsa")),
		}

		name, version := "ocm.software/tsa-test-component", "v1.0.0"
		uploadComponentVersion(t, repo, name, version, localResource)

		ref := fmt.Sprintf("http://%s//%s:%s", registry.RegistryAddress, name, version)

		// Sign with --tsa-url flag; credential graph provides signing keys + generic TSA root certs
		signCMD := cmd.New()
		signCMD.SetArgs([]string{"sign", "cv", ref, "--config", cfgPath, "--tsa-url", tsaServer.URL})
		r.NoError(signCMD.ExecuteContext(t.Context()))

		// Verify: generic TSA root certs resolved from credential graph (type: TSA/v1alpha1, no URL attrs)
		verifyCMD := cmd.New()
		verifyCMD.SetArgs([]string{"verify", "cv", ref, "--config", cfgPath})
		r.NoError(verifyCMD.ExecuteContext(t.Context()))
	})

	t.Run("sign with --tsa-url flag and verify with URL-specific TSA root certs from credential graph", func(t *testing.T) {
		r := require.New(t)

		localResource := resource{
			Resource: &descriptor.Resource{
				ElementMeta: descriptor.ElementMeta{
					ObjectMeta: descriptor.ObjectMeta{
						Name:    "raw-data",
						Version: "v1.0.0",
					},
				},
				Type:         "plainText",
				Access:       &v2.LocalBlob{},
				CreationTime: descriptor.CreationTime(time.Now()),
			},
			ReadOnlyBlob: direct.NewFromBytes([]byte("hello tsa flags")),
		}

		name, version := "ocm.software/tsa-test-flags", "v1.0.0"
		uploadComponentVersion(t, repo, name, version, localResource)

		ref := fmt.Sprintf("http://%s//%s:%s", registry.RegistryAddress, name, version)

		// Sign with explicit --tsa-url (no TSA in credential graph)
		signCMD := cmd.New()
		signCMD.SetArgs([]string{"sign", "cv", ref, "--config", cfgNoTSAPath, "--tsa-url", tsaServer.URL})
		r.NoError(signCMD.ExecuteContext(t.Context()))

		// Verify with TSA root certs from credential graph (no flag needed)
		verifyCMD := cmd.New()
		verifyCMD.SetArgs([]string{"verify", "cv", ref, "--config", cfgTSARootsOnlyPath})
		r.NoError(verifyCMD.ExecuteContext(t.Context()))
	})

	t.Run("verify with wrong TSA root cert in credential graph fails", func(t *testing.T) {
		r := require.New(t)

		localResource := resource{
			Resource: &descriptor.Resource{
				ElementMeta: descriptor.ElementMeta{
					ObjectMeta: descriptor.ObjectMeta{
						Name:    "raw-data",
						Version: "v1.0.0",
					},
				},
				Type:         "plainText",
				Access:       &v2.LocalBlob{},
				CreationTime: descriptor.CreationTime(time.Now()),
			},
			ReadOnlyBlob: direct.NewFromBytes([]byte("hello tsa wrong root")),
		}

		name, version := "ocm.software/tsa-test-wrong-root", "v1.0.0"
		uploadComponentVersion(t, repo, name, version, localResource)

		ref := fmt.Sprintf("http://%s//%s:%s", registry.RegistryAddress, name, version)

		// Sign with TSA
		signCMD := cmd.New()
		signCMD.SetArgs([]string{"sign", "cv", ref, "--config", cfgNoTSAPath, "--tsa-url", tsaServer.URL})
		r.NoError(signCMD.ExecuteContext(t.Context()))

		// Generate a wrong root cert and write credential graph config for it
		wrongKey := mustRSAKey(t)
		wrongCert := issueTSACert(t, wrongKey)
		wrongRootPath := filepath.Join(dir, "wrong-root.pem")
		r.NoError(os.WriteFile(wrongRootPath, pem.EncodeToMemory(&pem.Block{
			Type: "CERTIFICATE", Bytes: wrongCert.Raw,
		}), 0o600))

		cfgWrongRoot := fmt.Sprintf(`
type: generic.config.ocm.software/v1
configurations:
- type: credentials.config.ocm.software
  consumers:
  - identity:
      type: OCIRegistry
      hostname: %[1]q
      port: %[2]q
      scheme: http
    credentials:
    - type: Credentials/v1
      properties:
        username: %[3]q
        password: %[4]q
  - identity:
      type: RSA/v1alpha1
      algorithm: RSASSA-PSS
      signature: default
    credentials:
    - type: Credentials/v1
      properties:
        public_key_pem: %[5]q
        private_key_pem: %[6]q
  - identity:
      type: TSA/v1alpha1
      hostname: %[8]q
      port: %[9]q
      scheme: %[10]q
    credentials:
    - type: Credentials/v1
      properties:
        root_certs_pem_file: %[7]q
`, registry.Host, registry.Port, registry.User, registry.Password, pubPEM, privPEM, wrongRootPath, tsaServerURL.Hostname(), tsaServerURL.Port(), tsaServerURL.Scheme)
		cfgWrongRootPath := filepath.Join(dir, "ocmconfig-wrong-root.yaml")
		r.NoError(os.WriteFile(cfgWrongRootPath, []byte(cfgWrongRoot), os.ModePerm))

		// Verify with wrong root cert from credential graph — must fail
		verifyCMD := cmd.New()
		verifyCMD.SetArgs([]string{"verify", "cv", ref, "--config", cfgWrongRootPath})
		r.Error(verifyCMD.ExecuteContext(t.Context()))
	})

	t.Run("dry-run does not contact TSA server", func(t *testing.T) {
		r := require.New(t)

		localResource := resource{
			Resource: &descriptor.Resource{
				ElementMeta: descriptor.ElementMeta{
					ObjectMeta: descriptor.ObjectMeta{
						Name:    "raw-data",
						Version: "v1.0.0",
					},
				},
				Type:         "plainText",
				Access:       &v2.LocalBlob{},
				CreationTime: descriptor.CreationTime(time.Now()),
			},
			ReadOnlyBlob: direct.NewFromBytes([]byte("hello tsa dry-run")),
		}

		name, version := "ocm.software/tsa-test-dry-run", "v1.0.0"
		uploadComponentVersion(t, repo, name, version, localResource)

		ref := fmt.Sprintf("http://%s//%s:%s", registry.RegistryAddress, name, version)

		// Dry-run with TSA enabled — should not make a TSA request, should succeed
		signCMD := cmd.New()
		signCMD.SetArgs([]string{"sign", "cv", ref, "--config", cfgNoTSAPath, "--tsa-url", tsaServer.URL, "--dry-run"})
		r.NoError(signCMD.ExecuteContext(t.Context()))

		// The component should NOT have a signature (dry-run), so verify should fail
		verifyCMD := cmd.New()
		verifyCMD.SetArgs([]string{"verify", "cv", ref, "--config", cfgNoTSAPath})
		r.Error(verifyCMD.ExecuteContext(t.Context()))
	})
}

// issueTSACert creates a self-signed certificate suitable for timestamping.
func issueTSACert(t *testing.T, key *rsa.PrivateKey) *x509.Certificate {
	t.Helper()
	tmpl := &x509.Certificate{
		SerialNumber: mustRand128Bit(t),
		Subject:      pkix.Name{CommonName: "Test TSA"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(7 * 24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageTimeStamping},
		BasicConstraintsValid: true,
		IsCA:                  true, // self-signed root
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	require.NoError(t, err)
	cert, err := x509.ParseCertificate(der)
	require.NoError(t, err)
	return cert
}

// newMockTSAHandler returns an http.Handler that acts as an RFC 3161 TSA.
func newMockTSAHandler(t *testing.T, cert *x509.Certificate, key *rsa.PrivateKey) http.Handler {
	t.Helper()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "read body", http.StatusBadRequest)
			return
		}

		// Parse the TimeStampReq
		var req tsaRequest
		rest, err := asn1.Unmarshal(body, &req)
		if err != nil || len(rest) > 0 {
			http.Error(w, "unmarshal request", http.StatusBadRequest)
			return
		}

		// Build TSTInfo
		tstInfo := tsaInfo{
			Version:        1,
			Policy:         asn1.ObjectIdentifier{1, 3, 6, 1, 4, 1, 0}, // dummy policy
			MessageImprint: req.MessageImprint,
			SerialNumber:   mustRand128BitTest(t),
			GenTime:        time.Now().UTC().Truncate(time.Second),
			Nonce:          req.Nonce,
		}

		tstInfoDER, err := asn1.Marshal(tstInfo)
		if err != nil {
			http.Error(w, "marshal tstinfo", http.StatusInternalServerError)
			return
		}

		// Wrap in PKCS#7 SignedData
		sd, err := pkcs7.NewSignedData(tstInfoDER)
		if err != nil {
			http.Error(w, "new signed data", http.StatusInternalServerError)
			return
		}
		// Set the content type to TSTInfo OID
		sd.SetContentType(asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 9, 16, 1, 4})
		if err := sd.AddSigner(cert, key, pkcs7.SignerInfoConfig{
			ExtraSignedAttributes: nil,
		}); err != nil {
			http.Error(w, "add signer", http.StatusInternalServerError)
			return
		}
		p7DER, err := sd.Finish()
		if err != nil {
			http.Error(w, "finish signed data", http.StatusInternalServerError)
			return
		}

		// Build TimeStampResp
		resp := tsaResponse{
			Status: tsaPKIStatusInfo{Status: 0}, // granted
		}
		resp.TimeStampToken.FullBytes = p7DER
		resp.TimeStampToken.Class = asn1.ClassUniversal
		resp.TimeStampToken.Tag = asn1.TagSequence
		resp.TimeStampToken.IsCompound = true

		respDER, err := asn1.Marshal(resp)
		if err != nil {
			http.Error(w, "marshal response", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/timestamp-reply")
		w.Write(respDER) //nolint:errcheck
	})
}

func mustRand128BitTest(t *testing.T) *big.Int {
	t.Helper()
	n, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	require.NoError(t, err)
	return n
}

// Minimal ASN.1 types for the mock TSA server — just enough to build requests and responses.
type tsaRequest struct {
	Version        int
	MessageImprint tsaMessageImprint
	ReqPolicy      asn1.ObjectIdentifier `asn1:"optional"`
	Nonce          *big.Int              `asn1:"optional"`
	CertReq        bool                  `asn1:"optional,default:false"`
}

type tsaMessageImprint struct {
	HashAlgorithm pkix.AlgorithmIdentifier
	HashedMessage []byte
}

type tsaInfo struct {
	Version        int
	Policy         asn1.ObjectIdentifier
	MessageImprint tsaMessageImprint
	SerialNumber   *big.Int
	GenTime        time.Time `asn1:"generalized"`
	Nonce          *big.Int  `asn1:"optional"`
}

type tsaPKIStatusInfo struct {
	Status int
}

type tsaResponse struct {
	Status         tsaPKIStatusInfo
	TimeStampToken asn1.RawValue `asn1:"optional"`
}
