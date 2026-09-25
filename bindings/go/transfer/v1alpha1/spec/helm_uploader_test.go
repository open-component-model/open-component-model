package spec_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	genericv1 "ocm.software/open-component-model/bindings/go/configuration/generic/v1/spec"
	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/bindings/go/transfer/v1alpha1/spec"
)

func TestHelmUploaderConfig_Validate(t *testing.T) {
	valid := func() *spec.HelmUploaderConfig {
		return &spec.HelmUploaderConfig{
			Type:       runtime.NewVersionedType(spec.HelmUploaderConfigType, spec.Version),
			MatchSpec:  spec.UploaderMatch{AccessType: runtime.NewVersionedType("Helm", "v1")},
			Server:     spec.HelmRepositoryServerArtifactory,
			URL:        "https://common.repositories.cloud.sap",
			Repository: "open-component-model-helm-test",
		}
	}

	tests := []struct {
		name    string
		mutate  func(u *spec.HelmUploaderConfig)
		wantErr string
	}{
		{name: "valid minimal config", mutate: func(*spec.HelmUploaderConfig) {}},
		{name: "wrong type", mutate: func(u *spec.HelmUploaderConfig) {
			u.Type = runtime.NewVersionedType(spec.HTTPUploaderConfigType, spec.Version)
		}, wantErr: "invalid type"},
		{name: "missing server", mutate: func(u *spec.HelmUploaderConfig) { u.Server = "" }, wantErr: "server is required"},
		{name: "unknown server", mutate: func(u *spec.HelmUploaderConfig) { u.Server = "Harbor" }, wantErr: `server must be "Artifactory" or "Nexus", got "Harbor"`},
		{name: "nexus with reindex", mutate: func(u *spec.HelmUploaderConfig) {
			u.Server = spec.HelmRepositoryServerNexus
			u.Reindex = new(bool)
		}, wantErr: `reindex is only supported for server "Artifactory"`},
		{name: "valid nexus config", mutate: func(u *spec.HelmUploaderConfig) { u.Server = spec.HelmRepositoryServerNexus }},
		{name: "missing accessType", mutate: func(u *spec.HelmUploaderConfig) {
			u.MatchSpec.AccessType = runtime.Type{}
		}, wantErr: "match.accessType is required"},
		{name: "missing url", mutate: func(u *spec.HelmUploaderConfig) { u.URL = "" }, wantErr: "url is required"},
		{name: "scheme-less url", mutate: func(u *spec.HelmUploaderConfig) {
			u.URL = "common.repositories.cloud.sap"
		}, wantErr: "url must be an absolute http or https URL"},
		{name: "url with query", mutate: func(u *spec.HelmUploaderConfig) {
			u.URL = "https://common.repositories.cloud.sap?x=1"
		}, wantErr: "url must not carry a query or fragment"},
		{name: "empty repository", mutate: func(u *spec.HelmUploaderConfig) { u.Repository = "" }, wantErr: "repository is required"},
		{name: "nested repository", mutate: func(u *spec.HelmUploaderConfig) { u.Repository = "a/b" }, wantErr: "repository must be a single repository key"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := require.New(t)
			u := valid()
			tt.mutate(u)
			err := u.Validate()
			if tt.wantErr == "" {
				r.NoError(err)
				return
			}
			r.ErrorContains(err, tt.wantErr)
		})
	}
}

func TestLookupUploaderConfigs_Helm(t *testing.T) {
	r := require.New(t)
	var generic genericv1.Config
	r.NoError(genericv1.Scheme.Decode(strings.NewReader(`
type: generic.config.ocm.software/v1
configurations:
  - type: http.uploader.transfer.config.ocm.software/v1alpha1
    match:
      accessType: Wget/v1
    targetURL: '${"https://target.example/" + resource.name}'
  - type: helm.uploader.transfer.config.ocm.software/v1alpha1
    match:
      accessType: Helm/v1
    server: Nexus
    url: https://nexus.example.com
    repository: helm-hosted
`), &generic))

	uploaders, err := spec.LookupUploaderConfigs(&generic)
	r.NoError(err)
	r.Len(uploaders, 2)
	r.IsType(&spec.HTTPUploaderConfig{}, uploaders[0])
	u, ok := uploaders[1].(*spec.HelmUploaderConfig)
	r.True(ok, "second entry must decode as HelmUploaderConfig, got %T", uploaders[1])
	r.Equal(spec.HelmRepositoryServerNexus, u.Server)
	r.Equal("https://nexus.example.com", u.URL)
	r.Equal("helm-hosted", u.Repository)
}
