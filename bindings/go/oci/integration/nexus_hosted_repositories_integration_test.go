package integration_test

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json/v2"
	"testing"

	"github.com/opencontainers/go-digest"
	"github.com/opencontainers/image-spec/specs-go"
	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/require"
	"oras.land/oras-go/v2/content"

	"ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/blob/inmemory"
	"ocm.software/open-component-model/bindings/go/ctf"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	v1 "ocm.software/open-component-model/bindings/go/oci/spec/access/v1"
	ocitar "ocm.software/open-component-model/bindings/go/oci/tar"
)

const (
	dockerManifestMediaType     = "application/vnd.docker.distribution.manifest.v2+json"
	dockerManifestListMediaType = "application/vnd.docker.distribution.manifest.list.v2+json"
	dockerConfigMediaType       = "application/vnd.docker.container.image.v1+json"
	dockerLayerMediaType        = "application/vnd.docker.image.rootfs.diff.tar.gzip"
)

// Test_Integration_OCIRepository_NexusHostedRepositories checks whether Nexus Repository 3 hosted
// repositories of format oci and docker accept OCM component versions and legacy Docker images
// through their registry v2 API, with and without strict content type validation. Strict
// OCI-native registries reject Docker-typed manifests. Nexus must accept them unchanged, because
// the media type is part of the hashed manifest bytes and a conversion would break the resource
// digest.
func Test_Integration_OCIRepository_NexusHostedRepositories(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping Nexus hosted repository integration test in short mode")
	}
	t.Parallel()

	nexus := startNexus(t, t.Context())

	for _, repository := range []struct {
		format    string
		name      string
		connector string
		strict    bool
	}{
		{format: nexusFormatOCI, name: nexusOCIRepository, connector: nexusOCIConnector, strict: false},
		{format: nexusFormatOCI, name: nexusStrictOCIRepository, connector: nexusStrictOCIConnector, strict: true},
		{format: nexusFormatDocker, name: nexusDockerRepository, connector: nexusDockerConnector, strict: false},
		{format: nexusFormatDocker, name: nexusStrictDockerRepository, connector: nexusStrictDockerConnector, strict: true},
	} {
		resolver, repo, endpoint := nexus.addHostedRepository(t, t.Context(), repository.format, repository.name, repository.connector, repository.strict)

		t.Run(repository.name, func(t *testing.T) {
			t.Run("component version", func(t *testing.T) {
				r := require.New(t)
				ctx := t.Context()

				const component, version = "ocm.software/nexus-docker-test", "v1.0.0"
				uploadDownloadBarebonesComponentVersion(t, repo, component, version)

				// Record what Nexus stored: OCM writes an OCI image manifest. If this holds, a
				// Nexus docker repository accepts OCM component versions without format conversion.
				ref := resolver.ComponentVersionReference(ctx, component, version)
				store, err := resolver.StoreForReference(ctx, ref)
				r.NoError(err)
				desc, err := store.Resolve(ctx, ref)
				r.NoError(err)
				t.Logf("nexus stored %s as %s (%s)", ref, desc.MediaType, desc.Digest)
				r.Equal(ociImageSpecV1.MediaTypeImageManifest, desc.MediaType)
			})

			for _, image := range []struct {
				name string
				path string
				list bool
			}{
				{name: "docker manifest", path: "legacy/manifest", list: false},
				{name: "docker manifest list", path: "legacy/manifest-list", list: true},
			} {
				t.Run(image.name, func(t *testing.T) {
					r := require.New(t)
					ctx := t.Context()
					set := newDockerArtifactSet(t, image.list)

					target := endpoint + "/" + image.path + ":1.0.0"
					res := &descriptor.Resource{
						ElementMeta: descriptor.ElementMeta{ObjectMeta: descriptor.ObjectMeta{Name: "image", Version: "1.0.0"}},
						Type:        "ociImage",
						Access:      &v1.OCIImage{ImageReference: target},
					}

					uploaded, err := repo.UploadResource(ctx, res, set.blob())
					r.NoError(err, "nexus must accept the docker media types")
					r.Equal(set.root.Digest.Encoded(), uploaded.Digest.Value, "the resource digest must be the docker digest")

					_, err = repo.UploadResource(ctx, res, set.blob())
					r.NoError(err, "identical re-upload must be idempotent despite redeploy being disabled")

					store, err := resolver.StoreForReference(ctx, target)
					r.NoError(err)
					desc, err := store.Resolve(ctx, target)
					r.NoError(err)
					t.Logf("nexus stored %s as %s (%s)", target, desc.MediaType, desc.Digest)
					r.Equal(set.root.MediaType, desc.MediaType, "nexus must not convert the media type")
					r.Equal(set.root.Digest, desc.Digest, "nexus must not convert the manifest")

					downloaded, err := repo.DownloadResource(ctx, uploaded)
					r.NoError(err)
					set.requireStoredIn(t, ctx, downloaded)
				})
			}
		})
	}
}

// dockerArtifactSet is a legacy OCM artifact set holding a Docker image, as the OCM library
// synthesizes it for docker and dockermulti inputs.
type dockerArtifactSet struct {
	mediaType string
	archive   []byte
	root      ociImageSpecV1.Descriptor
	contents  []dockerImageContent
}

type dockerImageContent struct {
	desc ociImageSpecV1.Descriptor
	data []byte
}

