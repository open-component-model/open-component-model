// resource_test disclaimer: some parts of these tests have been generated.
package resource_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/opencontainers/go-digest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/blob/filesystem"
	"ocm.software/open-component-model/bindings/go/blob/inmemory"
	"ocm.software/open-component-model/bindings/go/ctf"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	artifactref "ocm.software/open-component-model/bindings/go/descriptor/runtime/labels/artifactref/v1alpha1"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/oci"
	"ocm.software/open-component-model/bindings/go/oci/compref"
	ocictf "ocm.software/open-component-model/bindings/go/oci/ctf"
	ctfv1 "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/ctf"
	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/cli/cmd/internal/test"
)

const (
	componentName    = "ocm.software/test-sbom"
	componentVersion = "1.0.0"
	targetName       = "image"
)

type resourceSpec struct {
	name          string
	resourceType  string
	extraIdentity runtime.Identity
	describes     runtime.Identity
	content       string
}

func setupComponent(t *testing.T, specs ...resourceSpec) string {
	t.Helper()
	r := require.New(t)

	archivePath := t.TempDir()
	fs, err := filesystem.NewFS(archivePath, os.O_RDWR)
	r.NoError(err)
	repo, err := oci.NewRepository(ocictf.WithCTF(ocictf.NewFromCTF(ctf.NewFileSystemCTF(fs))))
	r.NoError(err)

	desc := &descriptor.Descriptor{
		Meta: descriptor.Meta{Version: "v2"},
		Component: descriptor.Component{
			ComponentMeta: descriptor.ComponentMeta{
				ObjectMeta: descriptor.ObjectMeta{Name: componentName, Version: componentVersion},
			},
			Provider: descriptor.Provider{Name: "ocm.software"},
		},
	}

	for _, spec := range specs {
		content := []byte(spec.content)
		resource := &descriptor.Resource{
			ElementMeta: descriptor.ElementMeta{
				ObjectMeta:    descriptor.ObjectMeta{Name: spec.name, Version: componentVersion},
				ExtraIdentity: spec.extraIdentity,
			},
			Type:     spec.resourceType,
			Relation: descriptor.LocalRelation,
			Access: &v2.LocalBlob{
				LocalReference: digest.FromBytes(content).String(),
				MediaType:      "application/json",
			},
		}

		if spec.describes != nil {
			value, err := json.Marshal(artifactref.Reference{Identity: spec.describes})
			r.NoError(err)
			resource.Labels = []descriptor.Label{{
				Name:    artifactref.LabelName,
				Version: artifactref.Version,
				Value:   value,
			}}
		}

		added, err := repo.AddLocalResource(t.Context(), componentName, componentVersion,
			resource, inmemory.New(bytes.NewReader(content)))
		r.NoError(err)
		desc.Component.Resources = append(desc.Component.Resources, *added)
	}

	r.NoError(repo.AddComponentVersion(t.Context(), desc))

	ref := &compref.Ref{
		Repository: &ctfv1.Repository{FilePath: archivePath},
		Component:  componentName,
		Version:    componentVersion,
	}
	return ref.String()
}

func target() resourceSpec {
	return resourceSpec{name: targetName, resourceType: "blob", content: "the artifact itself"}
}

func describesTarget() runtime.Identity {
	return runtime.Identity{"name": targetName, "version": componentVersion}
}

