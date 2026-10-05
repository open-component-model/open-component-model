//go:build !windows

package download

import (
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/ssh/agent"

	accessv1 "ocm.software/open-component-model/bindings/go/git/spec/access/v1"
	credsv1 "ocm.software/open-component-model/bindings/go/git/spec/credentials/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

func TestDownloadClosesSSHAgentOnFailure(t *testing.T) {
	for _, tc := range []struct {
		name  string
		creds runtime.Typed
	}{
		{name: "implicit agent"},
		{name: "explicit agent", creds: &credsv1.GitSSHCredentials{Username: "git"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := require.New(t)
			// Unix socket path limits can be exceeded by t.TempDir on macOS.
			dir, err := os.MkdirTemp("", "ocm-agent-")
			r.NoError(err)
			t.Cleanup(func() { _ = os.RemoveAll(dir) })
			listener, err := net.Listen("unix", filepath.Join(dir, "agent.sock"))
			r.NoError(err)
			t.Cleanup(func() { _ = listener.Close() })
			closed := make(chan struct{}, 1)
			go func() {
				conn, err := listener.Accept()
				if err != nil {
					return
				}
				t.Cleanup(func() { _ = conn.Close() })
				defer conn.Close()
				_ = agent.ServeAgent(agent.NewKeyring(), conn)
				closed <- struct{}{}
			}()
			t.Setenv("SSH_AUTH_SOCK", listener.Addr().String())
			_, err = Download(t.Context(), &accessv1.Git{Repository: "ssh://git@127.0.0.1/repo", Ref: "HEAD"}, tc.creds, Options{TempDir: filepath.Join(t.TempDir(), "missing", "directory")})
			r.ErrorContains(err, "cannot create git storage")
			select {
			case <-closed:
			case <-time.After(2 * time.Second):
				t.Fatal("SSH agent connection remained open after Download failed")
			}
		})
	}
}
