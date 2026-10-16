package tar_test

import (
	stdtar "archive/tar"
	"bytes"
	"compress/gzip"
	"testing"

	"github.com/opencontainers/go-digest"
	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/blob/inmemory"
	ocitar "ocm.software/open-component-model/bindings/go/oci/tar"
)

// archiveWith builds a tar (optionally gzip'd) holding one blob at the given path with
// the given bytes, plus an unrelated entry that must be skipped without being hashed.
func archiveWith(t *testing.T, blobPath string, body []byte, gzipIt bool) []byte {
	t.Helper()
	var raw bytes.Buffer
	tw := stdtar.NewWriter(&raw)
	require.NoError(t, tw.WriteHeader(&stdtar.Header{Name: "oci-layout", Size: 3, Mode: 0o644}))
	_, err := tw.Write([]byte("{}\n"))
	require.NoError(t, err)
	require.NoError(t, tw.WriteHeader(&stdtar.Header{Name: blobPath, Size: int64(len(body)), Mode: 0o644}))
	_, err = tw.Write(body)
	require.NoError(t, err)
	require.NoError(t, tw.Close())
	if !gzipIt {
		return raw.Bytes()
	}
	var gz bytes.Buffer
	gw := gzip.NewWriter(&gz)
	_, err = gw.Write(raw.Bytes())
	require.NoError(t, err)
	require.NoError(t, gw.Close())
	return gz.Bytes()
}

func TestArtifactSetContainsDigest(t *testing.T) {
	body := []byte("the main artifact manifest")
	want := digest.FromBytes(body)

	cases := []struct {
		name     string
		blobPath string
		body     []byte
		gzip     bool
		found    bool
	}{
		{name: "OCM dot path", blobPath: "blobs/sha256." + want.Encoded(), body: body, found: true},
		{name: "OCI slash path", blobPath: "blobs/sha256/" + want.Encoded(), body: body, found: true},
		{name: "gzip-wrapped", blobPath: "blobs/sha256." + want.Encoded(), body: body, gzip: true, found: true},
		{name: "leading ./ path", blobPath: "./blobs/sha256." + want.Encoded(), body: body, found: true},
		{name: "absent blob", blobPath: "blobs/sha256." + digest.FromString("other").Encoded(), body: body, found: false},
		{name: "decoy at derived path with wrong bytes", blobPath: "blobs/sha256." + want.Encoded(), body: []byte("tampered"), found: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := require.New(t)
			b := inmemory.New(bytes.NewReader(archiveWith(t, tc.blobPath, tc.body, tc.gzip)))
			found, err := ocitar.ArtifactSetContainsDigest(b, want)
			r.NoError(err)
			r.Equal(tc.found, found)
		})
	}

	t.Run("real blob later shadows an earlier decoy", func(t *testing.T) {
		r := require.New(t)
		var raw bytes.Buffer
		tw := stdtar.NewWriter(&raw)
		p := "blobs/sha256." + want.Encoded()
		// decoy first: same path, wrong bytes; the real blob follows.
		for _, body := range [][]byte{[]byte("tampered"), body} {
			r.NoError(tw.WriteHeader(&stdtar.Header{Name: p, Size: int64(len(body)), Mode: 0o644}))
			_, err := tw.Write(body)
			r.NoError(err)
		}
		r.NoError(tw.Close())
		found, err := ocitar.ArtifactSetContainsDigest(inmemory.New(bytes.NewReader(raw.Bytes())), want)
		r.NoError(err)
		r.True(found, "a decoy must not shadow the real blob later in the tar")
	})

	t.Run("invalid digest rejected", func(t *testing.T) {
		r := require.New(t)
		b := inmemory.New(bytes.NewReader(archiveWith(t, "blobs/sha256."+want.Encoded(), body, false)))
		_, err := ocitar.ArtifactSetContainsDigest(b, digest.Digest("not-a-digest"))
		r.Error(err)
	})
}
