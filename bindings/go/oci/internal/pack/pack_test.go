package pack_test

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"io"
	"testing"

	"github.com/opencontainers/go-digest"
	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"oras.land/oras-go/v2"
	"oras.land/oras-go/v2/content"
	"oras.land/oras-go/v2/content/file"

	"ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/blob/compression"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	resourceblob "ocm.software/open-component-model/bindings/go/oci/blob"
	. "ocm.software/open-component-model/bindings/go/oci/internal/pack"
	"ocm.software/open-component-model/bindings/go/oci/internal/policy"
	oci "ocm.software/open-component-model/bindings/go/oci/spec/access"
	"ocm.software/open-component-model/bindings/go/oci/spec/layout"
	"ocm.software/open-component-model/bindings/go/oci/tar"
	"ocm.software/open-component-model/bindings/go/runtime"
)

type testBlob struct {
	content   []byte
	mediaType string
	digest    digest.Digest
}

func (b *testBlob) ReadCloser() (io.ReadCloser, error) {
	if b.content == nil {
		return nil, errors.New("blob not found")
	}
	return io.NopCloser(bytes.NewReader(b.content)), nil
}

func (b *testBlob) Size() int64 {
	if b.content == nil {
		return blob.SizeUnknown
	}
	return int64(len(b.content))
}

func (b *testBlob) MediaType() (string, bool) {
	return b.mediaType, b.mediaType != ""
}

func (b *testBlob) Digest() (string, bool) {
	return b.digest.String(), b.digest != ""
}

func TestNewResourceBlobOCILayer(t *testing.T) {
	tests := []struct {
		name          string
		blob          *testBlob
		res           *descriptor.Resource
		opts          ResourceBlobOCILayerOptions
		expectedError string
	}{
		{
			name: "success with all fields provided",
			blob: &testBlob{
				content:   []byte("test content"),
				mediaType: "application/vnd.test",
				digest:    digest.FromBytes([]byte("test content")),
			},
			res: &descriptor.Resource{},
			opts: ResourceBlobOCILayerOptions{
				BlobMediaType: "application/vnd.test",
				BlobDigest:    digest.FromBytes([]byte("test content")),
			},
		},
		{
			name: "error on missing content",
			blob: &testBlob{
				mediaType: "application/vnd.test",
				digest:    digest.FromBytes([]byte("test content")),
			},
			res: &descriptor.Resource{},
			opts: ResourceBlobOCILayerOptions{
				BlobMediaType: "application/vnd.test",
				BlobDigest:    digest.FromBytes([]byte("test content")),
			},
			expectedError: "blob not found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.NoError(t, resourceblob.UpdateArtifactWithInformationFromBlob(tt.res, tt.blob))
			resourceBlob, err := resourceblob.NewArtifactBlob(tt.res, tt.blob)
			require.NoError(t, err)

			resourceBlob, desc, err := PrepareArtifactBlobForOCI(resourceBlob, tt.opts)

			if tt.expectedError != "" {
				assert.ErrorContains(t, err, tt.expectedError)
				return
			}

			assert.NoError(t, err)
			assert.Equal(t, tt.blob.mediaType, desc.MediaType)
			assert.Equal(t, tt.blob.digest, desc.Digest)
			assert.Equal(t, int64(len(tt.blob.content)), desc.Size)
		})
	}
}

func TestBufferArtifactBlob(t *testing.T) {
	text := "test content"
	var b blob.ReadOnlyBlob
	b = &testBlob{
		content:   []byte(text),
		mediaType: "text/plain",
	}
	b = compression.Compress(b)

	resourceBlob, err := resourceblob.NewArtifactBlob(&descriptor.Resource{}, b)
	require.NoError(t, err)
	require.NotNil(t, resourceBlob)

	// Compressed blobs neither have a size nor a digest.
	assert.Equal(t, blob.SizeUnknown, resourceBlob.Size())
	dig, ok := resourceBlob.Digest()
	assert.Equal(t, "", dig)
	assert.False(t, ok)

	// wantData contains the expected compressed data to be compared with later in the test.
	var buf bytes.Buffer
	writer := gzip.NewWriter(&buf)
	_, err = io.Copy(writer, bytes.NewReader([]byte(text)))
	require.NoError(t, err)
	require.NoError(t, writer.Close())
	wantData := buf.Bytes()
	wantDig := digest.FromBytes(wantData).String()

	// Switch to buffered blob.
	resourceBlob, desc, err := PrepareArtifactBlobForOCI(resourceBlob, ResourceBlobOCILayerOptions{})
	require.NoError(t, err)
	require.NotNil(t, resourceBlob)

	// The new blob has to have the properties set.
	assert.Equal(t, int64(len(wantData)), resourceBlob.Size())
	dig, ok = resourceBlob.Digest()
	assert.True(t, ok)
	assert.Equal(t, wantDig, dig)

	// Check that the blob also has the right data.
	reader, err := resourceBlob.ReadCloser()
	require.NoError(t, err)
	data, err := io.ReadAll(reader)
	require.NoError(t, err)
	assert.Equal(t, string(wantData), string(data))

	// Check the descriptor.
	assert.Equal(t, "text/plain+gzip", desc.MediaType)
	assert.Equal(t, int64(len(wantData)), desc.Size)
	assert.Equal(t, wantDig, desc.Digest.String())
}

