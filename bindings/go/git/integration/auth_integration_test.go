package integration_test

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"

	filesystemv1alpha1 "ocm.software/open-component-model/bindings/go/configuration/filesystem/v1alpha1/spec"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/git/repository"
	accessv1 "ocm.software/open-component-model/bindings/go/git/spec/access/v1"
	credsv1 "ocm.software/open-component-model/bindings/go/git/spec/credentials/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

func Test_Integration_GitHTTPSAuthentication(t *testing.T) {
	path, first := newRepository(t)

	credsType := runtime.NewVersionedType(credsv1.GitCredentialsType, credsv1.Version)
	authMethods := []struct {
		name               string
		authorization      string
		credentials        *credsv1.GitCredentials
		invalidCredentials *credsv1.GitCredentials
	}{
		{
			name:               "token",
			authorization:      "Bearer fixture-token",
			credentials:        &credsv1.GitCredentials{Type: credsType, Token: "fixture-token"},
			invalidCredentials: &credsv1.GitCredentials{Type: credsType, Token: "wrong-secret"},
		},
		{
			name:               "basic",
			authorization:      "Basic " + base64.StdEncoding.EncodeToString([]byte("fixture-user:fixture-password")),
			credentials:        &credsv1.GitCredentials{Type: credsType, Username: "fixture-user", Password: "fixture-password"},
			invalidCredentials: &credsv1.GitCredentials{Type: credsType, Username: "fixture-user", Password: "wrong-secret"},
		},
	}

	scenarios := []struct {
		name               string
		invalidCredentials bool
		trustCertificate   bool
		expectedError      string
	}{
		{name: "valid credentials", trustCertificate: true},
		{
			name:               "invalid credentials",
			invalidCredentials: true,
			trustCertificate:   true,
			expectedError:      "authentication required",
		},
		{name: "untrusted certificate", expectedError: "x509: certificate signed by unknown authority"},
	}

	for _, method := range authMethods {
		t.Run(method.name, func(t *testing.T) {
			url, ca := newHTTPSServer(t, path, method.authorization)
			for _, revision := range []struct{ name, ref, commit, content string }{
				{"clone for ref", "HEAD", "", "second\n"},
				{"fetch for pinned commit", "", first.String(), "first\n"},
			} {
				t.Run(revision.name, func(t *testing.T) {
					spec := &descriptor.Resource{
						Access: &accessv1.Git{
							Type:       runtime.NewVersionedType("Git", "v1"),
							Repository: url,
							Ref:        revision.ref,
							Commit:     revision.commit,
						},
					}

					for _, scenario := range scenarios {
						t.Run(scenario.name, func(t *testing.T) {
							r := require.New(t)

							creds := method.credentials
							if scenario.invalidCredentials {
								creds = method.invalidCredentials
							}

							if scenario.trustCertificate {
								trustServerCertificate(t, ca)
							}

							tempDir := t.TempDir()
							repo := repository.NewResourceRepository(&filesystemv1alpha1.Config{TempFolder: &tempDir})
							b, downloadErr := repo.DownloadResource(t.Context(), spec, creds)
							if scenario.expectedError == "" {
								r.NoError(downloadErr)
								assertArchive(t, b, revision.content)
							} else {
								r.ErrorContains(downloadErr, scenario.expectedError)
								r.Nil(b)
								r.NotContains(downloadErr.Error(), "wrong-secret")
							}

							pinned, digestErr := repo.ProcessResourceDigest(t.Context(), spec, creds)
							if scenario.expectedError != "" {
								r.ErrorContains(digestErr, scenario.expectedError)
								r.Nil(pinned)
								r.NotContains(digestErr.Error(), "wrong-secret")
								return
							}

							r.NoError(digestErr)
							r.NotNil(pinned.Digest)
						})
					}
				})
			}
		})
	}
}

