package oci_test

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"testing"

	"github.com/opencontainers/go-digest"
	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"oras.land/oras-go/v2"
	"oras.land/oras-go/v2/content"

	"ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/blob/filesystem"
	"ocm.software/open-component-model/bindings/go/blob/inmemory"
	"ocm.software/open-component-model/bindings/go/ctf"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/oci"
	ocictf "ocm.software/open-component-model/bindings/go/oci/ctf"
	"ocm.software/open-component-model/bindings/go/oci/internal/identity"
	"ocm.software/open-component-model/bindings/go/oci/resolver/url"
	"ocm.software/open-component-model/bindings/go/oci/spec"
	access "ocm.software/open-component-model/bindings/go/oci/spec/access"
	v1 "ocm.software/open-component-model/bindings/go/oci/spec/access/v1"
	"ocm.software/open-component-model/bindings/go/oci/spec/layout"
	"ocm.software/open-component-model/bindings/go/oci/tar"
	"ocm.software/open-component-model/bindings/go/repository"
	"ocm.software/open-component-model/bindings/go/runtime"
)

var testScheme = runtime.NewScheme()

func init() {
	access.MustAddToScheme(testScheme)
	v2.MustAddToScheme(testScheme)
}

func Repository(t *testing.T, options ...oci.RepositoryOption) *oci.Repository {
	opts := append([]oci.RepositoryOption{oci.WithTempDir(t.TempDir())}, options...)
	repo, err := oci.NewRepository(opts...)
	require.NoError(t, err, "Failed to create repository")
	return repo
}

func TestRepository_AddComponentVersion(t *testing.T) {
	r := require.New(t)
	ctx := t.Context()

	fs, err := filesystem.NewFS(t.TempDir(), os.O_RDWR)
	r.NoError(err)
	store := ocictf.NewFromCTF(ctf.NewFileSystemCTF(fs))
	repo := Repository(t, ocictf.WithCTF(store))

	// Create a test component descriptor
	desc := &descriptor.Descriptor{
		Component: descriptor.Component{
			Provider: descriptor.Provider{
				Name: "test-provider",
			},
			ComponentMeta: descriptor.ComponentMeta{
				ObjectMeta: descriptor.ObjectMeta{
					Name:    "test-component",
					Version: "1.0.0",
				},
			},
		},
	}
	_, err = repo.GetComponentVersion(ctx, desc.Component.Name, desc.Component.Version)
	r.Error(err)
	r.ErrorIs(err, repository.ErrNotFound)

	// Test adding component version
	err = repo.AddComponentVersion(ctx, desc)
	r.NoError(err, "Failed to add component version when it should succeed")

	err = repo.AddComponentVersion(ctx, desc)
	r.NoError(err, "Failed to add component version when it should succeed")

	desc2, err := repo.GetComponentVersion(ctx, desc.Component.Name, desc.Component.Version)
	r.NoError(err, "Failed to get component version after adding it")

	r.NotNil(desc2, "Component version should not be nil after adding it")
	r.Equal(desc.Component.Name, desc2.Component.Name, "Component name should match")
}

func TestRepository_GetComponentVersion(t *testing.T) {
	r := require.New(t)
	ctx := t.Context()

	fs, err := filesystem.NewFS(t.TempDir(), os.O_RDWR)
	r.NoError(err)
	store := ocictf.NewFromCTF(ctf.NewFileSystemCTF(fs))
	repo := Repository(t, ocictf.WithCTF(store))

	// Test getting non-existent component version
	desc, err := repo.GetComponentVersion(ctx, "test-component", "1.0.0")
	r.Error(err, "Expected error when getting non-existent component version")
	r.Nil(desc, "Expected nil descriptor when getting non-existent component version")

	// Create a test component descriptor
	desc = &descriptor.Descriptor{
		Component: descriptor.Component{
			Provider: descriptor.Provider{
				Name: "test-provider",
			},
			ComponentMeta: descriptor.ComponentMeta{
				ObjectMeta: descriptor.ObjectMeta{
					Name:    "test-component",
					Version: "1.0.0",
				},
			},
		},
	}
	// Test adding component version
	err = repo.AddComponentVersion(ctx, desc)
	r.NoError(err, "Failed to add component version when it should succeed")

	desc, err = repo.GetComponentVersion(ctx, "test-component", "1.0.0")
	r.NoError(err, "No error expected when getting existing component version")
	r.NotNil(desc, "Expected non-nil descriptor when getting existing component version")

}

