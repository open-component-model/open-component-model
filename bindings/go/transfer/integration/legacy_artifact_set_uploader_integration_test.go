package integration_test

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"testing"

	"github.com/opencontainers/image-spec/specs-go"
	ocispecv1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/require"
	"oras.land/oras-go/v2/content"

	"ocm.software/open-component-model/bindings/go/blob/inmemory"
	"ocm.software/open-component-model/bindings/go/ctf"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	descriptorv2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/oci"
	"ocm.software/open-component-model/bindings/go/oci/repository/provider"
	ociresource "ocm.software/open-component-model/bindings/go/oci/repository/resource"
	urlresolver "ocm.software/open-component-model/bindings/go/oci/resolver/url"
	ociaccessv1 "ocm.software/open-component-model/bindings/go/oci/spec/access/v1"
	ctfrepospec "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/ctf"
	ocirepospec "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/oci"
	ocitar "ocm.software/open-component-model/bindings/go/oci/tar"
	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/bindings/go/transfer"
	transferv1alpha1 "ocm.software/open-component-model/bindings/go/transfer/v1alpha1/spec"
)

func Test_Integration_TransferLegacyOCIIndexArtifactSet_CTFToOCI(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name            string
		legacyMediaType string
		manifestType    string
		indexType       string
	}{
		{
			name:            "oci",
			legacyMediaType: "application/vnd.oci.image.index.v1+tar+gzip",
			manifestType:    ocispecv1.MediaTypeImageManifest,
			indexType:       ocispecv1.MediaTypeImageIndex,
		},
		{
			// Customer components carry docker media types, e.g. images
			// synthesized from docker inputs. The digest must survive the transfer
			// unchanged, i.e. no docker->OCI manifest conversion.
			name:            "docker",
			legacyMediaType: "application/vnd.docker.distribution.manifest.list.v2+tar+gzip",
			manifestType:    "application/vnd.docker.distribution.manifest.v2+json",
			indexType:       "application/vnd.docker.distribution.manifest.list.v2+json",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			transferLegacyArtifactSetCTFToOCI(t, tc.legacyMediaType, tc.manifestType, tc.indexType)
		})
	}
}

