package hack_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
)

// helperAbs resolves the absolute path to setup-helpers.sh.
// Tests run in the package directory, so a relative path is sufficient.
func helperAbs(t *testing.T) string {
	t.Helper()
	p, err := filepath.Abs("setup-helpers.sh")
	require.NoError(t, err)
	return p
}

// runRC runs a bash -c invocation and returns the process exit code.
// args[0] is $0, args[1] is $1, and so on; script must not interpolate args.
// On an unexpected error (not ExitError) the test is fatally failed.
func runRC(t *testing.T, script string, args ...string) int {
	t.Helper()
	cmdArgs := append([]string{"-c", script}, args...)
	cmd := exec.CommandContext(t.Context(), "bash", cmdArgs...)
	if err := cmd.Run(); err != nil {
		var e *exec.ExitError
		if errors.As(err, &e) {
			return e.ExitCode()
		}
		t.Fatalf("unexpected error running bash: %v", err)
	}
	return 0
}

// downloadScript sources the helper and calls download_file with positional args.
const downloadScript = `. "$0"; download_file "$1" "$2"`

// writeHostsScript sources the helper and calls write_scaffolding_hosts.
// $1=HOSTS_FILE, $2=IP, $3…=HOSTNAMES
const writeHostsScript = `. "$0"; write_scaffolding_hosts "$1" "$2" "${@:3}"`

// removeHostsScript sources the helper and calls remove_scaffolding_hosts.
const removeHostsScript = `. "$0"; remove_scaffolding_hosts "$1"`

func Test_Integration_ScaffoldingDownloads(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not on PATH")
	}
	if _, err := exec.LookPath("curl"); err != nil {
		t.Skip("curl not on PATH")
	}

	helper := helperAbs(t)

	tests := []struct {
		name    string
		handler func(*atomic.Int32) http.HandlerFunc
		setup   func(dir string) string // returns dest path; empty string = dest absent
		wantRC  int
		check   func(t *testing.T, dest string, counter *atomic.Int32)
	}{
		{
			name: "retries_then_succeeds",
			// First two requests return 503; the third returns the body.
			handler: func(counter *atomic.Int32) http.HandlerFunc {
				return func(w http.ResponseWriter, r *http.Request) {
					n := counter.Add(1)
					if n <= 2 {
						w.WriteHeader(http.StatusServiceUnavailable)
						return
					}
					w.WriteHeader(http.StatusOK)
					_, _ = w.Write([]byte("kind: ConfigMap"))
				}
			},
			setup:  func(dir string) string { return filepath.Join(dir, "out.yaml") },
			wantRC: 0,
			check: func(t *testing.T, dest string, counter *atomic.Int32) {
				t.Helper()
				got, err := os.ReadFile(dest)
				require.NoError(t, err)
				require.Equal(t, "kind: ConfigMap", string(got))
				require.Equal(t, int32(3), counter.Load())
			},
		},
		{
			name: "always_503_existing_dest",
			// Pre-existing DEST must survive intact after all retries exhaust.
			// curl retries 503 with exponential backoff; this case may take ~31 s.
			handler: func(counter *atomic.Int32) http.HandlerFunc {
				return func(w http.ResponseWriter, r *http.Request) {
					counter.Add(1)
					w.WriteHeader(http.StatusServiceUnavailable)
				}
			},
			setup: func(dir string) string {
				dest := filepath.Join(dir, "out.yaml")
				if err := os.WriteFile(dest, []byte("old"), 0o600); err != nil {
					panic(err)
				}
				return dest
			},
			wantRC: 1,
			check: func(t *testing.T, dest string, _ *atomic.Int32) {
				t.Helper()
				got, err := os.ReadFile(dest)
				require.NoError(t, err)
				require.Equal(t, "old", string(got))
			},
		},
		{
			name: "always_503_no_dest",
			handler: func(counter *atomic.Int32) http.HandlerFunc {
				return func(w http.ResponseWriter, r *http.Request) {
					counter.Add(1)
					w.WriteHeader(http.StatusServiceUnavailable)
				}
			},
			// dest path exists inside t.TempDir() but the file itself is not pre-created.
			setup:  func(dir string) string { return filepath.Join(dir, "out.yaml") },
			wantRC: 1,
			check: func(t *testing.T, dest string, _ *atomic.Int32) {
				t.Helper()
				_, err := os.Stat(dest)
				require.True(t, os.IsNotExist(err), "dest must not exist after failure")
			},
		},
		{
			name: "http_404_no_retry",
			handler: func(counter *atomic.Int32) http.HandlerFunc {
				return func(w http.ResponseWriter, r *http.Request) {
					counter.Add(1)
					w.WriteHeader(http.StatusNotFound)
				}
			},
			setup:  func(dir string) string { return filepath.Join(dir, "out.yaml") },
			wantRC: 4,
			check: func(t *testing.T, dest string, counter *atomic.Int32) {
				t.Helper()
				_, err := os.Stat(dest)
				require.True(t, os.IsNotExist(err), "dest must not exist after 404")
				// 404 must not be retried
				require.Equal(t, int32(1), counter.Load(), "404 must not be retried")
			},
		},
		{
			name: "empty_body",
			handler: func(counter *atomic.Int32) http.HandlerFunc {
				return func(w http.ResponseWriter, r *http.Request) {
					counter.Add(1)
					w.WriteHeader(http.StatusOK)
					// Write nothing: empty body
				}
			},
			setup:  func(dir string) string { return filepath.Join(dir, "out.yaml") },
			wantRC: 1,
			check: func(t *testing.T, dest string, _ *atomic.Int32) {
				t.Helper()
				_, err := os.Stat(dest)
				require.True(t, os.IsNotExist(err), "dest must not exist for empty body")
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var counter atomic.Int32
			srv := httptest.NewServer(tc.handler(&counter))
			defer srv.Close()

			dir := t.TempDir()
			dest := tc.setup(dir)

			rc := runRC(t, downloadScript, helper, srv.URL+"/file", dest)
			require.Equal(t, tc.wantRC, rc)

			tc.check(t, dest, &counter)

			// No temp files must remain in dest's directory after any outcome.
			base := filepath.Base(dest)
			matches, err := filepath.Glob(filepath.Join(dir, "."+base+".*"))
			require.NoError(t, err)
			require.Empty(t, matches, "temp files must be cleaned up")
		})
	}
}

