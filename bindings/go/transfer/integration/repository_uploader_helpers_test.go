package integration_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/credentials"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/oci/repository/provider"
	ctfrepospec "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/ctf"
	"ocm.software/open-component-model/bindings/go/repository"
	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/bindings/go/transfer"
	transferv1alpha1 "ocm.software/open-component-model/bindings/go/transfer/v1alpha1/spec"
	wgetaccess "ocm.software/open-component-model/bindings/go/wget/spec/access"
	wgetaccessv1 "ocm.software/open-component-model/bindings/go/wget/spec/access/v1"
)

// wgetFile is served over HTTP under its name and described as a Wget/v1 resource.
type wgetFile struct {
	name, resource, version, mediaType string
	data                               []byte
}

// addWgetComponent serves files from an HTTP server and adds a component version to a new
// source CTF with one Wget/v1 resource per file, each carrying the SHA-256 of its content.
func addWgetComponent(t *testing.T, component, version string, files ...wgetFile) (repository.ComponentVersionRepository, runtime.Typed) {
	t.Helper()
	r := require.New(t)

	mux := http.NewServeMux()
	for _, f := range files {
		mux.HandleFunc("/"+f.name, func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", f.mediaType)
			_, _ = w.Write(f.data)
		})
	}
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	desc := &descriptor.Descriptor{
		Meta: descriptor.Meta{Version: "v2"},
		Component: descriptor.Component{
			ComponentMeta: descriptor.ComponentMeta{ObjectMeta: descriptor.ObjectMeta{Name: component, Version: version}},
			Provider:      descriptor.Provider{Name: "test-provider"},
		},
	}
	for _, f := range files {
		access := &runtime.Raw{}
		r.NoError(runtime.NewScheme(runtime.WithAllowUnknown()).Convert(&wgetaccessv1.Wget{
			Type:      runtime.NewVersionedType(wgetaccess.WgetConsumerType, wgetaccessv1.Version),
			URL:       srv.URL + "/" + f.name,
			MediaType: f.mediaType,
		}, access))
		desc.Component.Resources = append(desc.Component.Resources, descriptor.Resource{
			ElementMeta: descriptor.ElementMeta{ObjectMeta: descriptor.ObjectMeta{Name: f.resource, Version: f.version}},
			Type:        "blob",
			Relation:    descriptor.ExternalRelation,
			Access:      access,
			Digest:      &descriptor.Digest{HashAlgorithm: "SHA-256", NormalisationAlgorithm: "genericBlobDigest/v1", Value: digestOf(f.data).Encoded()},
		})
	}

	path := t.TempDir()
	repo := createCTFRepository(t, path)
	r.NoError(repo.AddComponentVersion(t.Context(), desc))
	return repo, &ctfrepospec.Repository{Type: runtime.Type{Name: ctfrepospec.Type, Version: ctfrepospec.Version}, FilePath: path}
}

// newTargetCTF returns the path and specification of a new, empty target CTF.
func newTargetCTF(t *testing.T) (string, runtime.Typed) {
	t.Helper()
	path := t.TempDir()
	return path, &ctfrepospec.Repository{
		Type:       runtime.Type{Name: ctfrepospec.Type, Version: ctfrepospec.Version},
		FilePath:   path,
		AccessMode: "readwrite|create",
	}
}

// transferOnce transfers the component version from the source to the target CTF with the uploader.
func transferOnce(t *testing.T, ctfRepo repository.ComponentVersionRepository, sourceSpec, targetSpec runtime.Typed,
	uploader transferv1alpha1.UploaderConfig, resourceRepo repository.ResourceRepository, creds credentials.Resolver, component, version string,
) {
	t.Helper()
	r := require.New(t)
	tgd, err := transfer.BuildGraphDefinition(t.Context(),
		&transferv1alpha1.Config{},
		[]transferv1alpha1.UploaderConfig{uploader, &transferv1alpha1.LocalBlobUploaderConfig{}},
		transfer.Mapping{
			Components: []transfer.ComponentID{{Component: component, Version: version}},
			Target:     targetSpec,
			Resolver:   transfer.NewRepositoryResolver(ctfRepo, sourceSpec),
		},
	)
	r.NoError(err)
	repoProvider := provider.NewComponentVersionRepositoryProvider(provider.WithTempDir(t.TempDir()))
	graph, err := transfer.NewDefaultBuilder(repoProvider, resourceRepo, creds).BuildAndCheck(tgd)
	r.NoError(err)
	r.NoError(graph.Process(t.Context()))
}
