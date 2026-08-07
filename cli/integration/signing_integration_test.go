package integration

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/blob/direct"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/oci"
	urlresolver "ocm.software/open-component-model/bindings/go/oci/resolver/url"
	"ocm.software/open-component-model/cli/cmd"
	"ocm.software/open-component-model/cli/integration/internal"
)

func Test_Integration_Signing(t *testing.T) {
	r := require.New(t)
	t.Parallel()

	t.Logf("Starting OCI based integration test")
	registry, err := internal.CreateOCIRegistry(t)
	r.NoError(err)

	k, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	priv := x509.MarshalPKCS1PrivateKey(k)
	pub := x509.MarshalPKCS1PublicKey(&rsa.PublicKey{
		N: k.PublicKey.N,
		E: k.PublicKey.E,
	})
	require.NoError(t, err)
	privPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: priv})
	pubPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PUBLIC KEY", Bytes: pub})

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
`, registry.Host, registry.Port, registry.User, registry.Password, pubPEM, privPEM)
	cfgPath := filepath.Join(t.TempDir(), "ocmconfig.yaml")
	r.NoError(os.WriteFile(cfgPath, []byte(cfg), os.ModePerm))

	client := internal.CreateAuthClient(registry.RegistryAddress, registry.User, registry.Password)

	resolver, err := urlresolver.New(
		urlresolver.WithBaseURL(registry.RegistryAddress),
		urlresolver.WithPlainHTTP(true),
		urlresolver.WithBaseClient(client),
	)
	r.NoError(err)

	repo, err := oci.NewRepository(oci.WithResolver(resolver), oci.WithTempDir(t.TempDir()))
	r.NoError(err)

	t.Run("sign and verify component with arbitrary local resource", func(t *testing.T) {
		r := require.New(t)

		localResource := resource{
			Resource: &descriptor.Resource{
				ElementMeta: descriptor.ElementMeta{
					ObjectMeta: descriptor.ObjectMeta{
						Name:    "raw-foobar",
						Version: "v1.0.0",
					},
				},
				Type:         "some-arbitrary-type-packed-in-image",
				Access:       &v2.LocalBlob{},
				CreationTime: descriptor.CreationTime(time.Now()),
			},
			ReadOnlyBlob: direct.NewFromBytes([]byte("foobar")),
		}

		name, version := "ocm.software/test-component", "v1.0.0"

		uploadComponentVersion(t, repo, name, version, localResource)

		signCMD := cmd.New()
		signArgs := []string{
			"sign",
			"cv",
			fmt.Sprintf("http://%s//%s:%s", registry.RegistryAddress, name, version),
			"--config",
			cfgPath,
		}
		signArgsWithDryRun := append(signArgs, "--dry-run")
		signCMD.SetArgs(signArgsWithDryRun)
		r.NoError(signCMD.ExecuteContext(t.Context()))

		verifyCMD := cmd.New()
		verifyArgs := []string{
			"verify",
			"cv",
			fmt.Sprintf("http://%s//%s:%s", registry.RegistryAddress, name, version),
			"--config",
			cfgPath,
		}
		verifyCMD.SetArgs(verifyArgs)
		r.Error(verifyCMD.ExecuteContext(t.Context()), "should fail to verify component version with dry-run signature")

		signCMD = cmd.New()
		signCMD.SetArgs(signArgs)
		r.NoError(signCMD.ExecuteContext(t.Context()))

		verifyCMD = cmd.New()
		verifyCMD.SetArgs(verifyArgs)
		r.NoError(verifyCMD.ExecuteContext(t.Context()))
	})
}

func Test_Integration_Signing_PEM(t *testing.T) {
	r := require.New(t)
	t.Parallel()

	t.Logf("Starting PEM-encoded signing integration test")
	registry, err := internal.CreateOCIRegistry(t)
	r.NoError(err)

	// Build a 3-tier certificate chain: root CA → intermediate CA → leaf
	rootKey := mustRSAKey(t)
	root := issuePEMCert(t, nil, rootKey, "pem-test-root", true, &rootKey.PublicKey)
	intermKey := mustRSAKey(t)
	interm := issuePEMCert(t, root, rootKey, "pem-test-interm", true, &intermKey.PublicKey)
	leafKey := mustRSAKey(t)
	leaf := issuePEMCert(t, interm, intermKey, "pem-test-leaf", false, &leafKey.PublicKey)

	dir := t.TempDir()

	// Leaf private key (PKCS#1)
	leafKeyPath := filepath.Join(dir, "leaf.key")
	r.NoError(os.WriteFile(leafKeyPath,
		pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(leafKey)}),
		0o600))

	// [leaf + intermediate] chain — root must NOT be included (verifier rejects embedded self-signed certs)
	chainPath := writePEMCertFile(t, dir, "chain.pem", leaf, interm)

	// Root CA — used as trust anchor by the verifier
	rootCAPath := writePEMCertFile(t, dir, "root.pem", root)

	// Signer spec: enable PEM encoding policy
	signerSpec := "type: RSASigningConfiguration/v1alpha1\nsignatureAlgorithm: RSASSA-PSS\nsignatureEncodingPolicy: PEM\n"
	signerSpecPath := filepath.Join(dir, "signer-spec.yaml")
	r.NoError(os.WriteFile(signerSpecPath, []byte(signerSpec), 0o600))

	// Signing config: private key + [leaf, interm] chain
	signCfg := fmt.Sprintf(`
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
        private_key_pem_file: %[5]q
        public_key_pem_file: %[6]q
