package integration_test

import (
	"bytes"
	"context"
	"encoding/pem"
	"fmt"
	"net/http"
	"net/http/cgi"
	"net/http/httptest"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/filemode"
	"github.com/go-git/go-git/v6/plumbing/object"
	godigest "github.com/opencontainers/go-digest"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/registry"
	"github.com/testcontainers/testcontainers-go/wait"
	"oras.land/oras-go/v2"
	"oras.land/oras-go/v2/content"
	"oras.land/oras-go/v2/registry/remote"
)

const (
	// Images as in bindings/go/oci/integration and bindings/go/s3/integration.
	distributionRegistryImage = "registry:3.0.0"
	rustfsImage               = "rustfs/rustfs:1.0.0-rc.6"
	rustfsAccessKey           = "ocm-test"
	rustfsSecretKey           = "ocm-test-secret"
	s3Region                  = "us-east-1"
)

// startRegistry starts an OCI distribution registry, reachable over plain HTTP, and returns
// its host:port.
func startRegistry(t *testing.T, ctx context.Context) string {
	t.Helper()
	r := require.New(t)

	container, err := registry.Run(ctx, distributionRegistryImage)
	r.NoError(err)
	t.Cleanup(func() { r.NoError(testcontainers.TerminateContainer(container)) })

	address, err := container.HostAddress(ctx)
	r.NoError(err)
	return address
}

// pushArtifact pushes a single-layer OCI artifact to repository:tag in the registry at
// address and returns its manifest digest.
func pushArtifact(t *testing.T, ctx context.Context, address, repository, tag string, layer []byte) godigest.Digest {
	t.Helper()
	r := require.New(t)

	repo, err := remote.NewRepository(address + "/" + repository)
	r.NoError(err)
	repo.PlainHTTP = true

	layerDesc := content.NewDescriptorFromBytes(ocispec.MediaTypeImageLayer, layer)
	r.NoError(repo.Push(ctx, layerDesc, bytes.NewReader(layer)))
	manifest, err := oras.PackManifest(ctx, repo, oras.PackManifestVersion1_1, "application/vnd.ocm.software.test", oras.PackManifestOptions{
		Layers: []ocispec.Descriptor{layerDesc},
	})
	r.NoError(err)
	r.NoError(repo.Tag(ctx, manifest, tag))
	return manifest.Digest
}

// startRustFS starts a RustFS S3 server and returns its endpoint URL.
func startRustFS(t *testing.T, ctx context.Context) string {
	t.Helper()
	r := require.New(t)

	container, err := testcontainers.Run(ctx, rustfsImage,
		testcontainers.WithExposedPorts("9000/tcp"),
		testcontainers.WithEnv(map[string]string{"RUSTFS_ACCESS_KEY": rustfsAccessKey, "RUSTFS_SECRET_KEY": rustfsSecretKey}),
		testcontainers.WithWaitStrategy(wait.ForHTTP("/health/ready").WithPort("9000/tcp")),
	)
	r.NoError(err)
	t.Cleanup(func() { r.NoError(testcontainers.TerminateContainer(container)) })

	hostPort, err := container.PortEndpoint(ctx, "9000/tcp", "")
	r.NoError(err)
	return "http://" + hostPort
}

// putS3Object creates bucket in the S3 server at endpoint and uploads content as key.
func putS3Object(t *testing.T, ctx context.Context, endpoint, bucket, key string, data []byte) {
	t.Helper()
	r := require.New(t)

	cfg, err := config.LoadDefaultConfig(ctx,
		config.WithRegion(s3Region),
		config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(rustfsAccessKey, rustfsSecretKey, "")),
	)
	r.NoError(err)
	c := s3.NewFromConfig(cfg, func(o *s3.Options) {
		o.BaseEndpoint = aws.String(endpoint)
		o.UsePathStyle = true
	})
	_, err = c.CreateBucket(ctx, &s3.CreateBucketInput{Bucket: aws.String(bucket)})
	r.NoError(err)
	_, err = c.PutObject(ctx, &s3.PutObjectInput{Bucket: aws.String(bucket), Key: aws.String(key), Body: bytes.NewReader(data)})
	r.NoError(err)
}