func TestRepository_GetLocalResource(t *testing.T) {
	type getLocalResourceTestCase struct {
		name                     string
		resource                 *descriptor.Resource
		content                  []byte
		identity                 map[string]string
		expectError              bool
		errorContains            string
		setupComponent           bool
		setupComponentLikeOldOCM bool
		setupManifest            func(t *testing.T, store spec.Store, ctx context.Context, content []byte, resource *descriptor.Resource) error
		checkContent             func(t *testing.T, original []byte, actual []byte)
	}

	// Create test resources with different configurations
	testCases := []getLocalResourceTestCase{
		{
			name: "non-existent component",
			resource: &descriptor.Resource{
				ElementMeta: descriptor.ElementMeta{
					ObjectMeta: descriptor.ObjectMeta{
						Name:    "test-resource",
						Version: "1.0.0",
					},
				},
				Type: "test-type",
				Access: &v2.LocalBlob{
					LocalReference: "sha256:1234567890",
					MediaType:      "application/octet-stream",
				},
			},
			identity: map[string]string{
				"name":    "test-resource",
				"version": "1.0.0",
			},
			expectError:    true,
			setupComponent: false,
			checkContent: func(t *testing.T, original []byte, actual []byte) {
				assert.Equal(t, string(original), string(actual))
			},
		},
		{
			name: "non-existent resource in existing component",
			resource: &descriptor.Resource{
				ElementMeta: descriptor.ElementMeta{
					ObjectMeta: descriptor.ObjectMeta{
						Name:    "test-resource",
						Version: "1.0.0",
					},
				},
				Type: "test-type",
				Access: &v2.LocalBlob{
					LocalReference: digest.FromString("test content").String(),
					MediaType:      "application/octet-stream",
				},
			},
			identity: map[string]string{
				"name":    "test-resource",
				"version": "1.0.0",
			},
			setupComponent: true,
			checkContent: func(t *testing.T, original []byte, actual []byte) {
				assert.Equal(t, string(original), string(actual))
			},
		},
		{
			name: "resource with platform-specific identity",
			resource: &descriptor.Resource{
				ElementMeta: descriptor.ElementMeta{
					ObjectMeta: descriptor.ObjectMeta{
						Name:    "platform-resource",
						Version: "1.0.0",
					},
					ExtraIdentity: map[string]string{
						"architecture": "amd64",
						"os":           "linux",
					},
				},
				Type: "test-type",
				Access: &v2.LocalBlob{
					LocalReference: digest.FromString("platform specific content").String(),
					MediaType:      "application/octet-stream",
				},
			},
			content: []byte("platform specific content"),
			identity: map[string]string{
				"name":         "platform-resource",
				"version":      "1.0.0",
				"architecture": "amd64",
				"os":           "linux",
			},
			setupComponent: true,
			checkContent: func(t *testing.T, original []byte, actual []byte) {
				assert.Equal(t, string(original), string(actual))
			},
		},
		{
			name: "resource from legacy component version without top-level index",
			resource: &descriptor.Resource{
				ElementMeta: descriptor.ElementMeta{
					ObjectMeta: descriptor.ObjectMeta{
						Name:    "legacy-resource",
						Version: "1.0.0",
					},
				},
				Type: "test-type",
				Access: &v2.LocalBlob{
					LocalReference: digest.FromString("legacy content").String(),
					MediaType:      "application/octet-stream",
				},
			},
			content: []byte("legacy content"),
			identity: map[string]string{
				"name":    "legacy-resource",
				"version": "1.0.0",
			},
			setupComponentLikeOldOCM: true,
			checkContent: func(t *testing.T, original []byte, actual []byte) {
				assert.Equal(t, string(original), string(actual))
			},
		},
		{
			name: "resource with invalid identity",
			resource: &descriptor.Resource{
				ElementMeta: descriptor.ElementMeta{
					ObjectMeta: descriptor.ObjectMeta{
						Name:    "platform-resource",
						Version: "1.0.0",
					},
					ExtraIdentity: map[string]string{
						"architecture": "amd64",
						"os":           "linux",
					},
				},
				Type: "test-type",
				Access: &v2.LocalBlob{
					LocalReference: digest.FromString("platform specific content").String(),
					MediaType:      "application/octet-stream",
				},
			},
			identity: map[string]string{
				"name":    "test-resource",
				"version": "1.0.0",
			},
			expectError:    true,
			errorContains:  "found 0 candidates while looking for resource \"name=test-resource,version=1.0.0\", but expected exactly one",
			setupComponent: true,
		},
		{
			name: "single layer image manifest",
			resource: &descriptor.Resource{
				ElementMeta: descriptor.ElementMeta{
					ObjectMeta: descriptor.ObjectMeta{
						Name:    "single-layer-manifest",
						Version: "1.0.0",
					},
				},
				Type: "test-type",
				Access: &v2.LocalBlob{
					LocalReference: digest.FromString("single layer manifest content").String(),
					MediaType:      ociImageSpecV1.MediaTypeImageManifest,
				},
			},
			content: []byte("single layer manifest content"),
			identity: map[string]string{
				"name":    "single-layer-manifest",
				"version": "1.0.0",
			},
			setupComponent: true,
			checkContent: func(t *testing.T, original []byte, actual []byte) {
				assert.Equal(t, string(original), string(actual))
			},
		},
		{
			name: "oci layout resource",
			resource: &descriptor.Resource{
				ElementMeta: descriptor.ElementMeta{
					ObjectMeta: descriptor.ObjectMeta{
						Name:    "oci-layout-resource",
						Version: "1.0.0",
					},
				},
				Type: "test-type",
				Access: &v2.LocalBlob{
					LocalReference: digest.FromString("oci layout content").String(),
					MediaType:      layout.MediaTypeOCIImageLayoutV1 + "+tar+gzip",
				},
			},
			content: func(t *testing.T) []byte {
				// Create a buffer to hold the OCI layout
				buf := bytes.NewBuffer(nil)
				layout := tar.NewOCILayoutWriter(buf)

				// Create a descriptor for our content
				content := []byte("oci layout content")
				desc := ociImageSpecV1.Descriptor{
					MediaType: ociImageSpecV1.MediaTypeImageLayer,
					Digest:    digest.FromBytes(content),
					Size:      int64(len(content)),
				}

				// Push the content
				require.NoError(t, layout.Push(t.Context(), desc, bytes.NewReader(content)))

				// Create a manifest
				manifest, err := oras.PackManifest(t.Context(), layout, oras.PackManifestVersion1_1, ociImageSpecV1.MediaTypeImageManifest, oras.PackManifestOptions{
					Layers: []ociImageSpecV1.Descriptor{desc},
				})
				require.NoError(t, err, "Failed to create manifest")

				// Tag the manifest
				require.NoError(t, layout.Tag(t.Context(), manifest, "test-tag"))

				// Close the layout
				require.NoError(t, layout.Close())

				return buf.Bytes()
			}(t),
			checkContent: func(t *testing.T, original []byte, actual []byte) {
				r := require.New(t)
				store, err := tar.ReadOCILayout(t.Context(), inmemory.New(bytes.NewReader(original)))
				r.NoError(err, "Failed to read OCI layout")
				t.Cleanup(func() {
					r.NoError(store.Close(), "Failed to close blob reader")
				})
				r.Len(store.Index.Manifests, 1, "Expected one manifest in the OCI layout")
			},
			identity: map[string]string{
				"name":    "oci-layout-resource",
				"version": "1.0.0",
			},
			expectError:    false,
			setupComponent: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			r := require.New(t)
			ctx := t.Context()

			fs, err := filesystem.NewFS(t.TempDir(), os.O_RDWR)
			r.NoError(err)
			store := ocictf.NewFromCTF(ctf.NewFileSystemCTF(fs))
			repo := Repository(t, ocictf.WithCTF(store))

			// Create a test component descriptor
			desc := &descriptor.Descriptor{
				Component: descriptor.Component{
					Provider: descriptor.Provider{
						Name: "test-provider",
					},
					ComponentMeta: descriptor.ComponentMeta{
						ObjectMeta: descriptor.ObjectMeta{
							Name:    "test-component",
							Version: "1.0.0",
						},
					},
				},
			}
			desc.Component.Resources = append(desc.Component.Resources, *tc.resource)

			// Setup component if needed
			if tc.setupComponent {
				// Add the resource first
				b := inmemory.New(bytes.NewReader(tc.content))
				newRes, err := repo.AddLocalResource(ctx, desc.Component.Name, desc.Component.Version, tc.resource, b)
				r.NoError(err, "Failed to add test resource")
				r.NotNil(newRes, "Resource should not be nil after adding")
				desc.Component.Resources[0] = *newRes

				// Then add the component version
				err = repo.AddComponentVersion(ctx, desc)
				r.NoError(err, "Failed to setup test component")
			} else if tc.setupComponentLikeOldOCM {
				// Setup legacy component version
				setupLegacyComponentVersion(t, store, ctx, tc.content, tc.resource)
			}

			// Test getting the resource
			blob, _, err := repo.GetLocalResource(ctx, desc.Component.Name, desc.Component.Version, tc.identity)
			if tc.expectError {
				r.Error(err, "Expected error but got none")
				if tc.errorContains != "" {
					r.Contains(err.Error(), tc.errorContains, "Error message should contain expected text")
				}
				r.Nil(blob, "Blob should be nil when error occurs")
			} else {
				r.NoError(err, "Unexpected error when getting resource")
				r.NotNil(blob, "Blob should not be nil for successful retrieval")

				// Verify blob content if it was provided
				if tc.content != nil {
					reader, err := blob.ReadCloser()
					r.NoError(err, "Failed to get blob reader")
					defer reader.Close()

					content, err := io.ReadAll(reader)
					r.NoError(err, fmt.Errorf("failed to read blob content: %w", err))

					// If the content is gzipped (starts with gzip magic number), decompress it
					if len(content) >= 2 && content[0] == 0x1f && content[1] == 0x8b {
						gzipReader, err := gzip.NewReader(bytes.NewReader(content))
						r.NoError(err, "Failed to create gzip reader")
						defer gzipReader.Close()
						content, err = io.ReadAll(gzipReader)
						r.NoError(err, "Failed to decompress content")
					}
					tc.checkContent(t, tc.content, content)
				}
			}
		})
	}
}

