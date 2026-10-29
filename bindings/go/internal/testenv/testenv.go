// Package testenv gates integration tests on the external systems they need.
//
// A test declares what it needs with Require. When a requirement is not
// available the test is skipped with a hint on how to provide it.
package testenv

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"

	"github.com/testcontainers/testcontainers-go"
)

// Requirement is an external system a test depends on.
type Requirement struct {
	name  string
	hint  string
	check func() error
}

var (
	// Docker is a reachable Docker daemon for testcontainers.
	Docker = &Requirement{
		name:  "docker",
		hint:  "start a Docker daemon reachable by testcontainers",
		check: sync.OnceValue(dockerHealthy),
	}
	// GPG is a GnuPG installation with gpg on PATH.
	GPG = &Requirement{
		name:  "gpg",
		hint:  "install GnuPG >= 2.2 and put gpg on PATH",
		check: sync.OnceValue(func() error { return onPath("gpg") }),
	}
	// GitHubToken is a GitHub token that lifts the anonymous API rate limit,
	// resolved by GitHubAuthToken.
	GitHubToken = &Requirement{
		name: "github-token",
		hint: "export GITHUB_TOKEN or log in with `gh auth login`",
		check: func() error {
			if GitHubAuthToken() == "" {
				return errors.New("GITHUB_TOKEN not set and `gh auth token` returned no token")
			}
			return nil
		},
	}
	// Sigstore is a running sigstore scaffolding stack, described by the
	// environment written by the sigstore/integration setup, and cosign on PATH.
	Sigstore = &Requirement{
		name: "sigstore",
		hint: "run `task bindings/go/sigstore/integration:test/integration`, or run its `setup` task, " +
			"source tmp/sigstore-env.sh and put tmp/bin on PATH",
		check: func() error {
			return errors.Join(
				envSet("SIGSTORE_OIDC_TOKEN", "SIGSTORE_SIGNING_CONFIG", "SIGSTORE_TRUSTED_ROOT",
					"SIGSTORE_OIDC_ISSUER", "SIGSTORE_OIDC_IDENTITY"),
				onPath("cosign"),
			)
		},
	}
)

// GitHubAuthToken returns GITHUB_TOKEN if set, otherwise the token of the gh
// CLI (`gh auth token`) if gh is installed and logged in, otherwise "".
var GitHubAuthToken = sync.OnceValue(func() string {
	if token := os.Getenv("GITHUB_TOKEN"); token != "" {
		return token
	}
	out, err := exec.CommandContext(context.Background(), "gh", "auth", "token").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
})

// Require skips tb unless every given requirement is available.
func Require(tb testing.TB, reqs ...*Requirement) {
	tb.Helper()
	for _, r := range reqs {
		if err := r.check(); err != nil {
			tb.Skipf("requirement %q is unavailable: %v (%s)", r.name, err, r.hint)
		}
	}
}

func dockerHealthy() error {
	provider, err := testcontainers.NewDockerProvider()
	if err != nil {
		return err
	}
	defer provider.Close()
	return provider.Health(context.Background())
}

func onPath(binaries ...string) error {
	var errs []error
	for _, b := range binaries {
		if _, err := exec.LookPath(b); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func envSet(keys ...string) error {
	var missing []string
	for _, k := range keys {
		if os.Getenv(k) == "" {
			missing = append(missing, k)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("environment variables not set: %s", strings.Join(missing, ", "))
	}
	return nil
}
