package cmd_test

import (
	"bytes"
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

// pinDigests computes both pins of the version the tag currently points to, independently of
// the CLI: the OCI digest of the artifact the tag resolves to, and the normalized pin over
// the jsonNormalisation/v4alpha1 bytes of its descriptor.
func pinDigests(t *testing.T, archive string) (ociDigest, normalized string) {
	t.Helper()
	r := require.New(t)
	fs, err := filesystem.NewFS(archive, os.O_RDONLY)
	r.NoError(err)
	store := ocictf.NewFromCTF(ctf.NewFileSystemCTF(fs))
	reference := store.ComponentVersionReference(t.Context(), pinComponent, pinVersion)
	repoStore, err := store.StoreForReference(t.Context(), reference)
	r.NoError(err)
	root, err := repoStore.Resolve(t.Context(), reference)
	r.NoError(err)

	repo, err := oci.NewRepository(ocictf.WithCTF(store))
	r.NoError(err)
	desc, err := repo.GetComponentVersion(t.Context(), pinComponent, pinVersion)
	r.NoError(err)
	normalised, err := normalisation.Normalise(desc, v4alpha1.Algorithm)
	r.NoError(err)
	sum := sha256.Sum256(normalised)
	return root.Digest.String(), "jsonNormalisation.v4alpha1+sha256:" + hex.EncodeToString(sum[:])
}

func Test_Pinned_Reference(t *testing.T) {
	archive := filepath.Join(t.TempDir(), "archive")
	addPinTestVersion(t, archive, "original", "first")
	ociDigest, normalized := pinDigests(t, archive)

	download := func(t *testing.T, ref string) (string, error) {
		t.Helper()
		output := filepath.Join(t.TempDir(), "data")
		_, err := test.OCM(t, test.WithArgs("download", "resource", archive+"//"+ref,
			"--identity", "name=data",
			"--output", output,
		))
		return output, err
	}
	requireDownloaded := func(t *testing.T, ref, want string) {
		t.Helper()
		output, err := download(t, ref)
		require.NoError(t, err)
		got, err := os.ReadFile(output)
		require.NoError(t, err)
		require.Equal(t, want, string(got))
	}
	versioned := pinComponent + ":" + pinVersion + "@"

	t.Run("OCI digest and normalized pins match", func(t *testing.T) {
		requireDownloaded(t, versioned+ociDigest, "original")
		requireDownloaded(t, versioned+normalized, "original")
	})

	t.Run("an OCI digest pin needs no version", func(t *testing.T) {
		r := require.New(t)
		requireDownloaded(t, pinComponent+"@"+ociDigest, "original")

		out := new(bytes.Buffer)
		_, err := test.OCM(t, test.WithArgs("get", "cv", archive+"//"+pinComponent+"@"+ociDigest, "-o", "yaml"), test.WithOutput(out))
		r.NoError(err)
		r.Contains(out.String(), "version: "+pinVersion)
	})

	t.Run("a normalized pin needs the version", func(t *testing.T) {
		_, err := download(t, pinComponent+"@"+normalized)
		require.ErrorContains(t, err, "has no version")
	})

	t.Run("a moved tag does not affect an OCI digest pin", func(t *testing.T) {
		r := require.New(t)
		addPinTestVersion(t, archive, "substituted", "second")
		moved, movedNormalized := pinDigests(t, archive)
		r.NotEqual(ociDigest, moved)
		r.NotEqual(normalized, movedNormalized)

		// The pinned artifact is still stored, so the pin keeps serving its content
		// (descriptor and local blob) instead of what the tag points to now.
		requireDownloaded(t, versioned+ociDigest, "original")
		requireDownloaded(t, pinComponent+":"+pinVersion, "substituted")

		output, err := download(t, versioned+normalized)
		r.ErrorContains(err, "does not match pinned digest "+normalized+": resolved digest is "+movedNormalized)
		r.NoFileExists(output)
	})

	t.Run("a transfer pinned to an OCI digest copies the pinned artifact", func(t *testing.T) {
		r := require.New(t)
		target := filepath.Join(t.TempDir(), "target")
		_, err := test.OCM(t,
			test.WithArgs("transfer", "component-version", archive+"//"+pinComponent+"@"+ociDigest, target),
			test.WithOutput(new(bytes.Buffer)),
		)
		r.NoError(err)

		// Descriptor and local blob both come from the pinned artifact, not from the moved tag.
		output := filepath.Join(t.TempDir(), "data")
		_, err = test.OCM(t, test.WithArgs("download", "resource", target+"//"+pinComponent+":"+pinVersion,
			"--identity", "name=data",
			"--output", output,
		))
		r.NoError(err)
		got, err := os.ReadFile(output)
		r.NoError(err)
		r.Equal("original", string(got))

		out := new(bytes.Buffer)
		_, err = test.OCM(t, test.WithArgs("get", "cv", target+"//"+pinComponent+":"+pinVersion, "-o", "yaml"), test.WithOutput(out))
		r.NoError(err)
		r.Contains(out.String(), "value: first", "the transferred descriptor is the pinned one")
	})

	t.Run("an unknown OCI digest reports what the tag resolves to", func(t *testing.T) {
		r := require.New(t)
		moved, _ := pinDigests(t, archive)
		unknown := "sha256:" + hex.EncodeToString(make([]byte, sha256.Size))

		output, err := download(t, versioned+unknown)
		r.ErrorContains(err, "does not match pinned digest "+unknown+": resolved digest is "+moved)
		r.NoFileExists(output)

		_, err = download(t, pinComponent+"@"+unknown)
		r.ErrorContains(err, "has no artifact with pinned digest "+unknown)
	})

	t.Run("an OCI digest pin must hold the named version", func(t *testing.T) {
		_, err := download(t, pinComponent+":2.0.0@"+ociDigest)
		require.ErrorContains(t, err, "holds component version "+pinComponent+":"+pinVersion+", not "+pinComponent+":2.0.0")
	})
}