func TestRepository_DownloadUploadResource(t *testing.T) {
	artifactMediaType := "application/custom"
	tests := []struct {
		name           string
		resource       *descriptor.Resource
		content        []byte
		wantErr        bool
		useLocalUpload bool
	}{
		{
			name: "resource with valid OCI image access",
			resource: &descriptor.Resource{
				ElementMeta: descriptor.ElementMeta{
					ObjectMeta: descriptor.ObjectMeta{
						Name:    "test-resource",
						Version: "1.0.0",
					},
				},
				Type: "ociImage",
				Access: &v1.OCIImage{
					ImageReference: "test-image:latest",
				},
			},
			content:        []byte("test content"),
			wantErr:        false,
			useLocalUpload: false,
		},
		{
			name: "resource with valid OCI image layer access",
			resource: &descriptor.Resource{
				ElementMeta: descriptor.ElementMeta{
					ObjectMeta: descriptor.ObjectMeta{
						Name:    "test-layer-resource",
						Version: "1.0.0",
					},
				},
				Type: "ociImageLayer",
				Access: &v2.LocalBlob{
					LocalReference: digest.FromString("test layer content").String(),
					MediaType:      artifactMediaType,
				},
			},
			content:        []byte("test layer content"),
			wantErr:        false,
			useLocalUpload: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := require.New(t)
			ctx := t.Context()

			// Create a mock resolver with a memory store
			fs, err := filesystem.NewFS(t.TempDir(), os.O_RDWR)
			r.NoError(err)
			store := ocictf.NewFromCTF(ctf.NewFileSystemCTF(fs))
			repo := Repository(t, ocictf.WithCTF(store))

			// Create a test component descriptor
			desc := &descriptor.Descriptor{
				Component: descriptor.Component{
					Provider: descriptor.Provider{
						Name: "test-provider",
					},
					ComponentMeta: descriptor.ComponentMeta{
						ObjectMeta: descriptor.ObjectMeta{
							Name:    "test-component",
							Version: "1.0.0",
						},
					},
				},
			}

			// Add the resource to the component descriptor
			desc.Component.Resources = append(desc.Component.Resources, *tc.resource)

			b := inmemory.New(bytes.NewReader(tc.content))

			var downloadedRes blob.ReadOnlyBlob
			if tc.useLocalUpload {
				// Use AddLocalResource for local uploads
				var newRes *descriptor.Resource
				newRes, err = repo.AddLocalResource(ctx, desc.Component.Name, desc.Component.Version, tc.resource, b)
				r.NoError(err, "Failed to add local resource")
				r.NotNil(newRes, "Resource should not be nil after adding")

				r.NotNil(newRes.Access)
				r.IsType(&v2.LocalBlob{}, newRes.Access)
				r.Nil(newRes.Access.(*v2.LocalBlob).GlobalAccess, "in CTF, there should not be any global access")

				// Add the component version
				err = repo.AddComponentVersion(ctx, desc)
				r.NoError(err, "Failed to add component version")

				// Try to get the resource back using GetResource with the global access
				downloadedRes, newRes, err = repo.GetLocalResource(ctx, desc.Component.Name, desc.Component.Version, newRes.ToIdentity())
				r.NoError(err, "Failed to download resource")
				r.NotNil(newRes, "received resource should not be nil")
				r.NotNil(downloadedRes, "downloaded resource blob should not be nil")
			} else {
				// Use UploadResource for global uploads
				// Create a temporary OCI store
				buf := bytes.NewBuffer(nil)
				store := tar.NewOCILayoutWriter(buf)

				base := content.NewDescriptorFromBytes("", tc.content)
				r.NoError(store.Push(ctx, base, bytes.NewReader(tc.content)), "Failed to push content to store")
				var manifestDesc ociImageSpecV1.Descriptor
				manifestDesc, err = oras.PackManifest(ctx, store, oras.PackManifestVersion1_1, artifactMediaType, oras.PackManifestOptions{
					Layers: []ociImageSpecV1.Descriptor{base},
				})
				r.NoError(err, "Failed to create manifest descriptor")

				// Tag the manifest
				err = store.Tag(ctx, manifestDesc, "test-image:latest")
				r.NoError(err, "Failed to tag manifest")

				r.NoError(store.Close())

				// Upload the resource with the store content
				b := inmemory.New(buf)
				newRes, err := repo.UploadResource(ctx, tc.resource, b)
				r.NoError(err, "Failed to upload test resource")
				r.NotNil(newRes, "Resource should not be nil after uploading")

				// Download the resource
				downloadedRes, err = repo.DownloadResource(ctx, newRes)
				r.NoError(err, "Failed to download resource")
				r.NotNil(downloadedRes, "Downloaded resource should not be nil")
			}

			if tc.wantErr {
				r.Error(err, "Expected error but got none")
				return
			}

			if tc.useLocalUpload {
				// for local resources, the resource is opinionated as single layer oci artifact, so we can directly
				// use the data
				var data bytes.Buffer
				r.NoError(blob.Copy(&data, downloadedRes), "Failed to copy blob content")
				r.Equal(tc.content, data.Bytes(), "Downloaded content should match original content")
			} else {
				// for global resources, the access is a generic oci layout that is not opinionated
				imageLayout, err := tar.ReadOCILayout(ctx, downloadedRes)
				r.NoError(err, "Failed to read OCI layout")
				t.Cleanup(func() {
					r.NoError(imageLayout.Close(), "Failed to close blob reader")
				})

				r.Len(imageLayout.Index.Manifests, 1, "Expected one manifest in the OCI layout")
				// Verify the downloaded content
				manifestRaw, err := imageLayout.Fetch(ctx, imageLayout.Index.Manifests[0])
				r.NoError(err, "Failed to fetch manifest")
				t.Cleanup(func() {
					r.NoError(manifestRaw.Close(), "Failed to close manifest reader")
				})
				var manifest ociImageSpecV1.Manifest
				r.NoError(json.NewDecoder(manifestRaw).Decode(&manifest), "Failed to unmarshal manifest")

				r.Equal(manifest.ArtifactType, artifactMediaType)

				r.Len(manifest.Layers, 1, "Expected one layer in the OCI layout")

				layer := manifest.Layers[0]

				layerRaw, err := imageLayout.Fetch(ctx, layer)
				r.NoError(err, "Failed to fetch layer")
				t.Cleanup(func() {
					r.NoError(layerRaw.Close(), "Failed to close layer reader")
				})

				downloadedContent, err := io.ReadAll(layerRaw)
				r.NoError(err, "Failed to read blob content")
				r.Equal(tc.content, downloadedContent, "Downloaded content should match original content")
			}

		})
	}
}

