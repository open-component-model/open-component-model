package internal

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

const (
	tsaImage = "ghcr.io/sigstore/timestamp-server:v2.0.5"
	tsaPort  = "3003"
	tsaAlias = "tsa-server"
)

// TSAInstance holds the running Timestamp Authority container details.
type TSAInstance struct {
	HostURL      string
	ContainerURL string
	CertChainPEM []byte
}

// StartTSA starts a Sigstore Timestamp Authority container with an in-memory signer.
func StartTSA(ctx context.Context, network string, stack *SigstoreStack) (*TSAInstance, error) {
	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        tsaImage,
			ExposedPorts: []string{tsaPort + "/tcp"},
			Cmd: []string{
				"serve",
				"--host=0.0.0.0",
				"--port=" + tsaPort,
				"--timestamp-signer=memory",
			},
			Networks:       []string{network},
			NetworkAliases: map[string][]string{network: {tsaAlias}},
			WaitingFor: wait.ForHTTP("/ping").
				WithPort(tsaPort + "/tcp").
				WithStatusCodeMatcher(func(status int) bool { return status == http.StatusOK }).
				WithStartupTimeout(60 * time.Second),
		},
		Started: true,
	})
	if err != nil {
		return nil, err
	}
	stack.track(container)

	mappedPort, err := container.MappedPort(ctx, tsaPort+"/tcp")
	if err != nil {
		return nil, fmt.Errorf("mapped port: %w", err)
	}

	host, err := container.Host(ctx)
	if err != nil {
		return nil, fmt.Errorf("host: %w", err)
	}

	hostURL := "http://" + net.JoinHostPort(host, mappedPort.Port())

	certChain, err := fetchTSACertChain(ctx, hostURL)
	if err != nil {
		return nil, fmt.Errorf("fetch TSA cert chain: %w", err)
	}

	return &TSAInstance{
		HostURL:      hostURL,
		ContainerURL: "http://" + net.JoinHostPort(tsaAlias, tsaPort),
		CertChainPEM: certChain,
	}, nil
}

// fetchTSACertChain retrieves the certificate chain from the TSA's public endpoint.
func fetchTSACertChain(ctx context.Context, hostURL string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, hostURL+"/api/v1/timestamp/certchain", nil)
	if err != nil {
		return nil, err
	}

	resp, err := testHTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status %d fetching TSA cert chain", resp.StatusCode)
	}

	chain, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if len(chain) == 0 {
		return nil, fmt.Errorf("empty TSA cert chain response")
	}

	return chain, nil
}
