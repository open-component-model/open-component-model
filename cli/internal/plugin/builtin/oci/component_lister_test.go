package oci

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"ocm.software/open-component-model/bindings/go/oci/spec/repository"
	ctfv1 "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/ctf"
	ociv1 "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/oci"
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/componentlister"
	"ocm.software/open-component-model/bindings/go/runtime"
)

func TestCTFComponentListerPlugin_Registration(t *testing.T) {
	// Setup.
	ctx := t.Context()
	scheme := runtime.NewScheme()
	repository.MustAddToScheme(scheme)
	registry := componentlister.NewComponentListerRegistry(ctx)
	p := &CTFComponentListerPlugin{}
	require.NoError(t, componentlister.RegisterInternalComponentListerPlugin(scheme, registry, p, &ctfv1.Repository{}))

	// Smoke test: try to retrieve a lister for a non-existing CTF repo.
	// We expect "path does not exist" error, meaning that the plug-in was found and tried to read the CTF.
	ctfSpec := &ctfv1.Repository{FilePath: "/non/existing/path"}
	_, err := registry.GetComponentLister(ctx, ctfSpec, nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "path does not exist: /non/existing/path")
}

func TestCTFComponentListerPlugin_CredentialConsumerIdentity(t *testing.T) {
	// Setup.
	ctx := t.Context()
	scheme := runtime.NewScheme()
	repository.MustAddToScheme(scheme)
	registry := componentlister.NewComponentListerRegistry(ctx)
	p := &CTFComponentListerPlugin{}
	require.NoError(t, componentlister.RegisterInternalComponentListerPlugin(scheme, registry, p, &ctfv1.Repository{}))

	// Credentials not supported. An error expected.
	ctfSpec := &ctfv1.Repository{}
	id, err := registry.GetComponentListerCredentialConsumerIdentity(t.Context(), ctfSpec)
	require.Nil(t, id)
	require.Error(t, err)
	require.True(t, errors.Is(err, ErrWrongUsage), "expected: %v, got: %v", ErrWrongUsage, err)
}

func TestCTFComponentListerPlugin_NotCTFSpec(t *testing.T) {
	p := &CTFComponentListerPlugin{}

	// Try to get a lister for a non-CTF repository spec.
	_, err := p.GetComponentLister(t.Context(), &ociv1.Repository{}, nil)
	require.Error(t, err)
	require.True(t, errors.Is(err, ErrWrongUsage), "expected: %v, got: %v", ErrWrongUsage, err)
}
