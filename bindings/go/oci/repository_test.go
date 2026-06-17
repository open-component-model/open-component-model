package oci_test

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"testing"

	"github.com/opencontainers/go-digest"
	"github.com/opencontainers/image-spec/specs-go"
	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"oras.land/oras-go/v2"
	"oras.land/oras-go/v2/content"
	"oras.land/oras-go/v2/content/memory"
	"oras.land/oras-go/v2/errdef"

	"ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/blob/filesystem"
	"ocm.software/open-component-model/bindings/go/blob/inmemory"
	"ocm.software/open-component-model/bindings/go/ctf"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/oci"
	ocictf "ocm.software/open-component-model/bindings/go/oci/ctf"
	"ocm.software/open-component-model/bindings/go/oci/internal/identity"
	"ocm.software/open-component-model/bindings/go/oci/internal/pack"
	"ocm.software/open-component-model/bindings/go/oci/resolver/url"
	"ocm.software/open-component-model/bindings/go/oci/spec"
	access "ocm.software/open-component-model/bindings/go/oci/spec/access"
	v1 "ocm.software/open-component-model/bindings/go/oci/spec/access/v1"
	"ocm.software/open-component-model/bindings/go/oci/spec/annotations"
	"ocm.software/open-component-model/bindings/go/oci/spec/layout"
	ocistream "ocm.software/open-component-model/bindings/go/oci/stream"
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
		Meta: descriptor.Meta{Version: "v2"},
		Component: descriptor.Component{
			Provider: descriptor.Provider{
				Name: "test-provider",
			},
			ComponentMeta: descriptor.ComponentMeta{
				ObjectMeta: descriptor.ObjectMeta{
					Name:    "ocm.software/test-component",
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
	desc, err := repo.GetComponentVersion(ctx, "ocm.software/test-component", "1.0.0")
	r.Error(err, "Expected error when getting non-existent component version")
	r.Nil(desc, "Expected nil descriptor when getting non-existent component version")

	// Create a test component descriptor
	desc = &descriptor.Descriptor{
		Meta: descriptor.Meta{Version: "v2"},
		Component: descriptor.Component{
			Provider: descriptor.Provider{
				Name: "test-provider",
			},
			ComponentMeta: descriptor.ComponentMeta{
				ObjectMeta: descriptor.ObjectMeta{
					Name:    "ocm.software/test-component",
					Version: "1.0.0",
				},
			},
		},
	}
	// Test adding component version
	err = repo.AddComponentVersion(ctx, desc)
	r.NoError(err, "Failed to add component version when it should succeed")

	desc, err = repo.GetComponentVersion(ctx, "ocm.software/test-component", "1.0.0")
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
				Relation: descriptor.LocalRelation,
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
				Relation: descriptor.LocalRelation,
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
				Relation: descriptor.LocalRelation,
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
				Relation: descriptor.LocalRelation,
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
				Relation: descriptor.LocalRelation,
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
				Relation: descriptor.LocalRelation,
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
				Relation: descriptor.LocalRelation,
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
				layout, err := tar.NewOCILayoutWriterWithTempFile(buf, t.TempDir())
				require.NoError(t, err)

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
				Meta: descriptor.Meta{Version: "v2"},
				Component: descriptor.Component{
					Provider: descriptor.Provider{
						Name: "test-provider",
					},
					ComponentMeta: descriptor.ComponentMeta{
						ObjectMeta: descriptor.ObjectMeta{
							Name:    "ocm.software/test-component",
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
				Relation: descriptor.LocalRelation,
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
				Relation: descriptor.LocalRelation,
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
				Meta: descriptor.Meta{Version: "v2"},
				Component: descriptor.Component{
					Provider: descriptor.Provider{
						Name: "test-provider",
					},
					ComponentMeta: descriptor.ComponentMeta{
						ObjectMeta: descriptor.ObjectMeta{
							Name:    "ocm.software/test-component",
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
				store, storeErr := tar.NewOCILayoutWriterWithTempFile(buf, t.TempDir())
				r.NoError(storeErr)

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
				Meta: descriptor.Meta{Version: "v2"},
				Component: descriptor.Component{
					Provider: descriptor.Provider{
						Name: "test-provider",
					},
					ComponentMeta: descriptor.ComponentMeta{
						ObjectMeta: descriptor.ObjectMeta{
							Name:    "ocm.software/test-component",
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
				store, storeErr := tar.NewOCILayoutWriterWithTempFile(buf, t.TempDir())
				r.NoError(storeErr)

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
		Meta: descriptor.Meta{Version: "v2"},
		Component: descriptor.Component{
			Provider: descriptor.Provider{
				Name: "test-provider",
			},
			ComponentMeta: descriptor.ComponentMeta{
				ObjectMeta: descriptor.ObjectMeta{
					Name:    "ocm.software/test-component",
					Version: "1.0.0",
				},
			},
		},
	}

	data, _ := createSingleLayerOCIImage(t, []byte("test content"), "test-image:latest")

	// Create a resource with OCI layout media type
	resource := &descriptor.Resource{
		Relation: descriptor.LocalRelation,
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
		Meta: descriptor.Meta{Version: "v2"},
		Component: descriptor.Component{
			Provider: descriptor.Provider{
				Name: "test-provider",
			},
			ComponentMeta: descriptor.ComponentMeta{
				ObjectMeta: descriptor.ObjectMeta{
					Name:    "ocm.software/test-component",
					Version: "1.0.0",
				},
			},
		},
	}

	content := []byte("test layer content")
	contentDigest := digest.FromBytes(content)

	// Create a resource with OCI image layer media type
	resource := &descriptor.Resource{
		Relation: descriptor.LocalRelation,
		ElementMeta: descriptor.ElementMeta{
			ObjectMeta: descriptor.ObjectMeta{
				Name:    "test-layer-resource",
				Version: "1.0.0",
			},
		},
		Type: "ociImageLayer",
		Access: &v2.LocalBlob{
			LocalReference: contentDigest.String(),
			ReferenceName:  "ocm/oci/repo:latest",
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
	blob, resource, err := repo.GetLocalResource(ctx, desc.Component.Name, desc.Component.Version, map[string]string{
		"name":    "test-layer-resource",
		"version": "1.0.0",
	})
	r.NotNil(resource)
	var localAccess v2.LocalBlob
	r.NoError(v2.Scheme.Convert(resource.Access, &localAccess))
	r.Equal(localAccess.ReferenceName, "ocm/oci/repo:latest", "Resource reference name should match expected value")

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
	w, err := tar.NewOCILayoutWriterWithTempFile(&buf, t.TempDir())
	r.NoError(err)

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
			Meta: descriptor.Meta{Version: "v2"},
			Component: descriptor.Component{
				Provider: descriptor.Provider{
					Name: "test-provider",
				},
				ComponentMeta: descriptor.ComponentMeta{
					ObjectMeta: descriptor.ObjectMeta{
						Name:    "ocm.software/test-component",
						Version: version,
					},
				},
			},
		}
		err := repo.AddComponentVersion(ctx, desc)
		r.NoError(err, "Failed to add component version %s", version)
	}

	// Test listing versions
	versions, err = repo.ListComponentVersions(ctx, "ocm.software/test-component")
	r.NoError(err, "Failed to list component versions")
	r.Len(versions, len(versionsToAdd), "Should return all added versions")

	// Verify versions are sorted in descending order
	expectedOrder := []string{"2.1.0", "2.0.0", "1.1.0", "1.0.0"}
	r.Equal(expectedOrder, versions, "Versions should be sorted in descending order")
}

func setupLegacyComponentVersion(t *testing.T, store *ocictf.Store, ctx context.Context, content []byte, resource *descriptor.Resource) {
	r := require.New(t)
	// Get a repository store for the component
	repoStore, err := store.StoreForReference(t.Context(), store.ComponentVersionReference(t.Context(), "ocm.software/test-component", "1.0.0"))
	r.NoError(err)

	// Create a descriptor for the component version
	desc := &descriptor.Descriptor{
		Meta: descriptor.Meta{Version: "v2"},
		Component: descriptor.Component{
			Provider: descriptor.Provider{
				Name: "test-provider",
			},
			ComponentMeta: descriptor.ComponentMeta{
				ObjectMeta: descriptor.ObjectMeta{
					Name:    "ocm.software/test-component",
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
	repoStore, err := store.StoreForReference(t.Context(), store.ComponentVersionReference(t.Context(), "ocm.software/test-component", "1.0.0"))
	r.NoError(err)

	// Create a descriptor for the component version
	desc := &descriptor.Descriptor{
		Meta: descriptor.Meta{Version: "v2"},
		Component: descriptor.Component{
			ComponentMeta: descriptor.ComponentMeta{
				ObjectMeta: descriptor.ObjectMeta{
					Name:    "ocm.software/test-component",
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
				layout, err := tar.NewOCILayoutWriterWithTempFile(buf, t.TempDir())
				require.NoError(t, err)

				// Create a descriptor for our content
				content := []byte("oci layout content")
				desc := ociImageSpecV1.Descriptor{
					MediaType: ociImageSpecV1.MediaTypeImageLayer,
					Digest:    digest.FromBytes(content),
					Size:      int64(len(content)),
				}

				// Push the content
				err = layout.Push(t.Context(), desc, bytes.NewReader(content))
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
				Meta: descriptor.Meta{Version: "v2"},
				Component: descriptor.Component{
					Provider: descriptor.Provider{
						Name: "test-provider",
					},
					ComponentMeta: descriptor.ComponentMeta{
						ObjectMeta: descriptor.ObjectMeta{
							Name:    "ocm.software/test-component",
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
				Relation: descriptor.LocalRelation,
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
				Relation: descriptor.LocalRelation,
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
				return assert.ErrorContains(t, err, "failed to verify existence of pinned digest")
			},
		},
		{
			name: "oci image with digest",
			resource: &descriptor.Resource{
				Relation: descriptor.LocalRelation,
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
				Relation: descriptor.LocalRelation,
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

func TestRepository_AddComponentVersionAlias(t *testing.T) {
	r := require.New(t)
	ctx := t.Context()

	fs, err := filesystem.NewFS(t.TempDir(), os.O_RDWR)
	r.NoError(err)
	store := ocictf.NewFromCTF(ctf.NewFileSystemCTF(fs))
	repo := Repository(t, ocictf.WithCTF(store))

	componentName := "ocm.software/test-component"

	// Helper to create descriptor
	makeDesc := func(version string) *descriptor.Descriptor {
		return &descriptor.Descriptor{
			Meta: descriptor.Meta{Version: "v2"},
			Component: descriptor.Component{
				Provider:      descriptor.Provider{Name: "test-provider"},
				ComponentMeta: descriptor.ComponentMeta{ObjectMeta: descriptor.ObjectMeta{Name: componentName, Version: version}},
			},
		}
	}

	// Add versions 1.0.0 and 2.0.0
	r.NoError(repo.AddComponentVersion(ctx, makeDesc("1.0.0")))
	r.NoError(repo.AddComponentVersion(ctx, makeDesc("2.0.0")))

	// Add multiple aliases to 1.0.0
	r.NoError(repo.AddComponentVersionAlias(ctx, componentName, "1.0.0", "latest"))
	r.NoError(repo.AddComponentVersionAlias(ctx, componentName, "1.0.0", "stable"))

	// Verify aliases resolve correctly
	for _, alias := range []string{"latest", "stable"} {
		got, err := repo.GetComponentVersion(ctx, componentName, alias)
		r.NoError(err)
		r.Equal("1.0.0", got.Component.Version)
	}

	got, err := repo.GetComponentVersion(ctx, componentName, "1.0.0")
	r.NoError(err, "original version must be retrievable after aliasing")
	r.Equal("1.0.0", got.Component.Version)

	// Move "latest" to 2.0.0
	r.NoError(repo.AddComponentVersionAlias(ctx, componentName, "2.0.0", "latest"))

	got, err = repo.GetComponentVersion(ctx, componentName, "latest")
	r.NoError(err)
	r.Equal("2.0.0", got.Component.Version, "latest should now point to 2.0.0")

	// Both versions must still be retrievable after alias move
	got, err = repo.GetComponentVersion(ctx, componentName, "1.0.0")
	r.NoError(err, "1.0.0 must be retrievable after alias moved")
	r.Equal("1.0.0", got.Component.Version)

	got, err = repo.GetComponentVersion(ctx, componentName, "2.0.0")
	r.NoError(err, "2.0.0 must be retrievable")
	r.Equal("2.0.0", got.Component.Version)

	// "stable" should still point to 1.0.0
	got, err = repo.GetComponentVersion(ctx, componentName, "stable")
	r.NoError(err)
	r.Equal("1.0.0", got.Component.Version, "stable should still point to 1.0.0")
}

func TestRepository_AddComponentVersionAlias_NonExistent(t *testing.T) {
	r := require.New(t)
	ctx := t.Context()

	fs, err := filesystem.NewFS(t.TempDir(), os.O_RDWR)
	r.NoError(err)
	store := ocictf.NewFromCTF(ctf.NewFileSystemCTF(fs))
	repo := Repository(t, ocictf.WithCTF(store))

	// Must fail for non-existent component
	err = repo.AddComponentVersionAlias(ctx, "non-existent", "1.0.0", "latest")
	r.Error(err)
	r.ErrorIs(err, repository.ErrNotFound)
}

func TestRepository_AddComponentVersionAlias_ChainedAndIdempotent(t *testing.T) {
	r := require.New(t)
	ctx := t.Context()

	fs, err := filesystem.NewFS(t.TempDir(), os.O_RDWR)
	r.NoError(err)
	store := ocictf.NewFromCTF(ctf.NewFileSystemCTF(fs))
	repo := Repository(t, ocictf.WithCTF(store))

	componentName := "ocm.software/test-component"
	desc := &descriptor.Descriptor{
		Meta: descriptor.Meta{Version: "v2"},
		Component: descriptor.Component{
			Provider:      descriptor.Provider{Name: "test-provider"},
			ComponentMeta: descriptor.ComponentMeta{ObjectMeta: descriptor.ObjectMeta{Name: componentName, Version: "1.0.0"}},
		},
	}
	r.NoError(repo.AddComponentVersion(ctx, desc))

	// Chain: version -> latest -> stable -> production
	r.NoError(repo.AddComponentVersionAlias(ctx, componentName, "1.0.0", "latest"))
	r.NoError(repo.AddComponentVersionAlias(ctx, componentName, "latest", "stable"))
	r.NoError(repo.AddComponentVersionAlias(ctx, componentName, "stable", "production"))

	// All must resolve to 1.0.0
	for _, ref := range []string{"1.0.0", "latest", "stable", "production"} {
		got, err := repo.GetComponentVersion(ctx, componentName, ref)
		r.NoError(err, "ref %s should resolve", ref)
		r.Equal("1.0.0", got.Component.Version)
	}

	// Idempotent: adding same alias multiple times should work
	for i := 0; i < 3; i++ {
		r.NoError(repo.AddComponentVersionAlias(ctx, componentName, "1.0.0", "latest"))
		got, err := repo.GetComponentVersion(ctx, componentName, "latest")
		r.NoError(err)
		r.Equal("1.0.0", got.Component.Version)
	}

	// version still retrievable after repeated operations
	got, err := repo.GetComponentVersion(ctx, componentName, "1.0.0")
	r.NoError(err)
	r.Equal("1.0.0", got.Component.Version)
}

func TestRepository_AddComponentVersionAlias_ManyVersionsStayRetrievable(t *testing.T) {
	r := require.New(t)
	ctx := t.Context()

	fs, err := filesystem.NewFS(t.TempDir(), os.O_RDWR)
	r.NoError(err)
	store := ocictf.NewFromCTF(ctf.NewFileSystemCTF(fs))
	repo := Repository(t, ocictf.WithCTF(store))

	componentName := "ocm.software/test-component"
	versions := []string{"1.0.0", "1.1.0", "2.0.0", "2.1.0"}

	// Add all versions
	for _, v := range versions {
		desc := &descriptor.Descriptor{
			Meta: descriptor.Meta{Version: "v2"},
			Component: descriptor.Component{
				Provider:      descriptor.Provider{Name: "test-provider"},
				ComponentMeta: descriptor.ComponentMeta{ObjectMeta: descriptor.ObjectMeta{Name: componentName, Version: v}},
			},
		}
		r.NoError(repo.AddComponentVersion(ctx, desc))
	}

	// Move "latest" through all versions
	for _, v := range versions {
		r.NoError(repo.AddComponentVersionAlias(ctx, componentName, v, "latest"))
	}

	// ALL versions must still be retrievable
	for _, v := range versions {
		got, err := repo.GetComponentVersion(ctx, componentName, v)
		r.NoError(err, "version %s must be retrievable after alias operations", v)
		r.Equal(v, got.Component.Version)
	}

	// "latest" should point to last version
	got, err := repo.GetComponentVersion(ctx, componentName, "latest")
	r.NoError(err)
	r.Equal("2.1.0", got.Component.Version)
}

func TestRepository_AddComponentVersionAlias_OCIImageIndex(t *testing.T) {
	// This test exercises the OCI image index code path in AddComponentVersionAlias.
	// When a component version has a local resource with an OCI layout media type,
	// AddComponentVersion wraps the manifest in an OCI image index. Aliasing must
	// work correctly through this index-based structure.
	r := require.New(t)
	ctx := t.Context()

	fs, err := filesystem.NewFS(t.TempDir(), os.O_RDWR)
	r.NoError(err)
	store := ocictf.NewFromCTF(ctf.NewFileSystemCTF(fs))
	repo := Repository(t, ocictf.WithCTF(store))

	componentName := "ocm.software/test-component"

	// Helper to create a component version with an OCI layout resource,
	// which triggers OCI image index creation in AddDescriptorToStore.
	addVersionWithOCILayoutResource := func(version string) {
		desc := &descriptor.Descriptor{
			Meta: descriptor.Meta{Version: "v2"},
			Component: descriptor.Component{
				Provider:      descriptor.Provider{Name: "test-provider"},
				ComponentMeta: descriptor.ComponentMeta{ObjectMeta: descriptor.ObjectMeta{Name: componentName, Version: version}},
			},
		}

		data, _ := createSingleLayerOCIImage(t, []byte("content-"+version), "image:"+version)

		resource := &descriptor.Resource{
			Relation: descriptor.LocalRelation,
			ElementMeta: descriptor.ElementMeta{
				ObjectMeta: descriptor.ObjectMeta{Name: "test-resource", Version: version},
			},
			Type: "ociImage",
			Access: &v2.LocalBlob{
				LocalReference: digest.FromBytes(data).String(),
				MediaType:      layout.MediaTypeOCIImageLayoutV1 + "+tar",
			},
		}
		desc.Component.Resources = append(desc.Component.Resources, *resource)

		newRes, err := repo.AddLocalResource(ctx, componentName, version, resource, inmemory.New(bytes.NewReader(data)))
		r.NoError(err)
		desc.Component.Resources[0] = *newRes

		r.NoError(repo.AddComponentVersion(ctx, desc))
	}

	addVersionWithOCILayoutResource("1.0.0")
	addVersionWithOCILayoutResource("2.0.0")

	// Alias "latest" to 1.0.0 (stored as an OCI image index)
	r.NoError(repo.AddComponentVersionAlias(ctx, componentName, "1.0.0", "latest"))

	// Verify the alias resolves to the correct component version
	got, err := repo.GetComponentVersion(ctx, componentName, "latest")
	r.NoError(err)
	r.Equal("1.0.0", got.Component.Version)

	// Original version must still be retrievable
	got, err = repo.GetComponentVersion(ctx, componentName, "1.0.0")
	r.NoError(err)
	r.Equal("1.0.0", got.Component.Version)

	// Move alias to 2.0.0
	r.NoError(repo.AddComponentVersionAlias(ctx, componentName, "2.0.0", "latest"))

	got, err = repo.GetComponentVersion(ctx, componentName, "latest")
	r.NoError(err)
	r.Equal("2.0.0", got.Component.Version, "latest should now point to 2.0.0")

	// Both original versions must remain retrievable after alias move
	got, err = repo.GetComponentVersion(ctx, componentName, "1.0.0")
	r.NoError(err, "1.0.0 must be retrievable after alias moved away")
	r.Equal("1.0.0", got.Component.Version)

	got, err = repo.GetComponentVersion(ctx, componentName, "2.0.0")
	r.NoError(err, "2.0.0 must be retrievable")
	r.Equal("2.0.0", got.Component.Version)

	// Resources must be accessible through the alias
	b, _, err := repo.GetLocalResource(ctx, componentName, "latest", map[string]string{
		"name":    "test-resource",
		"version": "2.0.0",
	})
	r.NoError(err, "resource should be accessible through alias")
	r.NotNil(b)

	// Chain alias through index-backed versions: latest -> stable
	r.NoError(repo.AddComponentVersionAlias(ctx, componentName, "latest", "stable"))
	got, err = repo.GetComponentVersion(ctx, componentName, "stable")
	r.NoError(err)
	r.Equal("2.0.0", got.Component.Version, "stable should resolve through latest to 2.0.0")
}

func TestRepository_ListComponentVersions_WithAliases(t *testing.T) {
	r := require.New(t)
	ctx := t.Context()

	fs, err := filesystem.NewFS(t.TempDir(), os.O_RDWR)
	r.NoError(err)
	store := ocictf.NewFromCTF(ctf.NewFileSystemCTF(fs))
	repo := Repository(t, ocictf.WithCTF(store))

	componentName := "ocm.software/test-component"

	makeDesc := func(version string) *descriptor.Descriptor {
		return &descriptor.Descriptor{
			Meta: descriptor.Meta{Version: "v2"},
			Component: descriptor.Component{
				Provider:      descriptor.Provider{Name: "test-provider"},
				ComponentMeta: descriptor.ComponentMeta{ObjectMeta: descriptor.ObjectMeta{Name: componentName, Version: version}},
			},
		}
	}

	// Add three versions
	r.NoError(repo.AddComponentVersion(ctx, makeDesc("1.0.0")))
	r.NoError(repo.AddComponentVersion(ctx, makeDesc("2.0.0")))
	r.NoError(repo.AddComponentVersion(ctx, makeDesc("3.0.0")))

	// Create multiple aliases pointing to 1.0.0
	r.NoError(repo.AddComponentVersionAlias(ctx, componentName, "1.0.0", "latest"))
	r.NoError(repo.AddComponentVersionAlias(ctx, componentName, "1.0.0", "stable"))

	// ListComponentVersions must return exactly the 3 unique versions, no duplicates from aliases
	versions, err := repo.ListComponentVersions(ctx, componentName)
	r.NoError(err)
	r.Equal([]string{"3.0.0", "2.0.0", "1.0.0"}, versions,
		"aliases must not produce duplicate entries in version listing")

	// Move "latest" to 3.0.0 — listing should still show exactly 3 unique versions
	r.NoError(repo.AddComponentVersionAlias(ctx, componentName, "3.0.0", "latest"))

	versions, err = repo.ListComponentVersions(ctx, componentName)
	r.NoError(err)
	r.Equal([]string{"3.0.0", "2.0.0", "1.0.0"}, versions,
		"moving an alias must not change the set of listed versions")
}

func TestRepository_AddComponentVersionAlias_RejectsSemverAlias(t *testing.T) {
	r := require.New(t)
	ctx := t.Context()

	fs, err := filesystem.NewFS(t.TempDir(), os.O_RDWR)
	r.NoError(err)
	store := ocictf.NewFromCTF(ctf.NewFileSystemCTF(fs))
	repo := Repository(t, ocictf.WithCTF(store))

	componentName := "ocm.software/test-component"

	makeDesc := func(version string) *descriptor.Descriptor {
		return &descriptor.Descriptor{
			Meta: descriptor.Meta{Version: "v2"},
			Component: descriptor.Component{
				Provider:      descriptor.Provider{Name: "test-provider"},
				ComponentMeta: descriptor.ComponentMeta{ObjectMeta: descriptor.ObjectMeta{Name: componentName, Version: version}},
			},
		}
	}

	r.NoError(repo.AddComponentVersion(ctx, makeDesc("1.0.0")))
	r.NoError(repo.AddComponentVersion(ctx, makeDesc("2.0.0")))

	// Attempting to alias 1.0.0 as "2.0.0" should fail because "2.0.0" is semver-formatted
	err = repo.AddComponentVersionAlias(ctx, componentName, "1.0.0", "2.0.0")
	r.Error(err)
	r.Contains(err.Error(), "uses semantic version format")

	// Verify both versions are still independently accessible
	got, err := repo.GetComponentVersion(ctx, componentName, "1.0.0")
	r.NoError(err)
	r.Equal("1.0.0", got.Component.Version)

	got, err = repo.GetComponentVersion(ctx, componentName, "2.0.0")
	r.NoError(err)
	r.Equal("2.0.0", got.Component.Version)

	// Both versions should be listed
	versions, err := repo.ListComponentVersions(ctx, componentName)
	r.NoError(err)
	r.ElementsMatch([]string{"1.0.0", "2.0.0"}, versions)
}

func TestRepository_RemoveComponentVersionAlias(t *testing.T) {
	const componentName = "ocm.software/test-component"

	makeDesc := func(version string) *descriptor.Descriptor {
		return &descriptor.Descriptor{
			Meta: descriptor.Meta{Version: "v2"},
			Component: descriptor.Component{
				Provider:      descriptor.Provider{Name: "test-provider"},
				ComponentMeta: descriptor.ComponentMeta{ObjectMeta: descriptor.ObjectMeta{Name: componentName, Version: version}},
			},
		}
	}

	newRepo := func(t *testing.T) *oci.Repository {
		t.Helper()
		fs, err := filesystem.NewFS(t.TempDir(), os.O_RDWR)
		require.NoError(t, err)
		return Repository(t, ocictf.WithCTF(ocictf.NewFromCTF(ctf.NewFileSystemCTF(fs))))
	}

	tests := []struct {
		name   string
		setup  func(t *testing.T, repo *oci.Repository)
		alias  string
		assert func(t *testing.T, repo *oci.Repository, removeErr error)
	}{
		{
			name: "removes alias leaving sibling alias and semver intact",
			setup: func(t *testing.T, repo *oci.Repository) {
				r := require.New(t)
				r.NoError(repo.AddComponentVersion(t.Context(), makeDesc("1.0.0")))
				r.NoError(repo.AddComponentVersionAlias(t.Context(), componentName, "1.0.0", "latest"))
				r.NoError(repo.AddComponentVersionAlias(t.Context(), componentName, "1.0.0", "stable"))
			},
			alias: "latest",
			assert: func(t *testing.T, repo *oci.Repository, removeErr error) {
				r := require.New(t)
				r.NoError(removeErr)

				_, err := repo.GetComponentVersion(t.Context(), componentName, "latest")
				r.ErrorIs(err, repository.ErrNotFound, "removed alias must not resolve")

				got, err := repo.GetComponentVersion(t.Context(), componentName, "stable")
				r.NoError(err)
				r.Equal("1.0.0", got.Component.Version, "sibling alias must still resolve")

				got, err = repo.GetComponentVersion(t.Context(), componentName, "1.0.0")
				r.NoError(err, "underlying semver version must still be accessible")
				r.Equal("1.0.0", got.Component.Version)
			},
		},
		{
			name:  "returns ErrNotFound when the alias does not exist",
			setup: func(*testing.T, *oci.Repository) {},
			alias: "nonexistent",
			assert: func(t *testing.T, _ *oci.Repository, removeErr error) {
				require.ErrorIs(t, removeErr, repository.ErrNotFound)
			},
		},
		{
			name: "rejects a semver version string and leaves it accessible",
			setup: func(t *testing.T, repo *oci.Repository) {
				require.NoError(t, repo.AddComponentVersion(t.Context(), makeDesc("1.0.0")))
			},
			alias: "1.0.0",
			assert: func(t *testing.T, repo *oci.Repository, removeErr error) {
				r := require.New(t)
				r.Error(removeErr)
				r.Contains(removeErr.Error(), "not an alias")

				got, err := repo.GetComponentVersion(t.Context(), componentName, "1.0.0")
				r.NoError(err, "version must be unaffected after rejected removal")
				r.Equal("1.0.0", got.Component.Version)
			},
		},
		{
			name: "removed alias does not appear in ListComponentVersions",
			setup: func(t *testing.T, repo *oci.Repository) {
				r := require.New(t)
				r.NoError(repo.AddComponentVersion(t.Context(), makeDesc("1.0.0")))
				r.NoError(repo.AddComponentVersion(t.Context(), makeDesc("2.0.0")))
				r.NoError(repo.AddComponentVersionAlias(t.Context(), componentName, "1.0.0", "latest"))
			},
			alias: "latest",
			assert: func(t *testing.T, repo *oci.Repository, removeErr error) {
				r := require.New(t)
				r.NoError(removeErr)

				versions, err := repo.ListComponentVersions(t.Context(), componentName)
				r.NoError(err)
				r.ElementsMatch([]string{"1.0.0", "2.0.0"}, versions, "removed alias must not appear in version list")
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			repo := newRepo(t)
			tc.setup(t, repo)
			err := repo.RemoveComponentVersionAlias(t.Context(), componentName, tc.alias)
			tc.assert(t, repo, err)
		})
	}
}

func TestRepository_AddComponentVersionAlias_GetLocalResource(t *testing.T) {
	r := require.New(t)
	ctx := t.Context()

	fs, err := filesystem.NewFS(t.TempDir(), os.O_RDWR)
	r.NoError(err)
	store := ocictf.NewFromCTF(ctf.NewFileSystemCTF(fs))
	repo := Repository(t, ocictf.WithCTF(store))

	componentName := "ocm.software/test-component"
	resourceContent := []byte("hello from the resource layer")
	contentDigest := digest.FromBytes(resourceContent)

	desc := &descriptor.Descriptor{
		Meta: descriptor.Meta{Version: "v2"},
		Component: descriptor.Component{
			Provider:      descriptor.Provider{Name: "test-provider"},
			ComponentMeta: descriptor.ComponentMeta{ObjectMeta: descriptor.ObjectMeta{Name: componentName, Version: "1.0.0"}},
		},
	}

	resource := &descriptor.Resource{
		Relation: descriptor.LocalRelation,
		ElementMeta: descriptor.ElementMeta{
			ObjectMeta: descriptor.ObjectMeta{Name: "my-resource", Version: "1.0.0"},
		},
		Type: "plainData",
		Access: &v2.LocalBlob{
			LocalReference: contentDigest.String(),
			MediaType:      ociImageSpecV1.MediaTypeImageLayer,
		},
	}
	desc.Component.Resources = append(desc.Component.Resources, *resource)

	newRes, err := repo.AddLocalResource(ctx, componentName, "1.0.0", resource, inmemory.New(bytes.NewReader(resourceContent)))
	r.NoError(err)
	desc.Component.Resources[0] = *newRes

	r.NoError(repo.AddComponentVersion(ctx, desc))

	// Alias 1.0.0 as "latest"
	r.NoError(repo.AddComponentVersionAlias(ctx, componentName, "1.0.0", "latest"))

	identity := map[string]string{"name": "my-resource", "version": "1.0.0"}

	// GetLocalResource through the alias must return the correct content
	blob, _, err := repo.GetLocalResource(ctx, componentName, "latest", identity)
	r.NoError(err, "GetLocalResource through alias must succeed")
	r.NotNil(blob)

	reader, err := blob.ReadCloser()
	r.NoError(err)
	defer reader.Close()
	downloaded, err := io.ReadAll(reader)
	r.NoError(err)
	r.Equal(resourceContent, downloaded, "content retrieved through alias must match original")

	// GetLocalResource through the original version must still work
	blob, _, err = repo.GetLocalResource(ctx, componentName, "1.0.0", identity)
	r.NoError(err, "GetLocalResource through original version must still succeed")
	r.NotNil(blob)

	reader2, err := blob.ReadCloser()
	r.NoError(err)
	defer reader2.Close()
	downloaded2, err := io.ReadAll(reader2)
	r.NoError(err)
	r.Equal(resourceContent, downloaded2, "content retrieved through original version must match")
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

// TestRepository_AddLocalSource_OCILayoutBody pins the regression that
// AddLocalSource must not create an ownership referrer for any OCI-packed
// source body. uploadAndUpdateLocalArtifact is shared with AddLocalResource,
// but it only builds a referrer for a *descriptor.Resource with local
// relation; a source is never a resource, so OwnershipReferrer is never
// invoked for sources.
func TestRepository_AddLocalSource_OCILayoutBody(t *testing.T) {
	r := require.New(t)
	ctx := t.Context()

	fs, err := filesystem.NewFS(t.TempDir(), os.O_RDWR)
	r.NoError(err)
	store := ocictf.NewFromCTF(ctf.NewFileSystemCTF(fs))
	repo := Repository(t,
		ocictf.WithCTF(store),
	)

	// Build an OCI-layout source body (single layer + manifest, tagged). The
	// pack pipeline routes OCI-compliant bodies down the manifest branch,
	// which is exactly where OwnershipReferrer would have been invoked.
	buf := bytes.NewBuffer(nil)
	layoutWriter, err := tar.NewOCILayoutWriterWithTempFile(buf, t.TempDir())
	r.NoError(err)
	layerBytes := []byte("source layer content")
	layerDesc := content.NewDescriptorFromBytes("application/octet-stream", layerBytes)
	r.NoError(layoutWriter.Push(ctx, layerDesc, bytes.NewReader(layerBytes)))
	manifestDesc, err := oras.PackManifest(ctx, layoutWriter, oras.PackManifestVersion1_1, "application/custom", oras.PackManifestOptions{
		Layers: []ociImageSpecV1.Descriptor{layerDesc},
	})
	r.NoError(err)
	r.NoError(layoutWriter.Tag(ctx, manifestDesc, "test-image:latest"))
	r.NoError(layoutWriter.Close())
	layoutBytes := buf.Bytes()

	source := &descriptor.Source{
		ElementMeta: descriptor.ElementMeta{
			ObjectMeta: descriptor.ObjectMeta{Name: "oci-packed-source", Version: "1.0.0"},
		},
		Type: "ociImage",
		Access: &v2.LocalBlob{
			LocalReference: digest.FromBytes(layoutBytes).String(),
			MediaType:      layout.MediaTypeOCIImageLayoutV1 + "+tar+gzip",
		},
	}

	_, err = repo.AddLocalSource(ctx, "ocm.software/test-component", "1.0.0", source, inmemory.New(bytes.NewReader(layoutBytes)))
	r.NoError(err, "AddLocalSource must not invoke OwnershipReferrer for sources")
}

func buildTestManifestStream(t *testing.T) (*memory.Store, ociImageSpecV1.Descriptor) {
	t.Helper()
	ctx := t.Context()
	r := require.New(t)

	store := memory.New()
	layerBytes := []byte("stream layer content")
	layerDesc := content.NewDescriptorFromBytes(ociImageSpecV1.MediaTypeImageLayer, layerBytes)
	r.NoError(store.Push(ctx, layerDesc, bytes.NewReader(layerBytes)))
	manifestDesc, err := oras.PackManifest(ctx, store, oras.PackManifestVersion1_1, "application/custom", oras.PackManifestOptions{
		Layers: []ociImageSpecV1.Descriptor{layerDesc},
	})
	r.NoError(err)
	return store, manifestDesc
}

func TestRepository_UploadResourceStream(t *testing.T) {
	tests := []struct {
		name        string
		imageRef    string                                              // empty = use digest-only form built from manifest
		refFunc     func(manifestDesc ociImageSpecV1.Descriptor) string // overrides imageRef when set
		wantErr     bool
		wantErrMsg  string
		checkResult func(t *testing.T, res *descriptor.Resource, manifestDesc ociImageSpecV1.Descriptor)
	}{
		{
			name:     "tag-only reference uploads and preserves tag",
			imageRef: "test-repo:v1.0.0",
			checkResult: func(t *testing.T, res *descriptor.Resource, manifestDesc ociImageSpecV1.Descriptor) {
				r := require.New(t)
				access := res.Access.(*v1.OCIImage)
				r.Equal("test-repo:v1.0.0", access.ImageReference, "tag preserved, no digest added for tag-only form")
				r.NotNil(res.Digest)
				r.NotEmpty(res.Digest.Value)
			},
		},
		{
			name: "digest-only reference uploads without tagging and preserves digest",
			checkResult: func(t *testing.T, res *descriptor.Resource, manifestDesc ociImageSpecV1.Descriptor) {
				r := require.New(t)
				access := res.Access.(*v1.OCIImage)
				r.Contains(access.ImageReference, manifestDesc.Digest.String(), "digest preserved")
				r.NotNil(res.Digest)
			},
		},
		{
			name: "tag+digest reference (OCM form) uploads and preserves both",
			refFunc: func(manifestDesc ociImageSpecV1.Descriptor) string {
				return "test-repo:v1.0.0@" + manifestDesc.Digest.String()
			},
			checkResult: func(t *testing.T, res *descriptor.Resource, manifestDesc ociImageSpecV1.Descriptor) {
				r := require.New(t)
				access := res.Access.(*v1.OCIImage)
				r.Contains(access.ImageReference, "v1.0.0", "tag preserved")
				r.Contains(access.ImageReference, manifestDesc.Digest.String(), "digest preserved")
				r.NotNil(res.Digest)
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := require.New(t)
			ctx := t.Context()

			fs, err := filesystem.NewFS(t.TempDir(), os.O_RDWR)
			r.NoError(err)
			ctfStore := ocictf.NewFromCTF(ctf.NewFileSystemCTF(fs))
			repo := Repository(t, ocictf.WithCTF(ctfStore), oci.WithScheme(testScheme))

			memStore, manifestDesc := buildTestManifestStream(t)

			imageRef := tc.imageRef
			switch {
			case tc.refFunc != nil:
				imageRef = tc.refFunc(manifestDesc)
			case imageRef == "":
				// Form A: digest-only
				imageRef = "test-repo@" + manifestDesc.Digest.String()
			}

			resource := &descriptor.Resource{
				ElementMeta: descriptor.ElementMeta{
					ObjectMeta: descriptor.ObjectMeta{Name: "stream-res", Version: "1.0.0"},
				},
				Type:   "ociImage",
				Access: &v1.OCIImage{ImageReference: imageRef},
			}

			stream := &ocistream.OCIResourceStream{
				ReadOnlyGraphStorage: memStore,
				Descriptor:           manifestDesc,
				ExtendedCopyOpts:     oras.DefaultExtendedCopyGraphOptions,
			}

			res, err := repo.UploadResourceStream(ctx, resource, stream)
			if tc.wantErr {
				r.Error(err)
				if tc.wantErrMsg != "" {
					r.ErrorContains(err, tc.wantErrMsg)
				}
				return
			}
			r.NoError(err)
			r.NotNil(res)
			if tc.checkResult != nil {
				tc.checkResult(t, res, manifestDesc)
			}
		})
	}
}

// ownershipArtifactAnnotation is a representative software.ocm.artifact value in
// the shape pack.OwnershipReferrer marshals for a resource subject.
const ownershipArtifactAnnotation = `[{"identity":{"name":"backend","version":"1.0.0"},"kind":"resource"}]`

// buildLayoutWithOwnershipReferrer serializes an OCI layout tar containing a
// one-layer image (tagged) and an ADR 0016 ownership referrer whose subject is
// that image — i.e. what GetLocalResource produces on the source side after
// pulling the referrer into the layout.
func buildLayoutWithOwnershipReferrer(t *testing.T, tag, component, version string) (layoutBytes []byte, main, referrer ociImageSpecV1.Descriptor) {
	t.Helper()
	r := require.New(t)
	ctx := context.Background()

	var buf bytes.Buffer
	w, err := tar.NewOCILayoutWriterWithTempFile(&buf, t.TempDir())
	r.NoError(err)

	layerData := []byte("layer-" + component)
	layer := content.NewDescriptorFromBytes(ociImageSpecV1.MediaTypeImageLayer, layerData)
	r.NoError(w.Push(ctx, layer, bytes.NewReader(layerData)))

	main, err = oras.PackManifest(ctx, w, oras.PackManifestVersion1_1, "application/vnd.test.artifact", oras.PackManifestOptions{
		Layers: []ociImageSpecV1.Descriptor{layer},
	})
	r.NoError(err)

	// PackManifest (v1.1) already pushed the empty config; tolerate the duplicate.
	empty := ociImageSpecV1.DescriptorEmptyJSON
	if err := w.Push(ctx, empty, bytes.NewReader(empty.Data)); err != nil && !errors.Is(err, errdef.ErrAlreadyExists) {
		r.NoError(err)
	}

	refBody, err := json.Marshal(ociImageSpecV1.Manifest{
		Versioned:    specs.Versioned{SchemaVersion: 2},
		MediaType:    ociImageSpecV1.MediaTypeImageManifest,
		ArtifactType: annotations.OwnershipArtifactType,
		Config:       empty,
		Layers:       []ociImageSpecV1.Descriptor{empty},
		Subject:      &main,
		// Mirror the three annotations production pack.OwnershipReferrer sets,
		// including the required software.ocm.artifact, so the layout carries a
		// spec-faithful ADR 0016 referrer rather than an incomplete one.
		Annotations: map[string]string{
			annotations.OwnershipComponentName:    component,
			annotations.OwnershipComponentVersion: version,
			annotations.ArtifactAnnotationKey:     ownershipArtifactAnnotation,
		},
	})
	r.NoError(err)
	referrer = ociImageSpecV1.Descriptor{
		MediaType:    ociImageSpecV1.MediaTypeImageManifest,
		ArtifactType: annotations.OwnershipArtifactType,
		Digest:       digest.FromBytes(refBody),
		Size:         int64(len(refBody)),
	}
	r.NoError(w.Push(ctx, referrer, bytes.NewReader(refBody)))

	r.NoError(w.Tag(ctx, main, tag))
	r.NoError(w.Close())
	return buf.Bytes(), main, referrer
}

// TestRepository_AddLocalResource_CopiesOwnershipReferrer proves the add-side
// copy path (ADR 0016): a by-value resource whose incoming layout already
// carries an ownership referrer transfers that referrer to the target even when
// referrer *creation* does not apply. The resource has external relation, so no
// referrer is created — any referrer in the target therefore proves the copy
// path ran. This is the upload half that pairs with GetLocalResource's referrer
// fetch, giving the local-resource path the same transfer behavior as the
// OCI-image path.
func TestRepository_AddLocalResource_CopiesOwnershipReferrer(t *testing.T) {
	r := require.New(t)
	ctx := context.Background()
	const (
		tag       = "latest"
		component = "ocm.software/test-component"
		version   = "1.0.0"
	)

	layoutBytes, main, referrer := buildLayoutWithOwnershipReferrer(t, tag, component, version)

	fs, err := filesystem.NewFS(t.TempDir(), os.O_RDWR)
	r.NoError(err)
	store := ocictf.NewFromCTF(ctf.NewFileSystemCTF(fs))
	repo := Repository(t, ocictf.WithCTF(store))

	// External relation => no referrer is created, so a referrer landing in the
	// target can only have come from the copy path, never from creation.
	resource := &descriptor.Resource{
		Relation:    descriptor.ExternalRelation,
		ElementMeta: descriptor.ElementMeta{ObjectMeta: descriptor.ObjectMeta{Name: "backend", Version: version}},
		Type:        "ociArtifact",
		Access: &v2.LocalBlob{
			LocalReference: digest.FromBytes(layoutBytes).String(),
			MediaType:      layout.MediaTypeOCIImageLayoutTarV1,
		},
	}

	_, err = repo.AddLocalResource(ctx, component, version, resource, inmemory.New(bytes.NewReader(layoutBytes)))
	r.NoError(err)

	componentStore, err := store.StoreForReference(ctx, store.ComponentVersionReference(ctx, component, version))
	r.NoError(err)

	mainExists, err := componentStore.Exists(ctx, main)
	r.NoError(err)
	r.True(mainExists, "the main artifact must be stored")

	referrerExists, err := componentStore.Exists(ctx, referrer)
	r.NoError(err)
	r.True(referrerExists, "ownership referrer must be copied to the target even when no referrer is created")

	// Existence isn't enough: the copied referrer must carry its ADR 0016 ownership
	// annotations verbatim, so fetch the manifest and check all three.
	rc, err := componentStore.Fetch(ctx, referrer)
	r.NoError(err)
	defer func() { r.NoError(rc.Close()) }()
	var copied ociImageSpecV1.Manifest
	r.NoError(json.NewDecoder(rc).Decode(&copied))
	r.Equal(component, copied.Annotations[annotations.OwnershipComponentName], "copied referrer must retain its component name")
	r.Equal(version, copied.Annotations[annotations.OwnershipComponentVersion], "copied referrer must retain its component version")
	r.Equal(ownershipArtifactAnnotation, copied.Annotations[annotations.ArtifactAnnotationKey], "copied referrer must retain its software.ocm.artifact annotation")
}

// TestRepository_UploadResource_CopiesOwnershipReferrer is the by-reference twin
// of TestRepository_AddLocalResource_CopiesOwnershipReferrer: it proves the
// UploadResource path (-> uploadOCIImage) carries an ADR-0016 ownership referrer
// that travels inside the resource's layout through to the target. The referrer
// is injected as a successor of the main artifact, so the single CopyGraph that
// uploads the image lands the referrer in the same traversal — there is no
// separate copy step. External relation means no referrer is created, so one
// landing in the target can only have come from the copy path.
func TestRepository_UploadResource_CopiesOwnershipReferrer(t *testing.T) {
	r := require.New(t)
	ctx := context.Background()
	const (
		imageRef  = "test-image:latest"
		tag       = "latest"
		component = "ocm.software/test-component"
		version   = "1.0.0"
	)

	layoutBytes, main, referrer := buildLayoutWithOwnershipReferrer(t, tag, component, version)

	fs, err := filesystem.NewFS(t.TempDir(), os.O_RDWR)
	r.NoError(err)
	store := ocictf.NewFromCTF(ctf.NewFileSystemCTF(fs))
	repo := Repository(t, ocictf.WithCTF(store))

	resource := &descriptor.Resource{
		Relation:    descriptor.ExternalRelation,
		ElementMeta: descriptor.ElementMeta{ObjectMeta: descriptor.ObjectMeta{Name: "backend", Version: version}},
		Type:        "ociImage",
		Access:      &v1.OCIImage{ImageReference: imageRef},
	}

	_, err = repo.UploadResource(ctx, resource, inmemory.New(bytes.NewReader(layoutBytes)))
	r.NoError(err)

	imgStore, err := store.StoreForReference(ctx, imageRef)
	r.NoError(err)

	mainExists, err := imgStore.Exists(ctx, main)
	r.NoError(err)
	r.True(mainExists, "the main artifact must be uploaded")

	referrerExists, err := imgStore.Exists(ctx, referrer)
	r.NoError(err)
	r.True(referrerExists, "ownership referrer must ride along in the same CopyGraph as the main artifact")

	// Existence is not enough: the referrer must arrive with its ADR-0016
	// ownership annotations intact.
	rc, err := imgStore.Fetch(ctx, referrer)
	r.NoError(err)
	defer func() { r.NoError(rc.Close()) }()
	var copied ociImageSpecV1.Manifest
	r.NoError(json.NewDecoder(rc).Decode(&copied))
	r.Equal(component, copied.Annotations[annotations.OwnershipComponentName], "copied referrer must retain its component name")
	r.Equal(version, copied.Annotations[annotations.OwnershipComponentVersion], "copied referrer must retain its component version")
	r.Equal(ownershipArtifactAnnotation, copied.Annotations[annotations.ArtifactAnnotationKey], "copied referrer must retain its software.ocm.artifact annotation")
}

// TestRepository_AddOwnershipByReference proves the by-reference attach path (ADR
// 0016): a resource kept by reference as an OCI image gets an ownership referrer
// pushed into the registry that hosts the image, without modifying the image
// itself. This is the half that backs OwnershipAwareRepository.AddOwnership for
// by-reference resources.
func TestRepository_AddOwnershipByReference(t *testing.T) {
	r := require.New(t)
	ctx := context.Background()
	const (
		imageRef  = "ghcr.io/acme/backend:latest"
		tag       = "latest"
		component = "ocm.software/test-component"
		version   = "1.0.0"
	)

	fs, err := filesystem.NewFS(t.TempDir(), os.O_RDWR)
	r.NoError(err)
	store := ocictf.NewFromCTF(ctf.NewFileSystemCTF(fs))
	repo := Repository(t, ocictf.WithCTF(store))

	// Stage a one-layer image in the hosting store and tag it, mimicking an
	// image that already lives in the registry and is referenced by the resource.
	imgStore, err := store.StoreForReference(ctx, imageRef)
	r.NoError(err)

	layerData := []byte("layer")
	layer := content.NewDescriptorFromBytes(ociImageSpecV1.MediaTypeImageLayer, layerData)
	r.NoError(imgStore.Push(ctx, layer, bytes.NewReader(layerData)))

	main, err := oras.PackManifest(ctx, imgStore, oras.PackManifestVersion1_1, "application/vnd.test.artifact", oras.PackManifestOptions{
		Layers: []ociImageSpecV1.Descriptor{layer},
	})
	r.NoError(err)
	r.NoError(imgStore.Tag(ctx, main, tag))

	resource := &descriptor.Resource{
		Relation:    descriptor.LocalRelation,
		ElementMeta: descriptor.ElementMeta{ObjectMeta: descriptor.ObjectMeta{Name: "backend-image", Version: version}},
		Type:        "ociArtifact",
		Access:      &v1.OCIImage{Type: runtime.NewVersionedType(v1.OCIImageType, v1.Version), ImageReference: imageRef},
	}

	r.NoError(repo.AddOwnership(ctx, component, version, resource, nil))

	// The expected referrer is content-addressed off the resolved subject; build
	// it the same way the repository does and assert it now exists in the store.
	resolved, err := imgStore.Resolve(ctx, tag)
	r.NoError(err)
	referrerDesc, referrerBody, err := pack.OwnershipReferrer(ctx, resolved, resource, component, version)
	r.NoError(err)
	r.NotNil(referrerBody, "ownership referrer must be produced for an OCI manifest subject")

	exists, err := imgStore.Exists(ctx, referrerDesc)
	r.NoError(err)
	r.True(exists, "ownership referrer manifest must be pushed into the hosting store")

	// The referenced image manifest must be untouched (still resolvable, same digest).
	stillThere, err := imgStore.Exists(ctx, main)
	r.NoError(err)
	r.True(stillThere, "the referenced image must remain present and unchanged")

	// Re-running attaches the same content-addressed referrer (idempotent). NoError
	// alone wouldn't prove that, so also assert the referrer is still present and the
	// subject is untouched after the second run. Enumeration ("exactly one referrer")
	// needs the live Referrers API, which the CTF store has no index for; that
	// guarantee is covered by Test_Integration_Ownership.
	r.NoError(repo.AddOwnership(ctx, component, version, resource, nil))

	stillExists, err := imgStore.Exists(ctx, referrerDesc)
	r.NoError(err)
	r.True(stillExists, "the same content-addressed referrer must remain after an idempotent re-run")

	resolvedAfter, err := imgStore.Resolve(ctx, tag)
	r.NoError(err)
	r.Equal(resolved, resolvedAfter, "the subject must be unchanged by re-running the attach")
}

// TestRepository_AddOwnership_ResolveErrors covers the subject-resolution failure
// branches of AddOwnership (ADR 0016) on both access paths. These are the branches
// most likely to regress silently: a by-reference image whose reference resolves to
// nothing, and a by-value local blob whose digest is not present in the component
// store. Both must surface as an error rather than a quietly-skipped referrer.
func TestRepository_AddOwnership_ResolveErrors(t *testing.T) {
	const (
		component = "ocm.software/test-component"
		version   = "1.0.0"
		// a well-formed but absent digest, so resolution fails rather than parsing.
		missingDigest = "sha256:0000000000000000000000000000000000000000000000000000000000000000"
	)
	tests := []struct {
		name    string
		access  runtime.Typed
		wantErr string
	}{
		{
			name:    "by-reference subject does not resolve",
			access:  &v1.OCIImage{Type: runtime.NewVersionedType(v1.OCIImageType, v1.Version), ImageReference: "ghcr.io/acme/missing:latest"},
			wantErr: "failed to resolve subject",
		},
		{
			name:    "by-value local blob does not resolve",
			access:  &v2.LocalBlob{Type: runtime.NewVersionedType(descriptor.LocalBlobAccessType, descriptor.LocalBlobAccessTypeVersion), LocalReference: missingDigest, MediaType: "application/octet-stream"},
			wantErr: "failed to resolve uploaded artifact",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := require.New(t)
			ctx := context.Background()
			fs, err := filesystem.NewFS(t.TempDir(), os.O_RDWR)
			r.NoError(err)
			store := ocictf.NewFromCTF(ctf.NewFileSystemCTF(fs))
			repo := Repository(t, ocictf.WithCTF(store))

			resource := &descriptor.Resource{
				Relation:    descriptor.LocalRelation,
				ElementMeta: descriptor.ElementMeta{ObjectMeta: descriptor.ObjectMeta{Name: "backend-image", Version: version}},
				Type:        "ociArtifact",
				Access:      tt.access,
			}

			err = repo.AddOwnership(ctx, component, version, resource, nil)
			r.Error(err)
			r.ErrorContains(err, tt.wantErr)
		})
	}
}

// blobValidatingResolver wraps a resolver so the stores it hands out reject a
// manifest whose referenced config/layer blobs are not yet present — the
// MANIFEST_BLOB_UNKNOWN behaviour of a conformant OCI registry, which the CTF
// store does not emulate.
type blobValidatingResolver struct{ oci.Resolver }

func (r blobValidatingResolver) StoreForReference(ctx context.Context, reference string) (spec.Store, error) {
	s, err := r.Resolver.StoreForReference(ctx, reference)
	if err != nil {
		return nil, err
	}
	return &blobValidatingStore{Store: s}, nil
}

// blobValidatingStore rejects a manifest push when a referenced blob is missing.
type blobValidatingStore struct{ spec.Store }

func (s *blobValidatingStore) Push(ctx context.Context, expected ociImageSpecV1.Descriptor, r io.Reader) error {
	if expected.MediaType != ociImageSpecV1.MediaTypeImageManifest {
		return s.Store.Push(ctx, expected, r)
	}
	raw, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	var m ociImageSpecV1.Manifest
	if err := json.Unmarshal(raw, &m); err != nil {
		return err
	}
	for _, ref := range append([]ociImageSpecV1.Descriptor{m.Config}, m.Layers...) {
		exists, err := s.Store.Exists(ctx, ref)
		if err != nil {
			return err
		}
		if !exists {
			return fmt.Errorf("manifest %s references missing blob %s: MANIFEST_BLOB_UNKNOWN", expected.Digest, ref.Digest)
		}
	}
	return s.Store.Push(ctx, expected, bytes.NewReader(raw))
}

// TestRepository_AddOwnershipByReference_PushesBlobBeforeManifest guards the push
// order (ADR 0016): the referrer manifest references the empty config/layer
// blob, so that blob must reach the registry before the manifest or a conformant
// registry rejects it with MANIFEST_BLOB_UNKNOWN. The subject image is staged
// with a real (non-empty) config so the empty blob is genuinely absent up front —
// otherwise the subject's own empty config would pre-seed it and mask the bug.
func TestRepository_AddOwnershipByReference_PushesBlobBeforeManifest(t *testing.T) {
	r := require.New(t)
	ctx := context.Background()
	const (
		imageRef  = "ghcr.io/acme/backend:latest"
		tag       = "latest"
		component = "ocm.software/test-component"
		version   = "1.0.0"
	)

	fs, err := filesystem.NewFS(t.TempDir(), os.O_RDWR)
	r.NoError(err)
	store := ocictf.NewFromCTF(ctf.NewFileSystemCTF(fs))
	repo := Repository(t, oci.WithResolver(blobValidatingResolver{Resolver: store}))

	// Stage the subject image directly on the CTF store (bypassing validation), with
	// an explicit non-empty config so the empty-JSON blob is not already present.
	imgStore, err := store.StoreForReference(ctx, imageRef)
	r.NoError(err)
	layer := []byte("layer")
	layerDesc := content.NewDescriptorFromBytes(ociImageSpecV1.MediaTypeImageLayer, layer)
	r.NoError(imgStore.Push(ctx, layerDesc, bytes.NewReader(layer)))
	config := []byte(`{"architecture":"amd64","os":"linux"}`)
	configDesc := content.NewDescriptorFromBytes(ociImageSpecV1.MediaTypeImageConfig, config)
	r.NoError(imgStore.Push(ctx, configDesc, bytes.NewReader(config)))
	manifestRaw, err := json.Marshal(ociImageSpecV1.Manifest{
		Versioned: specs.Versioned{SchemaVersion: 2},
		MediaType: ociImageSpecV1.MediaTypeImageManifest,
		Config:    configDesc,
		Layers:    []ociImageSpecV1.Descriptor{layerDesc},
	})
	r.NoError(err)
	manifestDesc := content.NewDescriptorFromBytes(ociImageSpecV1.MediaTypeImageManifest, manifestRaw)
	r.NoError(imgStore.Push(ctx, manifestDesc, bytes.NewReader(manifestRaw)))
	r.NoError(imgStore.Tag(ctx, manifestDesc, tag))

	emptyExists, err := imgStore.Exists(ctx, ociImageSpecV1.DescriptorEmptyJSON)
	r.NoError(err)
	r.False(emptyExists, "precondition: the empty config blob must be absent before the attach")

	resource := &descriptor.Resource{
		Relation:    descriptor.LocalRelation,
		ElementMeta: descriptor.ElementMeta{ObjectMeta: descriptor.ObjectMeta{Name: "backend-image", Version: version}},
		Type:        "ociArtifact",
		Access:      &v1.OCIImage{Type: runtime.NewVersionedType(v1.OCIImageType, v1.Version), ImageReference: imageRef},
	}

	// With the old manifest-first push order this fails MANIFEST_BLOB_UNKNOWN.
	r.NoError(repo.AddOwnership(ctx, component, version, resource, nil))

	emptyExists, err = imgStore.Exists(ctx, ociImageSpecV1.DescriptorEmptyJSON)
	r.NoError(err)
	r.True(emptyExists, "the empty config/layer blob must be pushed during the attach")
}

// TestRepository_AddOwnership_CreatesByValueReferrer proves the by-value create
// path (ADR 0016): after a resource is uploaded by value into the
// component's own store, AddOwnership resolves the uploaded manifest and pushes a
// fresh ownership referrer for it. The incoming layout carries no referrer, so a
// referrer landing in the store can only have come from creation. This backs the
// constructor's OwnershipAwareRepository capability for the by-value path; the
// by-reference half is TestRepository_AddOwnershipByReference.
func TestRepository_AddOwnership_CreatesByValueReferrer(t *testing.T) {
	r := require.New(t)
	ctx := context.Background()
	const (
		component = "ocm.software/test-component"
		version   = "1.0.0"
	)

	// A single-manifest OCI layout with no ownership referrer of its own.
	layoutBytes, _ := createSingleLayerOCIImage(t, []byte("by-value payload"), "irrelevant:latest")

	fs, err := filesystem.NewFS(t.TempDir(), os.O_RDWR)
	r.NoError(err)
	store := ocictf.NewFromCTF(ctf.NewFileSystemCTF(fs))
	repo := Repository(t, ocictf.WithCTF(store))

	resource := &descriptor.Resource{
		Relation:    descriptor.LocalRelation,
		ElementMeta: descriptor.ElementMeta{ObjectMeta: descriptor.ObjectMeta{Name: "backend", Version: version}},
		Type:        "ociArtifact",
		Access: &v2.LocalBlob{
			LocalReference: digest.FromBytes(layoutBytes).String(),
			MediaType:      layout.MediaTypeOCIImageLayoutTarV1,
		},
	}

	uploaded, err := repo.AddLocalResource(ctx, component, version, resource, inmemory.New(bytes.NewReader(layoutBytes)))
	r.NoError(err)

	// After upload, the resource's local reference points at the unpacked manifest
	// in the component store; that manifest is the ownership referrer subject.
	componentStore, err := store.StoreForReference(ctx, store.ComponentVersionReference(ctx, component, version))
	r.NoError(err)
	subject, err := componentStore.Resolve(ctx, uploaded.Access.(*v2.LocalBlob).LocalReference)
	r.NoError(err)
	expectedDesc, expectedBody, err := pack.OwnershipReferrer(ctx, subject, uploaded, component, version)
	r.NoError(err)
	r.NotNil(expectedBody, "an OCI manifest subject must yield an ownership referrer")

	existsBefore, err := componentStore.Exists(ctx, expectedDesc)
	r.NoError(err)
	r.False(existsBefore, "no ownership referrer must exist before AddOwnership (the layout carried none)")

	// credentials are unused for the repository's own store.
	r.NoError(repo.AddOwnership(ctx, component, version, uploaded, nil))

	existsAfter, err := componentStore.Exists(ctx, expectedDesc)
	r.NoError(err)
	r.True(existsAfter, "AddOwnership must create and push an ownership referrer for the uploaded manifest")

	// Idempotent: the content-addressed referrer is unchanged by a second run.
	r.NoError(repo.AddOwnership(ctx, component, version, uploaded, nil))
	stillExists, err := componentStore.Exists(ctx, expectedDesc)
	r.NoError(err)
	r.True(stillExists, "re-running AddOwnership must converge on the same referrer")
}

// The OCI-level opt-in gate was intentionally removed: AddOwnership now
// builds a referrer unconditionally for the resource it is handed, and the
// opt-in decision (options.ownershipPolicy: Always) lives in the constructor.
// That gate is covered by the constructor tests (TestDefaultConstructor_attachOwnership_CallSiteGating
// and the relocated-policy-gate case in construct_resource_test.go).

// TestRepository_AddOwnership_RawBlobSubjectSkipped locks the no-op contract (ADR
// 0016): when the resolved subject is a raw blob rather than an OCI manifest,
// AddOwnership records no referrer and returns nil instead of failing. A by-value
// resource whose local reference points at a plain blob exercises that branch.
func TestRepository_AddOwnership_RawBlobSubjectSkipped(t *testing.T) {
	r := require.New(t)
	ctx := context.Background()
	const (
		component = "ocm.software/test-component"
		version   = "1.0.0"
	)

	fs, err := filesystem.NewFS(t.TempDir(), os.O_RDWR)
	r.NoError(err)
	store := ocictf.NewFromCTF(ctf.NewFileSystemCTF(fs))
	repo := Repository(t, ocictf.WithCTF(store))

	// Stage a raw, non-manifest blob in the component store and reference it by digest.
	componentStore, err := store.StoreForReference(ctx, store.ComponentVersionReference(ctx, component, version))
	r.NoError(err)
	raw := []byte("not a manifest")
	rawDesc := content.NewDescriptorFromBytes("application/octet-stream", raw)
	r.NoError(componentStore.Push(ctx, rawDesc, bytes.NewReader(raw)))

	resource := &descriptor.Resource{
		Relation:    descriptor.LocalRelation,
		ElementMeta: descriptor.ElementMeta{ObjectMeta: descriptor.ObjectMeta{Name: "backend", Version: version}},
		Type:        "blob",
		Access: &v2.LocalBlob{
			Type:           runtime.NewVersionedType(descriptor.LocalBlobAccessType, descriptor.LocalBlobAccessTypeVersion),
			LocalReference: rawDesc.Digest.String(),
			MediaType:      "application/octet-stream",
		},
	}

	r.NoError(repo.AddOwnership(ctx, component, version, resource, nil))

	// The contract: a raw-blob subject yields no referrer, so nothing was pushed.
	_, body, err := pack.OwnershipReferrer(ctx, rawDesc, resource, component, version)
	r.NoError(err)
	r.Nil(body, "a raw-blob subject must yield no ownership referrer")
}
