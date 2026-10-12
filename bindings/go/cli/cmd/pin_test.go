package cmd_test

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/blob/filesystem"
	"ocm.software/open-component-model/bindings/go/cli/cmd/internal/test"
	"ocm.software/open-component-model/bindings/go/ctf"
	"ocm.software/open-component-model/bindings/go/descriptor/normalisation"
	v4alpha1 "ocm.software/open-component-model/bindings/go/descriptor/normalisation/json/v4alpha1"
	"ocm.software/open-component-model/bindings/go/oci"
	ocictf "ocm.software/open-component-model/bindings/go/oci/ctf"
)

const (
	pinComponent = "ocm.software/pinned"
	pinVersion   = "1.0.0"
)

// addPinTestVersion (re)publishes pinComponent:pinVersion with a single local text resource
// and a component label that is not signed (and thus not normalised).
func addPinTestVersion(t *testing.T, archive, text, note string) {
	t.Helper()
	r := require.New(t)
	constructor := filepath.Join(t.TempDir(), "component-constructor.yaml")
	r.NoError(os.WriteFile(constructor, fmt.Appendf(nil, `
name: %s
version: %s
provider:
  name: ocm.software
labels:
  - name: note
    value: %q
resources:
  - name: data
    type: blob
    input:
      type: utf8/v1
      text: %q
`, pinComponent, pinVersion, note, text), 0o600))
	_, err := test.OCM(t, test.WithArgs(
		"add", "cv",
		"--constructor", constructor,
		"--repository", archive,
		"--component-version-conflict-policy", "replace",
	))
	r.NoError(err, "could not add component version")
}

// pinDigests computes both pins of the stored version independently of the CLI: the raw
// pin over the stored descriptor bytes and the normalized pin over the
// jsonNormalisation/v4alpha1 bytes.
func pinDigests(t *testing.T, archive string) (raw, normalized string) {
	t.Helper()
	r := require.New(t)
	fs, err := filesystem.NewFS(archive, os.O_RDONLY)
	r.NoError(err)
	repo, err := oci.NewRepository(ocictf.WithCTF(ocictf.NewFromCTF(ctf.NewFileSystemCTF(fs))))
	r.NoError(err)
	desc, stored, err := repo.GetStoredComponentDescriptor(t.Context(), pinComponent, pinVersion)
	r.NoError(err)
	normalised, err := normalisation.Normalise(desc, v4alpha1.Algorithm)
	r.NoError(err)
	rawSum, normalisedSum := sha256.Sum256(stored), sha256.Sum256(normalised)
	return "sha256:" + hex.EncodeToString(rawSum[:]),
		"jsonNormalisation.v4alpha1+sha256:" + hex.EncodeToString(normalisedSum[:])
}

func Test_Pinned_Reference(t *testing.T) {
	archive := filepath.Join(t.TempDir(), "archive")
	addPinTestVersion(t, archive, "original", "first")
	raw, normalized := pinDigests(t, archive)

	download := func(t *testing.T, pin string) (string, error) {
		t.Helper()
		output := filepath.Join(t.TempDir(), "data")
		_, err := test.OCM(t, test.WithArgs("download", "resource", archive+"//"+pinComponent+":"+pinVersion+"@"+pin,
			"--identity", "name=data",
			"--output", output,
		))
		return output, err
	}
	requireDownloaded := func(t *testing.T, pin string) {
		t.Helper()
		output, err := download(t, pin)
		require.NoError(t, err)
		got, err := os.ReadFile(output)
		require.NoError(t, err)
		require.Equal(t, "original", string(got))
	}

	t.Run("raw and normalized pins match", func(t *testing.T) {
		requireDownloaded(t, raw)
		requireDownloaded(t, normalized)
	})

	t.Run("unsigned change breaks only the raw pin", func(t *testing.T) {
		r := require.New(t)
		addPinTestVersion(t, archive, "original", "second")
		movedRaw, movedNormalized := pinDigests(t, archive)
		r.NotEqual(raw, movedRaw)
		r.Equal(normalized, movedNormalized)

		output, err := download(t, raw)
		r.ErrorContains(err, "does not match pinned digest "+raw+": resolved digest is "+movedRaw)
		r.NoFileExists(output)
		requireDownloaded(t, normalized)
	})

	t.Run("content change breaks the normalized pin", func(t *testing.T) {
		r := require.New(t)
		addPinTestVersion(t, archive, "substituted", "second")
		_, moved := pinDigests(t, archive)
		r.NotEqual(normalized, moved)

		output, err := download(t, normalized)
		r.ErrorContains(err, "does not match pinned digest "+normalized+": resolved digest is "+moved)
		r.NoFileExists(output)
	})
}
