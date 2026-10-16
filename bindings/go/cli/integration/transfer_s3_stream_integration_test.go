package integration

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	awscreds "github.com/aws/aws-sdk-go-v2/credentials"
	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"

	"ocm.software/open-component-model/bindings/go/blob/filesystem"
	"ocm.software/open-component-model/bindings/go/cli/cmd"
	"ocm.software/open-component-model/bindings/go/cli/integration/internal"
	"ocm.software/open-component-model/bindings/go/ctf"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	descriptorv2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/oci"
	ocictf "ocm.software/open-component-model/bindings/go/oci/ctf"
	"ocm.software/open-component-model/bindings/go/repository"
	"ocm.software/open-component-model/bindings/go/runtime"
	s3access "ocm.software/open-component-model/bindings/go/s3/spec/access"
	s3accessv2 "ocm.software/open-component-model/bindings/go/s3/spec/access/v2"
	s3identityv1 "ocm.software/open-component-model/bindings/go/s3/spec/identity/v1"
)

// Test_Integration_TransferS3Resource_Stream_CLI is the end-to-end streaming proof through
// the ocm CLI: an external s3 resource is transferred by value into an OCI registry. The
// local blob uploader turns it into a localBlob, and the fused OCIStreamLocalResource node
// streams the object straight from the bucket into the target with the chunked push, without
// a temporary file. The test asserts both the "source=streamed" signal the transformer logs
// and that the content arrives intact.
func Test_Integration_TransferS3Resource_Stream_CLI(t *testing.T) {
	ctx := t.Context()
	r := require.New(t)

	// 1. Serve the resource content from a RustFS bucket.
	const s3AccessKey, s3SecretKey = "ocm-test", "ocm-test-secret"
	container, err := testcontainers.Run(ctx, "rustfs/rustfs:1.0.0-rc.6",
		testcontainers.WithExposedPorts("9000/tcp"),
		testcontainers.WithEnv(map[string]string{"RUSTFS_ACCESS_KEY": s3AccessKey, "RUSTFS_SECRET_KEY": s3SecretKey}),
		testcontainers.WithWaitStrategy(wait.ForHTTP("/health/ready").WithPort("9000/tcp")),
	)
	r.NoError(err)
	t.Cleanup(func() { r.NoError(testcontainers.TerminateContainer(container)) })
	hostPort, err := container.PortEndpoint(ctx, "9000/tcp", "")
	r.NoError(err)
	endpoint := "http://" + hostPort

	const bucket, objectKey = "transfer-bucket", "path/to/artifact.txt"
	resourceData := []byte("Hello streaming from s3 through the ocm CLI!")
	putS3ObjectCLI(t, ctx, endpoint, s3AccessKey, s3SecretKey, bucket, objectKey, resourceData)

	// 2. Start the target OCI registry.
	targetRegistry, err := internal.CreateOCIRegistry(t)
	r.NoError(err, "should start the target registry container")

	// 3. Build a source CTF whose single resource points at the bucket (stored by reference,
	// as a descriptor carries an s3 access on disk: raw JSON the CTF scheme does not decode).
	componentName := "ocm.software/s3-stream-cli-test"
	componentVersion := "1.0.0"
	sourceCTFPath := filepath.Join(t.TempDir(), "source-ctf")

	s3Access := &s3accessv2.S3{
		Type:         s3access.V2VersionedType,
		Region:       "us-east-1",
		BucketName:   bucket,
		ObjectKey:    objectKey,
		MediaType:    "text/plain",
		Endpoint:     endpoint,
		UsePathStyle: true,
	}
	rawS3Access := &runtime.Raw{}
	r.NoError(runtime.NewScheme(runtime.WithAllowUnknown()).Convert(s3Access, rawS3Access))

	ctfRepo := createCTFRepositoryForStream(t, sourceCTFPath)
	r.NoError(ctfRepo.AddComponentVersion(ctx, &descriptor.Descriptor{
		Meta: descriptor.Meta{Version: "v2"},
		Component: descriptor.Component{
			ComponentMeta: descriptor.ComponentMeta{
				ObjectMeta: descriptor.ObjectMeta{Name: componentName, Version: componentVersion},
			},
			Provider: descriptor.Provider{Name: "test-provider"},
			Resources: []descriptor.Resource{{
				ElementMeta: descriptor.ElementMeta{
					ObjectMeta: descriptor.ObjectMeta{Name: "s3-resource", Version: "1.0.0"},
				},
				Type:     "blob",
				Relation: descriptor.ExternalRelation,
				Access:   rawS3Access,
			}},
		},
	}))

	// 4. Write the OCM config: s3 bucket credentials, target registry credentials, and the
	// local blob uploader (the by-value trigger for an external resource; no CLI flag).
	cfgPath := writeS3StreamOCMConfig(t, endpoint, bucket, objectKey, s3AccessKey, s3SecretKey, targetRegistry)

	// 5. Run the real CLI transfer, capturing the logger output so the streaming signal is observable.
	sourceRef := fmt.Sprintf("ctf::%s//%s:%s", sourceCTFPath, componentName, componentVersion)
	targetRef := fmt.Sprintf("http://%s", targetRegistry.RegistryAddress)

	var logBuf bytes.Buffer
	transferCMD := cmd.New()
	transferCMD.SetErr(&logBuf)
	transferCMD.SetArgs([]string{
		"transfer", "component-version",
		sourceRef, targetRef,
		"--config", cfgPath,
		"--loglevel", "info",
		"--logformat", "json",
		"--logoutput", "stderr",
	})

	transferCtx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	r.NoError(transferCMD.ExecuteContext(transferCtx), "CLI transfer should succeed")

	logs := logBuf.String()
	t.Logf("transfer logs:\n%s", logs)

	// 6. The streaming signal: the OCIStreamLocalResource node logged source=streamed, which
	// proves the object was streamed (not downloaded to a temporary file first).
	r.Contains(logs, "adding local resource to target", "the stream-local-resource node must run")
	r.Contains(logs, `"source":"streamed"`, "the s3 resource must be streamed, not materialized")
	r.NotContains(logs, `"source":"downloaded"`, "an s3 resource must not fall back to the materialized download")

	// 7. The content arrived: the resource is a localBlob in the target with the right bytes.
	repo := targetRegistry.Connect(t)
	gotDesc, err := repo.GetComponentVersion(ctx, componentName, componentVersion)
	r.NoError(err, "transferred component must be present in the target registry")
	r.Len(gotDesc.Component.Resources, 1)
	gotAccess := gotDesc.Component.Resources[0].Access
	r.NotNil(gotAccess)
	r.Equal(descriptorv2.LocalBlobAccessType, gotAccess.GetType().Name,
		"the streamed s3 resource must be stored as a localBlob after transfer")
	// The source resource carries no digest, so the transfer must not record one (it
	// would change the signed descriptor). The content check below covers integrity.
	r.Nil(gotDesc.Component.Resources[0].Digest, "transfer must not inject a digest the source lacked")

	localBlob, _, err := repo.GetLocalResource(ctx, componentName, componentVersion,
		gotDesc.Component.Resources[0].ToIdentity())
	r.NoError(err, "the local blob must be retrievable from the target")
	reader, err := localBlob.ReadCloser()
	r.NoError(err)
	defer func() { r.NoError(reader.Close()) }()
	content, err := io.ReadAll(reader)
	r.NoError(err)
	r.Equal(resourceData, content, "the transferred blob content must match the object in the bucket")
}