func transferLegacyArtifactSetCTFToOCI(t *testing.T, legacyMediaType, manifestType, indexType string) {
	t.Helper()
	r := require.New(t)
	ctx := t.Context()

	registryAddr, user, password := startRegistry(t)
	archive, root := legacyOCIIndexArtifactSet(t, manifestType, indexType)

	const (
		componentName    = "example.com/acme/component"
		componentVersion = "v1.0.5"
		resourceName     = "webhooks-server"
		referenceName    = "acme/components/example.com/acme/component/images/webhooks-server:v1.0.5"
	)
	artifactSet, err := ctf.NewArtifactSetFromBlob(inmemory.New(bytes.NewReader(archive), inmemory.WithMediaType(legacyMediaType)))
	r.NoError(err)
	r.Equal(ctf.ArtifactSetFormatLegacyOCI, artifactSet.Format())
	r.NoError(artifactSet.Close())

	sourcePath := t.TempDir()
	sourceRepo := createCTFRepository(t, sourcePath)
	resource, err := sourceRepo.AddLocalResource(ctx, componentName, componentVersion, &descriptor.Resource{
		ElementMeta: descriptor.ElementMeta{ObjectMeta: descriptor.ObjectMeta{Name: resourceName, Version: componentVersion}},
		Type:        "ociImage",
		Relation:    descriptor.LocalRelation,
		Access: &descriptorv2.LocalBlob{
			Type:          runtime.NewVersionedType(descriptorv2.LocalBlobAccessType, descriptorv2.LocalBlobAccessTypeVersion),
			MediaType:     legacyMediaType,
			ReferenceName: referenceName,
		},
	}, inmemory.New(bytes.NewReader(archive), inmemory.WithMediaType(legacyMediaType)))
	r.NoError(err)
	resource.Digest = &descriptor.Digest{
		HashAlgorithm:          "SHA-256",
		NormalisationAlgorithm: "ociArtifactDigest/v1",
		Value:                  root.Digest.Encoded(),
	}
	r.NoError(sourceRepo.AddComponentVersion(ctx, &descriptor.Descriptor{
		Meta: descriptor.Meta{Version: "v2"},
		Component: descriptor.Component{
			ComponentMeta: descriptor.ComponentMeta{ObjectMeta: descriptor.ObjectMeta{Name: componentName, Version: componentVersion}},
			Provider:      descriptor.Provider{Name: "acme"},
			Resources:     []descriptor.Resource{*resource},
		},
	}))
	source, err := sourceRepo.GetComponentVersion(ctx, componentName, componentVersion)
	r.NoError(err)
	r.Len(source.Component.Resources, 1)
	var sourceAccess descriptorv2.LocalBlob
	r.NoError(descriptorv2.Scheme.Convert(source.Component.Resources[0].Access, &sourceAccess))
	r.Equal(legacyMediaType, sourceAccess.MediaType)
	r.Equal(referenceName, sourceAccess.ReferenceName)

	sourceSpec := &ctfrepospec.Repository{
		Type:     runtime.NewVersionedType(ctfrepospec.Type, ctfrepospec.Version),
		FilePath: sourcePath,
	}
	targetSpec := &ocirepospec.Repository{
		Type:    runtime.NewVersionedType(ocirepospec.Type, ocirepospec.Version),
		BaseUrl: "http://" + registryAddr,
	}
	definition, err := transfer.BuildGraphDefinition(ctx, &transferv1alpha1.Config{}, []transferv1alpha1.UploaderConfig{
		&transferv1alpha1.OCIUploaderConfig{},
		&transferv1alpha1.LocalBlobUploaderConfig{},
	}, transfer.Mapping{
		Components: []transfer.ComponentID{{Component: componentName, Version: componentVersion}},
		Target:     targetSpec,
		Resolver:   transfer.NewRepositoryResolver(sourceRepo, sourceSpec),
	})
	r.NoError(err)

	var uploadsOCIArtifact bool
	for _, transformation := range definition.Transformations {
		if transformation.Type.Name == "TransferCTFLocalBlobToOCIArtifact" {
			uploadsOCIArtifact = true
			break
		}
	}
	r.True(uploadsOCIArtifact, "legacy OCI artifact-set local blob must be selected by the OCI uploader")

	credentials := newCredResolver(t, registryCreds{registryAddr, user, password})
	builder := transfer.NewDefaultBuilder(
		provider.NewComponentVersionRepositoryProvider(provider.WithTempDir(t.TempDir())),
		ociresource.NewResourceRepository(nil),
		credentials,
	)
	graph, err := builder.BuildAndCheck(definition)
	r.NoError(err)
	r.NoError(graph.Process(ctx))

	resolver, err := urlresolver.New(
		urlresolver.WithBaseURL(registryAddr),
		urlresolver.WithPlainHTTP(true),
		urlresolver.WithBaseClient(createAuthClient(registryAddr, user, password)),
	)
	r.NoError(err)
	targetRepo, err := oci.NewRepository(oci.WithResolver(resolver), oci.WithTempDir(t.TempDir()))
	r.NoError(err)

	got, err := targetRepo.GetComponentVersion(ctx, componentName, componentVersion)
	r.NoError(err)
	r.Len(got.Component.Resources, 1)
	r.Equal(ociaccessv1.LegacyType, got.Component.Resources[0].Access.GetType().Name)

	downloaded, err := targetRepo.DownloadResource(ctx, &got.Component.Resources[0])
	r.NoError(err)
	layout, err := ocitar.ReadOCILayout(ctx, downloaded)
	r.NoError(err)
	t.Cleanup(func() { r.NoError(layout.Close()) })
	artifacts := layout.MainArtifacts(ctx)
	r.Len(artifacts, 1)
	r.Equal(root.Digest, artifacts[0].Digest)
}

