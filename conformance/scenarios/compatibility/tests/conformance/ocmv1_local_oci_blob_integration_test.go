package conformance

import (
	"archive/tar"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	godigest "github.com/opencontainers/go-digest"
	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/require"
	"sigs.k8s.io/yaml"

	"ocm.software/open-component-model/bindings/go/blob/inmemory"
	"ocm.software/open-component-model/bindings/go/ctf"
	ctfv1 "ocm.software/open-component-model/bindings/go/ctf/index/v1"
	componentconfig "ocm.software/open-component-model/bindings/go/oci/spec/config/component"
)

const (
	ocmv1LocalOciBlobComponentName = "ocm.software/ocmv1-compat-local-oci-blob"
	ocmv1LocalOciBlobResourceName  = "text"
	ocmv1LocalOciBlobPayload       = "OCM v1 localOciBlob fixture\n"
)

// Test_Integration_OCMv1LocalOciBlobResource covers the localOciBlob access type older OCM v1
// repositories carry: v1 reads it, and v2 must read, download and transfer it by value.
//
// The pinned v1 CLI no longer writes localOciBlob, so the fixture is a CTF that v1 builds and
// the test rewrites in place, exactly as the storage of an older v1 release looks. v1 reads
// the result back at the end, which keeps the rewritten fixture honest.
func Test_Integration_OCMv1LocalOciBlobResource(t *testing.T) {
	r := require.New(t)

	configPath := newCompatibilityConfig(t)
	sourcePath := prepareOCMv1LocalOciBlobCTF(t, configPath)
	sourceReference := "ctf::" + sourcePath + "//" + ocmv1LocalOciBlobComponentName + ":" + ocmv1Version

	// v2 reads the legacy access as stored.
	sourceDescriptor := readCompatDescriptor(t, configPath, sourceReference)
	r.Len(sourceDescriptor.Component.Resources, 1)
	resource := sourceDescriptor.Component.Resources[0]
	r.Equal(ocmv1LocalOciBlobResourceName, resource.Name)
	r.Equal("localOciBlob", resource.Access["type"])
	r.NotEmpty(resource.Access["digest"])

	// v2 downloads the blob the legacy access points at.
	r.Equal(ocmv1LocalOciBlobPayload, downloadCompatResource(t, configPath, sourceReference))

	// v2 transfers the component version, which must copy the blob into the target.
	targetPath := filepath.Join(t.TempDir(), "target")
	targetReference := "ctf::" + targetPath
	runOCMv2Command(t, configPath, "transfer", "cv", sourceReference, targetReference)

	transferredReference := targetReference + "//" + ocmv1LocalOciBlobComponentName + ":" + ocmv1Version
	transferredDescriptor := readCompatDescriptor(t, configPath, transferredReference)
	r.Len(transferredDescriptor.Component.Resources, 1)
	// The target records the canonical access: localOciBlob declares digest, not
	// localReference, so writing that type here would leave an access no reader resolves.
	transferredAccess := transferredDescriptor.Component.Resources[0].Access
	r.Equal("LocalBlob/v1", transferredAccess["type"])
	r.NotEmpty(transferredAccess["localReference"])
	// Downloading from the target proves the blob was copied and not just referenced.
	r.Equal(ocmv1LocalOciBlobPayload, downloadCompatResource(t, configPath, transferredReference))
}

func downloadCompatResource(t *testing.T, configPath, reference string) string {
	t.Helper()
	r := require.New(t)

	path := filepath.Join(t.TempDir(), "download")
	runOCMv2Command(t, configPath, "download", "resource", reference,
		"--identity", "name="+ocmv1LocalOciBlobResourceName, "--output", path, "--extraction-policy", "disable")
	data, err := os.ReadFile(path)
	r.NoError(err)

	return string(data)
}

// prepareOCMv1LocalOciBlobCTF has v1 build a CTF with one local resource and rewrites the
// stored access to the localOciBlob shape, which carries the local reference in digest.
func prepareOCMv1LocalOciBlobCTF(t *testing.T, v1Config string) string {
	t.Helper()
	r := require.New(t)

	workingDirectory := t.TempDir()
	payloadPath := filepath.Join(workingDirectory, "payload.txt")
	r.NoError(os.WriteFile(payloadPath, []byte(ocmv1LocalOciBlobPayload), 0o600))

	constructor := fmt.Sprintf(`name: %s
version: %s
provider:
  name: ocm.software
resources:
  - name: %s
    version: %s
    type: plainText
    input:
      type: file
      path: %q
      mediaType: text/plain
`, ocmv1LocalOciBlobComponentName, ocmv1Version, ocmv1LocalOciBlobResourceName, ocmv1Version, payloadPath)
	constructorPath := filepath.Join(workingDirectory, "constructor.yaml")
	r.NoError(os.WriteFile(constructorPath, []byte(constructor), 0o600))

	ctfPath := filepath.Join(workingDirectory, "v1-local-oci-blob-ctf")
	runCompatibilityTool(t, os.Getenv("OCM_V1_BIN"), "--config", v1Config,
		"add", "cv", "--create", "--file", ctfPath, constructorPath)
	rewriteAccessToLegacyLocalOciBlob(t, ctfPath)

	return ctfPath
}

