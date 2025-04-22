package plugin_test

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	v1 "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1"
	"ocm.software/open-component-model/bindings/go/plugin/manager/contracts"
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/componentversionrepository"
	"ocm.software/open-component-model/bindings/go/plugin/manager/types"
)

func TestPlugin(t *testing.T) {
	ctx := t.Context()
	r := require.New(t)

	spec := v1.OCIRepository{
		BaseUrl: "ghcr.io/open-component-model/ocm",
	}

	// The schema doesn't matter here, but this registry is required because Lock/Unlock is being called.
	// But that lock unlock will make for a poor lock since that will not work with multiple calls from
	// DIFFERENT registries.
	registry := componentversionrepository.NewComponentVersionRepositoryRegistry(nil)
	repo, err := componentversionrepository.GetReadWriteComponentVersionRepositoryPluginForType(ctx, registry, &spec)
	r.NoError(err)

	user, pass := getUserAndPasswordWithGitHubCLIAndJQ(t)

	request := types.GetComponentVersionRequest[*v1.OCIRepository]{
		Repository: &spec,
		Name:       "ocm.software/ocmcli",
		Version:    "0.22.1",
	}

	desc, err := repo.GetComponentVersion(ctx, request, contracts.Attributes{
		"username": contracts.Attribute(user),
		"password": contracts.Attribute(pass),
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
