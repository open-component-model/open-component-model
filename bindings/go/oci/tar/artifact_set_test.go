package tar

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/blob/inmemory"
	"ocm.software/open-component-model/bindings/go/ctf"
)

const (
	artifactSetMediaType     = "application/vnd.oci.image.manifest.v1+tar+gzip"
	artifactSetFixtureMain   = "sha256:62be4af3382a4493cb7f1dd4ec47bcb28f1863b615fc9e4a1dceefbe93898dd0"
	artifactSetFixtureConfig = "blobs/sha256.fd3d24bc39ab37023193443468452911b5ba52cc32f3f955a5cd16f06402f848"
)

// OCM v1 stores an OCI artifact in a local blob as an artifact set in one of three formats. Each
// is read, its main artifact is selected and every blob it references is checked.
func TestVerifyArtifactSet(t *testing.T) {
	legacy, err := os.ReadFile(filepath.Join("..", "..", "ctf", "testdata", "compatibility", "02", "with-resource", "blobs",
		"sha256.e40e3a2f1ab1a98328dfd14539a79d27aff5c4d5c34cd16a85f0288bfa76490b"))
	require.NoError(t, err)

	for _, tc := range []struct {
		name    string
		rewrite func(name string, data []byte) (string, []byte)
		format  ctf.ArtifactSetFormat
		wantErr string
	}{
		{name: "legacy OCI format", format: ctf.ArtifactSetFormatLegacyOCI},
		{
			name: "OCM format without layout file and index media types",
			rewrite: func(name string, data []byte) (string, []byte) {
				switch name {
				case "oci-layout":
					return "", nil
				case "index.json":
					return ctf.ArtifactSetDescriptorFileName, withoutIndexMediaTypes(t, data)
				}
				return name, data
			},
			format: ctf.ArtifactSetFormatOCM,
		},
		{
			name: "OCI format with blobs by algorithm",
			rewrite: func(name string, data []byte) (string, []byte) {
				if dir, file := path.Split(name); dir == "blobs/" {
					return "blobs/" + strings.Replace(file, ".", "/", 1), data
				}
				return name, data
			},
			format: ctf.ArtifactSetFormatOCI,
		},
		{
			name: "substituted blob is rejected",
			rewrite: func(name string, data []byte) (string, []byte) {
				if name == artifactSetFixtureConfig {
					return name, []byte("{}")
				}
				return name, data
			},
			format:  ctf.ArtifactSetFormatLegacyOCI,
			wantErr: "failed to verify artifact set",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := require.New(t)
			data := legacy
			if tc.rewrite != nil {
				data = rewriteArtifactSet(t, legacy, tc.rewrite)
			}
			set, err := ctf.NewArtifactSetFromBlob(inmemory.New(bytes.NewReader(data), inmemory.WithMediaType(artifactSetMediaType)))
			r.NoError(err)
			r.Equal(tc.format, set.Format())
			r.NoError(set.Close())

			main, err := VerifyArtifactSet(t.Context(), inmemory.New(bytes.NewReader(data), inmemory.WithMediaType(artifactSetMediaType)))
			if tc.wantErr != "" {
				r.ErrorContains(err, tc.wantErr)
				return
			}
			r.NoError(err)
			r.Equal(artifactSetFixtureMain, main.Digest.String())
			r.Equal("application/vnd.oci.image.manifest.v1+json", main.MediaType)
		})
	}
}

// rewriteArtifactSet copies a gzipped artifact set into an uncompressed tar, renaming or
// replacing entries with rewrite. An entry renamed to "" is dropped.
func rewriteArtifactSet(t *testing.T, archive []byte, rewrite func(name string, data []byte) (string, []byte)) []byte {
	t.Helper()
	r := require.New(t)
	gz, err := gzip.NewReader(bytes.NewReader(archive))
	r.NoError(err)
	tr := tar.NewReader(gz)
	var out bytes.Buffer
	tw := tar.NewWriter(&out)
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		r.NoError(err)
		data, err := io.ReadAll(tr)
		r.NoError(err)
		name := strings.TrimSuffix(header.Name, "/")
		if header.Typeflag == tar.TypeDir {
			r.NoError(tw.WriteHeader(header))
			continue
		}
		if name, data = rewrite(name, data); name == "" {
			continue
		}
		r.NoError(tw.WriteHeader(&tar.Header{Name: name, Mode: header.Mode, Size: int64(len(data)), Typeflag: tar.TypeReg}))
		_, err = tw.Write(data)
		r.NoError(err)
	}
	r.NoError(tw.Close())
	return out.Bytes()
}

// withoutIndexMediaTypes removes the media types the OCM format does not record in its index.
func withoutIndexMediaTypes(t *testing.T, index []byte) []byte {
	t.Helper()
	r := require.New(t)
	var idx map[string]any
	r.NoError(json.Unmarshal(index, &idx))
	delete(idx, "mediaType")
	for _, manifest := range idx["manifests"].([]any) {
		delete(manifest.(map[string]any), "mediaType")
	}
	data, err := json.Marshal(idx)
	r.NoError(err)
	return data
}