// rewriteAccessToLegacyLocalOciBlob replaces the localBlob access of the single resource with
// the localOciBlob shape and republishes the descriptor layer, the manifest and the index entry,
// leaving the resource blob itself untouched.
func rewriteAccessToLegacyLocalOciBlob(t *testing.T, ctfPath string) {
	t.Helper()
	r := require.New(t)
	ctx := t.Context()

	archive, err := ctf.OpenCTFFromOSPath(ctfPath, ctf.O_RDWR)
	r.NoError(err)

	index, err := archive.GetIndex(ctx)
	r.NoError(err)
	artifacts := index.GetArtifacts()
	r.Len(artifacts, 1)
	entry := artifacts[0]

	manifest := readCompatJSONBlob[ociImageSpecV1.Manifest](t, archive, entry.Digest)
	layerIndex := -1
	for i, layer := range manifest.Layers {
		if strings.HasPrefix(layer.MediaType, "application/vnd.ocm.software.component-descriptor") ||
			strings.Contains(layer.MediaType, "component-descriptor") {
			layerIndex = i
			break
		}
	}
	r.GreaterOrEqual(layerIndex, 0, "no component descriptor layer in %v", manifest.Layers)
	layer := manifest.Layers[layerIndex]

	name, descriptorYAML := readCompatDescriptorTar(t, archive, layer.Digest.String())
	var document map[string]any
	r.NoError(yaml.Unmarshal(descriptorYAML, &document))

	component, ok := document["component"].(map[string]any)
	r.True(ok, "descriptor has no component: %s", descriptorYAML)
	resources, ok := component["resources"].([]any)
	r.True(ok, "component has no resources: %s", descriptorYAML)
	r.Len(resources, 1)
	resource, ok := resources[0].(map[string]any)
	r.True(ok, "resource is not an object: %s", descriptorYAML)
	access, ok := resource["access"].(map[string]any)
	r.True(ok, "resource has no access: %s", descriptorYAML)
	localReference, ok := access["localReference"].(string)
	r.True(ok, "access has no localReference: %v", access)

	resource["access"] = map[string]any{"type": "localOciBlob", "digest": localReference}
	rewritten, err := yaml.Marshal(document)
	r.NoError(err)

	layerData := compatDescriptorTar(t, name, rewritten)
	r.NoError(archive.SaveBlob(ctx, inmemory.New(bytes.NewReader(layerData))))
	manifest.Layers[layerIndex].Digest = godigest.FromBytes(layerData)
	manifest.Layers[layerIndex].Size = int64(len(layerData))

	// The OCM component config points at the descriptor layer, and that pointer is what a
	// reader follows, so it has to move with the layer.
	configData, configDescriptor, err := componentconfig.New(manifest.Layers[layerIndex])
	r.NoError(err)
	r.NoError(archive.SaveBlob(ctx, inmemory.New(bytes.NewReader(configData))))
	manifest.Config = configDescriptor

	manifestData, err := json.Marshal(manifest)
	r.NoError(err)
	r.NoError(archive.SaveBlob(ctx, inmemory.New(bytes.NewReader(manifestData))))

	entry.Digest = godigest.FromBytes(manifestData).String()
	rewrittenIndex := ctfv1.NewIndex()
	rewrittenIndex.AddArtifact(entry)
	r.NoError(archive.SetIndex(ctx, rewrittenIndex))
}

func readCompatJSONBlob[T any](t *testing.T, archive ctf.CTF, digest string) T {
	t.Helper()
	r := require.New(t)

	blob, err := archive.GetBlob(t.Context(), digest)
	r.NoError(err)
	reader, err := blob.ReadCloser()
	r.NoError(err)
	t.Cleanup(func() { r.NoError(reader.Close()) })
	data, err := io.ReadAll(reader)
	r.NoError(err)

	var decoded T
	r.NoError(json.Unmarshal(data, &decoded))

	return decoded
}

// readCompatDescriptorTar returns the name and the content of the single file in the component
// descriptor layer, which OCM v1 stores as a tar archive.
func readCompatDescriptorTar(t *testing.T, archive ctf.CTF, digest string) (string, []byte) {
	t.Helper()
	r := require.New(t)

	blob, err := archive.GetBlob(t.Context(), digest)
	r.NoError(err)
	reader, err := blob.ReadCloser()
	r.NoError(err)
	t.Cleanup(func() { r.NoError(reader.Close()) })

	tarReader := tar.NewReader(reader)
	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			break
		}
		r.NoError(err)
		if header.Typeflag != tar.TypeReg {
			continue
		}
		data, err := io.ReadAll(tarReader)
		r.NoError(err)

		return header.Name, data
	}
	r.Fail("no component descriptor file in layer " + digest)

	return "", nil
}

func compatDescriptorTar(t *testing.T, name string, data []byte) []byte {
	t.Helper()
	r := require.New(t)

	var buffer bytes.Buffer
	writer := tar.NewWriter(&buffer)
	r.NoError(writer.WriteHeader(&tar.Header{
		Typeflag: tar.TypeReg,
		Name:     name,
		Mode:     0o644,
		Size:     int64(len(data)),
	}))
	_, err := writer.Write(data)
	r.NoError(err)
	r.NoError(writer.Close())

	return buffer.Bytes()
}
