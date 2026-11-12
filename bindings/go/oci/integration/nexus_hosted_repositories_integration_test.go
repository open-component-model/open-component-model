package integration_test

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json/v2"
	"strings"
	"testing"

	"github.com/opencontainers/go-digest"
	"github.com/opencontainers/image-spec/specs-go"
	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/oras-project/oras-go/v3/content"
	"github.com/stretchr/testify/require"

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

	for _, repository := range nexusHostedRepositories {
		resolver, repo, endpoint := nexus.addHostedRepository(t, t.Context(), repository)

		t.Run(repository.name, func(t *testing.T) {
			t.Run("component version", func(t *testing.T) {
				r := require.New(t)
				ctx := t.Context()

				const component, version = "ocm.software/nexus-hosted-test", "v1.0.0"
				uploadDownloadBarebonesComponentVersion(t, repo, component, version)

				// Record what Nexus stored: OCM writes an OCI image manifest. If this holds, a
				// Nexus hosted repository accepts OCM component versions without format conversion.
				ref := resolver.ComponentVersionReference(ctx, component, version)
				store, err := resolver.StoreForReference(ctx, ref)
				r.NoError(err)
				desc, err := store.Resolve(ctx, ref)
				r.NoError(err)
				t.Logf("nexus stored %s as %s (%s)", ref, desc.MediaType, desc.Digest)
				r.Equal(ociImageSpecV1.MediaTypeImageManifest, desc.MediaType)
			})

			for _, image := range []struct{ name, path, rootMediaType string }{
				{name: "docker manifest", path: "legacy/manifest", rootMediaType: dockerManifestMediaType},
				{name: "docker manifest list", path: "legacy/manifest-list", rootMediaType: dockerManifestListMediaType},
				{name: "oci index over docker manifest", path: "legacy/oci-index", rootMediaType: ociImageSpecV1.MediaTypeImageIndex},
			} {
				t.Run(image.name, func(t *testing.T) {
					r := require.New(t)
					ctx := t.Context()
					set := newDockerArtifactSet(t, image.rootMediaType)

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

// dockerArtifactSet is a legacy OCM artifact set holding a Docker manifest. The OCM library
// synthesizes the manifest root for docker inputs, the OCI index root for dockermulti inputs,
// and the manifest list root for resources whose source is a manifest list.
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

func newDockerImageContent(mediaType string, data []byte) dockerImageContent {
	return dockerImageContent{desc: content.NewDescriptorFromBytes(mediaType, data), data: data}
}

// newDockerArtifactSet builds an artifact set whose root has rootMediaType: the Docker schema2
// manifest itself, or an index of that type referencing it. Its content is unique per test, so
// that no subtest finds its blobs or manifests already stored by another one.
func newDockerArtifactSet(t *testing.T, rootMediaType string) *dockerArtifactSet {
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
	layer := newDockerImageContent(dockerLayerMediaType, compressed.Bytes())

	configData, err := json.Marshal(ociImageSpecV1.Image{
		Architecture: "amd64",
		OS:           "linux",
		RootFS:       ociImageSpecV1.RootFS{Type: "layers", DiffIDs: []digest.Digest{digest.FromBytes(uncompressed.Bytes())}},
	})
	r.NoError(err)
	config := newDockerImageContent(dockerConfigMediaType, configData)

	manifestData, err := json.Marshal(ociImageSpecV1.Manifest{
		Versioned: specs.Versioned{SchemaVersion: 2},
		MediaType: dockerManifestMediaType,
		Config:    config.desc,
		Layers:    []ociImageSpecV1.Descriptor{layer.desc},
	})
	r.NoError(err)
	manifest := newDockerImageContent(dockerManifestMediaType, manifestData)

	set := &dockerArtifactSet{
		mediaType: strings.TrimSuffix(rootMediaType, "+json") + ctf.SynthesizedBlobFormatSuffix,
		root:      manifest.desc,
		contents:  []dockerImageContent{layer, config, manifest},
	}
	if rootMediaType != dockerManifestMediaType {
		entry := manifest.desc
		entry.Platform = &ociImageSpecV1.Platform{Architecture: "amd64", OS: "linux"}
		rootData, err := json.Marshal(ociImageSpecV1.Index{
			Versioned: specs.Versioned{SchemaVersion: 2},
			MediaType: rootMediaType,
			Manifests: []ociImageSpecV1.Descriptor{entry},
		})
		r.NoError(err)
		root := newDockerImageContent(rootMediaType, rootData)
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
