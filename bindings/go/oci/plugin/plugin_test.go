package plugin_test

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	v1 "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1"
	"ocm.software/open-component-model/bindings/go/plugin/manager"
)

func TestPlugin(t *testing.T) {
	ctx := t.Context()
	r := require.New(t)

	spec := v1.OCIRepository{
		BaseUrl: "ghcr.io/open-component-model/ocm",
	}

	repo, err := manager.GetReadWriteComponentVersionRepository[*v1.OCIRepository](ctx, &spec)
	r.NoError(err)

	user, pass := getUserAndPasswordWithGitHubCLIAndJQ(t)

	request := manager.GetComponentVersionRequest[*v1.OCIRepository]{
		Repository: &spec,
		Name:       "ocm.software/ocmcli",
		Version:    "0.22.1",
	}

	desc, err := repo.GetComponentVersion(ctx, request, manager.Attributes{
		"username": manager.Attribute(user),
		"password": manager.Attribute(pass),
	})

	r.NoError(err)
	r.Equal(request.Name, desc.Component.Name)
}

func getUserAndPasswordWithGitHubCLIAndJQ(t *testing.T) (string, string) {
	t.Helper()
	gh, err := exec.LookPath("gh")
	if err != nil {
		t.Skip("gh CLI not found, skipping test")
	}

	out, err := exec.CommandContext(t.Context(), "sh", "-c", fmt.Sprintf("%s api user", gh)).CombinedOutput()
	if err != nil {
		t.Skipf("gh CLI for user failed: %v", err)
	}
	structured := map[string]interface{}{}
	if err := json.Unmarshal(out, &structured); err != nil {
		t.Skipf("gh CLI for user failed: %v", err)
	}
	user := structured["login"].(string)

	pw := exec.CommandContext(t.Context(), "sh", "-c", fmt.Sprintf("%s auth token", gh))
	if out, err = pw.CombinedOutput(); err != nil {
		t.Skipf("gh CLI for password failed: %v", err)
	}
	password := strings.TrimSpace(string(out))

	return user, password
}
