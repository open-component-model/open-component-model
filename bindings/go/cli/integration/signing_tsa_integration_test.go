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
	"sync/atomic"
	"testing"
	"time"

	"github.com/digitorus/pkcs7"
	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/blob/direct"
	"ocm.software/open-component-model/bindings/go/cli/cmd"
	"ocm.software/open-component-model/bindings/go/cli/integration/internal"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/oci"
	urlresolver "ocm.software/open-component-model/bindings/go/oci/resolver/url"
)

// Test_Integration_Signing_TSA verifies the full TSA signing and verification
// flow using the credential graph for TSA configuration.
func Test_Integration_Signing_TSA(t *testing.T) {
	r := require.New(t)
	t.Parallel()

	// --- TSA mock server (counts requests to assert no-network guarantees) ---
	tsaKey := mustRSAKey(t)
	tsaCert := issueTSACert(t, tsaKey)
	tsaRootPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: tsaCert.Raw})

	var tsaRequests atomic.Int64
	tsaServer := httptest.NewServer(newCountingMockTSAHandler(t, tsaCert, tsaKey, &tsaRequests))
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

	// Write TSA root cert to file for credential graph.
	tsaRootPath := filepath.Join(dir, "tsa-root.pem")
	r.NoError(os.WriteFile(tsaRootPath, tsaRootPEM, 0o600))

	registryCreds := fmt.Sprintf(`  - identity:
      type: OCIRegistry
      hostname: %[1]q
      port: %[2]q
      scheme: http
    credentials:
    - type: Credentials/v1
      properties:
        username: %[3]q
        password: %[4]q`, registry.Host, registry.Port, registry.User, registry.Password)

	rsaCreds := fmt.Sprintf(`  - identity:
      type: RSA/v1alpha1
      algorithm: RSASSA-PSS
      signature: default
    credentials:
    - type: Credentials/v1
      properties:
        public_key_pem: %[1]q
        private_key_pem: %[2]q`, pubPEM, privPEM)

	// URL-specific TSA credential entry (hostname/port/scheme set).
	tsaCredsURL := func(rootPath string) string {
		return fmt.Sprintf(`  - identity:
      type: TSA/v1alpha1
      hostname: %[1]q
      port: %[2]q
      scheme: %[3]q
    credentials:
    - type: Credentials/v1
      properties:
        root_certs_pem_file: %[4]q`, tsaServerURL.Hostname(), tsaServerURL.Port(), tsaServerURL.Scheme, rootPath)
	}

	// Generic TSA credential entry (no URL attributes) — exercises the
	// generic credential-graph fallback rather than URL-specific matching.
	tsaCredsGeneric := fmt.Sprintf(`  - identity:
      type: TSA/v1alpha1
    credentials:
    - type: Credentials/v1
      properties:
        root_certs_pem_file: %[1]q`, tsaRootPath)

	writeConfig := func(name string, tsaEntry string) string {
		body := "type: generic.config.ocm.software/v1\nconfigurations:\n- type: credentials.config.ocm.software\n  consumers:\n" + registryCreds + "\n" + rsaCreds + "\n"
		if tsaEntry != "" {
			body += tsaEntry + "\n"
		}
		p := filepath.Join(dir, name)
		r.NoError(os.WriteFile(p, []byte(body), os.ModePerm))
		return p
	}

	// Config WITHOUT any TSA entry — used for flag-only signing.
	cfgNoTSAPath := writeConfig("ocmconfig-no-tsa.yaml", "")
	// Config with a generic (URL-less) TSA entry — exercises generic lookup.
	cfgGenericPath := writeConfig("ocmconfig-tsa-generic.yaml", tsaCredsGeneric)
	// Config with a URL-specific TSA entry — exercises URL-specific lookup.
	cfgURLSpecificPath := writeConfig("ocmconfig-tsa-url.yaml", tsaCredsURL(tsaRootPath))

	client := internal.CreateAuthClient(registry.RegistryAddress, registry.User, registry.Password)
	resolver, err := urlresolver.New(
		urlresolver.WithBaseURL(registry.RegistryAddress),
		urlresolver.WithPlainHTTP(true),
		urlresolver.WithBaseClient(client),
	)
	r.NoError(err)
	repo, err := oci.NewRepository(oci.WithResolver(resolver), oci.WithTempDir(t.TempDir()))
	r.NoError(err)

	newResource := func(payload string) resource {
		return resource{
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
			ReadOnlyBlob: direct.NewFromBytes([]byte(payload)),
		}
	}

	t.Run("sign with --tsa-url flag and verify with generic TSA root certs from credential graph", func(t *testing.T) {
		r := require.New(t)

		name, version := "ocm.software/tsa-test-generic", "v1.0.0"
		uploadComponentVersion(t, repo, name, version, newResource("hello tsa generic"))
		ref := fmt.Sprintf("http://%s//%s:%s", registry.RegistryAddress, name, version)

		signCMD := cmd.New()
		signCMD.SetArgs([]string{"sign", "cv", ref, "--config", cfgNoTSAPath, "--tsa-url", tsaServer.URL})
		r.NoError(signCMD.ExecuteContext(t.Context()))

		// Verify using a generic TSA/v1alpha1 identity (no URL attributes) so the
		// generic credential-graph fallback is exercised.
		verifyCMD := cmd.New()
		verifyCMD.SetArgs([]string{"verify", "cv", ref, "--config", cfgGenericPath})
		r.NoError(verifyCMD.ExecuteContext(t.Context()))
	})

	t.Run("sign with --tsa-url flag and verify with URL-specific TSA root certs from credential graph", func(t *testing.T) {
		r := require.New(t)

		name, version := "ocm.software/tsa-test-url-specific", "v1.0.0"
		uploadComponentVersion(t, repo, name, version, newResource("hello tsa url specific"))
		ref := fmt.Sprintf("http://%s//%s:%s", registry.RegistryAddress, name, version)

		signCMD := cmd.New()
		signCMD.SetArgs([]string{"sign", "cv", ref, "--config", cfgNoTSAPath, "--tsa-url", tsaServer.URL})
		r.NoError(signCMD.ExecuteContext(t.Context()))

		verifyCMD := cmd.New()
		verifyCMD.SetArgs([]string{"verify", "cv", ref, "--config", cfgURLSpecificPath})
		r.NoError(verifyCMD.ExecuteContext(t.Context()))
	})

	t.Run("verify with wrong TSA root cert in credential graph fails", func(t *testing.T) {
		r := require.New(t)

		name, version := "ocm.software/tsa-test-wrong-root", "v1.0.0"
		uploadComponentVersion(t, repo, name, version, newResource("hello tsa wrong root"))
		ref := fmt.Sprintf("http://%s//%s:%s", registry.RegistryAddress, name, version)

		signCMD := cmd.New()
		signCMD.SetArgs([]string{"sign", "cv", ref, "--config", cfgNoTSAPath, "--tsa-url", tsaServer.URL})
		r.NoError(signCMD.ExecuteContext(t.Context()))

		// A different, unrelated root cert must make timestamp verification fail.
		wrongKey := mustRSAKey(t)
		wrongCert := issueTSACert(t, wrongKey)
		wrongRootPath := filepath.Join(dir, "wrong-root.pem")
		r.NoError(os.WriteFile(wrongRootPath, pem.EncodeToMemory(&pem.Block{
			Type: "CERTIFICATE", Bytes: wrongCert.Raw,
		}), 0o600))
		cfgWrongRootPath := writeConfig("ocmconfig-wrong-root.yaml", tsaCredsURL(wrongRootPath))

		verifyCMD := cmd.New()
		verifyCMD.SetArgs([]string{"verify", "cv", ref, "--config", cfgWrongRootPath})
		r.Error(verifyCMD.ExecuteContext(t.Context()))
	})

	t.Run("dry-run does not contact TSA server", func(t *testing.T) {
		r := require.New(t)

		name, version := "ocm.software/tsa-test-dry-run", "v1.0.0"
		uploadComponentVersion(t, repo, name, version, newResource("hello tsa dry-run"))
		ref := fmt.Sprintf("http://%s//%s:%s", registry.RegistryAddress, name, version)

		before := tsaRequests.Load()

		signCMD := cmd.New()
		signCMD.SetArgs([]string{"sign", "cv", ref, "--config", cfgNoTSAPath, "--tsa-url", tsaServer.URL, "--dry-run"})
		r.NoError(signCMD.ExecuteContext(t.Context()))

		// No network interaction with the TSA is allowed on a dry run.
		r.Equal(before, tsaRequests.Load(), "dry-run must not contact the TSA server")

		// The component must not have a signature persisted, so verify must fail.
		verifyCMD := cmd.New()
		verifyCMD.SetArgs([]string{"verify", "cv", ref, "--config", cfgNoTSAPath})
		r.Error(verifyCMD.ExecuteContext(t.Context()))
	})
}

