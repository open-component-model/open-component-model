// SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company and Open Component Model contributors.
//
// SPDX-License-Identifier: Apache-2.0

package integration

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/blob/direct"
	"ocm.software/open-component-model/bindings/go/cli/cmd"
	"ocm.software/open-component-model/bindings/go/cli/integration/internal"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/oci"
	urlresolver "ocm.software/open-component-model/bindings/go/oci/resolver/url"
)

func Test_Integration_Signing_GPG(t *testing.T) {
	r := require.New(t)
	t.Parallel()

	registry, err := internal.CreateOCIRegistry(t)
	r.NoError(err)

	dir := t.TempDir()

	privKeyPath, pubKeyPath := writeGPGKeyPair(t, dir, "signing", "")

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
      type: GPG/v1alpha1
      signature: default
    credentials:
    - type: Credentials/v1
      properties:
        privateKeyPGPFile: %[5]q
        publicKeyPGPFile: %[6]q
- type: signing.config.ocm.software/v1alpha1
  signer:
    type: GPGSigningConfiguration/v1alpha1
  verifier:
    type: GPGSigningConfiguration/v1alpha1
`, registry.Host, registry.Port, registry.User, registry.Password, privKeyPath, pubKeyPath)
	cfgPath := filepath.Join(dir, "ocmconfig.yaml")
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

	t.Run("sign and verify component with GPG key", func(t *testing.T) {
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

		name, version := "ocm.software/test-component-gpg", "v1.0.0"
		uploadComponentVersion(t, repo, name, version, localResource)

		signArgs := []string{
			"sign", "cv",
			fmt.Sprintf("http://%s//%s:%s", registry.RegistryAddress, name, version),
			"--config", cfgPath,
		}

		verifyArgs := []string{
			"verify", "cv",
			fmt.Sprintf("http://%s//%s:%s", registry.RegistryAddress, name, version),
			"--config", cfgPath,
		}

		// dry-run: signature not persisted — verify must fail
		dryRunCMD := cmd.New()
		dryRunCMD.SetArgs(append(signArgs, "--dry-run"))
		r.NoError(dryRunCMD.ExecuteContext(t.Context()))

		verifyCMD := cmd.New()
		verifyCMD.SetArgs(verifyArgs)
		r.Error(verifyCMD.ExecuteContext(t.Context()), "verify must fail after dry-run only")

		// real sign — verify must succeed
		signCMD := cmd.New()
		signCMD.SetArgs(signArgs)
		r.NoError(signCMD.ExecuteContext(t.Context()))

		verifyCMD = cmd.New()
		verifyCMD.SetArgs(verifyArgs)
		r.NoError(verifyCMD.ExecuteContext(t.Context()))
	})

	t.Run("verify with wrong public key fails", func(t *testing.T) {
		r := require.New(t)

		localResource := resource{
			Resource: &descriptor.Resource{
				ElementMeta: descriptor.ElementMeta{
					ObjectMeta: descriptor.ObjectMeta{
						Name:    "raw-wrongkey",
						Version: "v1.0.0",
					},
				},
				Type:         "some-arbitrary-type-packed-in-image",
				Access:       &v2.LocalBlob{},
				CreationTime: descriptor.CreationTime(time.Now()),
			},
			ReadOnlyBlob: direct.NewFromBytes([]byte("foobar-wrongkey")),
		}

		name, version := "ocm.software/test-component-gpg-wrongkey", "v1.0.0"
		uploadComponentVersion(t, repo, name, version, localResource)

		// sign with the original key
		signCMD := cmd.New()
		signCMD.SetArgs([]string{
			"sign", "cv",
			fmt.Sprintf("http://%s//%s:%s", registry.RegistryAddress, name, version),
			"--config", cfgPath,
		})
		r.NoError(signCMD.ExecuteContext(t.Context()))

		// generate a different key pair and configure a verify-only config with it
		otherDir := t.TempDir()
		_, otherPubKeyPath := writeGPGKeyPair(t, otherDir, "other", "")

		wrongKeyCfg := fmt.Sprintf(`
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
      type: GPG/v1alpha1
      signature: default
    credentials:
    - type: Credentials/v1
      properties:
        publicKeyPGPFile: %[5]q
- type: signing.config.ocm.software/v1alpha1
  verifier:
    type: GPGSigningConfiguration/v1alpha1
`, registry.Host, registry.Port, registry.User, registry.Password, otherPubKeyPath)
		wrongKeyCfgPath := filepath.Join(otherDir, "ocmconfig-wrongkey.yaml")
		r.NoError(os.WriteFile(wrongKeyCfgPath, []byte(wrongKeyCfg), os.ModePerm))

		verifyCMD := cmd.New()
		verifyCMD.SetArgs([]string{
			"verify", "cv",
			fmt.Sprintf("http://%s//%s:%s", registry.RegistryAddress, name, version),
			"--config", wrongKeyCfgPath,
		})
		r.Error(verifyCMD.ExecuteContext(t.Context()), "verify must fail with mismatched public key")
	})
}

// writeGPGKeyPair generates an RSA key pair with the gpg binary and writes the ASCII-armored
// secret and public keys to <dir>/<name>.asc and <dir>/<name>.pub.asc.
// A non-empty passphrase protects the exported secret key.
func writeGPGKeyPair(t *testing.T, dir, name, passphrase string) (privPath, pubPath string) {
	t.Helper()
	r := require.New(t)
	// Not t.TempDir(): its long path can overflow the Unix socket path limit of gpg-agent on macOS.
	home, err := os.MkdirTemp("", "ocm-gpg-test-")
	r.NoError(err)
	t.Cleanup(func() {
		_ = exec.Command("gpgconf", "--homedir", home, "--kill", "all").Run()
		_ = os.RemoveAll(home)
	})
	gpg := func(args ...string) []byte {
		base := []string{"--batch", "--homedir", home, "--pinentry-mode", "loopback", "--passphrase", passphrase}
		var stderr bytes.Buffer
		c := exec.CommandContext(t.Context(), "gpg", append(base, args...)...)
		c.Stderr = &stderr
		out, err := c.Output()
		r.NoError(err, "gpg %v: %s", args, stderr.String())
		return out
	}

	uid := fmt.Sprintf("OCM Test %s <%s@example.com>", name, name)
	gpg("--quick-gen-key", uid, "rsa3072", "sign", "never")
	privPath = filepath.Join(dir, name+".asc")
	pubPath = filepath.Join(dir, name+".pub.asc")
	r.NoError(os.WriteFile(privPath, gpg("--armor", "--export-secret-keys", uid), 0o600))
	r.NoError(os.WriteFile(pubPath, gpg("--armor", "--export", uid), 0o600))
	return privPath, pubPath
}
