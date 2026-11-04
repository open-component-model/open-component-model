package integration_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"testing"

	godigest "github.com/opencontainers/go-digest"
	ocispecv1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/require"
	"oras.land/oras-go/v2"
	"oras.land/oras-go/v2/content"
	"oras.land/oras-go/v2/registry/remote"

	"ocm.software/open-component-model/bindings/go/blob/inmemory"
	"ocm.software/open-component-model/bindings/go/descriptor/normalisation"
	"ocm.software/open-component-model/bindings/go/descriptor/normalisation/json/v4alpha1"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	descriptorv2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/oci"
	"ocm.software/open-component-model/bindings/go/oci/repository/provider"
	ociresource "ocm.software/open-component-model/bindings/go/oci/repository/resource"
	urlresolver "ocm.software/open-component-model/bindings/go/oci/resolver/url"
	ociaccessv1 "ocm.software/open-component-model/bindings/go/oci/spec/access/v1"
	"ocm.software/open-component-model/bindings/go/oci/spec/layout"
	ctfrepospec "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/ctf"
	ocirepospec "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/oci"
	ocitar "ocm.software/open-component-model/bindings/go/oci/tar"
	"ocm.software/open-component-model/bindings/go/repository"
	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/bindings/go/transfer"
	transferv1alpha1 "ocm.software/open-component-model/bindings/go/transfer/v1alpha1/spec"
)

