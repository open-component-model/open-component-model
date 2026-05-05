package integration

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/cli/cmd"
)

type sigstoreStack struct {
	OIDCToken        string
	SigningConfig     string
	TrustedRoot      string
	OIDCIssuer       string
	OIDCIdentity     string
}

func loadSigstoreStack(t *testing.T) sigstoreStack {
	t.Helper()
	stack := sigstoreStack{
		OIDCToken:    os.Getenv("SIGSTORE_OIDC_TOKEN"),
		SigningConfig: os.Getenv("SIGSTORE_SIGNING_CONFIG"),
		TrustedRoot:  os.Getenv("SIGSTORE_TRUSTED_ROOT"),
		OIDCIssuer:   os.Getenv("SIGSTORE_OIDC_ISSUER"),
		OIDCIdentity: os.Getenv("SIGSTORE_OIDC_IDENTITY"),
	}
	if stack.OIDCToken == "" || stack.SigningConfig == "" || stack.TrustedRoot == "" || stack.OIDCIssuer == "" || stack.OIDCIdentity == "" {
		t.Skip("sigstore scaffolding env vars not set (SIGSTORE_OIDC_TOKEN, SIGSTORE_SIGNING_CONFIG, SIGSTORE_TRUSTED_ROOT, SIGSTORE_OIDC_ISSUER, SIGSTORE_OIDC_IDENTITY)")
	}
	return stack
}

func createCTFWithComponent(t *testing.T, name, version string) string {
	t.Helper()
	r := require.New(t)

	dir := t.TempDir()
	constructorContent := fmt.Sprintf(`
components:
- name: %s
  version: %s
  provider:
    name: ocm.software
  resources:
  - name: test-data
    version: v1.0.0
    type: plainText
    input:
      type: utf8
      text: "sigstore integration test payload"
`, name, version)

	constructorPath := filepath.Join(dir, "constructor.yaml")
	r.NoError(os.WriteFile(constructorPath, []byte(constructorContent), os.ModePerm))

	ctfPath := filepath.Join(dir, "ctf")
	addCMD := cmd.New()
	addCMD.SetArgs([]string{
		"add", "component-version",
		"--repository", fmt.Sprintf("ctf::%s", ctfPath),
		"--constructor", constructorPath,
	})
	r.NoError(addCMD.ExecuteContext(t.Context()))

	return ctfPath
}

