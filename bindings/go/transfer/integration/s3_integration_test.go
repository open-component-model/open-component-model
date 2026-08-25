package integration_test

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"testing"

	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	awscreds "github.com/aws/aws-sdk-go-v2/credentials"
	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/minio"

	"ocm.software/open-component-model/bindings/go/credentials"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	descriptorv2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/oci"
	"ocm.software/open-component-model/bindings/go/oci/repository/provider"
	urlresolver "ocm.software/open-component-model/bindings/go/oci/resolver/url"
	credidentity "ocm.software/open-component-model/bindings/go/oci/spec/identity/v1"
	ctfrepospec "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/ctf"
	ocirepospec "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/oci"
	"ocm.software/open-component-model/bindings/go/runtime"
	s3repository "ocm.software/open-component-model/bindings/go/s3/repository"
	s3access "ocm.software/open-component-model/bindings/go/s3/spec/access"
	s3accessv1 "ocm.software/open-component-model/bindings/go/s3/spec/access/v1"
	s3identityv1 "ocm.software/open-component-model/bindings/go/s3/spec/identity/v1"
	s3v1alpha1 "ocm.software/open-component-model/bindings/go/s3/transformation/spec/v1alpha1"
	"ocm.software/open-component-model/bindings/go/transfer"
	transferv1alpha1 "ocm.software/open-component-model/bindings/go/transfer/v1alpha1/spec"
)

// Test_Integration_TransferS3Resource_CopyModeAllResources verifies that an s3 resource (an
// object in a bucket, not stored in the source CTF) is transferred by value: the object is
// downloaded from the bucket and embedded as a localBlob in the target OCI registry. s3
// resources are external, so they are only copied under CopyModeAllResources.
func Test_Integration_TransferS3Resource_CopyModeAllResources(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	ctx := t.Context()

	// 1. Serve the resource content from a MinIO bucket.
	container, err := minio.Run(ctx, "minio/minio")
	r.NoError(err)
	t.Cleanup(func() { r.NoError(testcontainers.TerminateContainer(container)) })
	hostPort, err := container.ConnectionString(ctx)
	r.NoError(err)
	endpoint := "http://" + hostPort

	const bucket, objectKey = "transfer-bucket", "path/to/artifact.txt"
	resourceData := []byte("Hello from s3 integration test!")
	putS3Object(t, ctx, endpoint, container.Username, container.Password, bucket, objectKey, resourceData)

	// 2. Start the target OCI registry.
	registryAddr, user, password := startRegistry(t)

	// 3. Create the source CTF with a component whose resource points at the bucket.
	componentName := "ocm.software/s3-resource-test"
	componentVersion := "1.0.0"
	sourceCTFPath := t.TempDir()
	ctfRepo := createCTFRepository(t, sourceCTFPath)

	// The s3 access lives in a separate binding the CTF/OCI repository scheme does not know,
	// so store it pre-encoded as raw JSON (how descriptors carry access on disk). The transfer
	// discovery decodes it via its own scheme, which registers the s3 access type.
	s3Access := &s3accessv1.S3Bucket{
		Type:         s3access.V1VersionedType,
		Region:       "us-east-1",
		BucketName:   bucket,
		ObjectKey:    objectKey,
		MediaType:    "text/plain",
		Endpoint:     endpoint,
		UsePathStyle: true,
	}
	rawS3Access := &runtime.Raw{}
	r.NoError(runtime.NewScheme(runtime.WithAllowUnknown()).Convert(s3Access, rawS3Access))

	desc := &descriptor.Descriptor{
		Meta: descriptor.Meta{Version: "v2"},
		Component: descriptor.Component{
			ComponentMeta: descriptor.ComponentMeta{
				ObjectMeta: descriptor.ObjectMeta{
					Name:    componentName,
					Version: componentVersion,
				},
			},
			Provider: descriptor.Provider{Name: "test-provider"},
			Resources: []descriptor.Resource{
				{
					ElementMeta: descriptor.ElementMeta{
						ObjectMeta: descriptor.ObjectMeta{Name: "s3-resource", Version: "1.0.0"},
					},
					Type:     "blob",
					Relation: descriptor.ExternalRelation,
					Access:   rawS3Access,
				},
			},
		},
	}
	r.NoError(ctfRepo.AddComponentVersion(ctx, desc))

	// 4. Build the transfer graph with CopyModeAllResources (external resources are skipped otherwise).
	sourceSpec := &ctfrepospec.Repository{
		Type:     runtime.Type{Name: ctfrepospec.Type, Version: ctfrepospec.Version},
		FilePath: sourceCTFPath,
	}
	targetSpec := &ocirepospec.Repository{
		Type:    runtime.Type{Name: ocirepospec.Type, Version: "v1"},
		BaseUrl: fmt.Sprintf("http://%s", registryAddr),
	}

	tgd, err := transfer.BuildGraphDefinition(ctx,
		&transferv1alpha1.Config{CopyMode: transferv1alpha1.CopyModeAllResources},
		transfer.Mapping{
			Components: []transfer.ComponentID{{Component: componentName, Version: componentVersion}},
			Target:     targetSpec,
			Resolver:   transfer.NewRepositoryResolver(ctfRepo, sourceSpec),
		},
	)
	r.NoError(err)
	r.NotNil(tgd)

	// An s3 resource should generate a DownloadS3Resource transformation.
	hasDownloadS3 := false
	for _, tr := range tgd.Transformations {
		if tr.Type.Name == s3v1alpha1.DownloadS3ResourceType {
			hasDownloadS3 = true
			break
		}
	}
	r.True(hasDownloadS3, "s3 resource should generate a DownloadS3Resource transformation")

	// 5. Build and execute the graph.
	credResolver := newS3CredResolver(t, endpoint, bucket, objectKey, container.Username, container.Password,
		registryCreds{registryAddr, user, password})
	repoProvider := provider.NewComponentVersionRepositoryProvider(provider.WithTempDir(t.TempDir()))
	// An s3 resource is downloaded via the s3 resource repository. In the CLI this is a dynamic
	// registry dispatching by access type; here the transfer only involves an s3 resource, so the
	// s3 repository is the concrete repo to provide (mirrors the OCI tests passing the OCI repo).
	resourceRepo := s3repository.NewResourceRepository(nil)
	b := transfer.NewDefaultBuilder(repoProvider, resourceRepo, credResolver)
	graph, err := b.BuildAndCheck(tgd)
	r.NoError(err)
	r.NoError(graph.Process(ctx))

	// 6. Verify the component arrived in the target registry with the resource stored as a localBlob.
	client := createAuthClient(registryAddr, user, password)
	urlRes, err := urlresolver.New(
		urlresolver.WithBaseURL(registryAddr),
		urlresolver.WithPlainHTTP(true),
		urlresolver.WithBaseClient(client),
	)
	r.NoError(err)
	targetRepo, err := oci.NewRepository(oci.WithResolver(urlRes), oci.WithTempDir(t.TempDir()))
	r.NoError(err)

	gotDesc, err := targetRepo.GetComponentVersion(ctx, componentName, componentVersion)
	r.NoError(err, "should find transferred component in target registry")
	r.Len(gotDesc.Component.Resources, 1)
	r.Equal("s3-resource", gotDesc.Component.Resources[0].Name)

	gotAccess := gotDesc.Component.Resources[0].Access
	r.NotNil(gotAccess, "resource access should not be nil")
	r.Equal(descriptorv2.LocalBlobAccessType, gotAccess.GetType().Name,
		"s3 resource should be stored as localBlob in target after transfer")

	accessScheme := runtime.NewScheme(runtime.WithAllowUnknown())
	descriptorv2.MustAddToScheme(accessScheme)
	var typedLocalBlob descriptorv2.LocalBlob
	r.NoError(accessScheme.Convert(gotAccess, &typedLocalBlob), "should convert access to LocalBlob")
	r.Nil(typedLocalBlob.GlobalAccess, "localBlob should not have globalAccess after transfer")
	r.Equal("text/plain", typedLocalBlob.MediaType, "localBlob should preserve the s3 media type")

	// The stored resource must be fully described: a digest over the downloaded content.
	r.NotNil(gotDesc.Component.Resources[0].Digest, "transferred resource should carry a digest")
	r.Equal(digestOf(resourceData).Encoded(), gotDesc.Component.Resources[0].Digest.Value,
		"resource digest should be the sha256 of the object content")

	// 7. Verify the blob is present and its content matches the object in the bucket.
	resourceIdentity := gotDesc.Component.Resources[0].ToIdentity()
	localBlob, _, err := targetRepo.GetLocalResource(ctx, componentName, componentVersion, resourceIdentity)
	r.NoError(err, "local blob should be retrievable from target repository")
	reader, err := localBlob.ReadCloser()
	r.NoError(err, "local blob should be readable")
	defer func() { r.NoError(reader.Close()) }()
	content, err := io.ReadAll(reader)
	r.NoError(err)
	r.Equal(resourceData, content, "transferred blob content should match the object content")
}

