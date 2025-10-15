package oci

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"ocm.software/open-component-model/bindings/go/oci/spec/repository"
	ctfv1 "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/ctf"
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/componentlister"
	"ocm.software/open-component-model/bindings/go/runtime"
)

func TestCTFComponentListerPluginRegistration(t *testing.T) {
	// Setup.
	ctx := context.Background()
	scheme := runtime.NewScheme()
	repository.MustAddToScheme(scheme)
	registry := componentlister.NewComponentListerRegistry(ctx)
	p := &CTFComponentListerPlugin{}
	require.NoError(t, componentlister.RegisterInternalComponentListerPlugin(scheme, registry, p, &ctfv1.Repository{}))

	// Smoke test: try to retrieve a lister for a non-existing CTF repo.
	// We expect "path does not exist" error, meaning that the plug-in was found and tied to read the CTF.
	ctfSpec := &ctfv1.Repository{Path: "/non/existing/path"}
	_, err := registry.GetComponentLister(ctx, ctfSpec, nil)
	require.Error(t, err)
	expectedErr := "path does not exist: /non/existing/path"
	require.Truef(t, strings.Contains(err.Error(), expectedErr), "returned error '%s' does not contain expected '%s'", err.Error(), expectedErr)
}
