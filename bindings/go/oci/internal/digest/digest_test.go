package digest

import (
	"testing"

	godigest "github.com/opencontainers/go-digest"
	"github.com/stretchr/testify/require"
)

func TestSHAMapping(t *testing.T) {
	r := require.New(t)
	r.Equal(godigest.SHA256, SHAMapping[HashAlgorithmSHA256])
	r.Equal(godigest.SHA512, SHAMapping[HashAlgorithmSHA512])
	r.Equal(HashAlgorithmSHA256, ReverseSHAMapping[godigest.SHA256])
	r.Equal(HashAlgorithmSHA512, ReverseSHAMapping[godigest.SHA512])
}
