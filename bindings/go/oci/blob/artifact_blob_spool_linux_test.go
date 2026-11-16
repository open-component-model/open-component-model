package blob_test

import (
	"io"
	"os"
	"testing"

	"github.com/stretchr/testify/require"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	ociblob "ocm.software/open-component-model/bindings/go/oci/blob"
)

// Each reader of a spooled blob is its own file: reading or closing one must not move or
// invalidate another, although the spool file has no name to open it by.
func TestArtifactBlob_Spool_ReadersAreIndependentFiles(t *testing.T) {
	r := require.New(t)
	const content = "0123456789abcdefghij"
	ab, err := ociblob.NewArtifactBlob(&descriptor.Resource{Access: &v2.LocalBlob{}}, unknownSizeBlob{content})
	r.NoError(err)
	spooled, release, err := ab.Spool(t.TempDir())
	r.NoError(err)

	first, err := spooled.ReadCloser()
	r.NoError(err)
	r.IsType(&os.File{}, first)
	half := make([]byte, len(content)/2)
	_, err = io.ReadFull(first, half)
	r.NoError(err)

	second, err := spooled.ReadCloser()
	r.NoError(err)
	all, err := io.ReadAll(second)
	r.NoError(err)
	r.Equal(content, string(all), "a new reader starts at the beginning")
	r.NoError(second.Close())

	rest, err := io.ReadAll(first)
	r.NoError(err)
	r.Equal(content, string(half)+string(rest), "closing one reader leaves the others usable")
	r.NoError(first.Close())

	third, err := spooled.ReadCloser()
	r.NoError(err)
	r.NoError(third.Close())
	r.NoError(release())
}