// Test_Integration_TransferOCIArtifact_ExcludedFromSignature transfers a resource excluded from
// the signature as an OCI artifact. It has no digest to verify, so the upload must succeed and
// keep the digest: rewriting it would change the signed descriptor.
func Test_Integration_TransferOCIArtifact_ExcludedFromSignature(t *testing.T) {
	t.Parallel()

	const componentName, componentVersion = "ocm.software/excluded-from-signature", "1.0.0"
	meta := descriptor.ElementMeta{ObjectMeta: descriptor.ObjectMeta{Name: "artifact", Version: "1.0.0"}}

	for _, tc := range []struct {
		name     string
		wantType string
		uploader *transferv1alpha1.OCIUploaderConfig
		seed     func(t *testing.T, repo repository.ComponentVersionRepository, creds registryCreds) (descriptor.Resource, godigest.Digest)
	}{
		{
			name:     "local blob",
			wantType: "TransferCTFLocalBlobToOCIArtifact",
			uploader: &transferv1alpha1.OCIUploaderConfig{Match: `resource.access.isType("LocalBlob")`},
			seed: func(t *testing.T, repo repository.ComponentVersionRepository, _ registryCreds) (descriptor.Resource, godigest.Digest) {
				t.Helper()
				r := require.New(t)
				var archive bytes.Buffer
				writer, err := ocitar.NewOCILayoutWriterWithTempFile(&archive, t.TempDir())
				r.NoError(err)
				layer := []byte("test layer content")
				layerDesc := content.NewDescriptorFromBytes(ocispecv1.MediaTypeImageLayer, layer)
				r.NoError(writer.Push(t.Context(), layerDesc, bytes.NewReader(layer)))
				root, err := oras.PackManifest(t.Context(), writer, oras.PackManifestVersion1_1, "application/vnd.ocm.test", oras.PackManifestOptions{
					Layers: []ocispecv1.Descriptor{layerDesc},
				})
				r.NoError(err)
				r.NoError(writer.Close())

				resource, err := repo.AddLocalResource(t.Context(), componentName, componentVersion, &descriptor.Resource{
					ElementMeta: meta,
					Type:        "ociImage",
					Relation:    descriptor.LocalRelation,
					Access: &descriptorv2.LocalBlob{
						Type:          runtime.NewVersionedType(descriptorv2.LocalBlobAccessType, descriptorv2.LocalBlobAccessTypeVersion),
						MediaType:     layout.MediaTypeOCIImageLayoutTarV1,
						ReferenceName: "test/artifact:1.0.0",
					},
				}, inmemory.New(bytes.NewReader(archive.Bytes()), inmemory.WithMediaType(layout.MediaTypeOCIImageLayoutTarV1)))
				r.NoError(err)
				return *resource, root.Digest
			},
		},
		{
			name:     "OCI image",
			wantType: "TransferOCIArtifact",
			uploader: &transferv1alpha1.OCIUploaderConfig{},
			seed: func(t *testing.T, _ repository.ComponentVersionRepository, creds registryCreds) (descriptor.Resource, godigest.Digest) {
				t.Helper()
				r := require.New(t)
				imageRef := pushTestOCIImage(t, creds.address, creds.username, creds.password, "test/image", "v1")
				source, err := remote.NewRepository(strings.TrimPrefix(imageRef, "http://"))
				r.NoError(err)
				source.PlainHTTP = true
				source.Client = createAuthClient(creds.address, creds.username, creds.password)
				root, err := source.Resolve(t.Context(), "v1")
				r.NoError(err)
				return descriptor.Resource{
					ElementMeta: meta,
					Type:        "ociImage",
					Relation:    descriptor.ExternalRelation,
					Access: &ociaccessv1.OCIImage{
						Type:           runtime.NewVersionedType(ociaccessv1.LegacyType, ociaccessv1.LegacyTypeVersion),
						ImageReference: imageRef,
					},
				}, root.Digest
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			r := require.New(t)
			ctx := t.Context()

			sourceAddr, sourceUser, sourcePwd := startRegistry(t)
			targetAddr, targetUser, targetPwd := startRegistry(t)
			sourceCreds := registryCreds{sourceAddr, sourceUser, sourcePwd}

			sourcePath := t.TempDir()
			sourceRepo := createCTFRepository(t, sourcePath)
			resource, wantRoot := tc.seed(t, sourceRepo, sourceCreds)
			resource.Digest = descriptor.NewExcludeFromSignatureDigest()
			desc := &descriptor.Descriptor{
				Meta: descriptor.Meta{Version: "v2"},
				Component: descriptor.Component{
					ComponentMeta: descriptor.ComponentMeta{
						ObjectMeta: descriptor.ObjectMeta{Name: componentName, Version: componentVersion},
					},
					Provider:  descriptor.Provider{Name: "test-provider"},
					Resources: []descriptor.Resource{resource},
				},
			}
			r.NoError(sourceRepo.AddComponentVersion(ctx, desc))

			definition, err := transfer.BuildGraphDefinition(ctx,
				&transferv1alpha1.Config{},
				[]transferv1alpha1.UploaderConfig{tc.uploader},
				transfer.Mapping{
					Components: []transfer.ComponentID{{Component: componentName, Version: componentVersion}},
					Target: &ocirepospec.Repository{
						Type:    runtime.Type{Name: ocirepospec.Type, Version: "v1"},
						BaseUrl: fmt.Sprintf("http://%s", targetAddr),
					},
					Resolver: transfer.NewRepositoryResolver(sourceRepo, &ctfrepospec.Repository{
						Type:     runtime.NewVersionedType(ctfrepospec.Type, ctfrepospec.Version),
						FilePath: sourcePath,
					}),
				},
			)
			r.NoError(err)
			var types []string
			for _, transformation := range definition.Transformations {
				types = append(types, transformation.Type.Name)
			}
			r.Contains(types, tc.wantType)

			graph, err := transfer.NewDefaultBuilder(
				provider.NewComponentVersionRepositoryProvider(provider.WithTempDir(t.TempDir())),
				ociresource.NewResourceRepository(nil),
				newCredResolver(t, sourceCreds, registryCreds{targetAddr, targetUser, targetPwd}),
			).BuildAndCheck(definition)
			r.NoError(err)
			r.NoError(graph.Process(ctx))

			resolver, err := urlresolver.New(
				urlresolver.WithBaseURL(targetAddr),
				urlresolver.WithPlainHTTP(true),
				urlresolver.WithBaseClient(createAuthClient(targetAddr, targetUser, targetPwd)),
			)
			r.NoError(err)
			targetRepo, err := oci.NewRepository(oci.WithResolver(resolver), oci.WithTempDir(t.TempDir()))
			r.NoError(err)
			got, err := targetRepo.GetComponentVersion(ctx, componentName, componentVersion)
			r.NoError(err)
			r.Len(got.Component.Resources, 1)
			r.Equal(descriptor.NewExcludeFromSignatureDigest(), got.Component.Resources[0].Digest)

			want, err := normalisation.Normalise(desc, v4alpha1.Algorithm)
			r.NoError(err)
			gotNormalised, err := normalisation.Normalise(got, v4alpha1.Algorithm)
			r.NoError(err)
			r.JSONEq(string(want), string(gotNormalised), "transfer must not change the normalised descriptor")

			var access ociaccessv1.OCIImage
			raw, err := json.Marshal(got.Component.Resources[0].Access)
			r.NoError(err)
			r.NoError(json.Unmarshal(raw, &access))
			r.Contains(access.ImageReference, targetAddr, "the artifact must be uploaded to the target registry")
			stream, err := targetRepo.DownloadResourceStream(ctx, &got.Component.Resources[0])
			r.NoError(err)
			r.Equal(wantRoot, stream.Root().Digest, "the target must hold the source artifact")

			// Downloading is lazy, so only reading it proves the artifact arrived in full.
			downloaded, err := targetRepo.DownloadResource(ctx, &got.Component.Resources[0])
			r.NoError(err)
			reader, err := downloaded.ReadCloser()
			r.NoError(err)
			t.Cleanup(func() { r.NoError(reader.Close()) })
			_, err = io.Copy(io.Discard, reader)
			r.NoError(err, "the uploaded artifact must be readable from the target registry")
		})
	}
}
