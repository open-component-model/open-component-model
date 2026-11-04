package integration_test

import (
	"fmt"
	"io"
	"testing"

	ocispecv1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/require"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	descriptorv2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/oci"
	"ocm.software/open-component-model/bindings/go/oci/repository/provider"
	ociresource "ocm.software/open-component-model/bindings/go/oci/repository/resource"
	urlresolver "ocm.software/open-component-model/bindings/go/oci/resolver/url"
	ociaccessv1 "ocm.software/open-component-model/bindings/go/oci/spec/access/v1"
	ctfrepospec "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/ctf"
	ocirepospec "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/oci"
	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/bindings/go/transfer"
	transferv1alpha1 "ocm.software/open-component-model/bindings/go/transfer/v1alpha1/spec"
)

// Test_Integration_TransferOCIImageLayer verifies that an OCIImageLayer resource is
// transferred by value: the layer blob is fetched from the source registry and stored as
// a local blob in the target, with its media type kept. A layer is not an OCI artifact,
// so this also holds when an OCI uploader with its default match comes first.
func Test_Integration_TransferOCIImageLayer(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name      string
		uploaders []transferv1alpha1.UploaderConfig
	}{
		{
			name:      "local blob uploader",
			uploaders: []transferv1alpha1.UploaderConfig{&transferv1alpha1.LocalBlobUploaderConfig{}},
		},
		{
			name: "OCI uploader before the local blob uploader",
			uploaders: []transferv1alpha1.UploaderConfig{
				&transferv1alpha1.OCIUploaderConfig{},
				&transferv1alpha1.LocalBlobUploaderConfig{},
			},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			r := require.New(t)
			ctx := t.Context()

			sourceAddr, sourceUser, sourcePwd := startRegistry(t)
			targetAddr, targetUser, targetPwd := startRegistry(t)

			// The layer is pushed as part of an image, so the registry keeps it referenced.
			pushTestOCIImage(t, sourceAddr, sourceUser, sourcePwd, "test/image", "v1")
			layerContent := []byte("test layer content for integration test")
			layerDigest := digestOf(layerContent)

			componentName := "ocm.software/oci-image-layer-test"
			componentVersion := "1.0.0"
			sourceCTFPath := t.TempDir()
			ctfRepo := createCTFRepository(t, sourceCTFPath)
			r.NoError(ctfRepo.AddComponentVersion(ctx, &descriptor.Descriptor{
				Meta: descriptor.Meta{Version: "v2"},
				Component: descriptor.Component{
					ComponentMeta: descriptor.ComponentMeta{
						ObjectMeta: descriptor.ObjectMeta{Name: componentName, Version: componentVersion},
					},
					Provider: descriptor.Provider{Name: "test-provider"},
					Resources: []descriptor.Resource{{
						ElementMeta: descriptor.ElementMeta{
							ObjectMeta: descriptor.ObjectMeta{Name: "layer", Version: "1.0.0"},
						},
						Type:     "blob",
						Relation: descriptor.ExternalRelation,
						Access: &ociaccessv1.OCIImageLayer{
							Type:      runtime.NewVersionedType(ociaccessv1.OCIImageLayerType, ociaccessv1.Version),
							Reference: fmt.Sprintf("http://%s/test/image", sourceAddr),
							MediaType: ocispecv1.MediaTypeImageLayer,
							Digest:    layerDigest,
							Size:      int64(len(layerContent)),
						},
					}},
				},
			}))

			tgd, err := transfer.BuildGraphDefinition(ctx,
				&transferv1alpha1.Config{},
				tt.uploaders,
				transfer.Mapping{
					Components: []transfer.ComponentID{{Component: componentName, Version: componentVersion}},
					Target: &ocirepospec.Repository{
						Type:    runtime.Type{Name: ocirepospec.Type, Version: "v1"},
						BaseUrl: fmt.Sprintf("http://%s", targetAddr),
					},
					Resolver: transfer.NewRepositoryResolver(ctfRepo, &ctfrepospec.Repository{
						Type:     runtime.Type{Name: ctfrepospec.Type, Version: ctfrepospec.Version},
						FilePath: sourceCTFPath,
					}),
				},
			)
			r.NoError(err)

			credResolver := newCredResolver(t,
				registryCreds{sourceAddr, sourceUser, sourcePwd},
				registryCreds{targetAddr, targetUser, targetPwd},
			)
			repoProvider := provider.NewComponentVersionRepositoryProvider(provider.WithTempDir(t.TempDir()))
			graph, err := transfer.NewDefaultBuilder(repoProvider, ociresource.NewResourceRepository(nil), credResolver).BuildAndCheck(tgd)
			r.NoError(err)
			r.NoError(graph.Process(ctx))

			urlRes, err := urlresolver.New(
				urlresolver.WithBaseURL(targetAddr),
				urlresolver.WithPlainHTTP(true),
				urlresolver.WithBaseClient(createAuthClient(targetAddr, targetUser, targetPwd)),
			)
			r.NoError(err)
			targetRepo, err := oci.NewRepository(oci.WithResolver(urlRes), oci.WithTempDir(t.TempDir()))
			r.NoError(err)

			gotDesc, err := targetRepo.GetComponentVersion(ctx, componentName, componentVersion)
			r.NoError(err)
			r.Len(gotDesc.Component.Resources, 1)
			resource := gotDesc.Component.Resources[0]
			r.Equal(layerDigest.Encoded(), resource.Digest.Value)

			accessScheme := runtime.NewScheme(runtime.WithAllowUnknown())
			descriptorv2.MustAddToScheme(accessScheme)
			var localBlob descriptorv2.LocalBlob
			r.NoError(accessScheme.Convert(resource.Access, &localBlob), "a layer is stored as a local blob in the target")
			r.Equal(layerDigest.String(), localBlob.LocalReference)
			r.Equal(ocispecv1.MediaTypeImageLayer, localBlob.MediaType, "the layer media type is kept")

			b, _, err := targetRepo.GetLocalResource(ctx, componentName, componentVersion, resource.ToIdentity())
			r.NoError(err)
			reader, err := b.ReadCloser()
			r.NoError(err)
			t.Cleanup(func() { r.NoError(reader.Close()) })
			data, err := io.ReadAll(reader)
			r.NoError(err)
			r.Equal(layerContent, data, "the target holds the layer bytes, not an OCI layout")
		})
	}
}
