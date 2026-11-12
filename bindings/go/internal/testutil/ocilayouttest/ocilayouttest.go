// Package ocilayouttest writes OCI image layout tars by hand in tests.
//
// [Tar] returns an uncompressed OCI image layout tar that holds the given blobs and an
// index with no manifests:
//
//	layoutTar := ocilayouttest.Tar(t, []byte("blob one"), []byte("blob two"))
//
// It imports no OCM package, so the in-package tests of oci/tar can use it. Use the ocitest
// package for a layout with a real image in it.
package ocilayouttest

import (
	"archive/tar"
	"bytes"
	"testing"

	"github.com/opencontainers/go-digest"
	"github.com/stretchr/testify/require"
)

const (
	// LayoutFile is the content of the oci-layout file in a [Tar] archive.
	LayoutFile = `{"imageLayoutVersion": "1.0.0"}`
	// IndexFile is the content of the index.json file in a [Tar] archive.
	IndexFile = `{"schemaVersion": 2, "manifests": []}`
)

// Tar returns an OCI image layout tar. It holds, in this order, the oci-layout file, the
// blobs and blobs/sha256 directories, each blob under blobs/sha256/<hex digest>, and an
// index.json with no manifests.
func Tar(t *testing.T, blobs ...[]byte) []byte {
	t.Helper()
	r := require.New(t)

	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	writeFile := func(name string, data []byte) {
		r.NoError(tw.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: int64(len(data)), Typeflag: tar.TypeReg}))
		_, err := tw.Write(data)
		r.NoError(err)
	}

	writeFile("oci-layout", []byte(LayoutFile))
	for _, dir := range []string{"blobs", "blobs/sha256"} {
		r.NoError(tw.WriteHeader(&tar.Header{Name: dir, Mode: 0o755, Typeflag: tar.TypeDir}))
	}
	for _, b := range blobs {
		writeFile("blobs/sha256/"+digest.FromBytes(b).Encoded(), b)
	}
	writeFile("index.json", []byte(IndexFile))
	r.NoError(tw.Close())

	return buf.Bytes()
}