func TestBlob(t *testing.T) {
	store, err := file.New(t.TempDir())
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, store.Close())
	})

	ctx := t.Context()
	content := []byte("test content")
	digest := digest.FromBytes(content)

	tests := []struct {
		name          string
		blob          *testBlob
		desc          ociImageSpecV1.Descriptor
		expectedError string
	}{
		{
			name: "successful push",
			blob: &testBlob{
				content:   content,
				mediaType: "application/vnd.test",
				digest:    digest,
			},
			desc: ociImageSpecV1.Descriptor{
				MediaType: "application/vnd.test",
				Digest:    digest,
				Size:      int64(len(content)),
			},
		},
		{
			name: "error on read closer failure",
			blob: &testBlob{
				content:   nil,
				mediaType: "application/vnd.test",
				digest:    digest,
			},
			desc: ociImageSpecV1.Descriptor{
				MediaType: "application/vnd.test",
				Digest:    digest,
				Size:      int64(len(content)),
			},
			expectedError: "failed to get blob reader",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := Blob(ctx, store, tt.blob, tt.desc)

			if tt.expectedError != "" {
				assert.ErrorContains(t, err, tt.expectedError)
				return
			}

			assert.NoError(t, err)
		})
	}
}

func TestResourceBlob(t *testing.T) {
	ctx := t.Context()
	content := []byte("test content")
	digest := digest.FromBytes(content)

	tests := []struct {
		name                     string
		blob                     *testBlob
		resource                 *descriptor.Resource
		opts                     Options
		expectedError            string
		nilOutResourceBlobDigest bool
		checkGlobalAccess        func(t *testing.T, resource *descriptor.Resource)
	}{
		{
			name: "success with local blob access",
			blob: &testBlob{
				content:   content,
				mediaType: "application/vnd.test",
				digest:    digest,
			},
			resource: &descriptor.Resource{
				Access: &v2.LocalBlob{
					Type:           runtime.NewVersionedType(v2.LocalBlobAccessType, v2.LocalBlobAccessTypeVersion),
					LocalReference: digest.String(),
					MediaType:      "application/vnd.test",
				},
			},
			opts: Options{
				AccessScheme:  runtime.NewScheme(),
				BaseReference: "test-ref",
			},
		},
		{
			name: "success with local blob access (nil resource digest)",
			blob: &testBlob{
				content:   content,
				mediaType: "application/vnd.test",
				digest:    digest,
			},
			resource: &descriptor.Resource{
				Access: &v2.LocalBlob{
					Type:           runtime.NewVersionedType(v2.LocalBlobAccessType, v2.LocalBlobAccessTypeVersion),
					LocalReference: digest.String(),
					MediaType:      "application/vnd.test",
				},
			},
			opts: Options{
				AccessScheme:  runtime.NewScheme(),
				BaseReference: "test-ref",
			},
			nilOutResourceBlobDigest: true,
		},
		{
			name: "success with local blob access (but media type derived from blob not access)",
			blob: &testBlob{
				content:   content,
				mediaType: "application/vnd.test",
				digest:    digest,
			},
			resource: &descriptor.Resource{
				Access: &v2.LocalBlob{
					Type:           runtime.NewVersionedType(v2.LocalBlobAccessType, v2.LocalBlobAccessTypeVersion),
					LocalReference: digest.String(),
				},
			},
			opts: Options{
				AccessScheme:  runtime.NewScheme(),
				BaseReference: "test-ref",
			},
		},
		{
			name: "success with never global access policy",
			blob: &testBlob{
				content:   content,
				mediaType: "application/vnd.test",
				digest:    digest,
			},
			resource: &descriptor.Resource{
				Access: &v2.LocalBlob{
					Type:           runtime.NewVersionedType(v2.LocalBlobAccessType, v2.LocalBlobAccessTypeVersion),
					LocalReference: digest.String(),
					MediaType:      "application/vnd.test",
				},
			},
			opts: Options{
				AccessScheme:       runtime.NewScheme(),
				BaseReference:      "test-ref",
				GlobalAccessPolicy: policy.GlobalAccessPolicyNever,
			},
			checkGlobalAccess: func(t *testing.T, resource *descriptor.Resource) {
				access, ok := resource.Access.(*v2.LocalBlob)
				require.True(t, ok, "access should be of type LocalBlob")
				assert.Nil(t, access.GlobalAccess, "global access should not be set with Never policy")
			},
		},
		{
			name: "success with auto global access policy on local store",
			blob: &testBlob{
				content:   content,
				mediaType: "application/vnd.test",
				digest:    digest,
			},
			resource: &descriptor.Resource{
				Access: &v2.LocalBlob{
					Type:           runtime.NewVersionedType(v2.LocalBlobAccessType, v2.LocalBlobAccessTypeVersion),
					LocalReference: digest.String(),
					MediaType:      "application/vnd.test",
				},
			},
			opts: Options{
				AccessScheme:       runtime.NewScheme(),
				BaseReference:      "test-ref",
				GlobalAccessPolicy: policy.GlobalAccessPolicyAuto,
			},
			checkGlobalAccess: func(t *testing.T, resource *descriptor.Resource) {
				access, ok := resource.Access.(*v2.LocalBlob)
				require.True(t, ok, "access should be of type LocalBlob")
				// Auto on local (non-remote) store should not set global access
				assert.Nil(t, access.GlobalAccess, "global access should not be set for auto policy on local store")
			},
		},
		{
			name: "empty type but typed access",
			blob: &testBlob{
				content:   content,
				mediaType: "application/vnd.test",
				digest:    digest,
			},
			resource: &descriptor.Resource{
				Access: &v2.LocalBlob{
					LocalReference: digest.String(),
					MediaType:      "application/vnd.test",
				},
			},
			opts: Options{
				AccessScheme:  runtime.NewScheme(),
				BaseReference: "test-ref",
			},
		},
		{
			name: "error on unsupported access type",
			blob: &testBlob{
				content:   content,
				mediaType: "application/vnd.test",
				digest:    digest,
			},
			resource: &descriptor.Resource{
				Access: &runtime.Raw{
					Type: runtime.NewVersionedType("unsupported", "v1"),
				},
			},
			opts: Options{
				AccessScheme: runtime.NewScheme(),
			},
			expectedError: "failed to convert artifact access to local blob",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store, err := file.New(t.TempDir())
			require.NoError(t, err)
			t.Cleanup(func() {
				require.NoError(t, store.Close())
			})

			v2.MustAddToScheme(tt.opts.AccessScheme)
			oci.MustAddToScheme(tt.opts.AccessScheme)
			require.NoError(t, resourceblob.UpdateArtifactWithInformationFromBlob(tt.resource, tt.blob))
			resourceBlob, err := resourceblob.NewArtifactBlob(tt.resource, tt.blob)
			require.NoError(t, err)
			if tt.nilOutResourceBlobDigest {
				resourceBlob.Artifact.(*descriptor.Resource).Digest = nil
			}
			desc, err := ArtifactBlob(ctx, store, resourceBlob, tt.opts)

			if tt.expectedError != "" {
				assert.ErrorContains(t, err, tt.expectedError)
				return
			}

			assert.NoError(t, err)
			assert.Equal(t, tt.blob.mediaType, desc.MediaType)

			data, err := store.Fetch(t.Context(), desc)
			require.NoError(t, err)
			t.Cleanup(func() {
				require.NoError(t, data.Close())
			})

			layerData, err := io.ReadAll(data)
			require.NoError(t, err)
			assert.Equal(t, tt.blob.content, layerData)

			if tt.checkGlobalAccess != nil {
				tt.checkGlobalAccess(t, tt.resource)
			}
		})
	}
}

