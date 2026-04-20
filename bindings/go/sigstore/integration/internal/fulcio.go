package internal

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

const (
	fulcioImage    = "gcr.io/projectsigstore/fulcio:v1.8.5"
	fulcioPort     = "5555"
	fulcioKeyPw    = "test-password"
	fulcioGRPCPort = "5554"
)

// FulcioInstance holds the running Fulcio container details.
type FulcioInstance struct {
	HostURL      string
	ContainerURL string
	RootCertPEM  []byte
	RootCertPath string
}

// StartFulcio starts a Fulcio CA container in fileca mode on the given network.
// If ctLogURL is non-empty, Fulcio will embed SCTs from that CT log in issued certificates.
func StartFulcio(ctx context.Context, network, dexContainerIssuerURL, ctLogURL string, stack *SigstoreStack) (*FulcioInstance, error) {
	containerAlias := "fulcio-server"

	rootCertPEM, rootKeyPEM, err := generateRootCA()
	if err != nil {
		return nil, fmt.Errorf("generate root CA: %w", err)
	}
	encKeyPEM, err := encryptPrivateKey(rootKeyPEM)
	if err != nil {
		return nil, fmt.Errorf("encrypt private key: %w", err)
	}
	fulcioConfig, err := generateFulcioConfig(dexContainerIssuerURL)
	if err != nil {
		return nil, fmt.Errorf("generate fulcio config: %w", err)
	}

	certPath := filepath.Join(stack.tmpDir, "fulcio-root.pem")
	keyPath := filepath.Join(stack.tmpDir, "fulcio-root.key")
	configPath := filepath.Join(stack.tmpDir, "fulcio-config.json")

	if err := os.WriteFile(certPath, rootCertPEM, 0o600); err != nil {
		return nil, fmt.Errorf("write cert: %w", err)
	}
	if err := os.WriteFile(keyPath, encKeyPEM, 0o600); err != nil {
		return nil, fmt.Errorf("write key: %w", err)
	}
	if err := os.WriteFile(configPath, fulcioConfig, 0o600); err != nil {
		return nil, fmt.Errorf("write config: %w", err)
	}

	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        fulcioImage,
			ExposedPorts: []string{fulcioPort + "/tcp", fulcioGRPCPort + "/tcp"},
			Cmd:          fulcioCmd(ctLogURL),
			Files: []testcontainers.ContainerFile{
				{HostFilePath: certPath, ContainerFilePath: "/etc/fulcio/root.pem", FileMode: 0o644},
				{HostFilePath: keyPath, ContainerFilePath: "/etc/fulcio/root.key", FileMode: 0o644},
				{HostFilePath: configPath, ContainerFilePath: "/etc/fulcio/config.json", FileMode: 0o644},
			},
			Networks:       []string{network},
			NetworkAliases: map[string][]string{network: {containerAlias}},
			WaitingFor: wait.ForHTTP("/healthz").
				WithPort(fulcioPort + "/tcp").
				WithStatusCodeMatcher(func(status int) bool { return status == http.StatusOK }).
				WithStartupTimeout(60 * time.Second),
		},
		Started: true,
	})
	if err != nil {
		return nil, err
	}
	stack.track(container)

	mappedPort, err := container.MappedPort(ctx, fulcioPort+"/tcp")
	if err != nil {
		return nil, fmt.Errorf("mapped port: %w", err)
	}

	host, err := container.Host(ctx)
	if err != nil {
		return nil, fmt.Errorf("host: %w", err)
	}

	return &FulcioInstance{
		HostURL:      "http://" + net.JoinHostPort(host, mappedPort.Port()),
		ContainerURL: "http://" + net.JoinHostPort(containerAlias, fulcioPort),
		RootCertPEM:  rootCertPEM,
		RootCertPath: certPath,
	}, nil
}

// generateRootCA creates a self-signed EC P-256 root CA certificate.
func generateRootCA() (certPEM, keyPEM []byte, err error) {
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, err
	}

	serialNumber, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, nil, err
	}

	template := &x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			Organization: []string{"OCM Integration Test"},
			CommonName:   "test-fulcio-root",
		},
		NotBefore:             time.Now().Add(-1 * time.Minute),
		NotAfter:              time.Now().Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}

	certDER, err := x509.CreateCertificate(rand.Reader, template, template, &priv.PublicKey, priv)
	if err != nil {
		return nil, nil, err
	}

	certPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})

	keyDER, err := x509.MarshalECPrivateKey(priv)
	if err != nil {
		return nil, nil, err
	}
	keyPEM = pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})

	return certPEM, keyPEM, nil
}

// encryptPrivateKey encrypts a PEM-encoded EC private key with the Fulcio test password.
//
//nolint:staticcheck // x509.EncryptPEMBlock is deprecated but Fulcio fileca still requires RFC 1423 encrypted keys.
func encryptPrivateKey(keyPEM []byte) ([]byte, error) {
	block, _ := pem.Decode(keyPEM)
	if block == nil {
		return nil, fmt.Errorf("failed to decode PEM block")
	}

	encBlock, err := x509.EncryptPEMBlock(rand.Reader, block.Type, block.Bytes, []byte(fulcioKeyPw), x509.PEMCipher3DES)
	if err != nil {
		return nil, err
	}

	return pem.EncodeToMemory(encBlock), nil
}

// generateFulcioConfig creates the Fulcio OIDC issuer config JSON.
func generateFulcioConfig(dexIssuerURL string) ([]byte, error) {
	config := map[string]any{
		"OIDCIssuers": map[string]any{
			dexIssuerURL: map[string]any{
				"IssuerURL": dexIssuerURL,
				"ClientID":  dexClientID,
				"Type":      "email",
			},
		},
	}
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal fulcio config: %w", err)
	}
	return data, nil
}

// fulcioCmd builds the Fulcio container command. When ctLogURL is non-empty,
// Fulcio will submit certificates to that CT log and embed the SCT in issued certs.
func fulcioCmd(ctLogURL string) []string {
	cmd := []string{
		"serve",
		"--host=0.0.0.0",
		"--port=" + fulcioPort,
		"--grpc-port=" + fulcioGRPCPort,
		"--ca=fileca",
		"--fileca-cert=/etc/fulcio/root.pem",
		"--fileca-key=/etc/fulcio/root.key",
		"--fileca-key-passwd=" + fulcioKeyPw,
		"--config-path=/etc/fulcio/config.json",
	}
	if ctLogURL != "" {
		cmd = append(cmd, "--ct-log-url="+ctLogURL)
	} else {
		cmd = append(cmd, "--ct-log-url=")
	}
	return cmd
}
