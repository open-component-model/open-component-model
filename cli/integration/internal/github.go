package internal

import (
	"archive/tar"
	"cmp"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// The github access integration test drives the CLI against live GitHub,
// using the same repository, ref and commit as the binding-level integration
// test (bindings/go/github/integration/integration_test.go). The ref is a
// published release tag, so resolving it must always yield exactly
// GitHubCommit: branches advance, release tags do not.
const (
	GitHubRepoURL = "https://github.com/open-component-model/open-component-model"
	GitHubRef     = "refs/tags/v0.8.0"
	GitHubCommit  = "b4bb4e880aa5c159366db7cc2ae800e1ee14dbda"
)

// gitHubArchiveRoot is what GitHub's REST tarball endpoint roots every
// archive entry at: "<owner>-<repo>-<abbreviated-commit>/".
const gitHubArchiveRoot = "open-component-model-open-component-model-"

// CreateOCMConfigForGitHub writes an ocmconfig carrying the registry
// credentials and, when a token is in the environment, a GitHubRepository
// consumer holding it. Unauthenticated GitHub allows only 60 requests per hour
// per IP, so on shared CI egress the token is what keeps the run from flaking
// on rate limits. Without one the run stays anonymous, which the github access
// supports.
func CreateOCMConfigForGitHub(t *testing.T, registry *OCIRegistry) string {
	t.Helper()

	cfg := fmt.Sprintf(`
type: generic.config.ocm.software/v1
configurations:
- type: credentials.config.ocm.software
  consumers:
  - identity:
      type: OCIRegistry
      hostname: %[1]q
      port: %[2]q
      scheme: http
    credentials:
    - type: Credentials/v1
      properties:
        username: %[3]q
        password: %[4]q
`, registry.Host, registry.Port, registry.User, registry.Password)

	// "token" is the only credential key the github access reads.
	if token := gitHubToken(); token != "" {
		cfg += fmt.Sprintf(`  - identity:
      type: GitHubRepository
      hostname: github.com
      scheme: https
    credentials:
    - type: Credentials/v1
      properties:
        token: %q
`, token)
	}

	cfgPath := filepath.Join(t.TempDir(), "ocmconfig.yaml")
	require.NoError(t, os.WriteFile(cfgPath, []byte(cfg), 0o600))
	return cfgPath
}

func gitHubToken() string {
	return cmp.Or(os.Getenv("GITHUB_TOKEN"), os.Getenv("GH_TOKEN"))
}

// AssertGitHubArchiveAtCommit verifies the file at path is GitHub's gzipped
// tar source archive of the test repository at GitHubCommit.
//
// It asserts the commit, not merely that a README.md exists: every revision
// of the repository has one, so a download that ignored the pinned commit
// would satisfy a README-only check. Two parts of the payload name the
// commit — the pax global header records it in full, the root directory
// abbreviated.
func AssertGitHubArchiveAtCommit(t *testing.T, path string) {
	t.Helper()
	r := require.New(t)

	f, err := os.Open(path)
	r.NoError(err)
	t.Cleanup(func() { r.NoError(f.Close()) })

	gz, err := gzip.NewReader(f)
	r.NoError(err)
	t.Cleanup(func() { r.NoError(gz.Close()) })

	var commitFound, readmeFound bool
	tr := tar.NewReader(gz)
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		r.NoError(err)

		// git names the commit an archive was cut from in the "comment"
		// record of the pax global header, as the full sha.
		if header.Typeflag == tar.TypeXGlobalHeader {
			r.Equal(GitHubCommit, header.PAXRecords["comment"],
				"the archive must be cut from the commit the download was asked for")
			commitFound = true
			continue
		}

		root, _, ok := strings.Cut(header.Name, "/")
		r.True(ok, "archive entry %q must live under the root directory", header.Name)
		abbrev, ok := strings.CutPrefix(root, gitHubArchiveRoot)
		r.True(ok, "archive entry %q must be rooted at %q<commit>", header.Name, gitHubArchiveRoot)
		// git abbreviates the sha only as far as it must to stay unambiguous,
		// so match a prefix instead of a fixed width.
		r.True(abbrev != "" && strings.HasPrefix(GitHubCommit, abbrev),
			"archive is rooted at commit %q, but the download was asked for %q", abbrev, GitHubCommit)

		if root+"/README.md" == header.Name {
			readmeFound = true
		}
	}

	r.True(commitFound, "the archive must carry the pax global header naming its commit")
	r.True(readmeFound, "the archive must contain the repository's README.md under the commit-prefixed root")
}
