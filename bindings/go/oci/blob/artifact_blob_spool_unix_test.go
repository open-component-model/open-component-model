//go:build unix

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

// A process that is killed cannot clean up. The spool file must therefore have no name
// while it is in use, so the operating system frees it together with the process.
func TestArtifactBlob_Spool_FileHasNoNameWhileInUse(t *testing.T) {
	r := require.New(t)
	dir := t.TempDir()
	const content = "streamed content of unknown size"
	ab, err := ociblob.NewArtifactBlob(&descriptor.Resource{Access: &v2.LocalBlob{}}, unknownSizeBlob{content})
	r.NoError(err)

	spooled, release, err := ab.Spool(dir)
	r.NoError(err)
	entries, err := os.ReadDir(dir)
	r.NoError(err)
	r.Empty(entries)

	rc, err := spooled.ReadCloser()
	r.NoError(err)
	data, err := io.ReadAll(rc)
	r.NoError(err)
	r.Equal(content, string(data))
	r.NoError(release())
}
