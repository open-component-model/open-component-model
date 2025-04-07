package v1

import (
	"encoding/json"
	"testing"

	"github.com/opencontainers/go-digest"
	"github.com/stretchr/testify/require"
)

func Test_Manifest(t *testing.T) {
	r := require.New(t)

	data, err := json.Marshal(Manifest)
	r.NoError(err)

	dig := digest.FromBytes(data)

	r.Equal(dig, Descriptor.Digest)
	r.Equal(int64(len(data)), Descriptor.Size)
	r.Equal(Manifest.MediaType, Descriptor.MediaType)
	r.Equal(Manifest.ArtifactType, Descriptor.ArtifactType)
}
