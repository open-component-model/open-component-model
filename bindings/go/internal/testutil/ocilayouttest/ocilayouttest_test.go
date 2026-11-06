package ocilayouttest_test

import (
	"archive/tar"
	"bytes"
	"errors"
	"io"
	"testing"

	"github.com/opencontainers/go-digest"
	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/internal/testutil/ocilayouttest"
)

type entry struct {
	name     string
	typeflag byte
	data     string
}

func TestTar(t *testing.T) {
	one, two := []byte("blob one"), []byte("blob two")
	head := []entry{
		{name: "oci-layout", typeflag: tar.TypeReg, data: ocilayouttest.LayoutFile},
		{name: "blobs", typeflag: tar.TypeDir},
		{name: "blobs/sha256", typeflag: tar.TypeDir},
	}
	index := entry{name: "index.json", typeflag: tar.TypeReg, data: ocilayouttest.IndexFile}
	blobEntry := func(b []byte) entry {
		return entry{name: "blobs/sha256/" + digest.FromBytes(b).Encoded(), typeflag: tar.TypeReg, data: string(b)}
	}

	tests := []struct {
		name  string
		blobs [][]byte
		want  []entry
	}{
		{name: "no blobs", want: append(append([]entry{}, head...), index)},
		{name: "one blob", blobs: [][]byte{one}, want: append(append([]entry{}, head...), blobEntry(one), index)},
		{name: "two blobs in order", blobs: [][]byte{one, two}, want: append(append([]entry{}, head...), blobEntry(one), blobEntry(two), index)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := require.New(t)

			tr := tar.NewReader(bytes.NewReader(ocilayouttest.Tar(t, tt.blobs...)))
			var got []entry
			for {
				hdr, err := tr.Next()
				if errors.Is(err, io.EOF) {
					break
				}
				r.NoError(err)
				data, err := io.ReadAll(tr)
				r.NoError(err)
				got = append(got, entry{name: hdr.Name, typeflag: hdr.Typeflag, data: string(data)})
			}
			r.Equal(tt.want, got)
		})
	}
}
