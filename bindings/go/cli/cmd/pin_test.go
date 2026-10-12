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

// addPinTestVersion (re)publishes pinComponent:pinVersion with a single local text resource.
func addPinTestVersion(t *testing.T, archive, text string) {
	t.Helper()
	r := require.New(t)
	constructor := filepath.Join(t.TempDir(), "component-constructor.yaml")
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
      text: %q
`, pinComponent, pinVersion, text), 0o600))
	_, err := test.OCM(t, test.WithArgs(
		"add", "cv",
		"--constructor", constructor,
		"--repository", archive,
		"--component-version-conflict-policy", "replace",
	))
	r.NoError(err, "could not add component version")
}

// componentVersionDigest computes the OCM component version digest of the stored
// version independently of the CLI: sha256 over the jsonNormalisation/v4alpha1 bytes.
func componentVersionDigest(t *testing.T, archive string) string {
	t.Helper()
	r := require.New(t)
	fs, err := filesystem.NewFS(archive, os.O_RDONLY)
	r.NoError(err)
	repo, err := oci.NewRepository(ocictf.WithCTF(ocictf.NewFromCTF(ctf.NewFileSystemCTF(fs))))
	r.NoError(err)
	desc, err := repo.GetComponentVersion(t.Context(), pinComponent, pinVersion)
	r.NoError(err)
	normalised, err := normalisation.Normalise(desc, v4alpha1.Algorithm)
	r.NoError(err)
	sum := sha256.Sum256(normalised)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func pinnedRef(archive, digest string) string {
	return archive + "//" + pinComponent + ":" + pinVersion + "@" + digest
}

func Test_Pinned_Reference(t *testing.T) {
	archive := filepath.Join(t.TempDir(), "archive")
	addPinTestVersion(t, archive, "original")
	original := componentVersionDigest(t, archive)

	download := func(t *testing.T, pin string) (string, error) {
		t.Helper()
		output := filepath.Join(t.TempDir(), "data")
		_, err := test.OCM(t, test.WithArgs("download", "resource", pinnedRef(archive, pin),
			"--identity", "name=data",
			"--output", output,
		))
		return output, err
	}

	t.Run("matching pin", func(t *testing.T) {
		r := require.New(t)
		output, err := download(t, original)
		r.NoError(err)
		got, err := os.ReadFile(output)
		r.NoError(err)
		r.Equal("original", string(got))
	})

	t.Run("pin taken before the tag moved", func(t *testing.T) {
		r := require.New(t)
		addPinTestVersion(t, archive, "substituted")
		moved := componentVersionDigest(t, archive)
		r.NotEqual(original, moved)

		output, err := download(t, original)
		r.ErrorContains(err, "does not match pinned digest "+original)
		r.ErrorContains(err, "resolved digest is "+moved)
		r.NoFileExists(output)
	})
}
