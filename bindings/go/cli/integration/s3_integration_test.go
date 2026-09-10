package integration

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	awscreds "github.com/aws/aws-sdk-go-v2/credentials"
	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"
	godigest "github.com/opencontainers/go-digest"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/minio"

	"ocm.software/open-component-model/bindings/go/blob/filesystem"
	"ocm.software/open-component-model/bindings/go/cli/cmd"
	"ocm.software/open-component-model/bindings/go/cli/integration/internal"
)

// Test_Integration_S3 verifies the built-in S3 plugin end to end against MinIO: an
// S3/v2 input is stored as a local blob, an S3/v2 access is recorded as-is with its
// digest computed from the object, and both resources download to the object's
// content. The bucket is reached with typed S3Credentials/v1 from the ocm config.
func Test_Integration_S3(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	ctx := t.Context()

	container, err := minio.Run(ctx, "minio/minio")
	r.NoError(err)
	t.Cleanup(func() { r.NoError(testcontainers.TerminateContainer(container)) })
	hostPort, err := container.ConnectionString(ctx)
	r.NoError(err)
	host, port, err := net.SplitHostPort(hostPort)
	r.NoError(err)
	endpoint := "http://" + hostPort

	const bucket, objectKey = "cli-bucket", "path/to/artifact.txt"
	content := []byte("hello from s3")
	putS3Object(t, ctx, endpoint, container.Username, container.Password, bucket, objectKey, content)

	registry, err := internal.CreateOCIRegistry(t)
	r.NoError(err)

	cfg := fmt.Sprintf(`
type: generic.config.ocm.software/v1
configurations:
- type: credentials.config.ocm.software
  consumers:
  - identity:
      type: OCIRegistry
      hostname: %[1]q
      port: %[2]q
      scheme: http
    credentials:
    - type: Credentials/v1
      properties:
        username: %[3]q
        password: %[4]q
  - identity:
      type: S3
      hostname: %[5]q
      port: %[6]q
      scheme: http
    credentials:
    - type: S3Credentials/v1
      accessKeyId: %[7]q
      secretAccessKey: %[8]q
`, registry.Host, registry.Port, registry.User, registry.Password, host, port, container.Username, container.Password)
	cfgPath := filepath.Join(t.TempDir(), "ocmconfig.yaml")
	r.NoError(os.WriteFile(cfgPath, []byte(cfg), os.ModePerm))

	const componentName = "ocm.software/s3-component"
	const componentVersion = "v1.0.0"

	constructorContent := fmt.Sprintf(`
components:
- name: %[1]s
  version: %[2]s
  provider:
    name: ocm.software
  resources:
  - name: s3-input
    version: v1.0.0
    type: blob
    input:
      type: S3/v2
      endpoint: %[3]s
      usePathStyle: true
      region: us-east-1
      bucketName: %[4]s
      objectKey: %[5]s
      mediaType: text/plain
  - name: s3-access
    version: v1.0.0
    type: blob
    relation: external
    access:
      type: S3/v2
      endpoint: %[3]s
      usePathStyle: true
      region: us-east-1
      bucketName: %[4]s
      objectKey: %[5]s
      mediaType: text/plain
`, componentName, componentVersion, endpoint, bucket, objectKey)
	constructorPath := filepath.Join(t.TempDir(), "constructor.yaml")
	r.NoError(os.WriteFile(constructorPath, []byte(constructorContent), os.ModePerm))

	addCMD := cmd.New()
	addCMD.SetArgs([]string{
		"add",
		"component-version",
		"--repository", fmt.Sprintf("http://%s", registry.RegistryAddress),
		"--constructor", constructorPath,
		"--config", cfgPath,
	})
	addCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	r.NoError(addCMD.ExecuteContext(addCtx), "add cv with s3 input and access should succeed")

	repo := registry.Connect(t)
	desc, err := repo.GetComponentVersion(ctx, componentName, componentVersion)
	r.NoError(err)
	r.Len(desc.Component.Resources, 2)

	// The input took the object in, so no s3 access is left on the resource.
	input := desc.Component.Resources[0]
	r.Equal("s3-input", input.Name)
	r.NotEqual("S3/v2", input.Access.GetType().String(), "an s3 input must be stored as a local blob")

	// The access is recorded as-is and the s3 digest processor hashed the object.
	access := desc.Component.Resources[1]
	r.Equal("s3-access", access.Name)
	r.Equal("S3/v2", access.Access.GetType().String())
	r.NotNil(access.Digest, "s3 digest processor should have set a digest")
	r.Equal("SHA-256", access.Digest.HashAlgorithm)
	r.Equal("genericBlobDigest/v1", access.Digest.NormalisationAlgorithm)
	r.Equal(godigest.FromBytes(content).Encoded(), access.Digest.Value)

	for _, name := range []string{"s3-input", "s3-access"} {
		output := filepath.Join(t.TempDir(), name)
		downloadCMD := cmd.New()
		downloadCMD.SetArgs([]string{
			"download",
			"resource",
			fmt.Sprintf("http://%s//%s:%s", registry.RegistryAddress, componentName, componentVersion),
			"--identity", fmt.Sprintf("name=%s,version=v1.0.0", name),
			"--output", output,
			"--config", cfgPath,
		})
		r.NoError(downloadCMD.ExecuteContext(ctx), "download resource %s should succeed", name)

		outputBlob, err := filesystem.GetBlobFromOSPath(output)
		r.NoError(err)
		rc, err := outputBlob.ReadCloser()
		r.NoError(err)
		data, err := io.ReadAll(rc)
		r.NoError(rc.Close())
		r.NoError(err)
		r.Equal(content, data, "downloaded %s should match the object in the bucket", name)
	}
}

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