func TestRepository_DownloadUploadSource(t *testing.T) {
	artifactMediaType := "application/custom"
	tests := []struct {
		name           string
		source         *descriptor.Source
		content        []byte
		wantErr        bool
		useLocalUpload bool
	}{
		{
			name: "source with valid OCI image access",
			source: &descriptor.Source{
				ElementMeta: descriptor.ElementMeta{
					ObjectMeta: descriptor.ObjectMeta{
						Name:    "test-source",
						Version: "1.0.0",
					},
				},
				Type: "ociImage",
				Access: &v1.OCIImage{
					ImageReference: "test-image:latest",
				},
			},
			content:        []byte("test content"),
			wantErr:        false,
			useLocalUpload: false,
		},
		{
			name: "source with valid OCI image layer access",
			source: &descriptor.Source{
				ElementMeta: descriptor.ElementMeta{
					ObjectMeta: descriptor.ObjectMeta{
						Name:    "test-layer-source",
						Version: "1.0.0",
					},
				},
				Type: "ociImageLayer",
				Access: &v2.LocalBlob{
					LocalReference: digest.FromString("test layer content").String(),
					MediaType:      artifactMediaType,
				},
			},
			content:        []byte("test layer content"),
			wantErr:        false,
			useLocalUpload: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := require.New(t)
			ctx := t.Context()

			// Create a mock resolver with a memory store
			fs, err := filesystem.NewFS(t.TempDir(), os.O_RDWR)
			r.NoError(err)
			store := ocictf.NewFromCTF(ctf.NewFileSystemCTF(fs))
			repo := Repository(t, ocictf.WithCTF(store))

			// Create a test component descriptor
			desc := &descriptor.Descriptor{
				Component: descriptor.Component{
					Provider: descriptor.Provider{
						Name: "test-provider",
					},
					ComponentMeta: descriptor.ComponentMeta{
						ObjectMeta: descriptor.ObjectMeta{
							Name:    "test-component",
							Version: "1.0.0",
						},
					},
				},
			}

			// Add the source to the component descriptor
			desc.Component.Sources = append(desc.Component.Sources, *tc.source)

			b := inmemory.New(bytes.NewReader(tc.content))

			var downloadedSrc blob.ReadOnlyBlob
			if tc.useLocalUpload {
				// Use AddLocalSource for local uploads
				var newSrc *descriptor.Source
				newSrc, err = repo.AddLocalSource(ctx, desc.Component.Name, desc.Component.Version, tc.source, b)
				r.NoError(err, "Failed to add local source")
				r.NotNil(newSrc, "Source should not be nil after adding")

				r.NotNil(newSrc.Access)
				r.IsType(&v2.LocalBlob{}, newSrc.Access)
				r.Nil(newSrc.Access.(*v2.LocalBlob).GlobalAccess, "in CTF, there should not be any global access")

				// Add the component version
				err = repo.AddComponentVersion(ctx, desc)
				r.NoError(err, "Failed to add component version")

				// Try to get the source back using GetSource with the global access
				downloadedSrc, newSrc, err = repo.GetLocalSource(ctx, desc.Component.Name, desc.Component.Version, newSrc.ToIdentity())
				r.NoError(err, "Failed to download source")
				r.NotNil(newSrc, "received source should not be nil")
				r.NotNil(downloadedSrc, "downloaded source blob should not be nil")
			} else {
				// Use UploadSource for global uploads
				// Create a temporary OCI store
				buf := bytes.NewBuffer(nil)
				store := tar.NewOCILayoutWriter(buf)

				base := content.NewDescriptorFromBytes("", tc.content)
				r.NoError(store.Push(ctx, base, bytes.NewReader(tc.content)), "Failed to push content to store")
				var manifestDesc ociImageSpecV1.Descriptor
				manifestDesc, err = oras.PackManifest(ctx, store, oras.PackManifestVersion1_1, artifactMediaType, oras.PackManifestOptions{
					Layers: []ociImageSpecV1.Descriptor{base},
				})
				r.NoError(err, "Failed to create manifest descriptor")

				// Tag the manifest
				err = store.Tag(ctx, manifestDesc, "test-image:latest")
				r.NoError(err, "Failed to tag manifest")

				r.NoError(store.Close())

				// Upload the source with the store content
				b := inmemory.New(buf)
				newSrc, err := repo.UploadSource(ctx, tc.source, b)
				r.NoError(err, "Failed to upload test source")
				r.NotNil(newSrc, "Source should not be nil after uploading")

				// Download the source
				downloadedSrc, err = repo.DownloadSource(ctx, newSrc)
				r.NoError(err, "Failed to download source")
				r.NotNil(downloadedSrc, "Downloaded source should not be nil")
			}

			if tc.wantErr {
				r.Error(err, "Expected error but got none")
				return
			}

			if tc.useLocalUpload {
				// for local resources, the resource is opinionated as single layer oci artifact, so we can directly
				// use the data
				var data bytes.Buffer
				r.NoError(blob.Copy(&data, downloadedSrc), "Failed to copy blob content")
				r.Equal(tc.content, data.Bytes(), "Downloaded content should match original content")
			} else {
				// for global resources, the access is a generic oci layout that is not opinionated
				imageLayout, err := tar.ReadOCILayout(ctx, downloadedSrc)
				r.NoError(err, "Failed to read OCI layout")
				t.Cleanup(func() {
					r.NoError(imageLayout.Close(), "Failed to close blob reader")
				})

				r.Len(imageLayout.Index.Manifests, 1, "Expected one manifest in the OCI layout")
				// Verify the downloaded content
				manifestRaw, err := imageLayout.Fetch(ctx, imageLayout.Index.Manifests[0])
				r.NoError(err, "Failed to fetch manifest")
				t.Cleanup(func() {
					r.NoError(manifestRaw.Close(), "Failed to close manifest reader")
				})
				var manifest ociImageSpecV1.Manifest
				r.NoError(json.NewDecoder(manifestRaw).Decode(&manifest), "Failed to unmarshal manifest")

				r.Equal(manifest.ArtifactType, artifactMediaType)

				r.Len(manifest.Layers, 1, "Expected one layer in the OCI layout")

				layer := manifest.Layers[0]

				layerRaw, err := imageLayout.Fetch(ctx, layer)
				r.NoError(err, "Failed to fetch layer")
				t.Cleanup(func() {
					r.NoError(layerRaw.Close(), "Failed to close layer reader")
				})

				downloadedContent, err := io.ReadAll(layerRaw)
				r.NoError(err, "Failed to read blob content")
				r.Equal(tc.content, downloadedContent, "Downloaded content should match original content")
			}
		})
	}
}

