package integration

import (
	"archive/tar"
	"compress/gzip"
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/github/repository/resource"
	v1 "ocm.software/open-component-model/bindings/go/github/spec/access/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// The canonical "Hello World" repository of the octocat user. Both the
// repository and this commit (its long-frozen master tip) have been stable
// for over a decade and are used widely as GitHub API test fixtures.
const (
	helloWorldRepo   = "https://github.com/octocat/Hello-World"
	helloWorldCommit = "7fd1a60b01f91b314f59955a4e4d4e80d8edf11d"
)

func Test_Integration_GitHub_DownloadResource(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	res := &descriptor.Resource{
		ElementMeta: descriptor.ElementMeta{
			ObjectMeta: descriptor.ObjectMeta{Name: "hello-world", Version: "1.0.0"},
		},
		Access: &v1.GitHub{
			Type:    runtime.NewVersionedType(v1.LegacyType, v1.Version),
			RepoURL: helloWorldRepo,
			Commit:  helloWorldCommit,
		},
	}

	downloaded, err := resource.NewResourceRepository(nil).DownloadResource(t.Context(), res, nil)
	require.NoError(t, err)

	reader, err := downloaded.ReadCloser()
	require.NoError(t, err)
	defer func() { require.NoError(t, reader.Close()) }()

	// Matching old OCM, the blob is GitHub's gzipped tar source archive whose
	// entries are prefixed with "<repo>-<commit>/".
	gz, err := gzip.NewReader(reader)
	require.NoError(t, err)

	var readmeFound bool
	tr := tar.NewReader(gz)
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		require.NoError(t, err)
		if strings.HasSuffix(header.Name, "/README") {
			readmeFound = true
		}
	}

	assert.True(t, readmeFound, "the Hello-World archive must contain its README under the commit-prefixed directory")
}
