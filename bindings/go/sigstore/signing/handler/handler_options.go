package handler

import (
	"context"
	"net/http"
	"time"
)

// HandlerOption configures a Handler.
type HandlerOption func(*Handler)

// WithTempDir sets the base directory for temporary files created during signing/verification.
// Corresponds to the filesystem.config.ocm.software TempFolder field.
func WithTempDir(dir string) HandlerOption {
	return func(h *Handler) {
		h.tempDir = dir
	}
}

// WithHTTPClient sets the HTTP client used for cosign binary auto-downloads.
// Only relevant when cosign is not already on PATH. If not set, a default client with a 5-minute
// timeout is used.
func WithHTTPClient(c *http.Client) HandlerOption {
	return func(h *Handler) {
		h.runner.HttpClient = c
	}
}

// WithOperationTimeout sets the maximum duration for a single cosign subprocess invocation.
// The timeout is applied via context.WithTimeout before exec. If not set, defaults to 3 minutes.
func WithOperationTimeout(d time.Duration) HandlerOption {
	return func(h *Handler) {
		h.runner.OperationTimeout = d
	}
}

// WithExecCosign overrides the function used to execute cosign subprocesses.
func WithExecCosign(fn func(ctx context.Context, binaryPath string, args, env []string) error) HandlerOption {
	return func(h *Handler) {
		h.runner.ExecCosign = fn
	}
}

// WithLookPath overrides the function used to locate the cosign binary on PATH.
func WithLookPath(fn func(string) (string, error)) HandlerOption {
	return func(h *Handler) {
		h.runner.LookPath = fn
	}
}
