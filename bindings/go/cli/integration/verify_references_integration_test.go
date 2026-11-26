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

	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/cli/cmd"
)

// Test_Integration_VerifyComponentVersion_References covers the OCM v1 verify
// behaviour of following component references: a signed component version
// must not verify when a referenced component version is missing or changed.
func Test_Integration_VerifyComponentVersion_References(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	k, err := rsa.GenerateKey(rand.Reader, 2048)
	r.NoError(err)
	privPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(k)})
	pubPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PUBLIC KEY", Bytes: x509.MarshalPKCS1PublicKey(&k.PublicKey)})

	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "ocmconfig.yaml")
	r.NoError(os.WriteFile(cfgPath, fmt.Appendf(nil, `
type: generic.config.ocm.software/v1
configurations:
- type: credentials.config.ocm.software
  consumers:
  - identity:
      type: RSA/v1alpha1
      algorithm: RSASSA-PSS
      signature: default
    credentials:
    - type: Credentials/v1
      properties:
        public_key_pem: %q
        private_key_pem: %q
`, pubPEM, privPEM), os.ModePerm))

	const (
		app     = "acme.org/app"
		base    = "acme.org/base"
		version = "1.0.0"
	)

	run := func(args ...string) error {
		c := cmd.New()
		c.SetArgs(append(args, "--config", cfgPath))
		return c.ExecuteContext(t.Context())
	}
	addConstructor := func(ctf, content string) {
		t.Helper()
		path := filepath.Join(t.TempDir(), "constructor.yaml")
		require.NoError(t, os.WriteFile(path, []byte(content), os.ModePerm))
		require.NoError(t, run("add", "component-version", "--repository", "ctf::"+ctf, "--constructor", path))
	}
	cvRef := func(ctf string) string { return fmt.Sprintf("ctf::%s//%s:%s", ctf, app, version) }

	source := filepath.Join(dir, "source")
	addConstructor(source, fmt.Sprintf(`
components:
- name: %[1]s
  version: %[3]s
  provider:
    name: acme.org
  componentReferences:
  - name: base
    componentName: %[2]s
    version: %[3]s
- name: %[2]s
  version: %[3]s
  provider:
    name: acme.org
`, app, base, version))
	r.NoError(run("sign", "cv", cvRef(source)))

	t.Run("references present", func(t *testing.T) {
		require.NoError(t, run("verify", "cv", cvRef(source)))

		recursive := filepath.Join(t.TempDir(), "recursive")
		require.NoError(t, run("transfer", "cv", cvRef(source), "ctf::"+recursive, "--recursive"))
		require.NoError(t, run("verify", "cv", cvRef(recursive)))
	})

	t.Run("referenced component version missing", func(t *testing.T) {
		appOnly := filepath.Join(t.TempDir(), "app-only")
		require.NoError(t, run("transfer", "cv", cvRef(source), "ctf::"+appOnly))

		err := run("verify", "cv", cvRef(appOnly))
		require.ErrorContains(t, err, "REFERENCE VERIFICATION FAILED")
		require.ErrorContains(t, err, "failed resolving component reference base["+base+":"+version+"]")

		require.NoError(t, run("verify", "cv", cvRef(appOnly), "--skip-references"))
	})

	t.Run("referenced component version changed", func(t *testing.T) {
		tampered := filepath.Join(t.TempDir(), "tampered")
		require.NoError(t, run("transfer", "cv", cvRef(source), "ctf::"+tampered))
		addConstructor(tampered, fmt.Sprintf(`
components:
- name: %s
  version: %s
  provider:
    name: someone-else
`, base, version))

		err := run("verify", "cv", cvRef(tampered))
		require.ErrorContains(t, err, "REFERENCE VERIFICATION FAILED")
		require.ErrorContains(t, err, "does not match the digest stored in the reference")
	})
}
