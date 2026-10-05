// Package fips140 runs OCM CLI commands with GODEBUG=fips140=only.
//
// Strict FIPS 140-3 mode panics or fails on any non-approved algorithm, also
// deep inside dependencies. These tests run the construct, sign and verify path
// end to end, so such a use shows up as a test failure. The mode is fixed per
// process, so the directive below applies to this package's test binary only.
//
//go:debug fips140=only
package fips140

import (
	"crypto/fips140"
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

	"ocm.software/open-component-model/bindings/go/cli/cmd/internal/test"
)

func TestSignAndVerifyRSA(t *testing.T) {
	r := require.New(t)
	r.True(fips140.Enforced(), "test binary must run with fips140=only")
	tmp := t.TempDir()

	name, version := "ocm.software/fips140", "1.0.0"
	constructor := filepath.Join(tmp, "component-constructor.yaml")
	r.NoError(os.WriteFile(constructor, fmt.Appendf(nil, `
name: %s
version: %s
provider:
  name: ocm.software
resources:
  - name: data
    type: blob
    input:
      type: utf8/v1
      text: "signed in fips140=only"
`, name, version), 0o600))

	archive := filepath.Join(tmp, "transport-archive")
	_, err := test.OCM(t, test.WithArgs("add", "cv", "--constructor", constructor, "--repository", archive))
	r.NoError(err, "construct component version")

	const signature = "fips"
	keyPath, chainPath := writeRSAKeyAndCert(t, tmp)
	config := filepath.Join(tmp, "ocm-config.yaml")
	r.NoError(os.WriteFile(config, fmt.Appendf(nil, `
type: generic.config.ocm.software/v1
configurations:
- type: credentials.config.ocm.software
  consumers:
  - identity:
      type: RSA/v1alpha1
      algorithm: RSASSA-PSS
      signature: %s
    credentials:
    - type: Credentials/v1
      properties:
        public_key_pem_file: %s
        private_key_pem_file: %s
`, signature, chainPath, keyPath), 0o600))

	reference := archive + "//" + name + ":" + version
	_, err = test.OCM(t, test.WithArgs("sign", "component-version", reference, "--signature", signature, "--config", config))
	r.NoError(err, "sign component version")
	_, err = test.OCM(t, test.WithArgs("verify", "component-version", reference, "--signature", signature, "--config", config))
	r.NoError(err, "verify component version")
}

// writeRSAKeyAndCert writes a 2048-bit RSA key and a self-signed certificate,
// both FIPS-approved, as PEM files.
func writeRSAKeyAndCert(t *testing.T, dir string) (keyPath, certPath string) {
	t.Helper()
	r := require.New(t)
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	r.NoError(err)
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	r.NoError(err)
	tmpl := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: "fips140"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	r.NoError(err)

	keyPath = filepath.Join(dir, "key.pem")
	r.NoError(os.WriteFile(keyPath, pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}), 0o600))
	certPath = filepath.Join(dir, "chain.pem")
	r.NoError(os.WriteFile(certPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600))
	return keyPath, certPath
}
