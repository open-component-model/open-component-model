package integration

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/blob/direct"
	"ocm.software/open-component-model/bindings/go/cli/cmd"
	"ocm.software/open-component-model/bindings/go/cli/integration/internal"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/internal/testutil/pkitest"
	"ocm.software/open-component-model/bindings/go/oci"
	urlresolver "ocm.software/open-component-model/bindings/go/oci/resolver/url"
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
		N: k.N,
		E: k.E,
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
- type: http.config.ocm.software/v1alpha1
  plainHTTP: true`, registry.Host, registry.Port, registry.User, registry.Password, pubPEM, privPEM)
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
		signCMD.SetArgs(append(signArgs, "--dry-run"))
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
	rootKey := pkitest.RSAKey(t)
	root := pkitest.Issue(t, nil, rootKey, "pem-test-root", true, &rootKey.PublicKey)
	intermKey := pkitest.RSAKey(t)
	interm := pkitest.Issue(t, root, rootKey, "pem-test-interm", true, &intermKey.PublicKey)
	leafKey := pkitest.RSAKey(t)
	leaf := pkitest.Issue(t, interm, intermKey, "pem-test-leaf", false, &leafKey.PublicKey)

	dir := t.TempDir()

	// Leaf private key (PKCS#1)
	leafKeyPath := filepath.Join(dir, "leaf.key")
	r.NoError(os.WriteFile(leafKeyPath,
		pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(leafKey)}),
		0o600))

	// [leaf + intermediate] chain — root must NOT be included (verifier rejects embedded self-signed certs)
	chainPath := pkitest.WriteCerts(t, dir, "chain.pem", leaf, interm)

	// Root CA — used as trust anchor by the verifier
	rootCAPath := pkitest.WriteCerts(t, dir, "root.pem", root)

	// Signing config: private key + [leaf, interm] chain, signer with PEM encoding policy
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
- type: signing.config.ocm.software/v1alpha1
  signer:
    type: RSASigningConfiguration/v1alpha1
    signatureAlgorithm: RSASSA-PSS
    signatureEncodingPolicy: PEM
- type: http.config.ocm.software/v1alpha1
  plainHTTP: true`, registry.Host, registry.Port, registry.User, registry.Password, leafKeyPath, chainPath)
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
- type: http.config.ocm.software/v1alpha1
  plainHTTP: true`, registry.Host, registry.Port, registry.User, registry.Password, rootCAPath)
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
