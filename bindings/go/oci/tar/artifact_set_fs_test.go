package tar

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/blob/inmemory"
)

// OCM v1 stores an OCI artifact in a local blob as an artifact set with flat blob names. It is
// read like an OCI layout, so its main manifest and every referenced blob can be checked.
func TestReadOCILayout_ArtifactSet(t *testing.T) {
	r := require.New(t)
	data, err := os.ReadFile(filepath.Join("..", "..", "ctf", "testdata", "compatibility", "02", "with-resource", "blobs",
		"sha256.e40e3a2f1ab1a98328dfd14539a79d27aff5c4d5c34cd16a85f0288bfa76490b"))
	r.NoError(err)

	store, err := ReadOCILayout(t.Context(), inmemory.New(bytes.NewReader(data)))
	r.NoError(err)
	t.Cleanup(func() { r.NoError(store.Close()) })

	mains := store.MainArtifacts(t.Context())
	r.Len(mains, 1)
	r.Equal("sha256:62be4af3382a4493cb7f1dd4ec47bcb28f1863b615fc9e4a1dceefbe93898dd0", mains[0].Digest.String())
	r.NoError(VerifyGraph(t.Context(), store, mains[0]))
}
