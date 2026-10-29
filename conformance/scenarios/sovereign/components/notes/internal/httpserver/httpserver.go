// Package httpserver runs the notes HTTP server with graceful shutdown.
//
// Kubernetes stops a pod with SIGTERM during rollouts. Exiting immediately drops
// in-flight requests, so the server instead stops accepting new connections and
// lets running requests finish before returning.
package httpserver

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

// Run serves srv until it fails or the process receives SIGTERM or SIGINT. On a
// signal it shuts srv down, waiting up to shutdownTimeout for in-flight requests.
func Run(srv *http.Server, shutdownTimeout time.Duration) error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, os.Interrupt)
	defer stop()

	serveErr := make(chan error, 1)
	go func() { serveErr <- srv.ListenAndServe() }()

	select {
	case err := <-serveErr:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
	}

	log.Print("Shutdown signal received, draining in-flight requests")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("graceful shutdown: %w", err)
	}
	log.Print("Server stopped")
	return nil
}
