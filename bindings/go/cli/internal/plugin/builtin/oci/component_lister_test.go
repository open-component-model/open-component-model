package oci

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	identityv1 "ocm.software/open-component-model/bindings/go/oci/spec/identity/v1"
	"ocm.software/open-component-model/bindings/go/oci/spec/repository"
	ctfv1 "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/ctf"
	ociv1 "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/oci"
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/componentlister"
	"ocm.software/open-component-model/bindings/go/runtime"
)

func TestOCIComponentListerPlugin_Registration(t *testing.T) {
	// Setup.
	ctx := t.Context()
	scheme := runtime.NewScheme()
	repository.MustAddToScheme(scheme)
	registry := componentlister.NewComponentListerRegistry(ctx)
	p := NewComponentListerPlugin(nil, nil)
	require.NoError(t, registry.RegisterInternalComponentListerPlugin(p))

	// Smoke test: try to retrieve a lister for a non-existing CTF repo.
	// We expect "path does not exist" error, meaning that the plug-in was found and tried to read the CTF.
	nonExisting := filepath.Join(t.TempDir(), "non", "existing", "path")
	ctfSpec := &ctfv1.Repository{FilePath: nonExisting}
	_, err := registry.GetComponentLister(ctx, ctfSpec, nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "path does not exist: "+nonExisting)
}

func TestOCIComponentListerPlugin_CTFCredentialConsumerIdentity(t *testing.T) {
	// Setup.
	ctx := t.Context()
	scheme := runtime.NewScheme()
	repository.MustAddToScheme(scheme)
	registry := componentlister.NewComponentListerRegistry(ctx)
	p := NewComponentListerPlugin(nil, nil)
	require.NoError(t, registry.RegisterInternalComponentListerPlugin(p))

	// Credentials not supported for CTF. An error is expected.
	ctfSpec := &ctfv1.Repository{}
	id, err := registry.GetComponentListerCredentialConsumerIdentity(t.Context(), ctfSpec)
	require.Nil(t, id)
	require.Error(t, err)
	require.ErrorIs(t, err, ErrWrongUsage, "expected: %v, got: %v", ErrWrongUsage, err)
}

func TestOCIComponentListerPlugin_OCICredentialConsumerIdentity(t *testing.T) {
	p := NewComponentListerPlugin(nil, nil)
	ociSpec := &ociv1.Repository{BaseUrl: "ghcr.io", SubPath: "open-component-model/ocm"}
	id, err := p.GetComponentListerCredentialConsumerIdentity(t.Context(), ociSpec)
	require.NoError(t, err)
	require.Equal(t, "ghcr.io", id[runtime.IdentityAttributeHostname])
	require.Equal(t, "open-component-model/ocm", id[runtime.IdentityAttributePath])
	require.Equal(t, identityv1.Type, id.GetType())
}

func TestOCIComponentListerPlugin_OCISpecBuildsLister(t *testing.T) {
	p := NewComponentListerPlugin(nil, nil)
	// A valid OCI spec yields a lister without contacting the registry.
	ociSpec := &ociv1.Repository{BaseUrl: "ghcr.io", SubPath: "open-component-model/ocm"}
	lister, err := p.GetComponentLister(t.Context(), ociSpec, nil)
	require.NoError(t, err)
	require.NotNil(t, lister)
}

func TestOCIComponentListerPlugin_UnsupportedSpec(t *testing.T) {
	p := NewComponentListerPlugin(nil, nil)
	_, err := p.GetComponentLister(t.Context(), &unsupportedSpec{}, nil)
	require.Error(t, err)
	require.ErrorIs(t, err, ErrWrongUsage, "expected: %v, got: %v", ErrWrongUsage, err)
}

// unsupportedSpec is a runtime.Typed the lister plugin does not handle.
type unsupportedSpec struct{}

func (s *unsupportedSpec) GetType() runtime.Type        { return runtime.Type{} }
func (s *unsupportedSpec) SetType(runtime.Type)         {}
func (s *unsupportedSpec) DeepCopyTyped() runtime.Typed { return &unsupportedSpec{} }

func TestOCIComponentListerPlugin_Archive(t *testing.T) {
	root := getRepoRootBasedOnGit(t)
	for _, name := range []string{"transport-archive.tar", "transport-archive.tar.gz"} {
		t.Run(name, func(t *testing.T) {
			r := require.New(t)
			path := filepath.Join(root, "bindings", "go", "oci", "ctf", "testdata", "compatibility", "01", name)
			lister, err := NewComponentListerPlugin(nil, nil).GetComponentLister(t.Context(), &ctfv1.Repository{FilePath: path}, nil)
			r.NoError(err)
			var names []string
			r.NoError(lister.ListComponents(t.Context(), "", func(found []string) error {
				names = found
				return nil
			}))
			r.Equal([]string{"github.com/acme.org/helloworld"}, names)
		})
	}
}

func getRepoRootBasedOnGit(t *testing.T) string {
	t.Helper()
	r := require.New(t)
	git, err := exec.LookPath("git")
	r.NoError(err, "git binary should be available in PATH to locate test data")
	rootRaw, err := exec.CommandContext(t.Context(), git, "rev-parse", "--show-toplevel").Output()
	r.NoError(err, "git rev-parse --show-toplevel must succeed to get repository root")
	return strings.TrimSpace(string(rootRaw))
}