// putS3Object creates bucket in the store at endpoint and stores content under key.
func putS3Object(t *testing.T, ctx context.Context, endpoint, user, pass, bucket, key string, content []byte) {
	t.Helper()
	cfg, err := awsconfig.LoadDefaultConfig(ctx,
		awsconfig.WithRegion("us-east-1"),
		awsconfig.WithCredentialsProvider(awscreds.NewStaticCredentialsProvider(user, pass, "")),
	)
	require.NoError(t, err)
	client := awss3.NewFromConfig(cfg, func(o *awss3.Options) {
		o.BaseEndpoint = &endpoint
		o.UsePathStyle = true
	})
	_, err = client.CreateBucket(ctx, &awss3.CreateBucketInput{Bucket: &bucket})
	require.NoError(t, err)
	_, err = client.PutObject(ctx, &awss3.PutObjectInput{Bucket: &bucket, Key: &key, Body: bytes.NewReader(content)})
	require.NoError(t, err)
}

// newS3CredResolver resolves the bucket credentials for the s3 consumer identity in addition to
// the OCI registry credentials the transfer needs for its target.
func newS3CredResolver(t *testing.T, endpoint, bucket, key, accessKeyID, secretAccessKey string, registries ...registryCreds) *credentials.StaticCredentialsResolver {
	t.Helper()
	credMap := make(map[string]map[string]string)
	for _, reg := range registries {
		repo := &ocirepospec.Repository{
			Type:    runtime.Type{Name: ocirepospec.Type, Version: "v1"},
			BaseUrl: fmt.Sprintf("http://%s", reg.address),
		}
		identity, err := credidentity.IdentityFromOCIRepository(repo)
		require.NoError(t, err)
		credMap[identity.String()] = map[string]string{
			"username": reg.username,
			"password": reg.password,
		}
	}
	s3Identity, err := s3identityv1.IdentityFromObject(bucket, key, endpoint)
	require.NoError(t, err)
	credMap[s3Identity.String()] = map[string]string{
		"accessKeyId":     accessKeyID,
		"secretAccessKey": secretAccessKey,
	}
	return credentials.NewStaticCredentialsResolver(credMap)
}
