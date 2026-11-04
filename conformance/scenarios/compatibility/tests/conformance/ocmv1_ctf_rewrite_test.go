package conformance

import (
	"archive/tar"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"sigs.k8s.io/yaml"
)

// Helpers to rewrite a component descriptor inside a CTF that OCM v1 produced,
// for legacy shapes no released v1 CLI writes any more. The CTF layout is an
// artifact index pointing at an OCI manifest whose first layer is a tar with
// the descriptor, so every digest on that path has to be recomputed.

const (
	ocmv1ArtifactIndexFile = "artifact-index.json"
	ocmv1DescriptorFile    = "component-descriptor.yaml"
)

type ocmv1BlobReference struct {
	MediaType   string            `json:"mediaType"`
	Digest      string            `json:"digest"`
	Size        int64             `json:"size"`
	Annotations map[string]string `json:"annotations,omitempty"`
}

type ocmv1Manifest struct {
	SchemaVersion int                  `json:"schemaVersion"`
	MediaType     string               `json:"mediaType"`
	Config        ocmv1BlobReference   `json:"config"`
	Layers        []ocmv1BlobReference `json:"layers"`
	Annotations   map[string]string    `json:"annotations,omitempty"`
}

type ocmv1ArtifactIndex struct {
	SchemaVersion int `json:"schemaVersion"`
	Artifacts     []struct {
		Repository string `json:"repository"`
		Tag        string `json:"tag"`
		Digest     string `json:"digest"`
		MediaType  string `json:"mediaType"`
	} `json:"artifacts"`
}

// rewriteOCMv1Descriptor applies mutate to the single component descriptor in a
// CTF directory and rewrites the blob, config, manifest and index digests.
func rewriteOCMv1Descriptor(t *testing.T, repository string, mutate func(descriptor map[string]any)) {
	t.Helper()
	r := require.New(t)

	indexPath := filepath.Join(repository, ocmv1ArtifactIndexFile)
	raw, err := os.ReadFile(indexPath)
	r.NoError(err)
	var index ocmv1ArtifactIndex
	r.NoError(json.Unmarshal(raw, &index))
	r.Len(index.Artifacts, 1, "the fixture holds exactly one component version")

	var manifest ocmv1Manifest
	r.NoError(json.Unmarshal(readOCMv1Blob(t, repository, index.Artifacts[0].Digest), &manifest))
	r.NotEmpty(manifest.Layers)

	var descriptor map[string]any
	r.NoError(yaml.Unmarshal(readOCMv1DescriptorLayer(t, repository, manifest.Layers[0].Digest), &descriptor))
	mutate(descriptor)
	updated, err := yaml.Marshal(descriptor)
	r.NoError(err)

	manifest.Layers[0] = writeOCMv1DescriptorLayer(t, repository, manifest.Layers[0], updated)
	config := map[string]ocmv1BlobReference{"componentDescriptorLayer": manifest.Layers[0]}
	rawConfig, err := json.Marshal(config)
	r.NoError(err)
	manifest.Config = writeOCMv1Blob(t, repository, manifest.Config.MediaType, rawConfig)

	rawManifest, err := json.Marshal(manifest)
	r.NoError(err)
	index.Artifacts[0].Digest = writeOCMv1Blob(t, repository, manifest.MediaType, rawManifest).Digest

	raw, err = json.Marshal(index)
	r.NoError(err)
	r.NoError(os.WriteFile(indexPath, raw, 0o600))
}

func readOCMv1Blob(t *testing.T, repository, digest string) []byte {
	t.Helper()
	r := require.New(t)

	raw, err := os.ReadFile(filepath.Join(repository, "blobs", ocmv1BlobName(digest)))
	r.NoError(err)

	return raw
}

func writeOCMv1Blob(t *testing.T, repository, mediaType string, content []byte) ocmv1BlobReference {
	t.Helper()
	r := require.New(t)

	hash := sha256.Sum256(content)
	digest := "sha256:" + hex.EncodeToString(hash[:])
	r.NoError(os.WriteFile(filepath.Join(repository, "blobs", ocmv1BlobName(digest)), content, 0o600))

	return ocmv1BlobReference{MediaType: mediaType, Digest: digest, Size: int64(len(content))}
}

func readOCMv1DescriptorLayer(t *testing.T, repository, digest string) []byte {
	t.Helper()
	r := require.New(t)

	reader := tar.NewReader(bytes.NewReader(readOCMv1Blob(t, repository, digest)))
	for {
		header, err := reader.Next()
		r.NoError(err, "%s not found in the descriptor layer", ocmv1DescriptorFile)
		if header.Name != ocmv1DescriptorFile {
			continue
		}
		content, err := io.ReadAll(reader)
		r.NoError(err)

		return content
	}
}

func writeOCMv1DescriptorLayer(t *testing.T, repository string, layer ocmv1BlobReference, content []byte) ocmv1BlobReference {
	t.Helper()
	r := require.New(t)

	archive := &bytes.Buffer{}
	writer := tar.NewWriter(archive)
	r.NoError(writer.WriteHeader(&tar.Header{Name: ocmv1DescriptorFile, Mode: 0o600, Size: int64(len(content))}))
	_, err := writer.Write(content)
	r.NoError(err)
	r.NoError(writer.Close())

	return writeOCMv1Blob(t, repository, layer.MediaType, archive.Bytes())
}

func ocmv1BlobName(digest string) string {
	return "sha256." + digest[len("sha256:"):]
}
