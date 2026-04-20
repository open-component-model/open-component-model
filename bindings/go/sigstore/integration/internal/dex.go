package internal

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
	"golang.org/x/crypto/bcrypt"
)

const (
	dexImage = "ghcr.io/dexidp/dex:v2.45.0"
	dexPort  = "8888"

	dexUser     = "foo@bar.com"
	dexPassword = "sigstore-test-password"
	dexClientID = "fulcio"
)

// DexInstance holds the running Dex container details.
type DexInstance struct {
	HostURL            string
	ContainerIssuerURL string
}

// FetchOIDCToken retrieves an OIDC id_token from Dex via the password grant.
func (d *DexInstance) FetchOIDCToken(ctx context.Context) (string, error) {
	tokenURL := d.HostURL + "/token"

	data := url.Values{
		"grant_type": {"password"},
		"scope":      {"openid email"},
		"username":   {dexUser},
		"password":   {dexPassword},
		"client_id":  {dexClientID},
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL, strings.NewReader(data.Encode()))
	if err != nil {
		return "", fmt.Errorf("creating token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := testHTTPClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("requesting token: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("reading token response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("token request failed (%d): %s", resp.StatusCode, string(body))
	}

	var tokenResp struct {
		IDToken string `json:"id_token"`
	}
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return "", fmt.Errorf("parsing token response: %w", err)
	}
	if tokenResp.IDToken == "" {
		return "", fmt.Errorf("no id_token in response: %s", string(body))
	}

	return tokenResp.IDToken, nil
}

// StartDex starts a Dex OIDC provider container on the given network.
func StartDex(ctx context.Context, network string, stack *SigstoreStack) (*DexInstance, error) {
	containerAlias := "dex-idp"
	issuerPath := "/auth"
	containerIssuerURL := "http://" + net.JoinHostPort(containerAlias, dexPort) + issuerPath

	configContent, err := generateDexConfig(containerIssuerURL)
	if err != nil {
		return nil, fmt.Errorf("generate dex config: %w", err)
	}

	configPath := filepath.Join(stack.tmpDir, "dex-config.yaml")
	if err := os.WriteFile(configPath, []byte(configContent), 0o600); err != nil {
		return nil, fmt.Errorf("write dex config: %w", err)
	}

	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        dexImage,
			ExposedPorts: []string{dexPort + "/tcp"},
			Cmd:          []string{"dex", "serve", "/etc/dex/config.yaml"},
			Files: []testcontainers.ContainerFile{
				{
					HostFilePath:      configPath,
					ContainerFilePath: "/etc/dex/config.yaml",
					FileMode:          0o644,
				},
			},
			Networks:       []string{network},
			NetworkAliases: map[string][]string{network: {containerAlias}},
			WaitingFor: wait.ForHTTP(issuerPath + "/.well-known/openid-configuration").
				WithPort(dexPort + "/tcp").
				WithStatusCodeMatcher(func(status int) bool { return status == http.StatusOK }).
				WithStartupTimeout(60 * time.Second),
		},
		Started: true,
	})
	if err != nil {
		return nil, err
	}
	stack.track(container)

	mappedPort, err := container.MappedPort(ctx, dexPort+"/tcp")
	if err != nil {
		return nil, fmt.Errorf("mapped port: %w", err)
	}

	host, err := container.Host(ctx)
	if err != nil {
		return nil, fmt.Errorf("host: %w", err)
	}

	hostURL := "http://" + net.JoinHostPort(host, mappedPort.Port()) + issuerPath

	return &DexInstance{
		HostURL:            hostURL,
		ContainerIssuerURL: containerIssuerURL,
	}, nil
}

func generateDexConfig(issuerURL string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(dexPassword), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}

	return fmt.Sprintf(`issuer: %s

storage:
  type: memory

web:
  http: 0.0.0.0:%s

oauth2:
  responseTypes: ["code"]
  skipApprovalScreen: true
  passwordConnector: local

enablePasswordDB: true

staticPasswords:
  - email: "%s"
    hash: "%s"
    username: "test-user"
    userID: "test-user-id"

staticClients:
  - id: %s
    public: true
    name: "Fulcio"
    redirectURIs:
      - "http://localhost/callback"
`, issuerURL, dexPort, dexUser, string(hash), dexClientID), nil
}