// startFileServer starts an HTTPS server serving files by path and returns its URL.
func startFileServer(t *testing.T, files map[string][]byte) *httptest.Server {
	t.Helper()
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		data, ok := files[req.URL.Path]
		if !ok {
			http.NotFound(w, req)
			return
		}
		_, _ = w.Write(data)
	}))
	t.Cleanup(server.Close)
	return server
}

// helmIndex is a Helm repository index listing mychart 0.1.0 at chartURL, with digest if
// it is set.
func helmIndex(chartURL string, digest godigest.Digest) []byte {
	index := fmt.Sprintf(`apiVersion: v1
entries:
  mychart:
  - apiVersion: v2
    name: mychart
    version: 0.1.0
    urls:
    - %s
`, chartURL)
	if digest != "" {
		index += fmt.Sprintf("    digest: %s\n", digest.Encoded())
	}
	return []byte(index)
}

// newGitRepository creates a bare repository with one commit of README.md on main and
// returns its path and the commit. The objects are written directly, so no Git
// configuration of the host (for example commit signing) applies.
func newGitRepository(t *testing.T, readme string) (string, plumbing.Hash) {
	t.Helper()
	r := require.New(t)

	path := filepath.Join(t.TempDir(), "fixture.git")
	repo, err := git.PlainInit(path, true)
	r.NoError(err)

	write := func(typ plumbing.ObjectType, encode func(obj plumbing.EncodedObject) error) plumbing.Hash {
		obj := repo.Storer.NewEncodedObject()
		obj.SetType(typ)
		r.NoError(encode(obj))
		hash, err := repo.Storer.SetEncodedObject(obj)
		r.NoError(err)
		return hash
	}

	blobHash := write(plumbing.BlobObject, func(obj plumbing.EncodedObject) error {
		w, err := obj.Writer()
		if err != nil {
			return err
		}
		if _, err := w.Write([]byte(readme)); err != nil {
			return err
		}
		return w.Close()
	})
	treeHash := write(plumbing.TreeObject, (&object.Tree{Entries: []object.TreeEntry{
		{Name: "README.md", Mode: filemode.Regular, Hash: blobHash},
	}}).Encode)
	signature := object.Signature{Name: "OCM fixture", Email: "fixture@example.invalid", When: time.Unix(1700000000, 0).UTC()}
	commitHash := write(plumbing.CommitObject, (&object.Commit{
		Author:    signature,
		Committer: signature,
		Message:   "fixture\n",
		TreeHash:  treeHash,
	}).Encode)

	main := plumbing.NewBranchReferenceName("main")
	r.NoError(repo.Storer.SetReference(plumbing.NewHashReference(main, commitHash)))
	r.NoError(repo.Storer.SetReference(plumbing.NewSymbolicReference(plumbing.HEAD, main)))

	return path, commitHash
}

// startGitServer serves the bare repository at path over HTTPS with git http-backend and
// returns its URL. Without a git binary it returns no server.
func startGitServer(t *testing.T, path string) (url string, server *httptest.Server) {
	t.Helper()

	executable, err := exec.LookPath("git")
	if err != nil {
		return "", nil
	}
	backend := &cgi.Handler{
		Path: executable,
		Args: []string{"http-backend"},
		Env:  []string{"GIT_PROJECT_ROOT=" + filepath.Dir(path), "GIT_HTTP_EXPORT_ALL=1"},
	}
	server = httptest.NewTLSServer(backend)
	t.Cleanup(server.Close)
	return server.URL + "/" + filepath.Base(path), server
}

// certificatePEM returns the PEM of the certificate server presents.
func certificatePEM(server *httptest.Server) string {
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw}))
}
