package handler

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/Masterminds/semver/v3"
)

const defaultOperationTimeout = 3 * time.Minute

// CosignMinimumVersion is the minimum cosign version required on PATH.
// v3.0.4 introduced --use-signing-config which this handler depends on.
const CosignMinimumVersion = "v3.0.4"

// Executor abstracts cosign CLI invocations for testability.
// Callers must call Ensure before Run.
type Executor interface {
	Run(ctx context.Context, args []string, env []string) error
	Ensure(ctx context.Context) error
}

// ErrNotResolved is returned by Run when Ensure has not been called.
var ErrNotResolved = errors.New("executor not resolved: call Ensure before Run")

// DefaultExecutor invokes the cosign binary via os/exec.
// Call Ensure to resolve the binary, then Run to execute commands.
type DefaultExecutor struct {
	mu         sync.Mutex
	binaryPath string
	resolved   bool
	httpClient *http.Client
}

// ExecutorOption configures a DefaultExecutor.
type ExecutorOption func(*DefaultExecutor)

// WithHTTPClient sets the HTTP client used for cosign binary downloads.
func WithHTTPClient(c *http.Client) ExecutorOption {
	return func(e *DefaultExecutor) {
		e.httpClient = c
	}
}

// NewDefaultExecutor returns an executor that shells out to the cosign binary.
func NewDefaultExecutor(opts ...ExecutorOption) *DefaultExecutor {
	e := &DefaultExecutor{binaryPath: "cosign"}
	for _, opt := range opts {
		opt(e)
	}
	if e.httpClient == nil {
		e.httpClient = &http.Client{Timeout: 2 * time.Minute}
	}
	return e
}

// Ensure resolves the cosign binary (PATH lookup or auto-download).
// Must be called before Run. Safe to call multiple times (no-op after first success).
func (e *DefaultExecutor) Ensure(ctx context.Context) error {
	return e.ensureCosignAvailable(ctx)
}

func (e *DefaultExecutor) ensureCosignAvailable(ctx context.Context) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.resolved {
		return nil
	}
	path, err := exec.LookPath(e.binaryPath)
	if err == nil {
		if verr := e.checkVersion(ctx, path); verr != nil {
			return verr
		}
		e.binaryPath = path
		e.resolved = true
		return nil
	}
	path, dlErr := ensureOrDownloadCosign(ctx, e.httpClient)
	if dlErr != nil {
		return fmt.Errorf(
			"cosign binary not found on PATH and auto-download failed: %w "+
				"(install cosign from https://github.com/sigstore/cosign?tab=readme-ov-file#installation "+
				"and ensure it is on PATH, or fix the download error)", dlErr)
	}
	e.binaryPath = path
	e.resolved = true
	return nil
}

// Run executes the cosign binary with the given arguments and environment.
// Ensure must be called before Run; returns ErrNotResolved otherwise.
func (e *DefaultExecutor) Run(ctx context.Context, args []string, env []string) error {
	if !e.resolved {
		return ErrNotResolved
	}
	ctx, cancel := context.WithTimeout(ctx, defaultOperationTimeout)
	defer cancel()

	var stdout, stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, e.binaryPath, args...) //nolint:gosec // G204: args are constructed from trusted config, not user input
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	cmd.Env = env

	if err := cmd.Run(); err != nil {
		const maxStderr = 4096
		msg := strings.TrimSpace(stderr.String())
		if len(msg) > maxStderr {
			msg = msg[:maxStderr]
		}
		msg = scrubStderr(msg)
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return fmt.Errorf("cosign %s timed out: %w\nstderr: %s", args[0], err, msg)
		}
		return fmt.Errorf("cosign %s failed: %w\nstderr: %s", args[0], err, msg)
	}
	if out := strings.TrimSpace(stdout.String()); out != "" {
		slog.Debug("cosign output", "subcommand", args[0], "stdout", out)
	}
	return nil
}

func writeTemp(dir, pattern string, r io.Reader) (path string, err error) {
	f, err := os.CreateTemp(dir, pattern)
	if err != nil {
		return "", fmt.Errorf("create temp file %q: %w", pattern, err)
	}
	defer func() {
		if cerr := f.Close(); cerr != nil && err == nil {
			err = fmt.Errorf("close temp file %q: %w", pattern, cerr)
		}
		if err != nil {
			_ = os.Remove(f.Name())
		}
	}()
	if _, err = io.Copy(f, r); err != nil {
		return "", fmt.Errorf("write temp file %q: %w", pattern, err)
	}
	return f.Name(), nil
}