func TestRepository_AddLocalResourceOCILayout(t *testing.T) {
	r := require.New(t)
	ctx := t.Context()

	fs, err := filesystem.NewFS(t.TempDir(), os.O_RDWR)
	r.NoError(err)
	store := ocictf.NewFromCTF(ctf.NewFileSystemCTF(fs))
	repo := Repository(t, ocictf.WithCTF(store))

	// Create a test component descriptor
	desc := &descriptor.Descriptor{
		Component: descriptor.Component{
			Provider: descriptor.Provider{
				Name: "test-provider",
			},
			ComponentMeta: descriptor.ComponentMeta{
				ObjectMeta: descriptor.ObjectMeta{
					Name:    "test-component",
					Version: "1.0.0",
				},
			},
		},
	}

	data, _ := createSingleLayerOCIImage(t, []byte("test content"), "test-image:latest")

	// Create a resource with OCI layout media type
	resource := &descriptor.Resource{
		ElementMeta: descriptor.ElementMeta{
			ObjectMeta: descriptor.ObjectMeta{
				Name:    "test-resource",
				Version: "1.0.0",
			},
		},
		Type: "test-type",
		Access: &v2.LocalBlob{
			LocalReference: digest.FromBytes(data).String(),
			MediaType:      layout.MediaTypeOCIImageLayoutV1 + "+tar",
		},
	}

	// Add the resource to the component descriptor
	desc.Component.Resources = append(desc.Component.Resources, *resource)

	// Add the OCI layout as a local resource
	b := inmemory.New(bytes.NewReader(data))
	newRes, err := repo.AddLocalResource(ctx, desc.Component.Name, desc.Component.Version, resource, b)
	r.NoError(err, "Failed to add OCI layout resource")
	r.NotNil(newRes, "Resource should not be nil after adding")
	desc.Component.Resources[0] = *newRes

	// Add the component version
	err = repo.AddComponentVersion(ctx, desc)
	r.NoError(err, "Failed to add component version")

	// Try to get the resource back
	blob, _, err := repo.GetLocalResource(ctx, desc.Component.Name, desc.Component.Version, map[string]string{
		"name":    "test-resource",
		"version": "1.0.0",
	})
	r.NoError(err, "Failed to get OCI layout resource")
	r.NotNil(blob, "Blob should not be nil")

	layout, err := tar.ReadOCILayout(ctx, blob)
	r.NoError(err, "Failed to read OCI layout")
	t.Cleanup(func() {
		r.NoError(layout.Close(), "Failed to close OCI layout")
	})
	r.Len(layout.Index.Manifests, 1)
}

func TestRepository_AddLocalResourceOCIImageLayer(t *testing.T) {
	r := require.New(t)
	ctx := t.Context()

	fs, err := filesystem.NewFS(t.TempDir(), os.O_RDWR)
	r.NoError(err)
	store := ocictf.NewFromCTF(ctf.NewFileSystemCTF(fs))
	repo := Repository(t, ocictf.WithCTF(store))

	// Create a test component descriptor
	desc := &descriptor.Descriptor{
		Component: descriptor.Component{
			Provider: descriptor.Provider{
				Name: "test-provider",
			},
			ComponentMeta: descriptor.ComponentMeta{
				ObjectMeta: descriptor.ObjectMeta{
					Name:    "test-component",
					Version: "1.0.0",
				},
			},
		},
	}

	content := []byte("test layer content")
	contentDigest := digest.FromBytes(content)

	// Create a resource with OCI image layer media type
	resource := &descriptor.Resource{
		ElementMeta: descriptor.ElementMeta{
			ObjectMeta: descriptor.ObjectMeta{
				Name:    "test-layer-resource",
				Version: "1.0.0",
			},
		},
		Type: "ociImageLayer",
		Access: &v2.LocalBlob{
			LocalReference: contentDigest.String(),
			MediaType:      ociImageSpecV1.MediaTypeImageLayer,
		},
	}

	// Add the resource to the component descriptor
	desc.Component.Resources = append(desc.Component.Resources, *resource)

	// Add the OCI image layer as a local resource
	b := inmemory.New(bytes.NewReader(content))
	newRes, err := repo.AddLocalResource(ctx, desc.Component.Name, desc.Component.Version, resource, b)
	r.NoError(err, "Failed to add OCI image layer resource")
	r.NotNil(newRes, "Resource should not be nil after adding")

	// Add the component version
	err = repo.AddComponentVersion(ctx, desc)
	r.NoError(err, "Failed to add component version")

	// Try to get the resource back
	blob, _, err := repo.GetLocalResource(ctx, desc.Component.Name, desc.Component.Version, map[string]string{
		"name":    "test-layer-resource",
		"version": "1.0.0",
	})
	r.NoError(err, "Failed to get OCI image layer resource")
	r.NotNil(blob, "Blob should not be nil")

	// Verify the content
	reader, err := blob.ReadCloser()
	r.NoError(err, "Failed to get blob reader")
	defer reader.Close()

	downloadedContent, err := io.ReadAll(reader)
	r.NoError(err, "Failed to read blob content")
	r.Equal(content, downloadedContent, "Downloaded content should match original content")
}

func createSingleLayerOCIImage(t *testing.T, data []byte, ref string) ([]byte, *v1.OCIImage) {
	r := require.New(t)
	var buf bytes.Buffer
	w := tar.NewOCILayoutWriter(&buf)

	desc := content.NewDescriptorFromBytes(ociImageSpecV1.MediaTypeImageLayer, data)
	r.NoError(w.Push(t.Context(), desc, bytes.NewReader(data)))

	manifest, err := oras.PackManifest(t.Context(), w, oras.PackManifestVersion1_1, ociImageSpecV1.MediaTypeImageLayer, oras.PackManifestOptions{
		Layers: []ociImageSpecV1.Descriptor{desc},
	})
	r.NoError(err)

	r.NoError(w.Push(t.Context(), desc, bytes.NewReader(data)))

	r.NoError(w.Tag(t.Context(), manifest, ref))

	r.NoError(w.Close())

	return buf.Bytes(), &v1.OCIImage{
		ImageReference: ref,
	}
}

