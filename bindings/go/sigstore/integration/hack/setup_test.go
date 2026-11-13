package hack_test

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
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

// writeHostsScript sources the helper and calls write_scaffolding_hosts.
// $1=HOSTS_FILE, $2=IP, $3…=HOSTNAMES
const writeHostsScript = `. "$0"; write_scaffolding_hosts "$1" "$2" "${@:3}"`

// removeHostsScript sources the helper and calls remove_scaffolding_hosts.
const removeHostsScript = `. "$0"; remove_scaffolding_hosts "$1"`

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