// spdx is a minimal but complete SPDX 2.3 document.
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
  }],
  "relationships": [{
    "spdxElementId": "SPDXRef-DOCUMENT",
    "relatedSpdxElement": "SPDXRef-Package-` + name + `",
    "relationshipType": "DESCRIBES"
  }]
}`
}

// cyclonedx is a minimal but complete CycloneDX 1.6 document.
func cyclonedx(name string) string {
	return `{
  "bomFormat": "CycloneDX",
  "specVersion": "1.6",
  "version": 1,
  "metadata": {"component": {"type": "application", "name": "` + name + `"}},
  "components": [{"type": "library", "name": "pkg-` + name + `", "version": "1.0.0"}]
}`
}

// downloadSBOMs runs the command into a fresh directory, returning that directory and
// the paths the command printed to stdout.
func downloadSBOMs(t *testing.T, ref string, extra ...string) (string, []string, error) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "sboms")
	var out bytes.Buffer
	args := append([]string{"download", "resource", ref, "--identity", "name=" + targetName, "--sbom", "--output", dir}, extra...)
	_, err := test.OCM(t, test.WithArgs(args...), test.WithOutput(&out))

	var printed []string
	for line := range strings.SplitSeq(strings.TrimSpace(out.String()), "\n") {
		if line != "" {
			printed = append(printed, line)
		}
	}
	return dir, printed, err
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

func TestDownloadResourceSBOM_artifactReference(t *testing.T) {
	t.Run("writes the sbom a sibling resource declares it describes", func(t *testing.T) {
		ref := setupComponent(t,
			target(),
			resourceSpec{
				name:         "image-sbom",
				resourceType: "sbom",
				describes:    describesTarget(),
				content:      spdx("only"),
			},
		)

		dir, printed, err := downloadSBOMs(t, ref)
		require.NoError(t, err)

		require.Equal(t, []string{"image-sbom.spdx.json"}, names(t, dir))
		assert.Equal(t, []string{filepath.Join(dir, "image-sbom.spdx.json")}, printed,
			"the paths written go to stdout so they can be piped onwards")
	})

	t.Run("keeps the published bytes exactly", func(t *testing.T) {
		// The whole reason the combined document was dropped: a round trip through an
		// intermediate representation loses information, and once the bytes change no
		// digest or signature over the original applies any more.
		document := spdx("only")
		ref := setupComponent(t,
			target(),
			resourceSpec{name: "image-sbom", resourceType: "sbom", describes: describesTarget(), content: document},
		)

		dir, _, err := downloadSBOMs(t, ref)
		require.NoError(t, err)

		content, err := os.ReadFile(filepath.Join(dir, "image-sbom.spdx.json"))
		require.NoError(t, err)
		assert.Equal(t, document, string(content), "the document must be byte for byte what was published")
	})

	t.Run("writes one file per describing sbom", func(t *testing.T) {
		ref := setupComponent(t,
			target(),
			resourceSpec{
				name:          "image-sbom",
				resourceType:  "sbom",
				extraIdentity: runtime.Identity{"os": "linux", "architecture": "amd64"},
				describes:     describesTarget(),
				content:       spdx("amd64"),
			},
			resourceSpec{
				name:          "image-sbom",
				resourceType:  "sbom",
				extraIdentity: runtime.Identity{"os": "linux", "architecture": "arm64"},
				describes:     describesTarget(),
				content:       spdx("arm64"),
			},
		)

		dir, printed, err := downloadSBOMs(t, ref)
		require.NoError(t, err)

		assert.ElementsMatch(t, []string{
			"image-sbom_linux_amd64.spdx.json",
			"image-sbom_linux_arm64.spdx.json",
		}, names(t, dir), "both platforms are written, and the platform disambiguates the name")
		assert.Len(t, printed, 2)

		amd64, err := os.ReadFile(filepath.Join(dir, "image-sbom_linux_amd64.spdx.json"))
		require.NoError(t, err)
		assert.Contains(t, string(amd64), "pkg-amd64", "each file holds its own platform's document")
	})

	t.Run("names the file after the document format", func(t *testing.T) {
		ref := setupComponent(t,
			target(),
			resourceSpec{name: "image-sbom", resourceType: "sbom", describes: describesTarget(), content: cyclonedx("only")},
		)

		dir, _, err := downloadSBOMs(t, ref)
		require.NoError(t, err)
		assert.Equal(t, []string{"image-sbom.cdx.json"}, names(t, dir),
			"the format is sniffed from the document, not converted")
	})

	t.Run("defaults the directory to the identity values, not the identity itself", func(t *testing.T) {
		t.Chdir(t.TempDir())

		ref := setupComponent(t,
			target(),
			resourceSpec{name: "image-sbom", resourceType: "sbom", describes: describesTarget(), content: spdx("only")},
		)

		var out bytes.Buffer
		_, err := test.OCM(t, test.WithArgs(
			"download", "resource", ref, "--identity", "name="+targetName, "--sbom"), test.WithOutput(&out))
		require.NoError(t, err)

		// "name=image" needs quoting in a shell and reads as an assignment, so only the value is used.
		assert.NoDirExists(t, "name="+targetName)
		assert.Equal(t, []string{"image-sbom.spdx.json"}, names(t, targetName))
	})

	t.Run("ignores a describing resource that is not an sbom", func(t *testing.T) {
		ref := setupComponent(t,
			target(),
			resourceSpec{
				name:         "image-signature",
				resourceType: "signature",
				describes:    describesTarget(),
				content:      "not an sbom",
			},
		)
		_, _, err := downloadSBOMs(t, ref)
		require.Error(t, err, "the only describing resource is not an sbom, so the artifact is inspected instead")
	})

	t.Run("ignores a reference to a different resource", func(t *testing.T) {
		ref := setupComponent(t,
			target(),
			resourceSpec{
				name:         "other-sbom",
				resourceType: "sbom",
				describes:    runtime.Identity{"name": "something-else", "version": componentVersion},
				content:      spdx("other"),
			},
		)
		_, _, err := downloadSBOMs(t, ref)
		require.Error(t, err)
	})
}

func TestDownloadResourceSBOM_Errors(t *testing.T) {
	t.Run("reports when nothing describes the resource and it cannot be inspected", func(t *testing.T) {
		ref := setupComponent(t, target())

		_, _, err := downloadSBOMs(t, ref)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "no sbom found for resource")
	})

	t.Run("rejects a transformer alongside the flag", func(t *testing.T) {
		ref := setupComponent(t, target())
		_, _, err := downloadSBOMs(t, ref, "--transformer", "anything")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "[sbom transformer]")
	})

	t.Run("rejects an extraction policy alongside the flag", func(t *testing.T) {
		ref := setupComponent(t, target())
		_, _, err := downloadSBOMs(t, ref, "--extraction-policy", "disable")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "[sbom extraction-policy]")
	})
}

func TestDownloadResource_WithoutSBOMFlagIsUnaffected(t *testing.T) {
	ref := setupComponent(t,
		target(),
		resourceSpec{name: "image-sbom", resourceType: "sbom", describes: describesTarget(), content: spdx("sbom")},
	)
	output := filepath.Join(t.TempDir(), "resource.bin")

	_, err := test.OCM(t, test.WithArgs(
		"download", "resource", ref, "--identity", "name="+targetName, "--output", output))
	require.NoError(t, err)

	content, err := os.ReadFile(output)
	require.NoError(t, err)
	assert.Equal(t, "the artifact itself", string(content), "the resource itself is downloaded, not its sbom")
}
