// SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company and Open Component Model contributors.
//
// SPDX-License-Identifier: Apache-2.0

package integration

import (
	"debug/buildinfo"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	tcexec "github.com/testcontainers/testcontainers-go/exec"
	"github.com/testcontainers/testcontainers-go/log"
)

const gpgFIPSTestImage = "debian:trixie-slim"

// errGPGNotFoundText mirrors gpgbinary.ErrGPGNotFound, which is internal to the GPG handler.
const errGPGNotFoundText = `GPG signing in FIPS 140-3 mode requires the GnuPG "gpg" binary (>= 2.2.0) on PATH backed by a FIPS 140-3 validated libgcrypt; install it or set GODEBUG=fips140=off to use the built-in non-FIPS OpenPGP implementation`

// Test_Integration_Signing_GPG_FIPS runs a GOFIPS140 build of the CLI against a GnuPG whose
// libgcrypt runs in FIPS mode and checks that GPG signing is delegated to it and that its
// signatures interoperate with the built-in go-crypto implementation.
func Test_Integration_Signing_GPG_FIPS(t *testing.T) {
	r := require.New(t)
	t.Parallel()

	dir := t.TempDir()

	goBin, err := exec.LookPath("go")
	r.NoError(err)
	bin := filepath.Join(dir, "ocm")
	build := exec.CommandContext(t.Context(), goBin, "build", "-o", bin, "ocm.software/open-component-model/bindings/go/cli")
	build.Env = append(os.Environ(), "GOOS=linux", "GOARCH="+runtime.GOARCH, "CGO_ENABLED=0", "GOFIPS140=v1.0.0")
	out, err := build.CombinedOutput()
	r.NoError(err, "build FIPS CLI: %s", out)
	info, err := buildinfo.ReadFile(bin)
	r.NoError(err)
	var fipsSetting string
	for _, s := range info.Settings {
		if s.Key == "GOFIPS140" {
			fipsSetting = s.Value
		}
	}
	r.True(strings.HasPrefix(fipsSetting, "v1.0.0"), "GOFIPS140 build setting %q", fipsSetting)

	protected := mustGPGEntity(t)
	writeArmoredPubKey(t, protected, filepath.Join(dir, "protected.pub.asc"))
	r.NoError(protected.EncryptPrivateKeys([]byte("fips-test-passphrase"), nil))
	writeArmoredPrivKey(t, protected, filepath.Join(dir, "protected.asc"))
	plain := mustGPGEntity(t)
	writeArmoredPrivKey(t, plain, filepath.Join(dir, "plain.asc"))
	writeArmoredPubKey(t, plain, filepath.Join(dir, "plain.pub.asc"))
	writeArmoredPubKey(t, mustGPGEntity(t), filepath.Join(dir, "other.pub.asc"))

	hostFiles := map[string]string{
		"constructor.yaml": `
components:
- name: ocm.software/test-gpg-fips
  version: v1.0.0
  provider:
    name: ocm.software
  resources:
  - name: text
    version: v1.0.0
    type: plainText
    input:
      type: utf8
      text: "signed in FIPS 140-3 mode"
`,
		"ocmconfig.yaml": `
type: generic.config.ocm.software/v1
configurations:
- type: credentials.config.ocm.software
  consumers:
  - identity:
      type: GPG/v1alpha1
      signature: fips
    credentials:
    - type: Credentials/v1
      properties:
        privateKeyPGPFile: /work/protected.asc
        publicKeyPGPFile: /work/protected.pub.asc
        passphrase: fips-test-passphrase
  - identity:
      type: GPG/v1alpha1
      signature: gocrypto
    credentials:
    - type: Credentials/v1
      properties:
        privateKeyPGPFile: /work/plain.asc
        publicKeyPGPFile: /work/plain.pub.asc
- type: signing.config.ocm.software/v1alpha1
  signer:
    type: GPGSigningConfiguration/v1alpha1
  verifier:
    type: GPGSigningConfiguration/v1alpha1
`,
		"ocmconfig-wrongkey.yaml": `
type: generic.config.ocm.software/v1
configurations:
- type: credentials.config.ocm.software
  consumers:
  - identity:
      type: GPG/v1alpha1
      signature: fips
    credentials:
    - type: Credentials/v1
      properties:
        publicKeyPGPFile: /work/other.pub.asc
- type: signing.config.ocm.software/v1alpha1
  verifier:
    type: GPGSigningConfiguration/v1alpha1
`,
	}
	for name, content := range hostFiles {
		r.NoError(os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600))
	}

	files := []testcontainers.ContainerFile{{HostFilePath: bin, ContainerFilePath: "/usr/local/bin/ocm", FileMode: 0o755}}
	for _, name := range []string{"protected.asc", "protected.pub.asc", "plain.asc", "plain.pub.asc", "other.pub.asc", "constructor.yaml", "ocmconfig.yaml", "ocmconfig-wrongkey.yaml"} {
		files = append(files, testcontainers.ContainerFile{HostFilePath: filepath.Join(dir, name), ContainerFilePath: "/work/" + name, FileMode: 0o644})
	}
	c, err := testcontainers.Run(t.Context(), gpgFIPSTestImage,
		testcontainers.WithFiles(files...),
		testcontainers.WithCmd("sleep", "infinity"),
		testcontainers.WithLogger(log.TestLogger(t)),
	)
	r.NoError(err)
	t.Cleanup(func() { _ = testcontainers.TerminateContainer(c) })

	run := func(t *testing.T, env []string, cmd ...string) (int, string) {
		t.Helper()
		code, reader, err := c.Exec(t.Context(), cmd, tcexec.Multiplexed(), tcexec.WithEnv(env))
		require.NoError(t, err)
		output, err := io.ReadAll(reader)
		require.NoError(t, err)
		return code, string(output)
	}

	code, output := run(t, nil, "sh", "-c", "apt-get update -qq && DEBIAN_FRONTEND=noninteractive apt-get install -y -qq gnupg && mkdir -p /etc/gcrypt && touch /etc/gcrypt/fips_enabled")
	r.Zero(code, output)
	code, output = run(t, nil, "gpgconf", "--show-versions")
	r.Zero(code, output)
	r.Contains(output, "fips-mode:y", "libgcrypt must run in FIPS mode")

	const (
		ocm = "/usr/local/bin/ocm"
		ref = "ctf::/work/ctf//ocm.software/test-gpg-fips:v1.0.0"
		cfg = "/work/ocmconfig.yaml"
	)
	fipsOff := "GODEBUG=fips140=off"
	noPath := "PATH=/nonexistent"

	scenarios := []struct {
		name    string
		env     []string
		cmd     []string
		wantOK  bool
		wantOut string
	}{
		{name: "add component version", cmd: []string{ocm, "add", "cv", "--repository", "ctf::/work/ctf", "--constructor", "/work/constructor.yaml"}, wantOK: true},
		{name: "sign with gpg backend and protected key", cmd: []string{ocm, "sign", "cv", ref, "--signature", "fips", "--config", cfg}, wantOK: true},
		{name: "verify with gpg backend", cmd: []string{ocm, "verify", "cv", ref, "--signature", "fips", "--config", cfg}, wantOK: true},
		{name: "verify gpg signature with go-crypto", env: []string{fipsOff}, cmd: []string{ocm, "verify", "cv", ref, "--signature", "fips", "--config", cfg}, wantOK: true},
		{name: "sign with go-crypto", env: []string{fipsOff}, cmd: []string{ocm, "sign", "cv", ref, "--signature", "gocrypto", "--config", cfg}, wantOK: true},
		{name: "verify go-crypto signature with gpg backend", cmd: []string{ocm, "verify", "cv", ref, "--signature", "gocrypto", "--config", cfg}, wantOK: true},
		{name: "verify with wrong public key fails", cmd: []string{ocm, "verify", "cv", ref, "--signature", "fips", "--config", "/work/ocmconfig-wrongkey.yaml"}},
		{
			name:    "missing gpg is reported",
			env:     []string{noPath},
			cmd:     []string{ocm, "sign", "cv", ref, "--signature", "fips", "--config", cfg, "--dry-run", "--force"},
			wantOut: errGPGNotFoundText,
		},
		{
			name:   "fips140=off opts out of the gpg backend",
			env:    []string{noPath, fipsOff},
			cmd:    []string{ocm, "sign", "cv", ref, "--signature", "fips", "--config", cfg, "--dry-run", "--force"},
			wantOK: true,
		},
	}
	for _, s := range scenarios {
		t.Run(s.name, func(t *testing.T) {
			r := require.New(t)
			code, output := run(t, s.env, s.cmd...)
			if s.wantOK {
				r.Zero(code, output)
			} else {
				r.NotZero(code, output)
			}
			if s.wantOut != "" {
				r.Contains(output, s.wantOut)
			}
		})
	}
}
