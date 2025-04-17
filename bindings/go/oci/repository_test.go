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
	"ocm.software/open-component-model/bindings/go/ctf"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/oci"
	ocictf "ocm.software/open-component-model/bindings/go/oci/ctf"
	"ocm.software/open-component-model/bindings/go/oci/internal/identity"
	"ocm.software/open-component-model/bindings/go/oci/spec"
	access "ocm.software/open-component-model/bindings/go/oci/spec/access"
	v1 "ocm.software/open-component-model/bindings/go/oci/spec/access/v1"
	"ocm.software/open-component-model/bindings/go/oci/spec/layout"
	"ocm.software/open-component-model/bindings/go/oci/tar"
	"ocm.software/open-component-model/bindings/go/runtime"
)

var testScheme = runtime.NewScheme()

func init() {
	access.MustAddToScheme(testScheme)
	v2.MustAddToScheme(testScheme)
}

func Repository(t *testing.T, options ...oci.RepositoryOption) *oci.Repository {
	repo, err := oci.NewRepository(options...)
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
	r.NoError(err, "Expected error when getting non-existent component version")
	r.NotNil(desc, "Expected nil descriptor when getting non-existent component version")

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
				Access: &runtime.Raw{
					Type: runtime.Type{
						Name: "localBlob",
					},
					Data: []byte(`{"localReference":"sha256:1234567890","mediaType":"application/octet-stream"}`),
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
				Access: &runtime.Raw{
					Type: runtime.NewVersionedType(v2.LocalBlobAccessType, v2.LocalBlobAccessTypeVersion),
					Data: []byte(fmt.Sprintf(
						`{"type":"%s","localReference":"%s","mediaType":"application/octet-stream"}`,
						runtime.NewVersionedType(v2.LocalBlobAccessType, v2.LocalBlobAccessTypeVersion),
						digest.FromString("test content").String(),
					)),
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
				Access: &runtime.Raw{
					Type: runtime.NewVersionedType(v2.LocalBlobAccessType, v2.LocalBlobAccessTypeVersion),
					Data: []byte(fmt.Sprintf(
						`{"type":"%s","localReference":"%s","mediaType":"application/octet-stream"}`,
						runtime.NewVersionedType(v2.LocalBlobAccessType, v2.LocalBlobAccessTypeVersion),
						digest.FromString("platform specific content").String(),
					)),
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
				Access: &runtime.Raw{
					Type: runtime.NewVersionedType(v2.LocalBlobAccessType, v2.LocalBlobAccessTypeVersion),
					Data: []byte(fmt.Sprintf(
						`{"type":"%s","localReference":"%s","mediaType":"application/octet-stream"}`,
						runtime.NewVersionedType(v2.LocalBlobAccessType, v2.LocalBlobAccessTypeVersion),
						digest.FromString("legacy content").String(),
					)),
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
				Access: &runtime.Raw{
					Type: runtime.NewVersionedType(v2.LocalBlobAccessType, v2.LocalBlobAccessTypeVersion),
					Data: []byte(fmt.Sprintf(
						`{"type":"%s","localReference":"%s","mediaType":"application/octet-stream"}`,
						runtime.NewVersionedType(v2.LocalBlobAccessType, v2.LocalBlobAccessTypeVersion),
						digest.FromString("platform specific content").String(),
					)),
				},
			},
			identity: map[string]string{
				"name":    "test-resource",
				"version": "1.0.0",
			},
			expectError:    true,
			errorContains:  "found 0 candidates while looking for resource map[name:test-resource version:1.0.0], but expected exactly one",
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
				Access: &runtime.Raw{
					Type: runtime.NewVersionedType(v2.LocalBlobAccessType, v2.LocalBlobAccessTypeVersion),
					Data: []byte(fmt.Sprintf(
						`{"type":"%s","localReference":"%s","mediaType":"%s"}`,
						runtime.NewVersionedType(v2.LocalBlobAccessType, v2.LocalBlobAccessTypeVersion),
						digest.FromString("single layer manifest content").String(),
						ociImageSpecV1.MediaTypeImageManifest,
					)),
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
				Access: &runtime.Raw{
					Type: runtime.NewVersionedType(v2.LocalBlobAccessType, v2.LocalBlobAccessTypeVersion),
					Data: []byte(fmt.Sprintf(
						`{"type":"%s","localReference":"%s","mediaType":"%s"}`,
						runtime.NewVersionedType(v2.LocalBlobAccessType, v2.LocalBlobAccessTypeVersion),
						digest.FromString("oci layout content").String(),
						layout.MediaTypeOCIImageLayoutV1+"+tar+gzip",
					)),
				},
			},
			content: func() []byte {
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
				if err := layout.Push(t.Context(), desc, bytes.NewReader(content)); err != nil {
					panic(fmt.Sprintf("failed to push content: %v", err))
				}

				// Create a manifest
				manifest, err := oras.PackManifest(t.Context(), layout, oras.PackManifestVersion1_1, ociImageSpecV1.MediaTypeImageManifest, oras.PackManifestOptions{
					Layers: []ociImageSpecV1.Descriptor{desc},
				})
				if err != nil {
					panic(fmt.Sprintf("failed to create manifest: %v", err))
				}

				// Tag the manifest
				if err := layout.Tag(t.Context(), manifest, "test-tag"); err != nil {
					panic(fmt.Sprintf("failed to tag manifest: %v", err))
				}

				// Close the layout
				if err := layout.Close(); err != nil {
					panic(fmt.Sprintf("failed to close layout: %v", err))
				}

				return buf.Bytes()
			}(),
			checkContent: func(t *testing.T, original []byte, actual []byte) {
				r := require.New(t)
				store, err := tar.ReadOCILayout(t.Context(), blob.NewDirectReadOnlyBlob(bytes.NewReader(original)))
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
				b := blob.NewDirectReadOnlyBlob(bytes.NewReader(tc.content))
				newRes, err := repo.AddLocalResource(ctx, desc.Component.Name, desc.Component.Version, tc.resource, b)
				r.NoError(err, "Failed to add test resource")
				r.NotNil(newRes, "Resource should not be nil after adding")

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
				Access: &runtime.Raw{
					Type: runtime.NewVersionedType(v2.LocalBlobAccessType, v2.LocalBlobAccessTypeVersion),
					Data: []byte(fmt.Sprintf(
						`{"type":"%s","localReference":"%s","mediaType":"%s"}`,
						runtime.NewVersionedType(v2.LocalBlobAccessType, v2.LocalBlobAccessTypeVersion),
						digest.FromString("test layer content").String(),
						artifactMediaType,
					)),
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

			b := blob.NewDirectReadOnlyBlob(bytes.NewReader(tc.content))

			var downloadedRes blob.ReadOnlyBlob
			if tc.useLocalUpload {
				// Use AddLocalResource for local uploads
				var newRes *descriptor.Resource
				newRes, err = repo.AddLocalResource(ctx, desc.Component.Name, desc.Component.Version, tc.resource, b)
				r.NoError(err, "Failed to add local resource")
				r.NotNil(newRes, "Resource should not be nil after adding")

				// Add the component version
				err = repo.AddComponentVersion(ctx, desc)
				r.NoError(err, "Failed to add component version")

				// Try to get the resource back using GetResource with the global access
				downloadedRes, err = repo.DownloadResource(ctx, newRes)
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
				b := blob.NewDirectReadOnlyBlob(buf)
				r.NoError(repo.UploadResource(ctx, tc.resource.Access, tc.resource, b), "Failed to upload test resource")
				r.NotNil(tc.resource.Access, "Resource should not be nil after uploading")

				// Download the resource
				downloadedRes, err = repo.DownloadResource(ctx, tc.resource)
			}

			if tc.wantErr {
				r.Error(err, "Expected error but got none")
				return
			}
			r.NoError(err, "Failed to download resource")
			r.NotNil(downloadedRes, "Downloaded resource should not be nil")

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
		Access: &runtime.Raw{
			Type: runtime.NewVersionedType(v2.LocalBlobAccessType, v2.LocalBlobAccessTypeVersion),
			Data: []byte(fmt.Sprintf(
				`{"type":"%s","localReference":"%s","mediaType":"%s"}`,
				runtime.NewVersionedType(v2.LocalBlobAccessType, v2.LocalBlobAccessTypeVersion),
				digest.FromBytes(data).String(),
				layout.MediaTypeOCIImageLayoutV1+"+tar",
			)),
		},
	}

	// Add the resource to the component descriptor
	desc.Component.Resources = append(desc.Component.Resources, *resource)

	// Add the OCI layout as a local resource
	b := blob.NewDirectReadOnlyBlob(bytes.NewReader(data))
	newRes, err := repo.AddLocalResource(ctx, desc.Component.Name, desc.Component.Version, resource, b)
	r.NoError(err, "Failed to add OCI layout resource")
	r.NotNil(newRes, "Resource should not be nil after adding")

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
		Access: &runtime.Raw{
			Type: runtime.NewVersionedType(v2.LocalBlobAccessType, v2.LocalBlobAccessTypeVersion),
			Data: []byte(fmt.Sprintf(
				`{"type":"%s","localReference":"%s","mediaType":"%s"}`,
				runtime.NewVersionedType(v2.LocalBlobAccessType, v2.LocalBlobAccessTypeVersion),
				contentDigest.String(),
				ociImageSpecV1.MediaTypeImageLayer,
			)),
		},
	}

	// Add the resource to the component descriptor
	desc.Component.Resources = append(desc.Component.Resources, *resource)

	// Add the OCI image layer as a local resource
	b := blob.NewDirectReadOnlyBlob(bytes.NewReader(content))
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
	repoStore, err := store.StoreForReference(t.Context(), store.ComponentVersionReference("test-component", "1.0.0"))
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
	desc.Component.Resources = append(desc.Component.Resources, *resource)

	// Create a layer descriptor for the component version
	layerDesc := ociImageSpecV1.Descriptor{
		MediaType: ociImageSpecV1.MediaTypeImageLayer,
		Digest:    digest.FromBytes(content),
		Size:      int64(len(content)),
	}
	r.NoError(identity.AdoptAsResource(&layerDesc, resource))

	// Push the component version as a layer
	r.NoError(repoStore.Push(ctx, layerDesc, bytes.NewReader(content)))

	topDesc, err := oci.AddDescriptorToStore(ctx, repoStore, desc, oci.AddDescriptorOptions{
		Author:           "OLD OCM",
		AdditionalLayers: []ociImageSpecV1.Descriptor{layerDesc},
	})
	r.NoError(err)
	r.NoError(repoStore.Tag(ctx, *topDesc, "1.0.0"))
}