// createCTFRepositoryForStream creates a CTF-backed OCI repository at path.
func createCTFRepositoryForStream(t *testing.T, path string) repository.ComponentVersionRepository {
	t.Helper()
	fs, err := filesystem.NewFS(path, os.O_RDWR|os.O_CREATE)
	require.NoError(t, err)
	store := ocictf.NewFromCTF(ctf.NewFileSystemCTF(fs))
	repo, err := oci.NewRepository(oci.WithResolver(store), oci.WithTempDir(t.TempDir()))
	require.NoError(t, err)
	return repo
}

// putS3ObjectCLI creates bucket in the store at endpoint and stores content under key.
func putS3ObjectCLI(t *testing.T, ctx context.Context, endpoint, user, pass, bucket, key string, content []byte) {
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

// writeS3StreamOCMConfig composes the central OCM config: the s3 bucket consumer credentials
// (keyed on the exact consumer identity the s3 repository derives from the access), the target
// registry credentials, and a localblob uploader entry that drives the by-value transfer.
func writeS3StreamOCMConfig(t *testing.T, endpoint, bucket, key, accessKeyID, secretAccessKey string, reg *internal.OCIRegistry) string {
	t.Helper()

	s3Identity, err := s3identityv1.IdentityFromObject(bucket, key, endpoint)
	require.NoError(t, err)

	cfg := strings.Join([]string{
		"type: generic.config.ocm.software/v1",
		"configurations:",
		"- type: credentials.config.ocm.software",
		"  consumers:",
		"  - identity:",
		fmt.Sprintf("      type: %q", s3Identity[runtime.IdentityAttributeType]),
		fmt.Sprintf("      hostname: %q", s3Identity[runtime.IdentityAttributeHostname]),
		fmt.Sprintf("      scheme: %q", s3Identity[runtime.IdentityAttributeScheme]),
		fmt.Sprintf("      port: %q", s3Identity[runtime.IdentityAttributePort]),
		fmt.Sprintf("      path: %q", s3Identity[runtime.IdentityAttributePath]),
		"    credentials:",
		"    - type: Credentials/v1",
		"      properties:",
		fmt.Sprintf("        accessKeyId: %q", accessKeyID),
		fmt.Sprintf("        secretAccessKey: %q", secretAccessKey),
		"  - identity:",
		"      type: OCIRegistry",
		fmt.Sprintf("      hostname: %q", reg.Host),
		fmt.Sprintf("      port: %q", reg.Port),
		"      scheme: http",
		"    credentials:",
		"    - type: Credentials/v1",
		"      properties:",
		fmt.Sprintf("        username: %q", reg.User),
		fmt.Sprintf("        password: %q", reg.Password),
		"- type: localblob.uploader.transfer.config.ocm.software/v1alpha1",
		"",
	}, "\n")

	cfgPath := filepath.Join(t.TempDir(), "ocmconfig.yaml")
	require.NoError(t, os.WriteFile(cfgPath, []byte(cfg), 0o600))
	t.Logf("Generated config:\n%s", cfg)
	return cfgPath
}