func TestResourceLocalBlob(t *testing.T) {
	store, err := file.New(t.TempDir())
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, store.Close())
	})

	content := []byte("test content")
	dig := digest.FromBytes(content)

	tests := []struct {
		name          string
		blob          *testBlob
		resource      *descriptor.Resource
		access        *v2.LocalBlob
		opts          Options
		expectedError string
	}{
		{
			name: "success with OCI layout media type",
			blob: &testBlob{
				content:   content,
				mediaType: "application/vnd.oci.image.layout.v1+tar",
				digest:    dig,
			},
			resource: &descriptor.Resource{},
			access: &v2.LocalBlob{
				MediaType: "application/vnd.oci.image.layout.v1+tar",
			},
			opts: Options{
				AccessScheme:  runtime.NewScheme(),
				BaseReference: "test-ref",
			},
		},
		{
			name: "success with single layer artifact",
			blob: &testBlob{
				content:   content,
				mediaType: "application/vnd.test",
				digest:    dig,
			},
			resource: &descriptor.Resource{},
			access: &v2.LocalBlob{
				MediaType: "application/vnd.test",
			},
			opts: Options{
				AccessScheme:  runtime.NewScheme(),
				BaseReference: "test-ref",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v2.MustAddToScheme(tt.opts.AccessScheme)
			oci.MustAddToScheme(tt.opts.AccessScheme)

			require.NoError(t, resourceblob.UpdateArtifactWithInformationFromBlob(tt.resource, tt.blob))

			resourceBlob, err := resourceblob.NewArtifactBlob(tt.resource, tt.blob)
			require.NoError(t, err)
			desc, err := ResourceLocalBlob(t.Context(), store, resourceBlob, tt.access, tt.opts)

			if tt.expectedError != "" {
				assert.ErrorContains(t, err, tt.expectedError)
				return
			}

			assert.NoError(t, err)
			data, err := store.Fetch(t.Context(), desc)
			require.NoError(t, err)
			t.Cleanup(func() {
				require.NoError(t, data.Close())
			})

			layerData, err := io.ReadAll(data)
			require.NoError(t, err)
			assert.Equal(t, tt.blob.content, layerData)
		})
	}
}