// Test_Integration_TransferDockerArtifactSetLocalBlob_CTFToCTF covers the other half
// of the customer scenario: transferring a docker artifact-set local blob as a local
// blob (no OCI uploader). The CTF->CTF path copies the blob verbatim, so the stored
// blob must remain a docker artifact set with the same main-artifact digest.
func Test_Integration_TransferDockerArtifactSetLocalBlob_CTFToCTF(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	ctx := t.Context()

	const (
		componentName    = "example.com/acme/component"
		componentVersion = "v1.0.5"
		resourceName     = "webhooks-server"
		referenceName    = "acme/components/example.com/acme/component/images/webhooks-server:v1.0.5"
		dockerMediaType  = "application/vnd.docker.distribution.manifest.list.v2+tar+gzip"
		dockerManifest   = "application/vnd.docker.distribution.manifest.v2+json"
		dockerList       = "application/vnd.docker.distribution.manifest.list.v2+json"
	)
	archive, root := legacyOCIIndexArtifactSet(t, dockerManifest, dockerList)

	// Confirm the fixture is a genuine legacy artifact set (flat blobs/<algo>.<encoded>,
	// no oci-layout), not just an OCI layout with a docker media type stamped on.
	set, err := ctf.NewArtifactSetFromBlob(inmemory.New(bytes.NewReader(archive), inmemory.WithMediaType(dockerMediaType)))
	r.NoError(err)
	r.Equal(ctf.ArtifactSetFormatLegacyOCI, set.Format())
	r.NoError(set.Close())

	sourcePath := t.TempDir()
	sourceRepo := createCTFRepository(t, sourcePath)
	resource, err := sourceRepo.AddLocalResource(ctx, componentName, componentVersion, &descriptor.Resource{
		ElementMeta: descriptor.ElementMeta{ObjectMeta: descriptor.ObjectMeta{Name: resourceName, Version: componentVersion}},
		Type:        "ociImage",
		Relation:    descriptor.LocalRelation,
		Access: &descriptorv2.LocalBlob{
			Type:          runtime.NewVersionedType(descriptorv2.LocalBlobAccessType, descriptorv2.LocalBlobAccessTypeVersion),
			MediaType:     dockerMediaType,
			ReferenceName: referenceName,
		},
	}, inmemory.New(bytes.NewReader(archive), inmemory.WithMediaType(dockerMediaType)))
	r.NoError(err)
	resource.Digest = &descriptor.Digest{
		HashAlgorithm:          "SHA-256",
		NormalisationAlgorithm: "ociArtifactDigest/v1",
		Value:                  root.Digest.Encoded(),
	}
	r.NoError(sourceRepo.AddComponentVersion(ctx, &descriptor.Descriptor{
		Meta: descriptor.Meta{Version: "v2"},
		Component: descriptor.Component{
			ComponentMeta: descriptor.ComponentMeta{ObjectMeta: descriptor.ObjectMeta{Name: componentName, Version: componentVersion}},
			Provider:      descriptor.Provider{Name: "acme"},
			Resources:     []descriptor.Resource{*resource},
		},
	}))

	sourceSpec := &ctfrepospec.Repository{
		Type:     runtime.NewVersionedType(ctfrepospec.Type, ctfrepospec.Version),
		FilePath: sourcePath,
	}
	targetPath := t.TempDir()
	targetSpec := &ctfrepospec.Repository{
		Type:       runtime.NewVersionedType(ctfrepospec.Type, ctfrepospec.Version),
		FilePath:   targetPath,
		AccessMode: "readwrite|create",
	}
	// No uploaders: the artifact-set local blob is embedded as a local blob in the target CTF.
	definition, err := transfer.BuildGraphDefinition(ctx, &transferv1alpha1.Config{}, nil, transfer.Mapping{
		Components: []transfer.ComponentID{{Component: componentName, Version: componentVersion}},
		Target:     targetSpec,
		Resolver:   transfer.NewRepositoryResolver(sourceRepo, sourceSpec),
	})
	r.NoError(err)

	var localBlobToLocalBlob bool
	for _, transformation := range definition.Transformations {
		if transformation.Type.Name == "TransferCTFLocalBlobToCTFLocalBlob" {
			localBlobToLocalBlob = true
			break
		}
	}
	r.True(localBlobToLocalBlob, "docker artifact-set local blob must be transferred as a local blob")

	builder := transfer.NewDefaultBuilder(
		provider.NewComponentVersionRepositoryProvider(provider.WithTempDir(t.TempDir())),
		ociresource.NewResourceRepository(nil),
		nil,
	)
	graph, err := builder.BuildAndCheck(definition)
	r.NoError(err)
	r.NoError(graph.Process(ctx))

	targetRepo := createCTFRepository(t, targetPath)
	got, err := targetRepo.GetComponentVersion(ctx, componentName, componentVersion)
	r.NoError(err)
	r.Len(got.Component.Resources, 1)

	var targetAccess descriptorv2.LocalBlob
	r.NoError(descriptorv2.Scheme.Convert(got.Component.Resources[0].Access, &targetAccess))
	r.Equal(dockerMediaType, targetAccess.MediaType, "docker artifact-set media type must be preserved")

	stored, _, err := targetRepo.GetLocalResource(ctx, componentName, componentVersion, got.Component.Resources[0].ToIdentity())
	r.NoError(err)
	layout, err := ocitar.ReadOCILayout(ctx, stored)
	r.NoError(err)
	t.Cleanup(func() { r.NoError(layout.Close()) })
	artifacts := layout.MainArtifacts(ctx)
	r.Len(artifacts, 1)
	r.Equal(root.Digest, artifacts[0].Digest, "main artifact digest must not change across the local-blob transfer")
}

