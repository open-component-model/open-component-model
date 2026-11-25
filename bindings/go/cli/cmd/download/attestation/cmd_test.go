package attestation_test

import (
	"bytes"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/opencontainers/image-spec/specs-go"
	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/oras-project/oras-go/v3/content"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/blob/filesystem"
	"ocm.software/open-component-model/bindings/go/cli/cmd/internal/test"
	"ocm.software/open-component-model/bindings/go/ctf"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/oci"
	"ocm.software/open-component-model/bindings/go/oci/compref"
	ocictf "ocm.software/open-component-model/bindings/go/oci/ctf"
	ctfv1 "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/ctf"
	"ocm.software/open-component-model/bindings/go/runtime"
)

const (
	attestationMediaType = "application/vnd.in-toto+json"
	componentName        = "ocm.software/test-attestation"
	componentVersion     = "1.0.0"
	targetName           = "image"
)

// setupAttestedImage builds a CTF component whose single "image" resource is an OCI image
// index carrying one BuildKit in-toto SBOM attestation. It returns the component reference
// and the exact bytes of the stored in-toto statement, so a download can be checked
// byte-for-byte.
func setupAttestedImage(t *testing.T) (ref string, statementBytes []byte) {
	t.Helper()
	r := require.New(t)
	ctx := t.Context()

	archivePath := t.TempDir()
	fs, err := filesystem.NewFS(archivePath, os.O_RDWR)
	r.NoError(err)
	ctfStore := ocictf.NewFromCTF(ctf.NewFileSystemCTF(fs))
	repo, err := oci.NewRepository(ocictf.WithCTF(ctfStore))
	r.NoError(err)

	store, err := ctfStore.StoreForReference(ctx, ctfStore.ComponentVersionReference(ctx, componentName, componentVersion))
	r.NoError(err)

	push := func(mediaType string, value any) (ociImageSpecV1.Descriptor, []byte) {
		raw, err := json.Marshal(value, runtime.JSONOptions)
		r.NoError(err)
		desc := content.NewDescriptorFromBytes(mediaType, raw)
		r.NoError(store.Push(ctx, desc, bytes.NewReader(raw)))
		return desc, raw
	}

	empty := ociImageSpecV1.DescriptorEmptyJSON
	r.NoError(store.Push(ctx, empty, bytes.NewReader(empty.Data)))

	platform := ociImageSpecV1.Platform{OS: "linux", Architecture: "amd64"}
	config, _ := push(ociImageSpecV1.MediaTypeImageConfig, map[string]any{
		"architecture": platform.Architecture,
		"os":           platform.OS,
		"rootfs":       map[string]any{"type": "layers"},
	})
	image, _ := push(ociImageSpecV1.MediaTypeImageManifest, ociImageSpecV1.Manifest{
		Versioned: specs.Versioned{SchemaVersion: 2},
		MediaType: ociImageSpecV1.MediaTypeImageManifest,
		Config:    config,
		Layers:    []ociImageSpecV1.Descriptor{empty},
	})
	image.Platform = &platform

	statement, statementRaw := push(attestationMediaType, map[string]any{
		"_type":         "https://in-toto.io/Statement/v0.1",
		"predicateType": "https://spdx.dev/Document",
		"predicate":     jsontext.Value(spdx("attached")),
	})
	statement.Annotations = map[string]string{"in-toto.io/predicate-type": "https://spdx.dev/Document"}
	attested, _ := push(ociImageSpecV1.MediaTypeImageManifest, ociImageSpecV1.Manifest{
		Versioned: specs.Versioned{SchemaVersion: 2},
		MediaType: ociImageSpecV1.MediaTypeImageManifest,
		Config:    empty,
		Layers:    []ociImageSpecV1.Descriptor{statement},
	})
	attested.Platform = &ociImageSpecV1.Platform{OS: "unknown", Architecture: "unknown"}
	attested.Annotations = map[string]string{
		"vnd.docker.reference.type":   "attestation-manifest",
		"vnd.docker.reference.digest": image.Digest.String(),
	}

	index, _ := push(ociImageSpecV1.MediaTypeImageIndex, ociImageSpecV1.Index{
		Versioned: specs.Versioned{SchemaVersion: 2},
		MediaType: ociImageSpecV1.MediaTypeImageIndex,
		Manifests: []ociImageSpecV1.Descriptor{image, attested},
	})

	r.NoError(repo.AddComponentVersion(ctx, &descriptor.Descriptor{
		Meta: descriptor.Meta{Version: "v2"},
		Component: descriptor.Component{
			ComponentMeta: descriptor.ComponentMeta{
				ObjectMeta: descriptor.ObjectMeta{Name: componentName, Version: componentVersion},
			},
			Provider: descriptor.Provider{Name: "ocm.software"},
			Resources: []descriptor.Resource{{
				ElementMeta: descriptor.ElementMeta{
					ObjectMeta: descriptor.ObjectMeta{Name: targetName, Version: componentVersion},
				},
				Type:     "ociImage",
				Relation: descriptor.ExternalRelation,
				Access: &v2.LocalBlob{
					Type:           runtime.NewVersionedType(v2.LocalBlobAccessType, v2.LocalBlobAccessTypeVersion),
					LocalReference: index.Digest.String(),
					MediaType:      ociImageSpecV1.MediaTypeImageIndex,
				},
			}},
		},
	}))

	ref = (&compref.Ref{
		Repository: &ctfv1.Repository{FilePath: archivePath},
		Component:  componentName,
		Version:    componentVersion,
	}).String()
	return ref, statementRaw
}