// TestResourceLocalBlobMediaTypeDetection tests the specific logic for detecting
// OCI layout vs OCI layer based on both access and blob media types separately.
// This test verifies the fix where access media type is checked first, then blob
// media type is checked separately if access media type doesn't match OCI layout types.
func TestResourceLocalBlobMediaTypeDetection(t *testing.T) {
	store, err := file.New(t.TempDir())
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, store.Close())
	})

	// Create valid OCI layout content for layout tests
	ctx := t.Context()
	var buf bytes.Buffer
	writer, err := tar.NewOCILayoutWriterWithTempFile(&buf, t.TempDir())
	require.NoError(t, err)
	_, err = oras.PackManifest(ctx, writer, oras.PackManifestVersion1_1, "application/custom", oras.PackManifestOptions{})
	require.NoError(t, err)
	require.NoError(t, writer.Close())
	ociLayoutContent := buf.Bytes()
	ociLayoutDigest := digest.FromBytes(ociLayoutContent)

	// Regular content for layer tests
	layerContent := []byte("regular layer content")
	layerDigest := digest.FromBytes(layerContent)

	tests := []struct {
		name           string
		blob           *testBlob
		access         *v2.LocalBlob
		expectLayout   bool
		expectOCILayer bool
		expectedError  string
	}{
		{
			name: "access media type takes precedence - layout in access",
			blob: &testBlob{
				content:   ociLayoutContent,
				mediaType: "application/vnd.test", // Different blob media type
				digest:    ociLayoutDigest,
			},
			access: &v2.LocalBlob{
				MediaType: layout.MediaTypeOCIImageLayoutTarV1, // Access has layout type
			},
			expectLayout: true,
		},
		{
			name: "access media type takes precedence - gzip layout in access",
			blob: &testBlob{
				content:   ociLayoutContent,
				mediaType: "application/vnd.test", // Different blob media type
				digest:    ociLayoutDigest,
			},
			access: &v2.LocalBlob{
				MediaType: layout.MediaTypeOCIImageLayoutTarGzipV1, // Access has layout type
			},
			expectLayout: true,
		},
		{
			name: "fallback to blob media type when access is empty - layout in blob",
			blob: &testBlob{
				content:   ociLayoutContent,
				mediaType: layout.MediaTypeOCIImageLayoutTarV1, // Blob has layout type
				digest:    ociLayoutDigest,
			},
			access: &v2.LocalBlob{
				MediaType: "", // Empty access media type
			},
			expectLayout: true,
		},
		{
			name: "fallback to blob media type when access is empty - gzip layout in blob",
			blob: &testBlob{
				content:   ociLayoutContent,
				mediaType: layout.MediaTypeOCIImageLayoutTarGzipV1, // Blob has layout type
				digest:    ociLayoutDigest,
			},
			access: &v2.LocalBlob{
				MediaType: "", // Empty access media type
			},
			expectLayout: true,
		},
		{
			name: "fallback to OCI layer when neither access nor blob have layout types",
			blob: &testBlob{
				content:   layerContent,
				mediaType: "application/vnd.test", // Regular media type
				digest:    layerDigest,
			},
			access: &v2.LocalBlob{
				MediaType: "", // Empty access media type
			},
			expectOCILayer: true,
		},
		{
			name: "fallback to OCI layer when access is non-layout but blob has different type",
			blob: &testBlob{
				content:   layerContent,
				mediaType: "application/vnd.other", // Different but non-layout type
				digest:    layerDigest,
			},
			access: &v2.LocalBlob{
				MediaType: "application/vnd.test", // Non-layout access type
			},
			expectOCILayer: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := Options{
				AccessScheme:  runtime.NewScheme(),
				BaseReference: "test-ref",
			}
			v2.MustAddToScheme(opts.AccessScheme)
			oci.MustAddToScheme(opts.AccessScheme)

			resource := &descriptor.Resource{}
			require.NoError(t, resourceblob.UpdateArtifactWithInformationFromBlob(resource, tt.blob))

			resourceBlob, err := resourceblob.NewArtifactBlob(resource, tt.blob)
			require.NoError(t, err)

			desc, err := ResourceLocalBlob(ctx, store, resourceBlob, tt.access, opts)

			if tt.expectedError != "" {
				assert.ErrorContains(t, err, tt.expectedError)
				return
			}

			assert.NoError(t, err)

			// Verify the expected behavior based on media type detection
			if tt.expectLayout {
				// For OCI layout, we expect a manifest media type
				assert.Equal(t, ociImageSpecV1.MediaTypeImageManifest, desc.MediaType)
			} else if tt.expectOCILayer {
				// For OCI layer, we expect the original blob media type
				expectedMediaType := tt.blob.mediaType
				if expectedMediaType == "" && tt.access.MediaType != "" {
					expectedMediaType = tt.access.MediaType
				}
				assert.Equal(t, expectedMediaType, desc.MediaType)
			}
		})
	}
}