`, registry.Host, registry.Port, registry.User, registry.Password, leafKeyPath, chainPath)
	signCfgPath := filepath.Join(dir, "sign.yaml")
	r.NoError(os.WriteFile(signCfgPath, []byte(signCfg), os.ModePerm))

	// Verify config: root CA only (self-signed → used as isolated trust anchor)
	verifyCfg := fmt.Sprintf(`
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
        public_key_pem_file: %[5]q
`, registry.Host, registry.Port, registry.User, registry.Password, rootCAPath)
	verifyCfgPath := filepath.Join(dir, "verify.yaml")
	r.NoError(os.WriteFile(verifyCfgPath, []byte(verifyCfg), os.ModePerm))

	client := internal.CreateAuthClient(registry.RegistryAddress, registry.User, registry.Password)

	resolver, err := urlresolver.New(
		urlresolver.WithBaseURL(registry.RegistryAddress),
		urlresolver.WithPlainHTTP(true),
		urlresolver.WithBaseClient(client),
	)
	r.NoError(err)

	repo, err := oci.NewRepository(oci.WithResolver(resolver), oci.WithTempDir(t.TempDir()))
	r.NoError(err)

	t.Run("sign and verify component with PEM encoded signature", func(t *testing.T) {
		r := require.New(t)

		localResource := resource{
			Resource: &descriptor.Resource{
				ElementMeta: descriptor.ElementMeta{
					ObjectMeta: descriptor.ObjectMeta{
						Name:    "raw-foobar",
						Version: "v1.0.0",
					},
				},
				Type:         "some-arbitrary-type-packed-in-image",
				Access:       &v2.LocalBlob{},
				CreationTime: descriptor.CreationTime(time.Now()),
			},
			ReadOnlyBlob: direct.NewFromBytes([]byte("foobar")),
		}

		name, version := "ocm.software/test-component-pem", "v1.0.0"

		uploadComponentVersion(t, repo, name, version, localResource)

		ref := fmt.Sprintf("http://%s//%s:%s", registry.RegistryAddress, name, version)

		signArgs := []string{
			"sign", "cv", ref,
			"--config", signCfgPath,
			"--signer-spec", signerSpecPath,
		}

		signCMD := cmd.New()
		signCMD.SetArgs(append(signArgs, "--dry-run"))
		r.NoError(signCMD.ExecuteContext(t.Context()))

		verifyCMD := cmd.New()
		verifyCMD.SetArgs([]string{"verify", "cv", ref, "--config", verifyCfgPath})
		r.Error(verifyCMD.ExecuteContext(t.Context()), "should fail to verify component version with dry-run signature")

		signCMD = cmd.New()
		signCMD.SetArgs(signArgs)
		r.NoError(signCMD.ExecuteContext(t.Context()))

		verifyCMD = cmd.New()
		verifyCMD.SetArgs([]string{"verify", "cv", ref, "--config", verifyCfgPath})
		r.NoError(verifyCMD.ExecuteContext(t.Context()))
	})
}

func mustRSAKey(t *testing.T) *rsa.PrivateKey {
	t.Helper()
	k, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	return k
}

func mustRand128Bit(t *testing.T) *big.Int {
	t.Helper()
	n, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	require.NoError(t, err)
	return n
}

// issuePEMCert creates an X.509 certificate signed by parent/parentKey.
// When parent is nil the certificate is self-signed (root CA use case).
func issuePEMCert(t *testing.T, parent *x509.Certificate, parentKey *rsa.PrivateKey, cn string, isCA bool, pub *rsa.PublicKey) *x509.Certificate {
	t.Helper()
	tmpl := &x509.Certificate{
		SerialNumber:          mustRand128Bit(t),
		Subject:               pkix.Name{CommonName: cn},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(7 * 24 * time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: isCA,
		IsCA:                  isCA,
	}
	if isCA {
		tmpl.KeyUsage |= x509.KeyUsageCertSign | x509.KeyUsageCRLSign
	}
	if parent == nil {
		parent = tmpl
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, parent, pub, parentKey)
	require.NoError(t, err)
	cert, err := x509.ParseCertificate(der)
	require.NoError(t, err)
	return cert
}

// writePEMCertFile concatenates the given certificates as CERTIFICATE PEM blocks
// into dir/name and returns the file path.
func writePEMCertFile(t *testing.T, dir, name string, certs ...*x509.Certificate) string {
	t.Helper()
	var blob []byte
	for _, c := range certs {
		blob = append(blob, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: c.Raw})...)
	}
	p := filepath.Join(dir, name)
	require.NoError(t, os.WriteFile(p, blob, 0o600))
	return p
}
