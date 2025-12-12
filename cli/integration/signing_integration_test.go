package integration

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"net"
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
	user := "ocm"

	// Setup credentials and htpasswd
	password := internal.GenerateRandomPassword(t, 20)
	htpasswd := internal.GenerateHtpasswd(t, user, password)

	containerName := "signing-oci-repository"
	registryAddress := internal.StartDockerContainerRegistry(t, containerName, htpasswd)
	host, port, err := net.SplitHostPort(registryAddress)
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
      type: OCIRepository
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
`, host, port, user, password, pubPEM, privPEM)
	cfgPath := filepath.Join(t.TempDir(), "ocmconfig.yaml")
	r.NoError(os.WriteFile(cfgPath, []byte(cfg), os.ModePerm))

	client := internal.CreateAuthClient(registryAddress, user, password)

	resolver, err := urlresolver.New(
		urlresolver.WithBaseURL(registryAddress),
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
			fmt.Sprintf("http://%s//%s:%s", registryAddress, name, version),
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
			fmt.Sprintf("http://%s//%s:%s", registryAddress, name, version),
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
