package internal

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/network"
)

// testHTTPClient is shared by all internal test helpers that make HTTP requests
// to the test containers. Timeout prevents indefinite hangs in CI.
var testHTTPClient = &http.Client{Timeout: 30 * time.Second}

// SigstoreStack holds all the information needed to run sigstore keyless sign+verify tests.
type SigstoreStack struct {
	FulcioURL         string
	RekorURL          string
	TSAURL            string
	OIDCToken         string
	TrustedRootPath   string
	SigningConfigPath string
	OIDCIssuer        string
	OIDCIdentity      string

	// resources tracks Docker containers and the network for cleanup.
	containers []testcontainers.Container
	network    *testcontainers.DockerNetwork
	tmpDir     string
}

// StartSigstoreStack starts the complete sigstore infrastructure:
// Dex (OIDC) → Fulcio (CA) → TesseraCT (CT log) → Rekor v2 (transparency log) → TSA (timestamps).
// All containers share a Docker network. Call [SigstoreStack.Destroy] to tear
// everything down when the tests are done.
//
// Startup order matters:
//  1. Dex — OIDC provider (no dependencies)
//  2. Fulcio — needs Dex issuer URL and CT log URL
//  3. TesseraCT — needs Fulcio root cert for chain validation (started before Fulcio
//     since Fulcio needs the CT log URL, but TesseraCT only needs the root cert file)
//  4. Rekor v2 — standalone (POSIX backend, no database)
//  5. TSA — standalone (memory signer)
//
// If startup fails partway through, any containers already started are
// terminated before the error is returned — no resources are leaked.
func StartSigstoreStack(ctx context.Context) (*SigstoreStack, error) {
	stack := &SigstoreStack{}

	tmpDir, err := os.MkdirTemp("", "sigstore-integration-*")
	if err != nil {
		return nil, fmt.Errorf("create temp dir: %w", err)
	}
	stack.tmpDir = tmpDir

	net, err := network.New(ctx)
	if err != nil {
		os.RemoveAll(tmpDir)
		return nil, fmt.Errorf("create docker network: %w", err)
	}
	stack.network = net

	// From this point on any error must go through the cleanup path so that
	// already-started containers and the network are not left running.

	// 1. Dex (OIDC provider) — no dependencies
	dex, err := StartDex(ctx, net.Name, stack)
	if err != nil {
		stack.Destroy(ctx) //nolint:errcheck // best-effort cleanup; original error takes precedence
		return nil, fmt.Errorf("start dex: %w", err)
	}

	// 2. Fulcio generates its root CA keypair during startup. We need the root
	//    cert file on disk before starting TesseraCT, but we also need TesseraCT's
	//    URL for Fulcio's --ct-log-url. Solution: start Fulcio with the CT log
	//    URL (container alias is known in advance), and TesseraCT uses Fulcio's
	//    root cert file that was written to tmpDir during Fulcio setup.
	fulcio, err := StartFulcio(ctx, net.Name, dex.ContainerIssuerURL, CTLogContainerURL(), stack)
	if err != nil {
		stack.Destroy(ctx) //nolint:errcheck // best-effort cleanup; original error takes precedence
		return nil, fmt.Errorf("start fulcio: %w", err)
	}

	// 3. TesseraCT (CT log) — needs Fulcio root cert for chain validation
	ctlog, err := StartCTLog(ctx, net.Name, fulcio.RootCertPath, stack)
	if err != nil {
		stack.Destroy(ctx) //nolint:errcheck // best-effort cleanup; original error takes precedence
		return nil, fmt.Errorf("start ctlog: %w", err)
	}

	// 4. Rekor v2 (transparency log) — standalone POSIX backend
	rekor, err := StartRekor(ctx, net.Name, stack)
	if err != nil {
		stack.Destroy(ctx) //nolint:errcheck // best-effort cleanup; original error takes precedence
		return nil, fmt.Errorf("start rekor: %w", err)
	}

	// 5. TSA (timestamp authority) — standalone memory signer
	tsa, err := StartTSA(ctx, net.Name, stack)
	if err != nil {
		stack.Destroy(ctx) //nolint:errcheck // best-effort cleanup; original error takes precedence
		return nil, fmt.Errorf("start tsa: %w", err)
	}

	// Fetch OIDC token once for all tests.
	token, err := dex.FetchOIDCToken(ctx)
	if err != nil {
		stack.Destroy(ctx) //nolint:errcheck // best-effort cleanup; original error takes precedence
		return nil, fmt.Errorf("fetch OIDC token: %w", err)
	}

	trustedRootPath, err := BuildTrustedRoot(tmpDir, TrustedRootParams{
		FulcioRootPEM:     fulcio.RootCertPEM,
		RekorPublicKeyPEM: rekor.PublicKeyPEM,
		RekorOrigin:       "rekor.integration-test",
		RekorBaseURL:      rekor.ContainerURL,
		CTLogPublicKeyDER: ctlog.PublicKeyDER,
		CTLogID:           ctlog.LogID,
		TSACertChainPEM:   tsa.CertChainPEM,
	})
	if err != nil {
		stack.Destroy(ctx) //nolint:errcheck // best-effort cleanup; original error takes precedence
		return nil, fmt.Errorf("build trusted root: %w", err)
	}

	signingConfigPath, err := BuildSigningConfig(tmpDir, fulcio.HostURL, rekor.HostURL, tsa.HostURL)
	if err != nil {
		stack.Destroy(ctx) //nolint:errcheck // best-effort cleanup; original error takes precedence
		return nil, fmt.Errorf("build signing config: %w", err)
	}

	stack.FulcioURL = fulcio.HostURL
	stack.RekorURL = rekor.HostURL
	stack.TSAURL = tsa.HostURL
	stack.OIDCToken = token
	stack.TrustedRootPath = trustedRootPath
	stack.SigningConfigPath = signingConfigPath
	stack.OIDCIssuer = dex.ContainerIssuerURL
	stack.OIDCIdentity = dexUser

	return stack, nil
}

// track registers a container for later cleanup by [Destroy].
func (s *SigstoreStack) track(c testcontainers.Container) {
	s.containers = append(s.containers, c)
}

// Destroy tears down all containers, the Docker network, and temp files.
func (s *SigstoreStack) Destroy(ctx context.Context) error {
	var firstErr error
	cleanupCtx := ctx
	if cleanupCtx == nil || cleanupCtx.Err() != nil {
		var cancel context.CancelFunc
		cleanupCtx, cancel = context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
	}
	for i := len(s.containers) - 1; i >= 0; i-- {
		if err := testcontainers.TerminateContainer(s.containers[i]); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	if s.network != nil {
		if err := s.network.Remove(cleanupCtx); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	if s.tmpDir != "" {
		if err := os.RemoveAll(s.tmpDir); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}
