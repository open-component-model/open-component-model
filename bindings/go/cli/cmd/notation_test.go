package cmd_test

import (
	"bytes"
	"crypto/x509"
	"encoding/pem"
	"os"
	"path/filepath"
	"testing"

	"github.com/notaryproject/notation-core-go/testhelper"
	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/cli/cmd/internal/test"
)

// Test_Sign_And_Verify_With_Notation proves the Notation signing handler is
// registered in the CLI by default: a component version signed via a
// NotationSigningConfiguration in .ocmconfig verifies via a
// NotationVerificationConfiguration, with all key/trust material supplied as
// NotationCredentials. No plugin is installed.
func Test_Sign_And_Verify_With_Notation(t *testing.T) {
	r := require.New(t)
	tmp := t.TempDir()

	name, version := "ocm.software/notation-signing", "1.0.0"
	constructorYAMLFilePath := filepath.Join(tmp, "component-constructor.yaml")
	r.NoError(os.WriteFile(constructorYAMLFilePath, []byte(`
name: `+name+`
version: `+version+`
provider:
  name: ocm.software
resources:
  - name: my-resource
    type: blob
    input:
      type: utf8/v1
      text: "notation signing test"
`), 0o600))

	archiveFilePath := filepath.Join(tmp, "transport-archive")
	_, err := test.OCM(t, test.WithArgs("add", "cv",
		"--constructor", constructorYAMLFilePath,
		"--repository", archiveFilePath,
	))
	r.NoError(err, "could not construct component version")

	// Self-signed signing certificate (leaf == trust anchor), valid per Notary
	// Project certificate requirements.
	tuple := testhelper.GetRSASelfSignedSigningCertTuple("OCM Notation CLI Test")
	keyDER, err := x509.MarshalPKCS8PrivateKey(tuple.PrivateKey)
	r.NoError(err)
	keyPath := filepath.Join(tmp, "notation-key.pem")
	certPath := filepath.Join(tmp, "notation-cert.pem")
	r.NoError(os.WriteFile(keyPath, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER}), 0o600))
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: tuple.Cert.Raw})
	r.NoError(os.WriteFile(certPath, certPEM, 0o600))

	configFilePath := filepath.Join(tmp, "notation.ocmconfig.yaml")
	r.NoError(os.WriteFile(configFilePath, []byte(`
type: generic.config.ocm.software/v1
configurations:
- type: signing.config.ocm.software/v1alpha1
  signer:
    type: NotationSigningConfiguration/v1alpha1
  verifier:
    type: NotationVerificationConfiguration/v1alpha1
- type: credentials.config.ocm.software
  consumers:
  - identity:
      type: Notation/v1
      signature: default
    credentials:
    - type: NotationCredentials/v1
      privateKeyPEMFile: `+keyPath+`
      certificateChainPEMFile: `+certPath+`
      trustedCACertificatesPEMFile: `+certPath+`
`), 0o600))

	reference := archiveFilePath + "//" + name + ":" + version

	signOut := &bytes.Buffer{}
	_, err = test.OCM(t, test.WithArgs("sign", "component-version",
		reference,
		"--config", configFilePath,
	), test.WithOutput(signOut))
	r.NoError(err, "notation signing must succeed via the default-registered handler")
	r.Contains(signOut.String(), "Notation/v1alpha1", "signature must carry the Notation algorithm")

	_, err = test.OCM(t, test.WithArgs("verify", "component-version",
		reference,
		"--config", configFilePath,
	))
	r.NoError(err, "notation verification must succeed via the default-registered handler")
}