func TestResourceLocalBlobOCISingleLayerArtifact(t *testing.T) {
	store, err := file.New(t.TempDir())
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, store.Close())
	})

	content := []byte("test content")
	digest := digest.FromBytes(content)

	tests := []struct {
		name          string
		blob          *testBlob
		resource      *descriptor.Resource
		access        *v2.LocalBlob
		opts          Options
		expectedError string
	}{
		{
			name: "success with valid input",
			blob: &testBlob{
				content:   content,
				mediaType: "application/vnd.test",
				digest:    digest,
			},
			resource: &descriptor.Resource{},
			access: &v2.LocalBlob{
				MediaType:      "application/vnd.test",
				LocalReference: digest.String(),
			},
			opts: Options{
				AccessScheme:  runtime.NewScheme(),
				BaseReference: "test-ref",
			},
		},
		{
			name: "error on blob resource layer creation",
			blob: &testBlob{
				content:   nil,
				mediaType: "application/vnd.test",
				digest:    digest,
			},
			resource: &descriptor.Resource{},
			access: &v2.LocalBlob{
				MediaType:      "application/vnd.test",
				LocalReference: digest.String(),
			},
			opts: Options{
				AccessScheme:  runtime.NewScheme(),
				BaseReference: "test-ref",
			},
			expectedError: "failed to create resource layer based on blob",
		},
		{
			name: "error on push blob failure",
			blob: &testBlob{
				content:   content,
				mediaType: "application/vnd.test",
				digest:    digest,
			},
			resource: &descriptor.Resource{},
			access: &v2.LocalBlob{
				MediaType:      "application/vnd.test",
				LocalReference: digest.String(),
			},
			opts: Options{
				AccessScheme:  runtime.NewScheme(),
				BaseReference: "test-ref",
			},
			expectedError: "failed to push blob",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v2.MustAddToScheme(tt.opts.AccessScheme)
			oci.MustAddToScheme(tt.opts.AccessScheme)

			require.NoError(t, resourceblob.UpdateArtifactWithInformationFromBlob(tt.resource, tt.blob))

			resourceBlob, err := resourceblob.NewArtifactBlob(tt.resource, tt.blob)
			require.NoError(t, err)
			desc, err := ResourceLocalBlobOCILayer(t.Context(), store, resourceBlob, tt.access, tt.opts)

			if tt.expectedError != "" {
				assert.ErrorContains(t, err, tt.expectedError)
				return
			}

			data, err := store.Fetch(t.Context(), desc)
			require.NoError(t, err)
			t.Cleanup(func() {
				require.NoError(t, data.Close())
			})
			layerData, err := io.ReadAll(data)
			require.NoError(t, err)
			assert.Equal(t, tt.blob.content, layerData)
		})
	}
}