func legacyOCIIndexArtifactSet(t *testing.T, manifestType, indexType string) ([]byte, ocispecv1.Descriptor) {
	t.Helper()
	r := require.New(t)

	payload := []byte("webhooks server payload")
	layer := content.NewDescriptorFromBytes(ocispecv1.MediaTypeImageLayer, payload)
	configData := []byte(`{}`)
	config := content.NewDescriptorFromBytes(ocispecv1.MediaTypeImageConfig, configData)
	manifestData, err := json.Marshal(ocispecv1.Manifest{
		Versioned: specs.Versioned{SchemaVersion: 2},
		MediaType: manifestType,
		Config:    config,
		Layers:    []ocispecv1.Descriptor{layer},
	})
	r.NoError(err)
	manifest := content.NewDescriptorFromBytes(manifestType, manifestData)

	indexData, err := json.Marshal(ocispecv1.Index{
		Versioned: specs.Versioned{SchemaVersion: 2},
		MediaType: indexType,
		Manifests: []ocispecv1.Descriptor{manifest},
	})
	r.NoError(err)
	root := content.NewDescriptorFromBytes(indexType, indexData)
	root.Annotations = map[string]string{ocispecv1.AnnotationRefName: "v1.0.5"}

	artifactSetIndex, err := json.Marshal(ocispecv1.Index{
		Versioned: specs.Versioned{SchemaVersion: 2},
		MediaType: ocispecv1.MediaTypeImageIndex,
		Manifests: []ocispecv1.Descriptor{root},
	})
	r.NoError(err)

	var archive bytes.Buffer
	compressed := gzip.NewWriter(&archive)
	writer := tar.NewWriter(compressed)
	r.NoError(writer.WriteHeader(&tar.Header{Name: "blobs/", Mode: 0o700, Typeflag: tar.TypeDir}))
	writeTarEntry := func(name string, data []byte) {
		t.Helper()
		r.NoError(writer.WriteHeader(&tar.Header{Name: name, Mode: 0o600, Size: int64(len(data))}))
		_, err := writer.Write(data)
		r.NoError(err)
	}
	writeTarEntry(ocispecv1.ImageIndexFile, artifactSetIndex)
	for _, entry := range []struct {
		descriptor ocispecv1.Descriptor
		data       []byte
	}{
		{layer, payload},
		{config, configData},
		{manifest, manifestData},
		{root, indexData},
	} {
		name, err := ctf.ToBlobFileName(entry.descriptor.Digest.String())
		r.NoError(err)
		writeTarEntry("blobs/"+name, entry.data)
	}
	r.NoError(writer.Close())
	r.NoError(compressed.Close())

	return archive.Bytes(), root
}
