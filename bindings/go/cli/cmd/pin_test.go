package cmd_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
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

// newPinArchive creates a fresh CTF archive with pinComponent:pinVersion holding text "original"
// and returns the archive path plus both digest pins.
func newPinArchive(t *testing.T) (archive, ociDigest, normalized string) {
	t.Helper()
	archive = filepath.Join(t.TempDir(), "archive")
	addPinTestVersion(t, archive, "original", "first")
	ociDigest, normalized = pinDigests(t, archive)
	return archive, ociDigest, normalized
}

// downloadPinResource downloads the "data" resource from archive//ref.
func downloadPinResource(t *testing.T, archive, ref string) (string, error) {
	t.Helper()
	output := filepath.Join(t.TempDir(), "data")
	_, err := test.OCM(t, test.WithArgs("download", "resource", archive+"//"+ref,
		"--identity", "name=data",
		"--output", output,
	))
	return output, err
}

// requirePinDownloaded asserts downloading ref from archive yields want.
func requirePinDownloaded(t *testing.T, archive, ref, want string) {
	t.Helper()
	r := require.New(t)
	output, err := downloadPinResource(t, archive, ref)
	r.NoError(err)
	got, err := os.ReadFile(output)
	r.NoError(err)
	r.Equal(want, string(got))
}

// movePinTag adds a second version to archive, moving the tag away from the original.
// It returns the new OCI and normalized digests the tag now points to.
func movePinTag(t *testing.T, archive string) (moved, movedNormalized string) {
	t.Helper()
	addPinTestVersion(t, archive, "substituted", "second")
	return pinDigests(t, archive)
}

// signPinVersion signs the version the tag currently points to in archive with the given config.
func signPinVersion(t *testing.T, archive, config string) {
	t.Helper()
	r := require.New(t)
	current, _ := pinDigests(t, archive)
	versioned := pinComponent + ":" + pinVersion + "@"
	_, err := test.OCM(t,
		test.WithArgs("sign", "component-version", archive+"//"+versioned+current,
			"--signature", "pin", "--config", config),
		test.WithOutput(new(bytes.Buffer)),
	)
	r.NoError(err)
}

