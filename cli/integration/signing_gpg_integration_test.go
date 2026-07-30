// SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company and Open Component Model contributors.
//
// SPDX-License-Identifier: Apache-2.0

package integration

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ProtonMail/go-crypto/openpgp"
	"github.com/ProtonMail/go-crypto/openpgp/armor"
	"github.com/ProtonMail/go-crypto/openpgp/packet"
	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/blob/direct"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/oci"
	urlresolver "ocm.software/open-component-model/bindings/go/oci/resolver/url"
	"ocm.software/open-component-model/cli/cmd"
	"ocm.software/open-component-model/cli/integration/internal"
)

func Test_Integration_Signing_GPG(t *testing.T) {
	r := require.New(t)
	t.Parallel()

	registry, err := internal.CreateOCIRegistry(t)
	r.NoError(err)

	dir := t.TempDir()

	entity := mustGPGEntity(t)
	privKeyPath := filepath.Join(dir, "signing-key.asc")
	pubKeyPath := filepath.Join(dir, "verify-key.asc")
	writeArmoredPrivKey(t, entity, privKeyPath)
	writeArmoredPubKey(t, entity, pubKeyPath)

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
`, registry.Host, registry.Port, registry.User, registry.Password, privKeyPath, pubKeyPath)
	cfgPath := filepath.Join(dir, "ocmconfig.yaml")
	r.NoError(os.WriteFile(cfgPath, []byte(cfg), os.ModePerm))

	gpgSpec := "type: GPGSigningConfiguration/v1alpha1\n"
	gpgSpecPath := filepath.Join(dir, "gpg-spec.yaml")
	r.NoError(os.WriteFile(gpgSpecPath, []byte(gpgSpec), os.ModePerm))

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
			"--signer-spec", gpgSpecPath,
		}

		verifyArgs := []string{
			"verify", "cv",
			fmt.Sprintf("http://%s//%s:%s", registry.RegistryAddress, name, version),
			"--config", cfgPath,
			"--verifier-spec", gpgSpecPath,
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
			"--signer-spec", gpgSpecPath,
		})
		r.NoError(signCMD.ExecuteContext(t.Context()))

		// generate a different key pair and configure a verify-only config with it
		otherEntity := mustGPGEntity(t)
		otherDir := t.TempDir()
		otherPubKeyPath := filepath.Join(otherDir, "other-verify.asc")
		writeArmoredPubKey(t, otherEntity, otherPubKeyPath)

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
`, registry.Host, registry.Port, registry.User, registry.Password, otherPubKeyPath)
		wrongKeyCfgPath := filepath.Join(otherDir, "ocmconfig-wrongkey.yaml")
		r.NoError(os.WriteFile(wrongKeyCfgPath, []byte(wrongKeyCfg), os.ModePerm))

		verifyCMD := cmd.New()
		verifyCMD.SetArgs([]string{
			"verify", "cv",
			fmt.Sprintf("http://%s//%s:%s", registry.RegistryAddress, name, version),
			"--config", wrongKeyCfgPath,
			"--verifier-spec", gpgSpecPath,
		})
		r.Error(verifyCMD.ExecuteContext(t.Context()), "verify must fail with mismatched public key")
	})
}

func mustGPGEntity(t *testing.T) *openpgp.Entity {
	t.Helper()
	entity, err := openpgp.NewEntity("ocm-test", "", "ocm-test@example.com", &packet.Config{RSABits: 2048})
	require.NoError(t, err)
	return entity
}

func writeArmoredPrivKey(t *testing.T, entity *openpgp.Entity, path string) {
	t.Helper()
	var buf bytes.Buffer
	w, err := armor.Encode(&buf, openpgp.PrivateKeyType, nil)
	require.NoError(t, err)
	require.NoError(t, entity.SerializePrivateWithoutSigning(w, nil))
	require.NoError(t, w.Close())
	require.NoError(t, os.WriteFile(path, buf.Bytes(), 0o600))
}

func writeArmoredPubKey(t *testing.T, entity *openpgp.Entity, path string) {
	t.Helper()
	var buf bytes.Buffer
	w, err := armor.Encode(&buf, openpgp.PublicKeyType, nil)
	require.NoError(t, err)
	require.NoError(t, entity.Serialize(w))
	require.NoError(t, w.Close())
	require.NoError(t, os.WriteFile(path, buf.Bytes(), 0o600))
}
