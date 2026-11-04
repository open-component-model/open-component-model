package ctf

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/fs"
	"path"
	"strings"
	"time"

	ociimagespec "github.com/opencontainers/image-spec/specs-go/v1"
)

// OCILayoutFS returns a read-only view of the ArtifactSet as an OCI Image Layout,
// e.g. for oras' oci.NewFromFS. Unlike ConvertToOCIImageLayout it does not rewrite
// the archive: index.json and oci-layout are synthesized and blob paths are mapped
// onto the artifact set's blob naming. Index annotations are kept as is.
//
// The view is backed by the ArtifactSet and must not be used after closing it.
func (a *ArtifactSet) OCILayoutFS() fs.FS {
	return &artifactSetLayoutFS{set: a}
}

type artifactSetLayoutFS struct {
	set *ArtifactSet
}

func (l *artifactSetLayoutFS) Open(name string) (fs.File, error) {
	if !fs.ValidPath(name) {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrInvalid}
	}

	switch name {
	case ociimagespec.ImageLayoutFile:
		return newSynthesizedFile(name, ociimagespec.ImageLayout{Version: ociimagespec.ImageLayoutVersion})
	case ociimagespec.ImageIndexFile:
		// ArtifactSetFormatOCM stores its index as ArtifactSetDescriptorFileName.
		return newSynthesizedFile(name, l.set.GetIndex())
	}

	if l.set.Format() != ArtifactSetFormatOCI {
		// OCI layouts address blobs as blobs/<algorithm>/<encoded>, older formats as blobs/<algorithm>.<encoded>.
		if parts := strings.Split(name, "/"); len(parts) == 3 && parts[0] == BlobsDirectoryName {
			name = path.Join(BlobsDirectoryName, parts[1]+"."+parts[2])
		}
	}
	return l.set.fs.Open(name)
}

type synthesizedFile struct {
	*bytes.Reader
	name string
}

var _ fs.File = (*synthesizedFile)(nil)

func newSynthesizedFile(name string, v any) (fs.File, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("unable to marshal %s: %w", name, err)
	}
	return &synthesizedFile{Reader: bytes.NewReader(data), name: name}, nil
}

func (f *synthesizedFile) Stat() (fs.FileInfo, error) { return f, nil }
func (f *synthesizedFile) Close() error               { return nil }
func (f *synthesizedFile) Name() string               { return f.name }
func (f *synthesizedFile) Mode() fs.FileMode          { return 0o444 }
func (f *synthesizedFile) ModTime() time.Time         { return time.Time{} }
func (f *synthesizedFile) IsDir() bool                { return false }
func (f *synthesizedFile) Sys() any                   { return nil }