func TestPackingPreservesResourceDigest(t *testing.T) {
	for _, storageForm := range []string{"layer", "manifest", "wrapper index"} {
		t.Run(storageForm, func(t *testing.T) {
			r := require.New(t)
			ctx := t.Context()
			data := []byte("test content")
			mediaType := "application/vnd.test"
			root := ociImageSpecV1.Descriptor{
				MediaType: mediaType,
				Digest:    digest.FromBytes(data),
				Size:      int64(len(data)),
			}
			resourceDigest := root.Digest
			if storageForm != "layer" {
				var buf bytes.Buffer
				writer, err := tar.NewOCILayoutWriterWithTempFile(&buf, t.TempDir())
				r.NoError(err)
				root, err = oras.PackManifest(ctx, writer, oras.PackManifestVersion1_1, "application/custom", oras.PackManifestOptions{})
				r.NoError(err)
				resourceDigest = root.Digest
				if storageForm == "wrapper index" {
					index := ociImageSpecV1.Index{
						MediaType: ociImageSpecV1.MediaTypeImageIndex,
						Manifests: []ociImageSpecV1.Descriptor{root},
					}
					index.SchemaVersion = 2
					indexData, err := json.Marshal(index)
					r.NoError(err)
					root = content.NewDescriptorFromBytes(ociImageSpecV1.MediaTypeImageIndex, indexData)
					r.NoError(writer.Push(ctx, root, bytes.NewReader(indexData)))
					r.NotEqual(resourceDigest, root.Digest)
				}
				r.NoError(writer.Close())
				data = buf.Bytes()
				mediaType = layout.MediaTypeOCIImageLayoutTarV1
			}

			for _, tc := range []struct {
				name          string
				normalization string
				hasDigest     bool
			}{
				{name: "legacy OCI digest", normalization: "ociArtifactDigest/v1", hasDigest: true},
				{name: "generic blob digest", normalization: "genericBlobDigest/v1", hasDigest: true},
				{name: "normalized JSON digest", normalization: "jsonNormalisation/v1", hasDigest: true},
				{name: "intermediate blob digest", hasDigest: true},
				{name: "missing digest"},
			} {
				t.Run(tc.name, func(t *testing.T) {
					r := require.New(t)
					store, err := file.New(t.TempDir())
					r.NoError(err)
					t.Cleanup(func() { r.NoError(store.Close()) })
					scheme := runtime.NewScheme()
					v2.MustAddToScheme(scheme)
					oci.MustAddToScheme(scheme)

					access := &v2.LocalBlob{MediaType: mediaType, ReferenceName: "original"}
					resource := &descriptor.Resource{Access: access}
					var original descriptor.Digest
					if tc.hasDigest {
						original = descriptor.Digest{
							HashAlgorithm:          "SHA-256",
							NormalisationAlgorithm: tc.normalization,
							Value:                  resourceDigest.Encoded(),
						}
						resource.Digest = &original
					}
					before := original
					// Isolate packing from constructor digest defaulting and validation:
					// a layout's tar digest need not equal the OCM resource digest.
					b := &resourceblob.ArtifactBlob{
						Artifact: resource,
						ReadOnlyBlob: &testBlob{
							content: data, mediaType: mediaType, digest: digest.FromBytes(data),
						},
					}
					packed, err := ArtifactBlob(t.Context(), store, b, Options{AccessScheme: scheme})
					r.NoError(err)
					r.True(content.Equal(root, packed))
					updated, ok := resource.Access.(*v2.LocalBlob)
					r.True(ok)
					r.Equal(root.Digest.String(), updated.LocalReference)
					r.Equal(root.MediaType, updated.MediaType)
					r.Equal(access.ReferenceName, updated.ReferenceName)
					if tc.normalization != "" {
						r.Same(&original, resource.Digest)
						r.Equal(before, *resource.Digest)
					} else {
						r.Equal(&descriptor.Digest{
							HashAlgorithm:          "SHA-256",
							NormalisationAlgorithm: "genericBlobDigest/v1",
							Value:                  root.Digest.Encoded(),
						}, resource.Digest)
					}
				})
			}
		})
	}
}