func Test_Integration_GitSSHAuthentication(t *testing.T) {
	r := require.New(t)

	path, first := newRepository(t)
	public, private, err := ed25519.GenerateKey(rand.Reader)
	r.NoError(err)

	clientPublic, err := ssh.NewPublicKey(public)
	r.NoError(err)

	repoPath := "/srv/" + filepath.Base(path)
	container, err := testcontainers.GenericContainer(t.Context(), testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        "rockstorm/git-server:2.38",
			Cmd:          []string{"/usr/sbin/sshd", "-D", "-e", "-h", "/etc/ssh/ssh_host_ed25519_key"},
			Env:          map[string]string{"GIT_REPOSITORIES_PATH": "/srv"},
			ExposedPorts: []string{"22/tcp"},
			Files: []testcontainers.ContainerFile{
				{HostFilePath: path, ContainerFilePath: repoPath, FileMode: 0o700},
				{
					Reader:            bytes.NewReader(ssh.MarshalAuthorizedKey(clientPublic)),
					ContainerFilePath: "/home/git/.ssh/authorized_keys",
					FileMode:          0o600,
				},
			},
			WaitingFor: wait.ForListeningPort("22/tcp").WithStartupTimeout(30 * time.Second),
		},
		Started: true,
	})
	r.NoError(err)
	t.Cleanup(func() {
		if t.Failed() {
			logs, err := container.Logs(context.WithoutCancel(t.Context()))
			if err == nil {
				data, _ := io.ReadAll(logs)
				_ = logs.Close()
				t.Logf("git server logs:\n%s", data)
			}
		}
		r.NoError(testcontainers.TerminateContainer(container))
	})

	host, err := container.Host(t.Context())
	r.NoError(err)

	port, err := container.MappedPort(t.Context(), "22/tcp")
	r.NoError(err)

	publicKeyFile, err := container.CopyFileFromContainer(t.Context(), "/etc/ssh/ssh_host_ed25519_key.pub")
	r.NoError(err)

	publicKeyData, err := io.ReadAll(publicKeyFile)
	r.NoError(err)
	r.NoError(publicKeyFile.Close())

	hostKey, _, _, _, err := ssh.ParseAuthorizedKey(publicKeyData)
	r.NoError(err)

	assertSSHAccess := func(t *testing.T, gitCreds runtime.Typed) {
		t.Helper()

		for _, revision := range []struct{ name, ref, commit, content string }{
			{"clone for ref", "HEAD", "", "second\n"},
			{"fetch for pinned commit", "", first.String(), "first\n"},
		} {
			t.Run(revision.name, func(t *testing.T) {
				r := require.New(t)

				spec := &descriptor.Resource{
					Access: &accessv1.Git{
						Type:       runtime.NewVersionedType("Git", "v1"),
						Repository: fmt.Sprintf("ssh://git@%s%s", net.JoinHostPort(host, port.Port()), repoPath),
						Ref:        revision.ref,
						Commit:     revision.commit,
					},
				}
				tempDir := t.TempDir()
				repo := repository.NewResourceRepository(&filesystemv1alpha1.Config{TempFolder: &tempDir}, repository.WithHostKeyCallback(ssh.FixedHostKey(hostKey)))
				b, err := repo.DownloadResource(t.Context(), spec, gitCreds)
				r.NoError(err)
				assertArchive(t, b, revision.content)

				_, err = repo.ProcessResourceDigest(t.Context(), spec, gitCreds)
				r.NoError(err)

				rejectDir := t.TempDir()
				reject := repository.NewResourceRepository(&filesystemv1alpha1.Config{TempFolder: &rejectDir}, repository.WithHostKeyCallback(ssh.FixedHostKey(clientPublic)))
				b, err = reject.DownloadResource(t.Context(), spec, gitCreds)
				r.ErrorContains(err, "ssh: host key mismatch")
				r.Nil(b)

				pinned, err := reject.ProcessResourceDigest(t.Context(), spec, gitCreds)
				r.ErrorContains(err, "ssh: host key mismatch")
				r.Nil(pinned)
			})
		}
	}

	for _, encrypted := range []bool{false, true} {
		passphrase := ""
		var block *pem.Block
		if encrypted {
			passphrase = "fixture-passphrase"
			block, err = ssh.MarshalPrivateKeyWithPassphrase(private, "fixture", []byte(passphrase))
		} else {
			block, err = ssh.MarshalPrivateKey(private, "fixture")
		}
		r.NoError(err)
		keyPEM := pem.EncodeToMemory(block)

		t.Run(fmt.Sprintf("file/encrypted=%t", encrypted), func(t *testing.T) {
			keyPath := filepath.Join(t.TempDir(), "key")
			require.NoError(t, os.WriteFile(keyPath, keyPEM, 0o600))
			assertSSHAccess(t, &credsv1.GitCredentials{Type: runtime.NewVersionedType(credsv1.GitCredentialsType, credsv1.Version), PrivateKey: keyPath, Password: passphrase, Token: "ignored-by-key-precedence"})
		})

		t.Run(fmt.Sprintf("inline/encrypted=%t", encrypted), func(t *testing.T) {
			assertSSHAccess(t, &credsv1.GitCredentials{Type: runtime.NewVersionedType(credsv1.GitCredentialsType, credsv1.Version), PrivateKeyPEM: string(keyPEM), PrivateKey: "/ignored/by/inline/precedence", Password: passphrase})
		})
	}

	t.Run("agent", func(t *testing.T) {
		r := require.New(t)

		keyring := agent.NewKeyring()
		r.NoError(keyring.Add(agent.AddedKey{PrivateKey: private}))

		// Unix socket paths are limited to about 104 bytes, which t.TempDir can exceed on macOS.
		socketDir, err := os.MkdirTemp("", "ocm-agent-")
		r.NoError(err)
		t.Cleanup(func() { _ = os.RemoveAll(socketDir) })

		listener, err := net.Listen("unix", filepath.Join(socketDir, "agent.sock"))
		r.NoError(err)
		t.Cleanup(func() { _ = listener.Close() })
		go func() {
			for {
				conn, err := listener.Accept()
				if err != nil {
					return
				}
				go func() {
					defer func() { _ = conn.Close() }()
					_ = agent.ServeAgent(keyring, conn)
				}()
			}
		}()

		t.Setenv("SSH_AUTH_SOCK", listener.Addr().String())
		assertSSHAccess(t, nil)
	})
}