// cosignDenyEnvKeys is a denylist of environment variables that must never be forwarded
// to the cosign subprocess (library injection vectors).
var cosignDenyEnvKeys = map[string]bool{
	"LD_PRELOAD":            true,
	"DYLD_INSERT_LIBRARIES": true,
	"LD_LIBRARY_PATH":       true,
	"BASH_ENV":              true,
}

// cosignEnv builds the environment slice for the cosign subprocess by passing through
// the full process environment minus denylisted keys.
func cosignEnv() []string {
	var env []string
	for _, kv := range os.Environ() {
		key, _, _ := strings.Cut(kv, "=")
		if !cosignDenyEnvKeys[key] {
			env = append(env, kv)
		}
	}
	return env
}

func hasEnvKey(env []string, key string) bool {
	prefix := key + "="
	for _, kv := range env {
		if strings.HasPrefix(kv, prefix) {
			return true
		}
	}
	return false
}

var versionRegexp = regexp.MustCompile(`v\d+\.\d+\.\d+`)

var stderrScrubbers = []struct {
	pattern     *regexp.Regexp
	replacement string
}{
	{regexp.MustCompile(`(?i)bearer\s+\S+`), "bearer [REDACTED]"},
	{regexp.MustCompile(`SIGSTORE_ID_TOKEN=\S+`), "SIGSTORE_ID_TOKEN=[REDACTED]"},
	// Matches key=value where value may be quoted (single or double) or unquoted.
	{regexp.MustCompile(`(?i)(token|secret|password|key)="[^"]*"`), "${1}=[REDACTED]"},
	{regexp.MustCompile(`(?i)(token|secret|password|key)='[^']*'`), "${1}=[REDACTED]"},
	{regexp.MustCompile(`(?i)(token|secret|password|key)=\S+`), "${1}=[REDACTED]"},
	// Bare JWT tokens (header.payload.signature) — common in cosign error messages.
	{regexp.MustCompile(`eyJ[A-Za-z0-9_-]+\.eyJ[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+`), "[REDACTED-JWT]"},
}

func scrubStderr(s string) string {
	for _, sc := range stderrScrubbers {
		s = sc.pattern.ReplaceAllString(s, sc.replacement)
	}
	return s
}

func (e *DefaultExecutor) checkVersion(ctx context.Context, binaryPath string) error {
	detected, err := detectCosignVersion(ctx, binaryPath)
	if err != nil {
		slog.Warn("could not determine cosign version on PATH; if signing fails, "+
			"verify cosign is >= "+CosignMinimumVersion+" or remove it from PATH to trigger auto-download",
			"path", binaryPath, "error", err)
		return nil
	}
	detectedVer, err := semver.NewVersion(detected)
	if err != nil {
		slog.Warn("could not parse cosign version; if signing fails, "+
			"verify cosign is >= "+CosignMinimumVersion+" or remove it from PATH to trigger auto-download",
			"detected", detected, "error", err)
		return nil
	}
	minimumVer, err := semver.NewVersion(CosignMinimumVersion)
	if err != nil {
		return fmt.Errorf("parse minimum version constant: %w", err)
	}
	if detectedVer.LessThan(minimumVer) {
		return fmt.Errorf(
			"cosign on PATH (%s) is version %s, minimum required is %s "+
				"(--use-signing-config flag not available in older versions)",
			binaryPath, detected, CosignMinimumVersion)
	}
	pinnedVer, _ := semver.NewVersion(CosignVersion)
	if pinnedVer != nil && detectedVer.LessThan(pinnedVer) {
		slog.Warn("cosign on PATH is older than the tested/pinned version; consider upgrading",
			"path", binaryPath, "path_version", detected, "pinned_version", CosignVersion)
	}
	return nil
}

func detectCosignVersion(ctx context.Context, binaryPath string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var stdout bytes.Buffer
	cmd := exec.CommandContext(ctx, binaryPath, "version")
	cmd.Stdout = &stdout
	cmd.Stderr = nil
	cmd.Env = cosignEnv()
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("run cosign version: %w", err)
	}
	return parseCosignVersionOutput(stdout.String())
}

func parseCosignVersionOutput(output string) (string, error) {
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if v, ok := strings.CutPrefix(line, "GitVersion:"); ok {
			return strings.TrimSpace(v), nil
		}
	}
	if m := versionRegexp.FindString(output); m != "" {
		return m, nil
	}
	return "", errors.New("could not parse cosign version from output")
}
