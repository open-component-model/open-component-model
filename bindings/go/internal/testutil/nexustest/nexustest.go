// Package nexustest starts and talks to a Sonatype Nexus server in integration tests.
//
// [Start] needs Docker, accepts the EULA, terminates Nexus when the test ends, and returns
// its base URL and admin password. Repositories and realms stay with the caller:
//
//	nexus := nexustest.Start(t, nexustest.WithExposedPorts("5000/tcp"))
//
// [Request] sends one request as the admin user and returns the status code and the
// response body. A non-nil body is sent as JSON:
//
//	status, body := nexustest.Request(t, http.MethodPost, url, nexus.Password, jsonBody)
package nexustest

import (
	"bytes"
	"io"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

// Request sends body to target as the Nexus admin with the given password and returns the
// status code and the response body. A non-nil body is sent as JSON.
func Request(t *testing.T, method, target, password string, body []byte) (int, []byte) {
	t.Helper()
	r := require.New(t)

	req, err := http.NewRequestWithContext(t.Context(), method, target, bytes.NewReader(body))
	r.NoError(err)
	req.SetBasicAuth("admin", password)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := http.DefaultClient.Do(req)
	r.NoError(err)
	defer func() { _ = resp.Body.Close() }()
	respBody, err := io.ReadAll(resp.Body)
	r.NoError(err)
	return resp.StatusCode, respBody
}
