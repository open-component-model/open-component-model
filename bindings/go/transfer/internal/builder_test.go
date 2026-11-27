package internal

import (
	"bytes"
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"sigs.k8s.io/yaml"

	"ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/blob/inmemory"
	"ocm.software/open-component-model/bindings/go/credentials"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/repository"
	"ocm.software/open-component-model/bindings/go/runtime"
	transferv1alpha1 "ocm.software/open-component-model/bindings/go/transfer/v1alpha1/spec"
	transformv1alpha1 "ocm.software/open-component-model/bindings/go/transform/spec/v1alpha1"
)

type stubRepoProvider struct {
	repository.ComponentVersionRepositoryProvider
}

type stubResourceRepo struct {
	repository.ResourceRepository
}

type stubCredResolver struct {
	credentials.Resolver
}

func TestNewBuilder(t *testing.T) {
	b := NewDefaultBuilder(&stubRepoProvider{}, &stubResourceRepo{}, &stubCredResolver{}, nil, "")
	assert.NotNil(t, b)
}

func TestNewDefaultBuilder_CanBuildGitHubGraph(t *testing.T) {
	sourceRepo := testOCIRepo("ghcr.io/source")
	targetRepo := testOCIRepo("ghcr.io/target")
	desc := testDescriptor("ocm.software/test", "1.0.0",
		[]descriptor.Resource{githubResource("my-source", "1.0.0",
			"https://github.com/open-component-model/open-component-model",
			"f58349914e3c775747dc1ee9af1bc83db4652266")}, nil)
	resolver := testResolverFor("ocm.software/test", "1.0.0", sourceRepo, desc)
	roots := testTransferRoots("ocm.software/test", "1.0.0", targetRepo, resolver)
	tgd, err := BuildGraphDefinition(t.Context(), roots, transferv1alpha1.Config{}, withLocalBlobUploader())
	require.NoError(t, err)

	// BuildAndCheck resolves a transformer for every node type. Providers are only used at
	// Process time, so nil is fine here.
	_, err = NewDefaultBuilder(nil, nil, nil, nil, "").BuildAndCheck(tgd)
	require.NoError(t, err, "default builder must resolve the GetGitHubCommit transformer")
}

// TestNewDefaultBuilder_CanBuildHelmDigestGraphs runs the static type check over the chains that
// keep a chart digest, which proves their expressions fit the schemas of the node types and that
// every node type has a transformer.
func TestNewDefaultBuilder_CanBuildHelmDigestGraphs(t *testing.T) {
	tests := []struct {
		name      string
		target    runtime.Typed
		uploaders []transferv1alpha1.UploaderConfig
	}{
		{name: "local blob, registry target", target: testOCIRepo("ghcr.io/target"), uploaders: withLocalBlobUploader()},
		{name: "local blob, CTF target", target: testCTFRepo("./out"), uploaders: withLocalBlobUploader()},
		{name: "OCI uploader", target: testOCIRepo("ghcr.io/target"), uploaders: withLocalBlobUploader(ociUploaders()...)},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := require.New(t)
			tgd, err := helmDigestGraph(t, helmDigestDescriptor(chartDigest(), true, "https://charts.example.com"), tc.uploaders, tc.target)
			r.NoError(err)

			_, err = NewDefaultBuilder(nil, nil, nil, nil, "").BuildAndCheck(tgd)
			r.NoError(err)
		})
	}
}

// stubDownloadRepo answers every download with the same content, so that a graph can be
// processed without a registry.
type stubDownloadRepo struct {
	repository.ResourceRepository
	data []byte
}

func (s *stubDownloadRepo) DownloadResource(_ context.Context, _ *descriptor.Resource, _ runtime.Typed) (blob.ReadOnlyBlob, error) {
	return inmemory.New(bytes.NewReader(s.data)), nil
}

// TestNewDefaultBuilder_BuffersInConfiguredTempDir covers the wiring of the temporary folder
// into the transformers that buffer downloaded content to a file.
func TestNewDefaultBuilder_BuffersInConfiguredTempDir(t *testing.T) {
	r := require.New(t)
	// The folder does not exist yet: the transformers create it.
	tempDir := filepath.Join(t.TempDir(), "configured-temp-folder")

	var tgd transformv1alpha1.TransformationGraphDefinition
	r.NoError(yaml.Unmarshal([]byte(`
transformations:
- id: getOCIArtifact
  type: GetOCIArtifact/v1alpha1
  spec:
    resource:
      name: image
      version: 1.0.0
      type: ociImage
      relation: external
      access:
        type: ociArtifact/v1
        imageReference: ghcr.io/org/image:v1
- id: downloadWget
  type: DownloadWgetResource/v1alpha1
  spec:
    resource:
      name: file
      version: 1.0.0
      type: blob
      relation: external
      access:
        type: wget/v1
        url: https://example.com/file.txt
`), &tgd))

	repo := &stubDownloadRepo{data: []byte("downloaded content")}
	graph, err := NewDefaultBuilder(nil, repo, nil, nil, tempDir).BuildAndCheck(&tgd)
	r.NoError(err)
	r.NoError(graph.Process(t.Context()))

	for _, prefix := range []string{"oci-artifact-", "wget-"} {
		buffered, err := filepath.Glob(filepath.Join(tempDir, prefix+"*"))
		r.NoError(err)
		r.Len(buffered, 1, "content must be buffered in the configured temporary folder")
	}
}
