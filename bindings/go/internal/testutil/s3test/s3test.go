// Package s3test starts an S3-compatible server for integration tests.
//
// [Start] needs Docker, runs RustFS, terminates it when the test ends, and returns its
// host:port. Callers add the scheme they need. The server accepts [AccessKey] and
// [SecretKey]:
//
//	endpoint := "http://" + s3test.Start(t)
//	creds := &credv1.S3Credentials{
//		Type:            credv1.S3CredentialsVersionedType,
//		AccessKeyID:     s3test.AccessKey,
//		SecretAccessKey: s3test.SecretKey,
//	}
//
// [StartFake] needs no Docker: it serves one canned [Object] over httptest for unit tests
// and records the requests it answered:
//
//	fake := s3test.StartFake(t, s3test.Object{Body: []byte("content"), VersionID: "v-1"})
//	// point the client at fake.URL with path-style addressing, then
//	requests := fake.Recorded()
package s3test

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

const (
	// Image is the S3-compatible server [Start] runs.
	Image = "rustfs/rustfs:1.0.0-rc.6"
	// AccessKey is the access key [Start] configures.
	AccessKey = "ocm-test"
	// SecretKey is the secret key [Start] configures.
	SecretKey = "ocm-test-secret"

	port = "9000/tcp"
)

// Start starts a RustFS server with [AccessKey] and [SecretKey] and returns its host:port.
func Start(t *testing.T) string {
	t.Helper()
	r := require.New(t)
	ctx := t.Context()

	container, err := testcontainers.Run(ctx, Image,
		testcontainers.WithExposedPorts(port),
		testcontainers.WithEnv(map[string]string{"RUSTFS_ACCESS_KEY": AccessKey, "RUSTFS_SECRET_KEY": SecretKey}),
		testcontainers.WithWaitStrategy(wait.ForHTTP("/health/ready").WithPort(port)),
	)
	testcontainers.CleanupContainer(t, container)
	r.NoError(err)

	hostPort, err := container.PortEndpoint(ctx, port, "")
	r.NoError(err)
	return hostPort
}