func Test_Pinned_Reference(t *testing.T) {
	versioned := pinComponent + ":" + pinVersion + "@"

	t.Run("OCI digest and normalized pins match", func(t *testing.T) {
		archive, ociDigest, normalized := newPinArchive(t)
		requirePinDownloaded(t, archive, versioned+ociDigest, "original")
		requirePinDownloaded(t, archive, versioned+normalized, "original")
	})

	t.Run("an OCI digest pin needs no version", func(t *testing.T) {
		r := require.New(t)
		archive, ociDigest, _ := newPinArchive(t)
		requirePinDownloaded(t, archive, pinComponent+"@"+ociDigest, "original")

		out := new(bytes.Buffer)
		_, err := test.OCM(t, test.WithArgs("get", "cv", archive+"//"+pinComponent+"@"+ociDigest, "-o", "yaml"), test.WithOutput(out))
		r.NoError(err)
		r.Contains(out.String(), "version: "+pinVersion)
	})

	t.Run("a normalized pin needs the version", func(t *testing.T) {
		r := require.New(t)
		archive, _, normalized := newPinArchive(t)
		_, err := downloadPinResource(t, archive, pinComponent+"@"+normalized)
		r.ErrorContains(err, "has no version")
	})

	t.Run("a moved tag does not affect an OCI digest pin", func(t *testing.T) {
		r := require.New(t)
		archive, ociDigest, normalized := newPinArchive(t)
		moved, movedNormalized := movePinTag(t, archive)
		r.NotEqual(ociDigest, moved)
		r.NotEqual(normalized, movedNormalized)

		// The pinned artifact is still stored, so the pin keeps serving its content
		// (descriptor and local blob) instead of what the tag points to now.
		requirePinDownloaded(t, archive, versioned+ociDigest, "original")
		requirePinDownloaded(t, archive, pinComponent+":"+pinVersion, "substituted")

		output, err := downloadPinResource(t, archive, versioned+normalized)
		r.ErrorContains(err, "does not match pinned digest "+normalized+": resolved digest is "+movedNormalized)
		r.NoFileExists(output)
	})

	t.Run("a transfer pinned to an OCI digest copies the pinned artifact", func(t *testing.T) {
		r := require.New(t)
		archive, ociDigest, _ := newPinArchive(t)
		movePinTag(t, archive)

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
		archive, current, _ := newPinArchive(t)
		unknown := "sha256:" + hex.EncodeToString(make([]byte, sha256.Size))

		output, err := downloadPinResource(t, archive, versioned+unknown)
		r.ErrorContains(err, "does not match pinned digest "+unknown+": resolved digest is "+current)
		r.NoFileExists(output)

		_, err = downloadPinResource(t, archive, pinComponent+"@"+unknown)
		r.ErrorContains(err, "has no artifact with pinned digest "+unknown)
	})

	t.Run("an OCI digest pin must hold the named version", func(t *testing.T) {
		r := require.New(t)
		archive, ociDigest, _ := newPinArchive(t)
		_, err := downloadPinResource(t, archive, pinComponent+":2.0.0@"+ociDigest)
		r.ErrorContains(err, "holds component version "+pinComponent+":"+pinVersion+", not "+pinComponent+":2.0.0")
	})

	t.Run("signing an OCI digest pin does not overwrite a moved tag", func(t *testing.T) {
		r := require.New(t)
		archive, ociDigest, _ := newPinArchive(t)
		movePinTag(t, archive)
		config := pinSigningConfig(t)

		sign := func(ref string) error {
			_, err := test.OCM(t,
				test.WithArgs("sign", "component-version", archive+"//"+ref, "--signature", "pin", "--config", config),
				test.WithOutput(new(bytes.Buffer)),
			)
			return err
		}
		current, _ := pinDigests(t, archive)
		r.NotEqual(ociDigest, current, "the tag moved")

		r.ErrorContains(sign(versioned+ociDigest), "refusing to overwrite the moved tag")
		after, _ := pinDigests(t, archive)
		r.Equal(current, after, "the tag still resolves to what it resolved to before")

		r.NoError(sign(versioned + current))
		_, err := test.OCM(t, test.WithArgs("verify", "component-version", archive+"//"+pinComponent+":"+pinVersion,
			"--signature", "pin", "--config", config), test.WithOutput(new(bytes.Buffer)))
		r.NoError(err, "the signature was written to the version the tag resolved to")
	})

	t.Run("verify reports a reference pinned to the verified artifact", func(t *testing.T) {
		r := require.New(t)
		archive, _, _ := newPinArchive(t)
		config := pinSigningConfig(t)
		signPinVersion(t, archive, config)

		logs := test.NewJSONLogReader()
		_, err := test.OCM(t, test.WithArgs("verify", "component-version", archive+"//"+pinComponent+":"+pinVersion,
			"--signature", "pin", "--config", config), test.WithOutput(new(bytes.Buffer)), test.WithErrorOutput(logs))
		r.NoError(err)
		verified, _ := pinDigests(t, archive)
		entries, err := logs.List()
		r.NoError(err)
		var reference string
		for _, entry := range entries {
			if entry.Msg == "SIGNATURE VERIFICATION SUCCESSFUL" {
				reference, _ = entry.Extras["reference"].(string)
			}
		}
		// compref normalizes Windows path separators to forward slashes.
		r.True(strings.HasSuffix(reference, filepath.ToSlash(archive)+"//"+pinComponent+":"+pinVersion+"@"+verified), "reference %q pins the verified artifact", reference)

		// Tamper with the tag between verify and use: the reference still names the verified content.
		addPinTestVersion(t, archive, "tampered", "third")
		target := filepath.Join(t.TempDir(), "target")
		_, err = test.OCM(t,
			test.WithArgs("transfer", "component-version", reference, target),
			test.WithOutput(new(bytes.Buffer)),
		)
		r.NoError(err)
		_, err = test.OCM(t, test.WithArgs("verify", "component-version", target+"//"+pinComponent+":"+pinVersion,
			"--signature", "pin", "--config", config), test.WithOutput(new(bytes.Buffer)))
		r.NoError(err, "the transferred component version is the verified, signed one")
		output := filepath.Join(t.TempDir(), "data")
		_, err = test.OCM(t, test.WithArgs("download", "resource", target+"//"+pinComponent+":"+pinVersion,
			"--identity", "name=data",
			"--output", output,
		))
		r.NoError(err)
		got, err := os.ReadFile(output)
		r.NoError(err)
		r.Equal("original", string(got))
	})
}

// pinSigningConfig writes an OCM configuration with RSA credentials for the signature "pin".
func pinSigningConfig(t *testing.T) string {
	t.Helper()
	r := require.New(t)
	key := mustKey(t)
	privateKey, publicKey := writeKeyAndChain(t, t.TempDir(), key, mustSelfSigned(t, "CN=signer", key))
	config := filepath.Join(t.TempDir(), "ocm-config.yaml")
	r.NoError(os.WriteFile(config, fmt.Appendf(nil, `
type: generic.config.ocm.software/v1
configurations:
- type: credentials.config.ocm.software
  consumers:
  - identity:
      type: RSA/v1alpha1
      algorithm: RSASSA-PSS
      signature: pin
    credentials:
    - type: Credentials/v1
      properties:
        public_key_pem_file: %s
        private_key_pem_file: %s
`, publicKey, privateKey), 0o600))
	return config
}
