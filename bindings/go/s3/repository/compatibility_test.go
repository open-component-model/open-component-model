package repository

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	godigest "github.com/opencontainers/go-digest"
	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/blob"
	filesystemv1alpha1 "ocm.software/open-component-model/bindings/go/configuration/filesystem/v1alpha1/spec"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/runtime"
	accessspec "ocm.software/open-component-model/bindings/go/s3/spec/access"
	accessv1 "ocm.software/open-component-model/bindings/go/s3/spec/access/v1"
	credv1 "ocm.software/open-component-model/bindings/go/s3/spec/credentials/v1"
)

func TestRepository_LegacyAccess(t *testing.T) {
	for _, typ := range []string{"S3/v1", "s3/v1", "S3", "s3"} {
		for _, representation := range []string{"raw", "unstructured", "typed"} {
			t.Run(typ+"/"+representation, func(t *testing.T) {
				r := require.New(t)
				content := []byte("legacy object")
				srv := newFakeS3(t, content, "resolved-version")
				tempFolder := t.TempDir()
				repo := NewResourceRepository(&filesystemv1alpha1.Config{TempFolder: &tempFolder})
				data, err := json.Marshal(map[string]any{
					"type": typ, "bucket": "bucket", "key": "path/object",
					"region": "us-east-1", "mediaType": "application/custom",
					"endpoint": srv.URL, "usePathStyle": true,
				})
				r.NoError(err)
				raw := &runtime.Raw{}
				r.NoError(json.Unmarshal(data, raw))
				var access runtime.Typed = raw
				switch representation {
				case "unstructured":
					access = &runtime.Unstructured{}
					r.NoError(accessspec.Scheme.Convert(raw, access))
				case "typed":
					access, err = accessspec.Scheme.NewObject(raw.GetType())
					r.NoError(err)
					r.NoError(accessspec.Scheme.Convert(raw, access))
				}
				original, err := json.Marshal(access)
				r.NoError(err)
				resource := &descriptor.Resource{}
				resource.Access = access

				identity, err := repo.GetResourceCredentialConsumerIdentity(t.Context(), resource)
				r.NoError(err)
				r.Equal("bucket/path/object", identity[runtime.IdentityAttributePath])

				b, err := repo.DownloadResource(t.Context(), resource, fakeCredentials())
				r.NoError(err)
				mediaType, known := b.(blob.MediaTypeAware).MediaType()
				r.True(known)
				r.Equal("application/custom", mediaType)
				reader, err := b.ReadCloser()
				r.NoError(err)
				body, err := io.ReadAll(reader)
				r.NoError(err)
				r.NoError(reader.Close())
				r.Equal(content, body)

				pinned, err := repo.ProcessResourceDigest(t.Context(), resource, fakeCredentials())
				r.NoError(err)
				r.Equal(godigest.FromBytes(content).Encoded(), pinned.Digest.Value)
				r.Equal(accessspec.V2VersionedType, pinned.Access.GetType())
				encoded, err := json.Marshal(pinned.Access)
				r.NoError(err)
				var fields map[string]any
				r.NoError(json.Unmarshal(encoded, &fields))
				r.Equal("S3/v2", fields["type"])
				r.Equal("bucket", fields["bucketName"])
				r.Equal("path/object", fields["objectKey"])
				r.Equal("resolved-version", fields["version"])
				r.NotContains(fields, "bucket")
				r.NotContains(fields, "key")
				unchanged, err := json.Marshal(resource.Access)
				r.NoError(err)
				r.JSONEq(string(original), string(unchanged))

				_, err = repo.DownloadResource(t.Context(), pinned, fakeCredentials())
				r.NoError(err)
				requests := srv.recorded()
				r.Len(requests, 3)
				r.Equal("/bucket/path/object", requests[0].path)
				r.Empty(requests[0].versionID)
				r.Equal("resolved-version", requests[2].versionID)
			})
		}
	}
}

func TestRepository_LegacyAnonymousAccess(t *testing.T) {
	r := require.New(t)
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		if strings.HasPrefix(key, "AWS_") {
			t.Setenv(key, "")
		}
	}
	t.Setenv("AWS_EC2_METADATA_DISABLED", "true")
	t.Setenv("AWS_CONFIG_FILE", filepath.Join(t.TempDir(), "missing"))
	t.Setenv("AWS_SHARED_CREDENTIALS_FILE", filepath.Join(t.TempDir(), "missing"))

	srv := newFakeS3(t, []byte("public object"), "public-version")
	tempFolder := t.TempDir()
	repo := NewResourceRepository(&filesystemv1alpha1.Config{TempFolder: &tempFolder})
	resource := &descriptor.Resource{}
	resource.Access = &accessv1.S3{
		Bucket: "public", Key: "object", Endpoint: srv.URL, UsePathStyle: true,
	}
	creds := &credv1.S3Credentials{Type: credv1.S3CredentialsVersionedType, Anonymous: true}
	pinned, err := repo.ProcessResourceDigest(t.Context(), resource, creds)
	r.NoError(err)
	r.Equal(godigest.FromString("public object").Encoded(), pinned.Digest.Value)
	r.Equal(accessspec.V2VersionedType, pinned.Access.GetType())
	_, err = repo.DownloadResource(t.Context(), pinned, creds)
	r.NoError(err)
}
