package nexustest_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/internal/testutil/nexustest"
)

func Test_Integration_Start(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping Nexus integration test in short mode")
	}
	r := require.New(t)

	nexus := nexustest.Start(t, nexustest.WithExposedPorts("5000/tcp"))
	r.NotEmpty(nexus.Password)

	status, body := nexustest.Request(t, http.MethodGet, nexus.BaseURL+"/service/rest/v1/status/writable", nexus.Password, nil)
	r.Equal(http.StatusOK, status, string(body))

	endpoint, err := nexus.Container.PortEndpoint(t.Context(), "5000/tcp", "")
	r.NoError(err)
	r.NotEmpty(endpoint)
}