func TestRepository_ListComponentVersions(t *testing.T) {
	r := require.New(t)
	ctx := t.Context()

	fs, err := filesystem.NewFS(t.TempDir(), os.O_RDWR)
	r.NoError(err)
	store := ocictf.NewFromCTF(ctf.NewFileSystemCTF(fs))
	repo := Repository(t, ocictf.WithCTF(store))

	// Test listing versions for non-existent component
	versions, err := repo.ListComponentVersions(ctx, "non-existent-component")
	r.NoError(err, "Listing versions for non-existent component should not error")
	r.Empty(versions, "Should return empty list for non-existent component")

	// Add multiple component versions
	versionsToAdd := []string{"1.0.0", "2.0.0", "1.1.0", "2.1.0"}
	for _, version := range versionsToAdd {
		desc := &descriptor.Descriptor{
			Component: descriptor.Component{
				Provider: descriptor.Provider{
					Name: "test-provider",
				},
				ComponentMeta: descriptor.ComponentMeta{
					ObjectMeta: descriptor.ObjectMeta{
						Name:    "test-component",
						Version: version,
					},
				},
			},
		}
		err := repo.AddComponentVersion(ctx, desc)
		r.NoError(err, "Failed to add component version %s", version)
	}

	// Test listing versions
	versions, err = repo.ListComponentVersions(ctx, "test-component")
	r.NoError(err, "Failed to list component versions")
	r.Len(versions, len(versionsToAdd), "Should return all added versions")

	// Verify versions are sorted in descending order
	expectedOrder := []string{"2.1.0", "2.0.0", "1.1.0", "1.0.0"}
	r.Equal(expectedOrder, versions, "Versions should be sorted in descending order")
}

func setupLegacyComponentVersion(t *testing.T, store *ocictf.Store, ctx context.Context, content []byte, resource *descriptor.Resource) {
	r := require.New(t)
	// Get a repository store for the component
	repoStore, err := store.StoreForReference(t.Context(), store.ComponentVersionReference(t.Context(), "test-component", "1.0.0"))
	r.NoError(err)

	// Create a descriptor for the component version
	desc := &descriptor.Descriptor{
		Component: descriptor.Component{
			Provider: descriptor.Provider{
				Name: "test-provider",
			},
			ComponentMeta: descriptor.ComponentMeta{
				ObjectMeta: descriptor.ObjectMeta{
					Name:    "test-component",
					Version: "1.0.0",
				},
			},
		},
	}
	desc.Component.Resources = append(desc.Component.Resources, *resource)

	// Create a layer descriptor for the component version
	layerDesc := ociImageSpecV1.Descriptor{
		MediaType: ociImageSpecV1.MediaTypeImageLayer,
		Digest:    digest.FromBytes(content),
		Size:      int64(len(content)),
	}
	res := resource.DeepCopy()
	res.Version = ""
	r.NoError(identity.Adopt(&layerDesc, res))

	// Push the component version as a layer
	r.NoError(repoStore.Push(ctx, layerDesc, bytes.NewReader(content)))

	topDesc, err := oci.AddDescriptorToStore(ctx, repoStore, desc, oci.AddDescriptorOptions{
		Scheme:           oci.DefaultRepositoryScheme,
		Author:           "OLD OCM",
		AdditionalLayers: []ociImageSpecV1.Descriptor{layerDesc},
	})
	r.NoError(err)
	r.NoError(repoStore.Tag(ctx, *topDesc, "1.0.0"))
}

func setupLegacyComponentVersionWithSource(t *testing.T, store *ocictf.Store, ctx context.Context, content []byte, source *descriptor.Source) {
	r := require.New(t)
	// Get a repository store for the component
	repoStore, err := store.StoreForReference(t.Context(), store.ComponentVersionReference(t.Context(), "test-component", "1.0.0"))
	r.NoError(err)

	// Create a descriptor for the component version
	desc := &descriptor.Descriptor{
		Component: descriptor.Component{
			ComponentMeta: descriptor.ComponentMeta{
				ObjectMeta: descriptor.ObjectMeta{
					Name:    "test-component",
					Version: "1.0.0",
				},
			},
		},
	}
	desc.Component.Sources = append(desc.Component.Sources, *source)

	// Create a layer descriptor for the component version
	layerDesc := ociImageSpecV1.Descriptor{
		MediaType: ociImageSpecV1.MediaTypeImageLayer,
		Digest:    digest.FromBytes(content),
		Size:      int64(len(content)),
	}
	r.NoError(identity.Adopt(&layerDesc, source))

	// Push the component version as a layer
	r.NoError(repoStore.Push(ctx, layerDesc, bytes.NewReader(content)))

	topDesc, err := oci.AddDescriptorToStore(ctx, repoStore, desc, oci.AddDescriptorOptions{
		Author:           "OLD OCM",
		AdditionalLayers: []ociImageSpecV1.Descriptor{layerDesc},
	})
	r.NoError(err)
	r.NoError(repoStore.Tag(ctx, *topDesc, "1.0.0"))
}