func TestResourceLocalBlobOCILayout(t *testing.T) {
	store, err := file.New(t.TempDir())
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, store.Close())
	})

	ctx := t.Context()
	var buf bytes.Buffer
	writer, err := tar.NewOCILayoutWriterWithTempFile(&buf, t.TempDir())
	require.NoError(t, err)

	desc, err := oras.PackManifest(ctx, writer, oras.PackManifestVersion1_1, "application/custom", oras.PackManifestOptions{})
	require.NoError(t, err)

	require.NoError(t, writer.Close())
	ociLayout := buf.Bytes()

	tests := []struct {
		name              string
		blob              *testBlob
		resource          *descriptor.Resource
		opts              Options
		expectedError     string
		checkGlobalAccess func(t *testing.T, resource *descriptor.Resource)
	}{
		{
			name: "success with valid input",
			blob: &testBlob{
				content:   ociLayout,
				mediaType: "application/vnd.oci.image.layout.v1+tar",
				digest:    digest.FromBytes(ociLayout),
			},
			resource: &descriptor.Resource{},
			opts: Options{
				AccessScheme:  runtime.NewScheme(),
				BaseReference: "test-ref",
			},
		},
		{
			name: "error on invalid OCI layout",
			blob: &testBlob{
				content:   []byte("invalid layout"),
				mediaType: "application/vnd.oci.image.layout.v1+tar",
				digest:    digest.FromBytes([]byte("invalid layout")),
			},
			resource: &descriptor.Resource{},
			opts: Options{
				AccessScheme:  runtime.NewScheme(),
				BaseReference: "test-ref",
			},
			expectedError: "failed to copy OCI layout",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v2.MustAddToScheme(tt.opts.AccessScheme)
			oci.MustAddToScheme(tt.opts.AccessScheme)

			require.NoError(t, resourceblob.UpdateArtifactWithInformationFromBlob(tt.resource, tt.blob))

			resourceBlob, err := resourceblob.NewArtifactBlob(tt.resource, tt.blob)
			require.NoError(t, err)

			fromStore, err := ResourceLocalBlobOCILayout(ctx, store, resourceBlob, &v2.LocalBlob{}, tt.opts)

			if tt.expectedError != "" {
				assert.ErrorContains(t, err, tt.expectedError)
				return
			}

			assert.NoError(t, err)
			assert.Equal(t, ociImageSpecV1.MediaTypeImageManifest, fromStore.MediaType)
			content.Equal(fromStore, desc)

			if tt.checkGlobalAccess != nil {
				tt.checkGlobalAccess(t, tt.resource)
			}
		})
	}
}

// streamingStore is a fake content.Storage that also implements the streaming
// pusher contract used by ResourceLocalBlobOCILayer. It records which push path
// was taken and computes the descriptor from the streamed bytes.
type streamingStore struct {
	streamed      []byte
	streamDesc    ociImageSpecV1.Descriptor
	streamed64    bool
	pushed        bool
	partialDigest digest.Digest
	partialSize   int64
}

func (s *streamingStore) Push(_ context.Context, _ ociImageSpecV1.Descriptor, r io.Reader) error {
	s.pushed = true
	_, err := io.Copy(io.Discard, r)
	return err
}

func (s *streamingStore) Fetch(_ context.Context, _ ociImageSpecV1.Descriptor) (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader(s.streamed)), nil
}

func (s *streamingStore) Exists(_ context.Context, _ ociImageSpecV1.Descriptor) (bool, error) {
	return false, nil
}

func (s *streamingStore) PushStreaming(_ context.Context, partial ociImageSpecV1.Descriptor, r io.Reader) (ociImageSpecV1.Descriptor, error) {
	s.streamed64 = true
	data, err := io.ReadAll(r)
	if err != nil {
		return ociImageSpecV1.Descriptor{}, err
	}
	s.streamed = data
	s.partialDigest = partial.Digest
	s.partialSize = partial.Size
	dig := partial.Digest
	if dig == "" {
		dig = digest.FromBytes(data)
	}
	s.streamDesc = ociImageSpecV1.Descriptor{
		MediaType: partial.MediaType,
		Digest:    dig,
		Size:      int64(len(data)),
	}
	return s.streamDesc, nil
}

func TestResourceLocalBlobOCILayer_StreamsUnknownDigest(t *testing.T) {
	r := require.New(t)

	// A compressed blob has neither a known size nor digest, so without
	// streaming it would be buffered to compute the descriptor.
	text := "streamed layer content"
	var b blob.ReadOnlyBlob = &testBlob{content: []byte(text), mediaType: "text/plain"}
	b = compression.Compress(b)

	resource := &descriptor.Resource{}
	resourceBlob, err := resourceblob.NewArtifactBlob(resource, b)
	r.NoError(err)
	_, known := resourceBlob.Digest()
	r.False(known, "precondition: digest must be unknown to exercise streaming")

	// LocalReference is empty so no digest is supplied via the access.
	access := &v2.LocalBlob{MediaType: "text/plain+gzip"}
	opts := Options{AccessScheme: runtime.NewScheme(), BaseReference: "test-ref"}
	v2.MustAddToScheme(opts.AccessScheme)
	oci.MustAddToScheme(opts.AccessScheme)

	store := &streamingStore{}
	desc, err := ResourceLocalBlobOCILayer(t.Context(), store, resourceBlob, access, opts)
	r.NoError(err)

	// Streaming path was taken, not the buffering monolithic Push.
	r.True(store.streamed64, "expected PushStreaming to be used")
	r.False(store.pushed, "monolithic Push must not be used for unknown-digest streaming")

	// The descriptor digest/size were computed from the streamed bytes and the
	// resource access was populated with that digest.
	want := digest.FromBytes(store.streamed)
	r.Equal(want, desc.Digest)
	r.Equal(int64(len(store.streamed)), desc.Size)
	r.NotNil(resource.Digest)
	r.Equal(want.Encoded(), resource.Digest.Value)
}

