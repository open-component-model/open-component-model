package internal

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"time"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

const (
	// TesseraCT v0.1.1 — pinned by digest because the project tags images by
	// commit SHA, not semver. Commit 016d1dc corresponds to the v0.1.1 release.
	// See https://github.com/transparency-dev/tesseract/releases/tag/v0.1.1
	tesseractImage = "ghcr.io/transparency-dev/tesseract/posix@sha256:8269c32a1b1deb159ba75016421314cb5e68304c2813d444aca3efdf0e9d5027"
	ctlogPort      = "6962"
	ctlogAlias     = "tesseract"
)

// CTLogInstance holds the running TesseraCT container details.
type CTLogInstance struct {
	PublicKeyPEM []byte
	PublicKeyDER []byte
	LogID        [sha256.Size]byte
}

// StartCTLog starts a TesseraCT CT log container.
// The Fulcio root certificate PEM is required so TesseraCT can validate
// submitted certificate chains.
func StartCTLog(ctx context.Context, network string, fulcioRootCertPath string, stack *SigstoreStack) (*CTLogInstance, error) {
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generate ctlog key: %w", err)
	}

	privDER, err := x509.MarshalECPrivateKey(priv)
	if err != nil {
		return nil, fmt.Errorf("marshal ctlog private key: %w", err)
	}
	privPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: privDER})

	pubDER, err := x509.MarshalPKIXPublicKey(&priv.PublicKey)
	if err != nil {
		return nil, fmt.Errorf("marshal ctlog public key: %w", err)
	}
	pubPEM := pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: pubDER})

	logID := sha256.Sum256(pubDER)

	keyPath := filepath.Join(stack.tmpDir, "ctlog-privkey.pem")
	if err := os.WriteFile(keyPath, privPEM, 0o600); err != nil {
		return nil, fmt.Errorf("write ctlog key: %w", err)
	}

	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        tesseractImage,
			ExposedPorts: []string{ctlogPort + "/tcp"},
			Cmd: []string{
				"--private_key=/etc/ctfe/privkey.pem",
				"--origin=tesseract",
				"--storage_dir=/tmp/ctfe",
				"--roots_pem_file=/etc/fulcio/root.pem",
				"--ext_key_usages=CodeSigning",
				"--http_endpoint=0.0.0.0:" + ctlogPort,
			},
			Files: []testcontainers.ContainerFile{
				{HostFilePath: keyPath, ContainerFilePath: "/etc/ctfe/privkey.pem", FileMode: 0o644},
				{HostFilePath: fulcioRootCertPath, ContainerFilePath: "/etc/fulcio/root.pem", FileMode: 0o644},
			},
			Networks:       []string{network},
			NetworkAliases: map[string][]string{network: {ctlogAlias}},
			WaitingFor: wait.ForLog("CT HTTP Server Starting").
				WithStartupTimeout(60 * time.Second),
		},
		Started: true,
	})
	if err != nil {
		return nil, err
	}
	stack.track(container)

	return &CTLogInstance{
		PublicKeyPEM: pubPEM,
		PublicKeyDER: pubDER,
		LogID:        logID,
	}, nil
}

// CTLogContainerURL returns the in-network URL that Fulcio should use to reach TesseraCT.
func CTLogContainerURL() string {
	return "http://" + net.JoinHostPort(ctlogAlias, ctlogPort)
}