func TestRepository_GetLocalSource(t *testing.T) {
	type getLocalSourceTestCase struct {
		name                     string
		source                   *descriptor.Source
		content                  []byte
		identity                 map[string]string
		expectError              bool
		errorContains            string
		setupComponent           bool
		setupComponentLikeOldOCM bool
		setupManifest            func(t *testing.T, store spec.Store, ctx context.Context, content []byte, source *descriptor.Source) error
		checkContent             func(t *testing.T, original []byte, actual []byte)
	}

	// Create test sources with different configurations
	testCases := []getLocalSourceTestCase{
		{
			name: "non-existent component",
			source: &descriptor.Source{
				ElementMeta: descriptor.ElementMeta{
					ObjectMeta: descriptor.ObjectMeta{
						Name:    "test-source",
						Version: "1.0.0",
					},
				},
				Type: "test-type",
				Access: &v2.LocalBlob{
					LocalReference: "sha256:1234567890",
					MediaType:      "application/octet-stream",
				},
			},
			identity: map[string]string{
				"name":    "test-source",
				"version": "1.0.0",
			},
			expectError:    true,
			setupComponent: false,
			checkContent: func(t *testing.T, original []byte, actual []byte) {
				assert.Equal(t, string(original), string(actual))
			},
		},
		{
			name: "non-existent source in existing component",
			source: &descriptor.Source{
				ElementMeta: descriptor.ElementMeta{
					ObjectMeta: descriptor.ObjectMeta{
						Name:    "test-source",
						Version: "1.0.0",
					},
				},
				Type: "test-type",
				Access: &v2.LocalBlob{
					LocalReference: digest.FromString("test content").String(),
				},
			},
			identity: map[string]string{
				"name":    "test-source",
				"version": "1.0.0",
			},
			setupComponent: true,
			checkContent: func(t *testing.T, original []byte, actual []byte) {
				assert.Equal(t, string(original), string(actual))
			},
		},
		{
			name: "source with platform-specific identity",
			source: &descriptor.Source{
				ElementMeta: descriptor.ElementMeta{
					ObjectMeta: descriptor.ObjectMeta{
						Name:    "platform-source",
						Version: "1.0.0",
					},
					ExtraIdentity: map[string]string{
						"architecture": "amd64",
						"os":           "linux",
					},
				},
				Type: "test-type",
				Access: &v2.LocalBlob{
					LocalReference: digest.FromString("platform specific content").String(),
				},
			},
			content: []byte("platform specific content"),
			identity: map[string]string{
				"name":         "platform-source",
				"version":      "1.0.0",
				"architecture": "amd64",
				"os":           "linux",
			},
			setupComponent: true,
			checkContent: func(t *testing.T, original []byte, actual []byte) {
				assert.Equal(t, string(original), string(actual))
			},
		},
		{
			name: "source with invalid identity",
			source: &descriptor.Source{
				ElementMeta: descriptor.ElementMeta{
					ObjectMeta: descriptor.ObjectMeta{
						Name:    "platform-source",
						Version: "1.0.0",
					},
					ExtraIdentity: map[string]string{
						"architecture": "amd64",
						"os":           "linux",
					},
				},
				Type: "test-type",
				Access: &v2.LocalBlob{
					LocalReference: digest.FromString("platform specific content").String(),
				},
			},
			identity: map[string]string{
				"name":    "test-source",
				"version": "1.0.0",
			},
			expectError:    true,
			errorContains:  "found 0 candidates while looking for source \"name=test-source,version=1.0.0\", but expected exactly one",
			setupComponent: true,
		},
		{
			name: "oci layout source",
			source: &descriptor.Source{
				ElementMeta: descriptor.ElementMeta{
					ObjectMeta: descriptor.ObjectMeta{
						Name:    "oci-layout-source",
						Version: "1.0.0",
					},
				},
				Type: "test-type",
				Access: &v2.LocalBlob{
					LocalReference: digest.FromString("oci layout content").String(),
					MediaType:      layout.MediaTypeOCIImageLayoutV1 + "+tar+gzip",
				},
			},
			content: func(t *testing.T) []byte {
				// Create a buffer to hold the OCI layout
				buf := bytes.NewBuffer(nil)
				layout := tar.NewOCILayoutWriter(buf)

				// Create a descriptor for our content
				content := []byte("oci layout content")
				desc := ociImageSpecV1.Descriptor{
					MediaType: ociImageSpecV1.MediaTypeImageLayer,
					Digest:    digest.FromBytes(content),
					Size:      int64(len(content)),
				}

				// Push the content
				err := layout.Push(t.Context(), desc, bytes.NewReader(content))
				require.NoError(t, err)

				// Create a manifest
				manifest, err := oras.PackManifest(t.Context(), layout, oras.PackManifestVersion1_1, ociImageSpecV1.MediaTypeImageManifest, oras.PackManifestOptions{
					Layers: []ociImageSpecV1.Descriptor{desc},
				})
				require.NoError(t, err)

				// Tag the manifest
				require.NoError(t, layout.Tag(t.Context(), manifest, "test-tag"))

				// Close the layout
				require.NoError(t, layout.Close())

				return buf.Bytes()
			}(t),
			checkContent: func(t *testing.T, original []byte, actual []byte) {
				r := require.New(t)
				store, err := tar.ReadOCILayout(t.Context(), inmemory.New(bytes.NewReader(original)))
				r.NoError(err, "Failed to read OCI layout")
				t.Cleanup(func() {
					r.NoError(store.Close(), "Failed to close blob reader")
				})
				r.Len(store.Index.Manifests, 1, "Expected one manifest in the OCI layout")
			},
			identity: map[string]string{
				"name":    "oci-layout-source",
				"version": "1.0.0",
			},
			expectError:    false,
			setupComponent: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			r := require.New(t)
			ctx := t.Context()

			fs, err := filesystem.NewFS(t.TempDir(), os.O_RDWR)
			r.NoError(err)
			store := ocictf.NewFromCTF(ctf.NewFileSystemCTF(fs))
			repo := Repository(t, ocictf.WithCTF(store))

			// Create a test component descriptor
			desc := &descriptor.Descriptor{
				Component: descriptor.Component{
					Provider: descriptor.Provider{
						Name: "test-provider",
					},
					ComponentMeta: descriptor.ComponentMeta{
						ObjectMeta: descriptor.ObjectMeta{
							Name:    "test-component",
							Version: "1.0.0",
						},
					},
				},
			}
			desc.Component.Sources = append(desc.Component.Sources, *tc.source)

			// Setup component if needed
			if tc.setupComponent {
				// Add the source first
				b := inmemory.New(bytes.NewReader(tc.content))
				newSrc, err := repo.AddLocalSource(ctx, desc.Component.Name, desc.Component.Version, tc.source, b)
				r.NoError(err, "Failed to add test source")
				r.NotNil(newSrc, "Source should not be nil after adding")
				desc.Component.Sources[0] = *newSrc

				// Then add the component version
				err = repo.AddComponentVersion(ctx, desc)
				r.NoError(err, "Failed to setup test component")
			} else if tc.setupComponentLikeOldOCM {
				// Setup legacy component version
				setupLegacyComponentVersionWithSource(t, store, ctx, tc.content, tc.source)
			}

			// Test getting the source
			blob, _, err := repo.GetLocalSource(ctx, desc.Component.Name, desc.Component.Version, tc.identity)

			if tc.expectError {
				r.Error(err, "Expected error but got none")
				if tc.errorContains != "" {
					r.Contains(err.Error(), tc.errorContains, "Error message should contain expected text")
				}
				r.Nil(blob, "Blob should be nil when error occurs")
			} else {
				r.NoError(err, "Unexpected error when getting source")
				r.NotNil(blob, "Blob should not be nil for successful retrieval")

				// Verify blob content if it was provided
				if tc.content != nil {
					reader, err := blob.ReadCloser()
					r.NoError(err, "Failed to get blob reader")
					defer reader.Close()

					content, err := io.ReadAll(reader)
					r.NoError(err, "Failed to read blob content")

					// If the content is gzipped (starts with gzip magic number), decompress it
					if len(content) >= 2 && content[0] == 0x1f && content[1] == 0x8b {
						gzipReader, err := gzip.NewReader(bytes.NewReader(content))
						r.NoError(err, "Failed to create gzip reader")
						defer gzipReader.Close()
						content, err = io.ReadAll(gzipReader)
						r.NoError(err, "Failed to decompress content")
					}
					tc.checkContent(t, tc.content, content)
				}
			}
		})
	}
}

