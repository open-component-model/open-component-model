package integration_test

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/minio"

	filesystemaccess "ocm.software/open-component-model/bindings/go/blob/filesystem/spec/access"
	filesystemv1alpha1 "ocm.software/open-component-model/bindings/go/configuration/filesystem/v1alpha1/spec"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/bindings/go/s3/repository"
	accessspec "ocm.software/open-component-model/bindings/go/s3/spec/access"
	accessv1 "ocm.software/open-component-model/bindings/go/s3/spec/access/v1"
	credv1 "ocm.software/open-component-model/bindings/go/s3/spec/credentials/v1"
	"ocm.software/open-component-model/bindings/go/s3/transformation"
	"ocm.software/open-component-model/bindings/go/s3/transformation/spec/v1alpha1"
)

// staticCredentials resolves the same S3 credentials for every identity.
type staticCredentials struct {
	accessKeyID     string
	secretAccessKey string
}

func (s staticCredentials) Resolve(_ context.Context, _ runtime.Identity) (runtime.Typed, error) {
	return &credv1.S3Credentials{
		Type:            credv1.S3CredentialsVersionedType,
		AccessKeyID:     s.accessKeyID,
		SecretAccessKey: s.secretAccessKey,
	}, nil
}

// Test_Integration_S3Transformation exercises the DownloadS3Resource transformer end to end
// against a MinIO container: the object is downloaded and buffered to a file the transformation
// output points at, which is what the subsequent AddLocalResource transformation consumes.
func Test_Integration_S3Transformation(t *testing.T) {
	r := require.New(t)
	ctx := context.Background()

	container, err := minio.Run(ctx, minioImage)
	r.NoError(err)
	t.Cleanup(func() { r.NoError(testcontainers.TerminateContainer(container)) })

	hostPort, err := container.ConnectionString(ctx)
	r.NoError(err)
	endpoint := "http://" + hostPort

	const bucket, key = "transformation-bucket", "path/to/blob.txt"
	content := []byte("hello ocm from the s3 transformer")
	setup := newSetupClient(t, ctx, endpoint, container.Username, container.Password)
	createBucket(t, ctx, setup, bucket)
	putObject(t, ctx, setup, bucket, key, content)

	scheme := runtime.NewScheme()
	v2.MustAddToScheme(scheme)
	filesystemaccess.MustAddToScheme(scheme)
	accessspec.MustAddToScheme(scheme)
	scheme.MustRegisterWithAlias(&v1alpha1.DownloadS3Resource{}, v1alpha1.DownloadS3ResourceV1alpha1)

	rawAccess := &runtime.Raw{}
	r.NoError(accessspec.Scheme.Convert(&accessv1.S3Bucket{
		Type:         accessspec.V1VersionedType,
		Region:       "us-east-1",
		BucketName:   bucket,
		ObjectKey:    key,
		Endpoint:     endpoint,
		UsePathStyle: true,
	}, rawAccess))

	transform := &transformation.DownloadS3Resource{
		Scheme:             scheme,
		ResourceRepository: repository.NewResourceRepository(&filesystemv1alpha1.Config{TempFolder: t.TempDir()}),
		CredentialProvider: staticCredentials{container.Username, container.Password},
	}

	outputDir := t.TempDir()
	result, err := transform.Transform(ctx, &v1alpha1.DownloadS3Resource{
		Type: v1alpha1.DownloadS3ResourceV1alpha1,
		ID:   "get-s3-resource",
		Spec: &v1alpha1.DownloadS3ResourceSpec{
			Resource: &v2.Resource{
				ElementMeta: v2.ElementMeta{
					ObjectMeta: v2.ObjectMeta{Name: "blob", Version: "1.0.0"},
				},
				Type:   "blob",
				Access: rawAccess,
			},
			OutputPath: outputDir,
		},
	})
	r.NoError(err)

	out, ok := result.(*v1alpha1.DownloadS3Resource)
	r.True(ok)
	r.NotNil(out.Output)
	r.NotNil(out.Output.Resource)
	r.Equal("blob", out.Output.Resource.Name)

	filePath := strings.TrimPrefix(out.Output.File.URI, "file://")
	r.True(strings.HasPrefix(filePath, outputDir), "content should be buffered into the requested output directory")
	got, err := os.ReadFile(filePath)
	r.NoError(err)
	r.Equal(content, got)
}
