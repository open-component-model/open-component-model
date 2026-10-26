package ctf_test

import (
	"bytes"
	"io"
	"io/fs"
	"testing"

	ociimagespecv1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/require"
	"oras.land/oras-go/v2/content"
	"oras.land/oras-go/v2/content/oci"

	"ocm.software/open-component-model/bindings/go/blob/inmemory"
	"ocm.software/open-component-model/bindings/go/ctf"
)

func TestArtifactSet_OCILayoutFS(t *testing.T) {
	for _, format := range []ctf.ArtifactSetFormat{
		ctf.ArtifactSetFormatLegacyOCI,
		ctf.ArtifactSetFormatOCM,
		ctf.ArtifactSetFormatOCI,
	} {
		t.Run(format.String(), func(t *testing.T) {
			r := require.New(t)
			ctx := t.Context()

			archive, fixture := artifactSetArchive(t, format)
			as, err := ctf.NewArtifactSetFromBlob(inmemory.New(bytes.NewReader(archive), inmemory.WithMediaType(artifactSetMediaType)))
			r.NoError(err)
			t.Cleanup(func() { r.NoError(as.Close()) })
			r.Equal(format, as.Format())

			store, err := oci.NewFromFS(ctx, as.OCILayoutFS())
			r.NoError(err)

			desc, err := store.Resolve(ctx, "v1.0.0")
			r.NoError(err)
			r.Equal(fixture.manifest.Digest, desc.Digest)

			for _, d := range []ociimagespecv1.Descriptor{fixture.manifest, fixture.config, fixture.layer} {
				exists, err := store.Exists(ctx, d)
				r.NoError(err)
				r.True(exists, d.Digest)
				data, err := content.FetchAll(ctx, store, d)
				r.NoError(err)
				r.Equal(fixture.data[d.Digest.String()], data)
			}

			predecessors, err := store.Predecessors(ctx, fixture.layer)
			r.NoError(err)
			r.Len(predecessors, 1)
			r.Equal(fixture.manifest.Digest, predecessors[0].Digest)

			missing := content.NewDescriptorFromBytes(ociimagespecv1.MediaTypeImageLayer, []byte("missing"))
			exists, err := store.Exists(ctx, missing)
			r.NoError(err)
			r.False(exists)
		})
	}

	t.Run("rejects invalid paths", func(t *testing.T) {
		r := require.New(t)
		archive, _ := artifactSetArchive(t, ctf.ArtifactSetFormatLegacyOCI)
		as, err := ctf.NewArtifactSetFromBlob(inmemory.New(bytes.NewReader(archive), inmemory.WithMediaType(artifactSetMediaType)))
		r.NoError(err)
		t.Cleanup(func() { r.NoError(as.Close()) })

		_, err = as.OCILayoutFS().Open("../index.json")
		r.ErrorIs(err, fs.ErrInvalid)
	})

	t.Run("synthesized files are statable", func(t *testing.T) {
		r := require.New(t)
		archive, _ := artifactSetArchive(t, ctf.ArtifactSetFormatOCM)
		as, err := ctf.NewArtifactSetFromBlob(inmemory.New(bytes.NewReader(archive), inmemory.WithMediaType(artifactSetMediaType)))
		r.NoError(err)
		t.Cleanup(func() { r.NoError(as.Close()) })

		for _, name := range []string{ociimagespecv1.ImageLayoutFile, ociimagespecv1.ImageIndexFile} {
			f, err := as.OCILayoutFS().Open(name)
			r.NoError(err)
			info, err := f.Stat()
			r.NoError(err)
			data, err := io.ReadAll(f)
			r.NoError(err)
			r.NoError(f.Close())
			r.Equal(name, info.Name())
			r.Equal(int64(len(data)), info.Size())
			r.True(info.Mode().IsRegular())
		}
	})
}