// knownDigestUnknownSizeBlob exposes a digest (DigestAware) but reports an
// unknown size, so PrepareArtifactBlobForOCI would buffer it to learn the size.
type knownDigestUnknownSizeBlob struct {
	content   []byte
	mediaType string
}

func (b *knownDigestUnknownSizeBlob) ReadCloser() (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader(b.content)), nil
}
func (b *knownDigestUnknownSizeBlob) Size() int64               { return blob.SizeUnknown }
func (b *knownDigestUnknownSizeBlob) MediaType() (string, bool) { return b.mediaType, true }
func (b *knownDigestUnknownSizeBlob) Digest() (string, bool) {
	return digest.FromBytes(b.content).String(), true
}

func TestResourceLocalBlobOCILayer_StreamsKnownDigestUnknownSize(t *testing.T) {
	r := require.New(t)

	content := []byte("known digest but lazily loaded, unknown size")
	wantDigest := digest.FromBytes(content)
	src := &knownDigestUnknownSizeBlob{content: content, mediaType: "application/octet-stream"}

	// Precondition: digest known, size unknown.
	dig, known := src.Digest()
	r.True(known)
	r.Equal(wantDigest.String(), dig)
	r.Equal(blob.SizeUnknown, src.Size())

	resource := &descriptor.Resource{}
	resourceBlob, err := resourceblob.NewArtifactBlob(resource, src)
	r.NoError(err)

	access := &v2.LocalBlob{MediaType: "application/octet-stream"}
	opts := Options{AccessScheme: runtime.NewScheme(), BaseReference: "test-ref"}
	v2.MustAddToScheme(opts.AccessScheme)
	oci.MustAddToScheme(opts.AccessScheme)

	store := &streamingStore{}
	desc, err := ResourceLocalBlobOCILayer(t.Context(), store, resourceBlob, access, opts)
	r.NoError(err)

	// Streamed (not buffered), and the known digest was passed through and used.
	r.True(store.streamed64, "expected PushStreaming to be used for known-digest/unknown-size blob")
	r.False(store.pushed, "monolithic Push must not be used")
	r.Equal(wantDigest, store.partialDigest, "known digest must be passed to the streaming pusher")
	r.Equal(wantDigest, desc.Digest)
	r.Equal(content, store.streamed)
}

// sizeKnownUnknownDigestBlob exposes a size (SizeAware) but no digest, so the
// streaming path is taken (digest must be computed) while the size is known and
// must be forwarded to PushStreaming for short-reader verification.
type sizeKnownUnknownDigestBlob struct {
	content   []byte
	mediaType string
}

func (b *sizeKnownUnknownDigestBlob) ReadCloser() (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader(b.content)), nil
}
func (b *sizeKnownUnknownDigestBlob) Size() int64               { return int64(len(b.content)) }
func (b *sizeKnownUnknownDigestBlob) MediaType() (string, bool) { return b.mediaType, true }

// TestResourceLocalBlobOCILayer_ForwardsKnownSize verifies that when the blob
// size is known but the digest is not, streamResourceLayer forwards the known
// size to PushStreaming (rather than SizeUnknown), so a short reader is caught
// by the streaming size check instead of silently truncating the blob.
func TestResourceLocalBlobOCILayer_ForwardsKnownSize(t *testing.T) {
	r := require.New(t)

	content := []byte("size is known here, digest is not")
	src := &sizeKnownUnknownDigestBlob{content: content, mediaType: "application/octet-stream"}

	resource := &descriptor.Resource{}
	resourceBlob, err := resourceblob.NewArtifactBlob(resource, src)
	r.NoError(err)

	// Preconditions: size known, digest unknown — the exact case where the
	// size must be forwarded to the streaming pusher.
	r.NotEqual(blob.SizeUnknown, resourceBlob.Size(), "precondition: size must be known")
	_, digestKnown := resourceBlob.Digest()
	r.False(digestKnown, "precondition: digest must be unknown to exercise streaming")

	access := &v2.LocalBlob{MediaType: "application/octet-stream"}
	opts := Options{AccessScheme: runtime.NewScheme(), BaseReference: "test-ref"}
	v2.MustAddToScheme(opts.AccessScheme)
	oci.MustAddToScheme(opts.AccessScheme)

	store := &streamingStore{}
	_, err = ResourceLocalBlobOCILayer(t.Context(), store, resourceBlob, access, opts)
	r.NoError(err)

	r.True(store.streamed64, "expected PushStreaming to be used")
	r.False(store.pushed, "monolithic Push must not be used")
	r.Equal(int64(len(content)), store.partialSize, "known size must be forwarded to PushStreaming")
}
