package internal

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

const (
	rekorV2Image = "ghcr.io/sigstore/rekor-tiles/posix:v2.2.1"
	rekorPort    = "3000"
	rekorAlias   = "rekor-server"
)

// RekorInstance holds the running Rekor v2 POSIX container details.
type RekorInstance struct {
	HostURL      string
	ContainerURL string
	PublicKeyPEM []byte
}

// StartRekor starts a Rekor v2 POSIX transparency log container.
// Rekor v2 uses a local filesystem backend — no MySQL or Trillian required.
func StartRekor(ctx context.Context, network string, stack *SigstoreStack) (*RekorInstance, error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generate ed25519 key: %w", err)
	}

	privDER, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		return nil, fmt.Errorf("marshal private key: %w", err)
	}
	privPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privDER})

	pubDER, err := x509.MarshalPKIXPublicKey(pub)
	if err != nil {
		return nil, fmt.Errorf("marshal public key: %w", err)
	}
	pubPEM := pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: pubDER})

	keyPath := filepath.Join(stack.tmpDir, "rekor-ed25519.pem")
	if err := os.WriteFile(keyPath, privPEM, 0o600); err != nil {
		return nil, fmt.Errorf("write rekor key: %w", err)
	}

	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        rekorV2Image,
			ExposedPorts: []string{rekorPort + "/tcp"},
			Cmd: []string{
				"rekor-server", "serve",
				"--http-address=0.0.0.0",
				"--storage-dir=/tmp/posixlog",
				"--signer-filepath=/pki/rekor-key.pem",
				"--hostname=rekor.integration-test",
				"--checkpoint-interval=2s",
				"--persistent-antispam=true",
			},
			Files: []testcontainers.ContainerFile{
				{HostFilePath: keyPath, ContainerFilePath: "/pki/rekor-key.pem", FileMode: 0o644},
			},
			Networks:       []string{network},
			NetworkAliases: map[string][]string{network: {rekorAlias}},
			WaitingFor: wait.ForHTTP("/healthz").
				WithPort(rekorPort + "/tcp").
				WithStatusCodeMatcher(func(status int) bool { return status == http.StatusOK }).
				WithStartupTimeout(90 * time.Second),
		},
		Started: true,
	})
	if err != nil {
		return nil, err
	}
	stack.track(container)

	mappedPort, err := container.MappedPort(ctx, rekorPort+"/tcp")
	if err != nil {
		return nil, fmt.Errorf("mapped port: %w", err)
	}

	host, err := container.Host(ctx)
	if err != nil {
		return nil, fmt.Errorf("host: %w", err)
	}

	hostURL := "http://" + net.JoinHostPort(host, mappedPort.Port())

	return &RekorInstance{
		HostURL:      hostURL,
		ContainerURL: "http://" + net.JoinHostPort(rekorAlias, rekorPort),
		PublicKeyPEM: pubPEM,
	}, nil
}