// newDockerArtifactSet builds an artifact set whose root is a Docker schema2 manifest or, for
// list, a manifest list. Its content is valid for its media types, since Nexus can inspect it
// under strict content type validation, and unique per test, so that no subtest finds its blobs
// or manifests already stored by another one.
func newDockerArtifactSet(t *testing.T, list bool) *dockerArtifactSet {
	t.Helper()
	r := require.New(t)

	var uncompressed, compressed bytes.Buffer
	payload := []byte(t.Name())
	tarWriter := tar.NewWriter(&uncompressed)
	r.NoError(tarWriter.WriteHeader(&tar.Header{Name: "payload", Mode: 0o600, Size: int64(len(payload))}))
	_, err := tarWriter.Write(payload)
	r.NoError(err)
	r.NoError(tarWriter.Close())
	gzipWriter := gzip.NewWriter(&compressed)
	_, err = gzipWriter.Write(uncompressed.Bytes())
	r.NoError(err)
	r.NoError(gzipWriter.Close())
	layer := dockerImageContent{
		desc: content.NewDescriptorFromBytes(dockerLayerMediaType, compressed.Bytes()),
		data: compressed.Bytes(),
	}

	configData, err := json.Marshal(ociImageSpecV1.Image{
		Architecture: "amd64",
		OS:           "linux",
		RootFS:       ociImageSpecV1.RootFS{Type: "layers", DiffIDs: []digest.Digest{digest.FromBytes(uncompressed.Bytes())}},
	})
	r.NoError(err)
	config := dockerImageContent{desc: content.NewDescriptorFromBytes(dockerConfigMediaType, configData), data: configData}

	manifestData, err := json.Marshal(ociImageSpecV1.Manifest{
		Versioned: specs.Versioned{SchemaVersion: 2},
		MediaType: dockerManifestMediaType,
		Config:    config.desc,
		Layers:    []ociImageSpecV1.Descriptor{layer.desc},
	})
	r.NoError(err)
	manifest := dockerImageContent{desc: content.NewDescriptorFromBytes(dockerManifestMediaType, manifestData), data: manifestData}

	set := &dockerArtifactSet{
		mediaType: "application/vnd.docker.distribution.manifest.v2" + ctf.SynthesizedBlobFormatSuffix,
		root:      manifest.desc,
		contents:  []dockerImageContent{layer, config, manifest},
	}
	if list {
		entry := manifest.desc
		entry.Platform = &ociImageSpecV1.Platform{Architecture: "amd64", OS: "linux"}
		listData, err := json.Marshal(ociImageSpecV1.Index{
			Versioned: specs.Versioned{SchemaVersion: 2},
			MediaType: dockerManifestListMediaType,
			Manifests: []ociImageSpecV1.Descriptor{entry},
		})
		r.NoError(err)
		root := dockerImageContent{desc: content.NewDescriptorFromBytes(dockerManifestListMediaType, listData), data: listData}
		set.mediaType = "application/vnd.docker.distribution.manifest.list.v2" + ctf.SynthesizedBlobFormatSuffix
		set.root = root.desc
		set.contents = append(set.contents, root)
	}
	set.root.Annotations = map[string]string{ociImageSpecV1.AnnotationRefName: "1.0.0"}

	indexData, err := json.Marshal(ociImageSpecV1.Index{
		Versioned: specs.Versioned{SchemaVersion: 2},
		MediaType: ociImageSpecV1.MediaTypeImageIndex,
		Manifests: []ociImageSpecV1.Descriptor{set.root},
	})
	r.NoError(err)

	var archive bytes.Buffer
	archiveGzip := gzip.NewWriter(&archive)
	archiveTar := tar.NewWriter(archiveGzip)
	writeEntry := func(name string, data []byte) {
		t.Helper()
		r.NoError(archiveTar.WriteHeader(&tar.Header{Name: name, Mode: 0o600, Size: int64(len(data))}))
		_, err := archiveTar.Write(data)
		r.NoError(err)
	}
	r.NoError(archiveTar.WriteHeader(&tar.Header{Name: ctf.BlobsDirectoryName + "/", Mode: 0o700, Typeflag: tar.TypeDir}))
	writeEntry(ociImageSpecV1.ImageIndexFile, indexData)
	for _, entry := range set.contents {
		name, err := ctf.ToBlobFileName(entry.desc.Digest.String())
		r.NoError(err)
		writeEntry(ctf.BlobsDirectoryName+"/"+name, entry.data)
	}
	r.NoError(archiveTar.Close())
	r.NoError(archiveGzip.Close())
	set.archive = archive.Bytes()

	// Confirm the fixture is a genuine legacy artifact set, not an OCI layout with a
	// docker media type stamped on.
	parsed, err := ctf.NewArtifactSetFromBlob(set.blob())
	r.NoError(err)
	r.Equal(ctf.ArtifactSetFormatLegacyOCI, parsed.Format())
	r.NoError(parsed.Close())

	return set
}

func (s *dockerArtifactSet) blob() blob.ReadOnlyBlob {
	return inmemory.New(bytes.NewReader(s.archive), inmemory.WithMediaType(s.mediaType))
}

// requireStoredIn requires the downloaded layout to have the artifact set's root as its main
// artifact and to hold every manifest, config and layer byte for byte.
func (s *dockerArtifactSet) requireStoredIn(t *testing.T, ctx context.Context, downloaded blob.ReadOnlyBlob) {
	t.Helper()
	r := require.New(t)

	layout, err := ocitar.ReadOCILayout(ctx, downloaded)
	r.NoError(err)
	t.Cleanup(func() { r.NoError(layout.Close()) })

	artifacts := layout.MainArtifacts(ctx)
	r.Len(artifacts, 1)
	r.Equal(s.root.Digest, artifacts[0].Digest)
	r.Equal(s.root.MediaType, artifacts[0].MediaType)

	for _, entry := range s.contents {
		got, err := content.FetchAll(ctx, layout, entry.desc)
		r.NoError(err, entry.desc.MediaType)
		r.Equal(entry.data, got, entry.desc.MediaType)
	}
}
