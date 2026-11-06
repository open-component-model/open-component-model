package integration

import (
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"net/url"
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
	"ocm.software/open-component-model/bindings/go/internal/testutil/tsatest"
	"ocm.software/open-component-model/bindings/go/oci"
	urlresolver "ocm.software/open-component-model/bindings/go/oci/resolver/url"
	"ocm.software/open-component-model/bindings/go/signing/tsa"
)

// Test_Integration_Signing_TSA signs against a real registry with --tsa-url and
// verifies with URL-specific TSA roots from the credential graph. TSA guards and
// failure modes are covered by the cmd tests against a CTF.
func Test_Integration_Signing_TSA(t *testing.T) {
	r := require.New(t)
	t.Parallel()

	tsaServer := tsatest.Start(t)
	tsaServerURL, err := url.Parse(tsaServer.URL)
	r.NoError(err)

	registry, err := internal.CreateOCIRegistry(t)
	r.NoError(err)

	k := pkitest.RSAKey(t)
	privPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(k)})
	pubPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PUBLIC KEY", Bytes: x509.MarshalPKCS1PublicKey(&k.PublicKey)})

	dir := t.TempDir()
	config := filepath.Join(dir, "ocmconfig.yaml")
	r.NoError(os.WriteFile(config, []byte(fmt.Sprintf(`type: generic.config.ocm.software/v1
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
      hostname: %[7]q
      port: %[8]q
      scheme: %[9]q
    credentials:
    - type: TSACredentials/v1alpha1
      rootCertsPEMFile: %[10]q
`, registry.Host, registry.Port, registry.User, registry.Password, pubPEM, privPEM,
		tsaServerURL.Hostname(), tsaServerURL.Port(), tsaServerURL.Scheme, pkitest.WriteCerts(t, dir, "tsa-root.pem", tsaServer.Cert))), 0o600))

	resolver, err := urlresolver.New(
		urlresolver.WithBaseURL(registry.RegistryAddress),
		urlresolver.WithPlainHTTP(true),
		urlresolver.WithBaseClient(internal.CreateAuthClient(registry.RegistryAddress, registry.User, registry.Password)),
	)
	r.NoError(err)
	repo, err := oci.NewRepository(oci.WithResolver(resolver), oci.WithTempDir(t.TempDir()))
	r.NoError(err)

	name, version := "ocm.software/tsa-test", "v1.0.0"
	uploadComponentVersion(t, repo, name, version, resource{
		Resource: &descriptor.Resource{
			ElementMeta:  descriptor.ElementMeta{ObjectMeta: descriptor.ObjectMeta{Name: "raw-data", Version: "v1.0.0"}},
			Type:         "plainText",
			Access:       &v2.LocalBlob{},
			CreationTime: descriptor.CreationTime(time.Now()),
		},
		ReadOnlyBlob: direct.NewFromBytes([]byte("hello tsa")),
	})
	ref := fmt.Sprintf("http://%s//%s:%s", registry.RegistryAddress, name, version)

	signCMD := cmd.New()
	signCMD.SetArgs([]string{"sign", "cv", ref, "--config", config, "--tsa-url", tsaServer.URL})
	r.NoError(signCMD.ExecuteContext(t.Context()))

	desc, err := repo.GetComponentVersion(t.Context(), name, version)
	r.NoError(err)
	r.Len(desc.Signatures, 1)
	r.NotNil(desc.Signatures[0].Timestamp)
	var labelURL string
	for _, l := range desc.Component.Labels {
		if l.Name == tsa.TSAURLLabelPrefix+"default" {
			r.False(l.Signing, "the TSA URL label must not be signing-relevant")
			r.NoError(l.GetValue(&labelURL))
		}
	}
	r.Equal(tsaServer.URL, labelURL)

	verifyCMD := cmd.New()
	verifyCMD.SetArgs([]string{"verify", "cv", ref, "--config", config})
	r.NoError(verifyCMD.ExecuteContext(t.Context()))
}