func Test_Integration_ScaffoldingHosts(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not on PATH")
	}

	helper := helperAbs(t)

	// writeFile is a helper to create a test hosts file with the given content.
	writeFile := func(t *testing.T, dir, name, content string) string {
		t.Helper()
		p := filepath.Join(dir, name)
		require.NoError(t, os.WriteFile(p, []byte(content), 0o644))
		return p
	}

	readFile := func(t *testing.T, p string) string {
		t.Helper()
		b, err := os.ReadFile(p)
		require.NoError(t, err)
		return string(b)
	}

	const (
		blockBegin = "# BEGIN OCM SIGSTORE SCAFFOLDING\n"
		blockEnd   = "# END OCM SIGSTORE SCAFFOLDING\n"
	)

	tests := []struct {
		name string
		fn   func(t *testing.T, helper string)
	}{
		{
			name: "append_block_preserves_unrelated",
			fn: func(t *testing.T, helper string) {
				t.Helper()
				dir := t.TempDir()
				unrelated := "127.0.0.1 localhost\n# a comment\n192.168.1.10 other.host\n"
				f := writeFile(t, dir, "hosts", unrelated)

				rc := runRC(t, writeHostsScript, helper, f, "10.0.0.1",
					"fulcio.example.com", "rekor.example.com")
				require.Equal(t, 0, rc)

				want := unrelated +
					blockBegin +
					"10.0.0.1 fulcio.example.com\n" +
					"10.0.0.1 rekor.example.com\n" +
					blockEnd
				require.Equal(t, want, readFile(t, f))
			},
		},
		{
			name: "idempotent_same_args",
			fn: func(t *testing.T, helper string) {
				t.Helper()
				dir := t.TempDir()
				unrelated := "127.0.0.1 localhost\n"
				f := writeFile(t, dir, "hosts", unrelated)

				rc := runRC(t, writeHostsScript, helper, f, "10.0.0.1", "fulcio.example.com")
				require.Equal(t, 0, rc)
				first := readFile(t, f)

				rc = runRC(t, writeHostsScript, helper, f, "10.0.0.1", "fulcio.example.com")
				require.Equal(t, 0, rc)
				require.Equal(t, first, readFile(t, f), "repeated write must be byte-identical")
			},
		},
		{
			name: "different_ip_replaces_block",
			fn: func(t *testing.T, helper string) {
				t.Helper()
				dir := t.TempDir()
				unrelated := "127.0.0.1 localhost\n"
				f := writeFile(t, dir, "hosts", unrelated)

				rc := runRC(t, writeHostsScript, helper, f, "10.0.0.1", "fulcio.example.com")
				require.Equal(t, 0, rc)

				rc = runRC(t, writeHostsScript, helper, f, "10.0.0.2", "fulcio.example.com")
				require.Equal(t, 0, rc)

				content := readFile(t, f)
				// Exactly one line per hostname under the new IP
				require.Contains(t, content, "10.0.0.2 fulcio.example.com\n")
				require.NotContains(t, content, "10.0.0.1 fulcio.example.com\n")
				// Exactly one managed block
				require.Equal(t, 1, countOccurrences(content, blockBegin))
				require.Equal(t, 1, countOccurrences(content, blockEnd))
				// Unrelated line preserved
				require.Contains(t, content, "127.0.0.1 localhost\n")
			},
		},
		{
			name: "conflict_other_ip_rejected",
			fn: func(t *testing.T, helper string) {
				t.Helper()
				dir := t.TempDir()
				initial := "192.168.1.1 fulcio.example.com\n"
				f := writeFile(t, dir, "hosts", initial)

				rc := runRC(t, writeHostsScript, helper, f, "10.0.0.1", "fulcio.example.com")
				require.Equal(t, 1, rc)
				require.Equal(t, initial, readFile(t, f), "file must be byte-identical after conflict")
			},
		},
		{
			name: "same_ip_unmanaged_allowed",
			fn: func(t *testing.T, helper string) {
				t.Helper()
				dir := t.TempDir()
				f := writeFile(t, dir, "hosts", "10.0.0.1 fulcio.example.com\n")

				rc := runRC(t, writeHostsScript, helper, f, "10.0.0.1", "fulcio.example.com")
				require.Equal(t, 0, rc)
			},
		},
		{
			name: "malformed_begin_without_end",
			fn: func(t *testing.T, helper string) {
				t.Helper()
				dir := t.TempDir()
				// BEGIN with no matching END
				initial := "127.0.0.1 localhost\n" +
					"# BEGIN OCM SIGSTORE SCAFFOLDING\n" +
					"10.0.0.1 host.example.com\n"
				f := writeFile(t, dir, "hosts", initial)

				rc := runRC(t, writeHostsScript, helper, f, "10.0.0.1", "host.example.com")
				require.Equal(t, 1, rc)
				require.Equal(t, initial, readFile(t, f))

				rc = runRC(t, removeHostsScript, helper, f)
				require.Equal(t, 1, rc)
				require.Equal(t, initial, readFile(t, f))
			},
		},
		{
			name: "malformed_two_blocks",
			fn: func(t *testing.T, helper string) {
				t.Helper()
				dir := t.TempDir()
				// Two complete managed blocks
				initial := blockBegin + "10.0.0.1 host.example.com\n" + blockEnd +
					blockBegin + "10.0.0.2 other.example.com\n" + blockEnd
				f := writeFile(t, dir, "hosts", initial)

				rc := runRC(t, writeHostsScript, helper, f, "10.0.0.3", "host.example.com")
				require.Equal(t, 1, rc)
				require.Equal(t, initial, readFile(t, f))

				rc = runRC(t, removeHostsScript, helper, f)
				require.Equal(t, 1, rc)
				require.Equal(t, initial, readFile(t, f))
			},
		},
		{
			name: "remove_block_preserves_unrelated",
			fn: func(t *testing.T, helper string) {
				t.Helper()
				dir := t.TempDir()
				initial := "127.0.0.1 localhost\n" +
					blockBegin +
					"10.0.0.1 fulcio.example.com\n" +
					blockEnd +
					"192.168.1.1 other.host\n"
				f := writeFile(t, dir, "hosts", initial)

				rc := runRC(t, removeHostsScript, helper, f)
				require.Equal(t, 0, rc)

				want := "127.0.0.1 localhost\n192.168.1.1 other.host\n"
				require.Equal(t, want, readFile(t, f))
			},
		},
		{
			name: "remove_no_block_is_noop",
			fn: func(t *testing.T, helper string) {
				t.Helper()
				dir := t.TempDir()
				initial := "127.0.0.1 localhost\n"
				f := writeFile(t, dir, "hosts", initial)

				rc := runRC(t, removeHostsScript, helper, f)
				require.Equal(t, 0, rc)
				require.Equal(t, initial, readFile(t, f))
			},
		},
		{
			name: "missing_hosts_file",
			fn: func(t *testing.T, helper string) {
				t.Helper()
				absent := filepath.Join(t.TempDir(), "nonexistent", "hosts")

				rc := runRC(t, writeHostsScript, helper, absent, "10.0.0.1", "host.example.com")
				require.Equal(t, 1, rc)

				_, err := os.Stat(absent)
				require.True(t, os.IsNotExist(err), "file must not be created")
			},
		},
		{
			name: "invalid_ip",
			fn: func(t *testing.T, helper string) {
				t.Helper()
				dir := t.TempDir()
				initial := "127.0.0.1 localhost\n"
				f := writeFile(t, dir, "hosts", initial)

				rc := runRC(t, writeHostsScript, helper, f, "not-an-ip", "host.example.com")
				require.Equal(t, 1, rc)
				require.Equal(t, initial, readFile(t, f))
			},
		},
		{
			name: "invalid_hostname",
			fn: func(t *testing.T, helper string) {
				t.Helper()
				dir := t.TempDir()
				initial := "127.0.0.1 localhost\n"
				f := writeFile(t, dir, "hosts", initial)

				// Leading dash is not a valid hostname character
				rc := runRC(t, writeHostsScript, helper, f, "10.0.0.1", "-bad-hostname")
				require.Equal(t, 1, rc)
				require.Equal(t, initial, readFile(t, f))
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tc.fn(t, helper)
		})
	}
}

// countOccurrences counts non-overlapping occurrences of substr in s.
func countOccurrences(s, substr string) int {
	n := 0
	for i := 0; i <= len(s)-len(substr); {
		if s[i:i+len(substr)] == substr {
			n++
			i += len(substr)
		} else {
			i++
		}
	}
	return n
}