func Test_Integration_Sigstore_CLI_SignAndVerify(t *testing.T) {
	r := require.New(t)
	stack := loadSigstoreStack(t)

	dir := t.TempDir()
	componentName := "ocm.software/sigstore-cli-test"
	componentVersion := "v1.0.0"

	ctfPath := createCTFWithComponent(t, componentName, componentVersion)
	ref := fmt.Sprintf("ctf::%s//%s:%s", ctfPath, componentName, componentVersion)

	signerSpec := fmt.Sprintf("type: SigstoreSigningConfiguration/v1alpha1\nsigningConfig: %s\n", stack.SigningConfig)
	signerSpecPath := filepath.Join(dir, "signer-spec.yaml")
	r.NoError(os.WriteFile(signerSpecPath, []byte(signerSpec), 0o600))

	verifierSpec := fmt.Sprintf(`type: SigstoreVerificationConfiguration/v1alpha1
certificateOIDCIssuer: %s
certificateIdentity: %s
trustedRoot: %s
`, stack.OIDCIssuer, stack.OIDCIdentity, stack.TrustedRoot)
	verifierSpecPath := filepath.Join(dir, "verifier-spec.yaml")
	r.NoError(os.WriteFile(verifierSpecPath, []byte(verifierSpec), 0o600))

	ocmconfig := fmt.Sprintf(`type: generic.config.ocm.software/v1
configurations:
- type: credentials.config.ocm.software
  consumers:
  - identity:
      type: OIDCIdentityToken/v1alpha1
      algorithm: sigstore
      signature: default
    credentials:
    - type: Credentials/v1
      properties:
        token: %q
`, stack.OIDCToken)
	cfgPath := filepath.Join(dir, "ocmconfig.yaml")
	r.NoError(os.WriteFile(cfgPath, []byte(ocmconfig), 0o600))

	t.Run("sign and verify", func(t *testing.T) {
		r := require.New(t)

		signCMD := cmd.New()
		signCMD.SetArgs([]string{"sign", "cv", ref, "--config", cfgPath, "--signer-spec", signerSpecPath})
		r.NoError(signCMD.ExecuteContext(t.Context()))

		verifyCMD := cmd.New()
		verifyCMD.SetArgs([]string{"verify", "cv", ref, "--config", cfgPath, "--verifier-spec", verifierSpecPath})
		r.NoError(verifyCMD.ExecuteContext(t.Context()))
	})

	t.Run("verify fails with wrong issuer", func(t *testing.T) {
		r := require.New(t)

		wrongSpec := fmt.Sprintf(`type: SigstoreVerificationConfiguration/v1alpha1
certificateOIDCIssuer: https://wrong-issuer.example.com
certificateIdentity: %s
trustedRoot: %s
`, stack.OIDCIdentity, stack.TrustedRoot)
		wrongSpecPath := filepath.Join(t.TempDir(), "wrong-verifier.yaml")
		r.NoError(os.WriteFile(wrongSpecPath, []byte(wrongSpec), 0o600))

		verifyCMD := cmd.New()
		verifyCMD.SetArgs([]string{"verify", "cv", ref, "--config", cfgPath, "--verifier-spec", wrongSpecPath})
		r.Error(verifyCMD.ExecuteContext(t.Context()))
	})

	t.Run("verify fails with wrong identity", func(t *testing.T) {
		r := require.New(t)

		wrongSpec := fmt.Sprintf(`type: SigstoreVerificationConfiguration/v1alpha1
certificateOIDCIssuer: %s
certificateIdentity: wrong-identity@example.com
trustedRoot: %s
`, stack.OIDCIssuer, stack.TrustedRoot)
		wrongSpecPath := filepath.Join(t.TempDir(), "wrong-identity-verifier.yaml")
		r.NoError(os.WriteFile(wrongSpecPath, []byte(wrongSpec), 0o600))

		verifyCMD := cmd.New()
		verifyCMD.SetArgs([]string{"verify", "cv", ref, "--config", cfgPath, "--verifier-spec", wrongSpecPath})
		r.Error(verifyCMD.ExecuteContext(t.Context()))
	})

	t.Run("sign fails without OIDC token in config", func(t *testing.T) {
		r := require.New(t)

		noTokenCfg := `type: generic.config.ocm.software/v1
configurations:
- type: credentials.config.ocm.software
  consumers: []
`
		noTokenCfgPath := filepath.Join(t.TempDir(), "no-token.yaml")
		r.NoError(os.WriteFile(noTokenCfgPath, []byte(noTokenCfg), 0o600))

		freshCTF := createCTFWithComponent(t, "ocm.software/sigstore-no-token", "v1.0.0")
		freshRef := fmt.Sprintf("ctf::%s//ocm.software/sigstore-no-token:v1.0.0", freshCTF)

		signCMD := cmd.New()
		signCMD.SetArgs([]string{"sign", "cv", freshRef, "--config", noTokenCfgPath, "--signer-spec", signerSpecPath})
		r.Error(signCMD.ExecuteContext(t.Context()))
	})

	t.Run("verify with private infrastructure", func(t *testing.T) {
		r := require.New(t)

		privateSpec := fmt.Sprintf(`type: SigstoreVerificationConfiguration/v1alpha1
certificateOIDCIssuer: %s
certificateIdentity: %s
trustedRoot: %s
privateInfrastructure: true
`, stack.OIDCIssuer, stack.OIDCIdentity, stack.TrustedRoot)
		privateSpecPath := filepath.Join(t.TempDir(), "private-verifier.yaml")
		r.NoError(os.WriteFile(privateSpecPath, []byte(privateSpec), 0o600))

		verifyCMD := cmd.New()
		verifyCMD.SetArgs([]string{"verify", "cv", ref, "--config", cfgPath, "--verifier-spec", privateSpecPath})
		r.NoError(verifyCMD.ExecuteContext(t.Context()))
	})
}
