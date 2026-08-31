package externalartifact

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/go-logr/logr"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

// readHeaderTimeout bounds how long the artifact server waits for request
// headers, guarding against slowloris-style clients.
const readHeaderTimeout = 10 * time.Second

// shutdownTimeout bounds graceful shutdown of the artifact server.
const shutdownTimeout = 10 * time.Second

// readinessDialTimeout bounds the readiness-probe TCP dial to the server.
const readinessDialTimeout = 2 * time.Second

// StorageServer serves packaged artifacts over HTTP(S). It implements
// manager.Runnable and manager.LeaderElectionRunnable so it runs on every
// replica (not just the elected leader), because each replica must be able to
// serve the artifacts on its own storage volume.
type StorageServer struct {
	addr     string
	storage  *Storage
	logger   logr.Logger
	certFile string
	keyFile  string

	mu         sync.RWMutex
	listenAddr string
}

var (
	_ manager.Runnable               = (*StorageServer)(nil)
	_ manager.LeaderElectionRunnable = (*StorageServer)(nil)
)

// StorageServerOption configures a StorageServer.
type StorageServerOption func(*StorageServer)

// WithTLS enables HTTPS using the given certificate and key files. When either
// path is empty the server serves plain HTTP.
func WithTLS(certFile, keyFile string) StorageServerOption {
	return func(s *StorageServer) {
		s.certFile = certFile
		s.keyFile = keyFile
	}
}

// NewStorageServer builds a StorageServer bound to addr, serving from storage.
func NewStorageServer(addr string, storage *Storage, logger logr.Logger, opts ...StorageServerOption) *StorageServer {
	s := &StorageServer{
		addr:    addr,
		storage: storage,
		logger:  logger.WithName("external-artifact-storage"),
	}
	for _, opt := range opts {
		opt(s)
	}

	return s
}

// NeedLeaderElection returns false so the storage server runs on all replicas.
// Each replica must serve the artifacts on its own volume; with a ReadWriteMany
// volume shared across replicas, any replica can serve any artifact.
func (s *StorageServer) NeedLeaderElection() bool {
	return false
}

// ReadyzCheck returns a healthz.Checker that succeeds only while the artifact
// HTTP server is accepting connections. Wiring this into the manager's readiness
// probe ensures a pod whose serve goroutine has died is marked NotReady instead
// of silently returning 404s to Flux.
func (s *StorageServer) ReadyzCheck() healthz.Checker {
	return func(_ *http.Request) error {
		s.mu.RLock()
		addr := s.listenAddr
		s.mu.RUnlock()
		if addr == "" {
			return fmt.Errorf("artifact storage server not started")
		}
		conn, err := net.DialTimeout("tcp", addr, readinessDialTimeout)
		if err != nil {
			return fmt.Errorf("artifact storage server not reachable: %w", err)
		}

		return conn.Close()
	}
}

// tlsEnabled reports whether the server is configured to serve HTTPS.
func (s *StorageServer) tlsEnabled() bool {
	return s.certFile != "" && s.keyFile != ""
}

// Start runs the HTTP(S) server until the context is cancelled.
func (s *StorageServer) Start(ctx context.Context) error {
	mux := http.NewServeMux()
	mux.Handle("/", s.storage.Server())

	server := &http.Server{
		Addr:              s.addr,
		Handler:           mux,
		ReadHeaderTimeout: readHeaderTimeout,
	}
	if s.tlsEnabled() {
		// Enforce modern TLS; the cert/key are loaded by ServeTLS.
		server.TLSConfig = &tls.Config{MinVersion: tls.VersionTLS12}
	}

	listener, err := net.Listen("tcp", s.addr)
	if err != nil {
		return fmt.Errorf("failed to listen on %q: %w", s.addr, err)
	}
	// Record the concrete listen address (resolves :0 to a real port) for the
	// readiness check.
	s.mu.Lock()
	s.listenAddr = listener.Addr().String()
	s.mu.Unlock()

	errCh := make(chan error, 1)
	go func() {
		s.logger.Info("starting external artifact storage server", "address", s.addr, "tls", s.tlsEnabled())
		var serveErr error
		if s.tlsEnabled() {
			serveErr = server.ServeTLS(listener, s.certFile, s.keyFile)
		} else {
			serveErr = server.Serve(listener)
		}
		if serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
			errCh <- serveErr
		}
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("failed to gracefully shut down storage server: %w", err)
		}

		return nil
	case err := <-errCh:
		return fmt.Errorf("storage server error: %w", err)
	}
}
