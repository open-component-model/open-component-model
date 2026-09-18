package git

import (
	"testing"

	"github.com/go-git/go-git/v5/plumbing/transport"
	gitclient "github.com/go-git/go-git/v5/plumbing/transport/client"
	githttp "github.com/go-git/go-git/v5/plumbing/transport/http"
	"github.com/stretchr/testify/require"

	filesystemv1alpha1 "ocm.software/open-component-model/bindings/go/configuration/filesystem/v1alpha1/spec"
	gitrepository "ocm.software/open-component-model/bindings/go/git/repository"
	accessv1 "ocm.software/open-component-model/bindings/go/git/spec/access/v1"
	httpv1alpha1 "ocm.software/open-component-model/bindings/go/http/spec/config/v1alpha1"
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/credentialrepository"
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/digestprocessor"
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/resource"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// Both the current and the ocmv1 spelling of the access type must reach the built-in repository.
func TestRegister_ResolvesGitAccess(t *testing.T) {
	ctx := t.Context()

	resources := resource.NewResourceRegistry(ctx)
	require.NoError(t, Register(
		resources,
		digestprocessor.NewDigestProcessorRegistry(ctx),
		credentialrepository.NewCredentialRepositoryRegistry(ctx),
		&filesystemv1alpha1.Config{},
		&httpv1alpha1.Config{},
	))

	for _, typ := range []runtime.Type{
		runtime.NewVersionedType(accessv1.Type, accessv1.Version),
		runtime.NewVersionedType(accessv1.LegacyType, "v1alpha1"),
	} {
		plugin, err := resources.GetResourcePlugin(ctx, &accessv1.Git{Type: typ, Repository: "https://example.com/repo.git", Ref: "main"})
		require.NoError(t, err, typ.String())
		require.IsType(t, &gitrepository.ResourceRepository{}, plugin, typ.String())
	}
}

// The configured HTTP client must replace go-git's default for http(s) remotes.
func TestRegister_InstallsHTTPClient(t *testing.T) {
	ctx := t.Context()

	require.NoError(t, Register(
		resource.NewResourceRegistry(ctx),
		digestprocessor.NewDigestProcessorRegistry(ctx),
		credentialrepository.NewCredentialRepositoryRegistry(ctx),
		&filesystemv1alpha1.Config{},
		&httpv1alpha1.Config{},
	))

	for _, protocol := range []string{"http", "https"} {
		installed, err := gitclient.NewClient(&transport.Endpoint{Protocol: protocol, Host: "example.com", Path: "/repo.git"})
		require.NoError(t, err, protocol)
		require.NotSame(t, githttp.DefaultClient, installed, protocol)
	}
}