// downloadAttestations runs the command into a fresh directory, returning that directory
// and the paths the command printed to stdout.
func downloadAttestations(t *testing.T, ref string, extra ...string) (string, []string, error) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "attestations")
	var out bytes.Buffer
	args := append([]string{"download", "attestation", ref, "--identity", "name=" + targetName, "--output", dir}, extra...)
	_, err := test.OCM(t, test.WithArgs(args...), test.WithOutput(&out))

	var printed []string
	for _, line := range strings.Split(strings.TrimSpace(out.String()), "\n") {
		if line != "" {
			printed = append(printed, line)
		}
	}
	return dir, printed, err
}

func TestDownloadAttestation_FetchesInTotoStatementByteForByte(t *testing.T) {
	r := require.New(t)
	ref, wantStatement := setupAttestedImage(t)

	dir, printed, err := downloadAttestations(t, ref)
	r.NoError(err)
	r.Len(printed, 1, "the one in-toto attestation is written to exactly one file")

	// The printed path is the file that was written.
	assert.Equal(t, dir, filepath.Dir(printed[0]))
	assert.Equal(t, ".json", filepath.Ext(printed[0]), "an in-toto JSON statement gets a .json extension")

	got, err := os.ReadFile(printed[0])
	r.NoError(err)
	assert.NotEmpty(t, got)
	assert.Equal(t, wantStatement, got, "the written document is the stored in-toto statement, byte for byte")

	// Only the one file exists in the output directory.
	assert.Equal(t, []string{filepath.Base(printed[0])}, names(t, dir))
}

func TestDownloadAttestation_TypeFilter(t *testing.T) {
	ref, _ := setupAttestedImage(t)

	t.Run("a matching type writes the document", func(t *testing.T) {
		r := require.New(t)
		_, printed, err := downloadAttestations(t, ref, "--type", "sbom")
		r.NoError(err)
		assert.Len(t, printed, 1, "the sbom alias keeps the SPDX in-toto attestation")
	})

	t.Run("a type matching nothing writes nothing", func(t *testing.T) {
		r := require.New(t)
		dir, printed, err := downloadAttestations(t, ref, "--type", "does-not-exist")
		r.NoError(err)
		assert.Empty(t, printed, "nothing is printed when the filter matches no attestation")
		assert.Empty(t, names(t, dir), "no files are written when the filter matches no attestation")
	})
}

// names lists the file names in dir.
func names(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	found := make([]string, 0, len(entries))
	for _, entry := range entries {
		found = append(found, entry.Name())
	}
	return found
}

// spdx is a minimal but complete SPDX 2.3 document, used as the in-toto predicate.
func spdx(name string) string {
	return `{
  "spdxVersion": "SPDX-2.3",
  "SPDXID": "SPDXRef-DOCUMENT",
  "name": "` + name + `",
  "documentNamespace": "https://example.com/` + name + `",
  "creationInfo": {"created": "2026-01-01T00:00:00Z", "creators": ["Tool: test"]},
  "packages": [{
    "SPDXID": "SPDXRef-Package-` + name + `",
    "name": "pkg-` + name + `",
    "versionInfo": "1.0.0",
    "downloadLocation": "NOASSERTION"
  }]
}`
}
