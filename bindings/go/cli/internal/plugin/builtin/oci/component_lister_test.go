package oci

import (
	"encoding/json/v2"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	httpv1alpha1 "ocm.software/open-component-model/bindings/go/http/spec/config/v1alpha1"
	identityv1 "ocm.software/open-component-model/bindings/go/oci/spec/identity/v1"
	"ocm.software/open-component-model/bindings/go/oci/spec/repository"
	ctfv1 "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/ctf"
	ociv1 "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/oci"
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/componentlister"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// TestOCIComponentListerPlugin_RawOCISpecListsViaCatalog drives the plugin with
// an OCI spec delivered as *runtime.Raw, so the scheme conversion path runs,
// and lists components from a fake /v2/_catalog registry. It covers the plugin,
// the raw-spec conversion, the subPath filter and the catalog listing together.
func TestOCIComponentListerPlugin_RawOCISpecListsViaCatalog(t *testing.T) {
	repos := []string{
		"aaa/before-the-block",
		"my/org/component-descriptors/comp-a",
		"my/org/component-descriptors/comp-b",
		"zzz/after-the-block",
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.MarshalWrite(w, struct {
			Repositories []string `json:"repositories"`
		}{Repositories: repos})
	}))
	t.Cleanup(srv.Close)

	host := strings.TrimPrefix(srv.URL, "http://")
	plainHTTP := &httpv1alpha1.Config{ProtocolConfig: httpv1alpha1.ProtocolConfig{PlainHTTP: new(true)}}
	p := NewComponentListerPlugin(plainHTTP, nil)

	// Build the spec as *runtime.Raw, as it arrives from configuration.
	spec := &ociv1.Repository{BaseUrl: host, SubPath: "my/org"}
	_, _ = repository.Scheme.DefaultType(spec)
	data, err := json.Marshal(spec, runtime.JSONOptions)
	require.NoError(t, err)
	raw := &runtime.Raw{}
	require.NoError(t, json.Unmarshal(data, raw, runtime.JSONOptions))

	lister, err := p.GetComponentLister(t.Context(), raw, nil)
	require.NoError(t, err)

	var names []string
	require.NoError(t, lister.ListComponents(t.Context(), "", func(found []string) error {
		names = append(names, found...)
		return nil
	}))

	assert.Equal(t, []string{"comp-a", "comp-b"}, names)
}

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

	// CTF archives need no credentials, so the identity is nil without an error.
	// The caller reads a nil identity as "list without credentials".
	ctfSpec := &ctfv1.Repository{}
	id, err := registry.GetComponentListerCredentialConsumerIdentity(t.Context(), ctfSpec)
	require.NoError(t, err)
	require.Nil(t, id)
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
