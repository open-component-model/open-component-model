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
	"net/http"
	"net/http/cgi"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
	"golang.org/x/crypto/ssh"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/git/repository"
	accessv1 "ocm.software/open-component-model/bindings/go/git/spec/access/v1"
	credsv1 "ocm.software/open-component-model/bindings/go/git/spec/credentials/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

func Test_Integration_GitHTTPSAuthentication(t *testing.T) {
	r := require.New(t)

	fixture := newRepository(t)
	executable, err := exec.LookPath("git")
	r.NoError(err)

	authMethods := []struct {
		name               string
		authorization      string
		credentials        *credsv1.GitCredentials
		invalidCredentials *credsv1.GitCredentials
	}{
		{
			name:               "token",
			authorization:      "Bearer fixture-token",
			credentials:        &credsv1.GitCredentials{Token: "fixture-token"},
			invalidCredentials: &credsv1.GitCredentials{Token: "wrong-secret"},
		},
		{
			name:               "basic",
			authorization:      "Basic " + base64.StdEncoding.EncodeToString([]byte("fixture-user:fixture-password")),
			credentials:        &credsv1.GitCredentials{Username: "fixture-user", Password: "fixture-password"},
			invalidCredentials: &credsv1.GitCredentials{Username: "fixture-user", Password: "wrong-secret"},
		},
	}

	scenarios := []struct {
		name               string
		invalidCredentials bool
		trustCertificate   bool
		wantError          bool
	}{
		{name: "valid credentials", trustCertificate: true},
		{
			name:               "invalid credentials",
			invalidCredentials: true,
			trustCertificate:   true,
			wantError:          true,
		},
		{name: "untrusted certificate", wantError: true},
	}

	for _, method := range authMethods {
		t.Run(method.name, func(t *testing.T) {
			backend := &cgi.Handler{
				Path: executable,
				Args: []string{"http-backend"},
				Env:  []string{"GIT_PROJECT_ROOT=" + filepath.Dir(fixture.Path), "GIT_HTTP_EXPORT_ALL=1"},
			}

			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				if req.Header.Get("Authorization") != method.authorization {
					w.WriteHeader(http.StatusUnauthorized)
					return
				}

				backend.ServeHTTP(w, req)
			}))
			t.Cleanup(server.Close)

			ca := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw})
			spec := &descriptor.Resource{
				Access: &accessv1.Git{
					Type:       runtime.NewVersionedType("Git", "v1"),
					Repository: server.URL + "/" + filepath.Base(fixture.Path),
					Ref:        "main",
				},
			}

			for _, scenario := range scenarios {
				t.Run(scenario.name, func(t *testing.T) {
					r := require.New(t)

					creds := method.credentials
					if scenario.invalidCredentials {
						creds = method.invalidCredentials
					}

					opts := []repository.Option{repository.WithTempDir(t.TempDir())}
					if scenario.trustCertificate {
						opts = append(opts, repository.WithCABundle(ca))
					}

					repo := repository.NewResourceRepository(opts...)
					b, downloadErr := repo.DownloadResource(t.Context(), spec, creds)
					if !scenario.wantError {
						r.NoError(downloadErr)
						assertArchive(t, b, "second\n")
					} else {
						r.Error(downloadErr)
						r.Nil(b)
						r.NotContains(downloadErr.Error(), "wrong-secret")
					}

					pinned, digestErr := repo.ProcessResourceDigest(t.Context(), spec, creds)
					if scenario.wantError {
						r.Error(digestErr)
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
}

func Test_Integration_GitSSHAuthentication(t *testing.T) {
	r := require.New(t)

	fixture := newRepository(t)
	public, private, err := ed25519.GenerateKey(rand.Reader)
	r.NoError(err)

	clientPublic, err := ssh.NewPublicKey(public)
	r.NoError(err)

	repoPath := "/srv/" + filepath.Base(fixture.Path)
	container, err := testcontainers.GenericContainer(t.Context(), testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        "rockstorm/git-server:2.38",
			Cmd:          []string{"/usr/sbin/sshd", "-D", "-e", "-h", "/etc/ssh/ssh_host_ed25519_key"},
			Env:          map[string]string{"GIT_REPOSITORIES_PATH": "/srv"},
			ExposedPorts: []string{"22/tcp"},
			Files: []testcontainers.ContainerFile{
				{HostFilePath: fixture.Path, ContainerFilePath: repoPath, FileMode: 0o700},
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

	for _, encrypted := range []bool{false, true} {
		t.Run(strconv.FormatBool(encrypted), func(t *testing.T) {
			r := require.New(t)

			passphrase := ""
			var block *pem.Block
			var err error
			if encrypted {
				passphrase = "fixture-passphrase"
				block, err = ssh.MarshalPrivateKeyWithPassphrase(private, "fixture", []byte(passphrase))
			} else {
				block, err = ssh.MarshalPrivateKey(private, "fixture")
			}
			r.NoError(err)

			keyPath := filepath.Join(t.TempDir(), "key")
			r.NoError(os.WriteFile(keyPath, pem.EncodeToMemory(block), 0o600))

			gitCreds := &credsv1.GitCredentials{PrivateKey: keyPath, Password: passphrase, Token: "ignored-by-key-precedence"}
			spec := &descriptor.Resource{
				Access: &accessv1.Git{
					Type:       runtime.NewVersionedType("Git", "v1"),
					Repository: fmt.Sprintf("ssh://git@%s%s", net.JoinHostPort(host, port.Port()), repoPath),
					Ref:        "main",
				},
			}
			repo := repository.NewResourceRepository(repository.WithHostKeyCallback(ssh.FixedHostKey(hostKey)), repository.WithTempDir(t.TempDir()))
			b, err := repo.DownloadResource(t.Context(), spec, gitCreds)
			r.NoError(err)
			assertArchive(t, b, "second\n")

			_, err = repo.ProcessResourceDigest(t.Context(), spec, gitCreds)
			r.NoError(err)

			reject := repository.NewResourceRepository(repository.WithHostKeyCallback(ssh.FixedHostKey(clientPublic)), repository.WithTempDir(t.TempDir()))
			_, err = reject.DownloadResource(t.Context(), spec, gitCreds)
			r.Error(err)
		})
	}
}