// issueTSACert creates a self-signed certificate suitable for timestamping.
func issueTSACert(t *testing.T, key *rsa.PrivateKey) *x509.Certificate {
	t.Helper()
	tmpl := &x509.Certificate{
		SerialNumber:          mustRand128Bit(t),
		Subject:               pkix.Name{CommonName: "Test TSA"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(7 * 24 * time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageTimeStamping},
		BasicConstraintsValid: true,
		IsCA:                  true, // self-signed root
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	require.NoError(t, err)
	cert, err := x509.ParseCertificate(der)
	require.NoError(t, err)
	return cert
}

// newCountingMockTSAHandler returns an http.Handler that acts as an RFC 3161 TSA
// and increments counter on every received request, enabling no-network assertions.
func newCountingMockTSAHandler(t *testing.T, cert *x509.Certificate, key *rsa.PrivateKey, counter *atomic.Int64) http.Handler {
	t.Helper()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		counter.Add(1)

		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "read body", http.StatusBadRequest)
			return
		}

		var req tsaRequest
		rest, err := asn1.Unmarshal(body, &req)
		if err != nil || len(rest) > 0 {
			http.Error(w, "unmarshal request", http.StatusBadRequest)
			return
		}

		tstInfo := tsaInfo{
			Version:        1,
			Policy:         asn1.ObjectIdentifier{1, 3, 6, 1, 4, 1, 0}, // dummy policy
			MessageImprint: req.MessageImprint,
			SerialNumber:   mustRand128Bit(t),
			GenTime:        time.Now().UTC().Truncate(time.Second),
			Nonce:          req.Nonce,
		}

		tstInfoDER, err := asn1.Marshal(tstInfo)
		if err != nil {
			http.Error(w, "marshal tstinfo", http.StatusInternalServerError)
			return
		}

		sd, err := pkcs7.NewSignedData(tstInfoDER)
		if err != nil {
			http.Error(w, "new signed data", http.StatusInternalServerError)
			return
		}
		sd.SetContentType(asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 9, 16, 1, 4})
		if err := sd.AddSigner(cert, key, pkcs7.SignerInfoConfig{}); err != nil {
			http.Error(w, "add signer", http.StatusInternalServerError)
			return
		}
		p7DER, err := sd.Finish()
		if err != nil {
			http.Error(w, "finish signed data", http.StatusInternalServerError)
			return
		}

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