func TestRepository_ProcessResourceDigest(t *testing.T) {
	r := require.New(t)
	ctx := t.Context()

	fs, err := filesystem.NewFS(t.TempDir(), os.O_RDWR)
	r.NoError(err)
	ctf := ctf.NewFileSystemCTF(fs)
	store := ocictf.NewFromCTF(ctf)
	repo := Repository(t, ocictf.WithCTF(store))

	testdata := []byte("test content")
	dig := digest.FromBytes(testdata)

	tests := []struct {
		name     string
		resource *descriptor.Resource
		setup    func(t *testing.T)
		check    func(*descriptor.Resource) error
		err      assert.ErrorAssertionFunc
	}{
		{
			name: "local blob without global access",
			resource: &descriptor.Resource{
				ElementMeta: descriptor.ElementMeta{
					ObjectMeta: descriptor.ObjectMeta{
						Name:    "test-resource",
						Version: "1.0.0",
					},
				},
				Type: "test-type",
				Access: &v2.LocalBlob{
					LocalReference: "test-ref",
					MediaType:      "application/octet-stream",
				},
			},
			err: assert.Error,
		},
		{
			name: "oci image with differing actual content and specified digest",
			resource: &descriptor.Resource{
				ElementMeta: descriptor.ElementMeta{
					ObjectMeta: descriptor.ObjectMeta{
						Name:    "test-resource",
						Version: "1.0.0",
					},
				},
				Type: "test-type",
				Access: &v1.OCIImage{
					ImageReference: "test-registry/test-image:v2.0.0@" + dig.String(),
				},
			},
			setup: func(t *testing.T) {
				ctx := t.Context()
				r := require.New(t)
				store, err := store.StoreForReference(ctx, "test-registry/test-image:v2.0.0")
				r.NoError(err, "Failed to get store for test registry")

				testdata := []byte("test content with differing digest")
				desc := content.NewDescriptorFromBytes(ociImageSpecV1.MediaTypeImageLayer, testdata)
				data := bytes.NewReader(testdata)

				r.NoError(store.Push(ctx, desc, data))
				r.NoError(store.Tag(ctx, desc, "v2.0.0"))
			},
			err: func(t assert.TestingT, err error, i ...interface{}) bool {
				return assert.ErrorContains(t, err, "expected pinned digest")
			},
		},
		{
			name: "oci image with digest",
			resource: &descriptor.Resource{
				ElementMeta: descriptor.ElementMeta{
					ObjectMeta: descriptor.ObjectMeta{
						Name:    "test-resource",
						Version: "1.0.0",
					},
				},
				Type: "test-type",
				Access: &v1.OCIImage{
					ImageReference: "test-registry/test-image:v2.0.0@" + dig.String(),
				},
			},
			setup: func(t *testing.T) {
				ctx := t.Context()
				r := require.New(t)
				store, err := store.StoreForReference(ctx, "test-registry/test-image:v2.0.0")
				r.NoError(err, "Failed to get store for test registry")

				desc := content.NewDescriptorFromBytes(ociImageSpecV1.MediaTypeImageLayer, testdata)
				data := bytes.NewReader(testdata)

				r.NoError(store.Push(ctx, desc, data))
				r.NoError(store.Tag(ctx, desc, "v2.0.0"))
			},
			check: func(resource *descriptor.Resource) error {
				r := require.New(t)
				// Check if the resource has the expected access
				ociImage, ok := resource.Access.(*v1.OCIImage)
				r.True(ok, "Access should be of type v1.OCIImage")
				r.Equal("test-registry/test-image:v2.0.0@"+dig.String(), ociImage.ImageReference, "Image reference should match expected value")
				return nil
			},
		},
		{
			name: "oci image without digest gets processed and is validated",
			resource: &descriptor.Resource{
				ElementMeta: descriptor.ElementMeta{
					ObjectMeta: descriptor.ObjectMeta{
						Name:    "test-resource",
						Version: "1.0.0",
					},
				},
				Type: "test-type",
				Access: &v1.OCIImage{
					ImageReference: "test-registry/test-image:v2.0.0",
				},
			},
			setup: func(t *testing.T) {
				ctx := t.Context()
				r := require.New(t)
				store, err := store.StoreForReference(ctx, "test-registry/test-image:v2.0.0")
				r.NoError(err, "Failed to get store for test registry")

				desc := content.NewDescriptorFromBytes(ociImageSpecV1.MediaTypeImageLayer, testdata)
				data := bytes.NewReader(testdata)

				r.NoError(store.Push(ctx, desc, data))
				r.NoError(store.Tag(ctx, desc, "v2.0.0"))
			},
			check: func(resource *descriptor.Resource) error {
				r := require.New(t)
				// Check if the resource has the expected access
				ociImage, ok := resource.Access.(*v1.OCIImage)
				r.True(ok, "Access should be of type v1.OCIImage")
				r.Equal("test-registry/test-image:v2.0.0@"+dig.String(), ociImage.ImageReference, "Image reference should match expected value")
				return nil
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.setup != nil {
				tt.setup(t)
			}

			res, err := repo.ProcessResourceDigest(ctx, tt.resource)
			if tt.err != nil && !tt.err(t, err) {
				return
			}

			if tt.check != nil {
				err := tt.check(res)
				r.NoError(err)
			}
		})
	}
}

func TestRepositoryHealthCheck(t *testing.T) {
	ctx := context.Background()

	t.Run("CTF health check succeeds", func(t *testing.T) {
		// Create a temporary CTF repository
		tmpdir := t.TempDir()
		fs, err := filesystem.NewFS(tmpdir, os.O_RDWR)
		require.NoError(t, err, "Failed to create filesystem")
		archive := ctf.NewFileSystemCTF(fs)

		// Create a repository with CTF
		repo := Repository(t,
			ocictf.WithCTF(ocictf.NewFromCTF(archive)),
			oci.WithScheme(testScheme),
		)

		// Test health check - should always succeed for CTF
		err = repo.CheckHealth(ctx)
		require.NoError(t, err)
	})

	t.Run("URL resolver health check with invalid URL fails", func(t *testing.T) {
		// Create a repository with URL resolver pointing to invalid URL
		resolver, err := url.New(url.WithBaseURL("http://invalid.nonexistent.domain"))
		require.NoError(t, err)

		repo := Repository(t,
			oci.WithResolver(resolver),
			oci.WithScheme(testScheme),
		)

		// Test health check - should fail for unreachable URL
		err = repo.CheckHealth(ctx)
		require.Error(t, err)
		// Should fail with ping error containing the domain
		assert.Contains(t, err.Error(), "failed to ping registry")
	})

	t.Run("URL resolver health check with malformed URL fails", func(t *testing.T) {
		// Create a repository with URL resolver pointing to malformed URL
		resolver, err := url.New(url.WithBaseURL("not-a-valid-url"))
		require.NoError(t, err)

		repo := Repository(t,
			oci.WithResolver(resolver),
			oci.WithScheme(testScheme),
		)

		// Test health check - should fail for malformed URL
		err = repo.CheckHealth(ctx)
		require.Error(t, err)
		// Should fail with ping error containing the URL
		assert.Contains(t, err.Error(), "failed to ping registry")
	})
}
