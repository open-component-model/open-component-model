package spec_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	genericv1 "ocm.software/open-component-model/bindings/go/configuration/generic/v1/spec"
	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/bindings/go/transfer/v1alpha1/spec"
)

func TestArtifactoryUploaderConfig_Validate(t *testing.T) {
	valid := func() *spec.ArtifactoryUploaderConfig {
		return &spec.ArtifactoryUploaderConfig{
			Type:       runtime.NewVersionedType(spec.ArtifactoryUploaderConfigType, spec.Version),
			MatchSpec:  spec.UploaderMatch{AccessType: runtime.NewVersionedType("Helm", "v1")},
			URL:        "https://myorg.jfrog.io",
			Repository: "helm-local",
		}
	}

	tests := []struct {
		name    string
		mutate  func(u *spec.ArtifactoryUploaderConfig)
		wantErr string
	}{
		{name: "valid minimal config", mutate: func(*spec.ArtifactoryUploaderConfig) {}},
		{name: "wrong type", mutate: func(u *spec.ArtifactoryUploaderConfig) {
			u.Type = runtime.NewVersionedType(spec.HTTPUploaderConfigType, spec.Version)
		}, wantErr: "invalid type"},
		{name: "missing accessType", mutate: func(u *spec.ArtifactoryUploaderConfig) {
			u.MatchSpec.AccessType = runtime.Type{}
		}, wantErr: "match.accessType is required"},
		{name: "missing url", mutate: func(u *spec.ArtifactoryUploaderConfig) { u.URL = "" }, wantErr: "url is required"},
		{name: "scheme-less url", mutate: func(u *spec.ArtifactoryUploaderConfig) {
			u.URL = "myorg.jfrog.io"
		}, wantErr: "url must be an absolute http or https URL"},
		{name: "url with query", mutate: func(u *spec.ArtifactoryUploaderConfig) {
			u.URL = "https://myorg.jfrog.io?x=1"
		}, wantErr: "url must not carry a query or fragment"},
		{name: "empty repository", mutate: func(u *spec.ArtifactoryUploaderConfig) { u.Repository = "" }, wantErr: "repository is required"},
		{name: "nested repository", mutate: func(u *spec.ArtifactoryUploaderConfig) { u.Repository = "a/b" }, wantErr: "repository must be a single repository key"},
		{name: "path is valid", mutate: func(u *spec.ArtifactoryUploaderConfig) { u.Path = "my/custom/path" }},
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

func TestNexusUploaderConfig_Validate(t *testing.T) {
	valid := func() *spec.NexusUploaderConfig {
		return &spec.NexusUploaderConfig{
			Type:       runtime.NewVersionedType(spec.NexusUploaderConfigType, spec.Version),
			MatchSpec:  spec.UploaderMatch{AccessType: runtime.NewVersionedType("Helm", "v1")},
			URL:        "https://nexus.example.com",
			Repository: "helm-hosted",
		}
	}

	tests := []struct {
		name    string
		mutate  func(u *spec.NexusUploaderConfig)
		wantErr string
	}{
		{name: "valid minimal config", mutate: func(*spec.NexusUploaderConfig) {}},
		{name: "wrong type", mutate: func(u *spec.NexusUploaderConfig) {
			u.Type = runtime.NewVersionedType(spec.HTTPUploaderConfigType, spec.Version)
		}, wantErr: "invalid type"},
		{name: "missing accessType", mutate: func(u *spec.NexusUploaderConfig) {
			u.MatchSpec.AccessType = runtime.Type{}
		}, wantErr: "match.accessType is required"},
		{name: "missing url", mutate: func(u *spec.NexusUploaderConfig) { u.URL = "" }, wantErr: "url is required"},
		{name: "scheme-less url", mutate: func(u *spec.NexusUploaderConfig) {
			u.URL = "nexus.example.com"
		}, wantErr: "url must be an absolute http or https URL"},
		{name: "url with query", mutate: func(u *spec.NexusUploaderConfig) {
			u.URL = "https://nexus.example.com?x=1"
		}, wantErr: "url must not carry a query or fragment"},
		{name: "empty repository", mutate: func(u *spec.NexusUploaderConfig) { u.Repository = "" }, wantErr: "repository is required"},
		{name: "nested repository", mutate: func(u *spec.NexusUploaderConfig) { u.Repository = "a/b" }, wantErr: "repository must be a single repository key"},
		{name: "path is valid", mutate: func(u *spec.NexusUploaderConfig) { u.Path = "my/custom/path" }},
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

func TestLookupUploaderConfigs_Artifactory(t *testing.T) {
	r := require.New(t)
	var generic genericv1.Config
	r.NoError(genericv1.Scheme.Decode(strings.NewReader(`
type: generic.config.ocm.software/v1
configurations:
  - type: http.uploader.transfer.config.ocm.software/v1alpha1
    match:
      accessType: Wget/v1
    targetURL: '${"https://target.example/" + resource.name}'
  - type: artifactory.uploader.transfer.config.ocm.software/v1alpha1
    match:
      accessType: Helm/v1
    url: https://artifactory.example.com
    repository: helm-local
`), &generic))

	uploaders, err := spec.LookupUploaderConfigs(&generic)
	r.NoError(err)
	r.Len(uploaders, 2)
	r.IsType(&spec.HTTPUploaderConfig{}, uploaders[0])
	u, ok := uploaders[1].(*spec.ArtifactoryUploaderConfig)
	r.True(ok, "second entry must decode as ArtifactoryUploaderConfig, got %T", uploaders[1])
	r.Equal("https://artifactory.example.com", u.URL)
	r.Equal("helm-local", u.Repository)
}

func TestLookupUploaderConfigs_Nexus(t *testing.T) {
	r := require.New(t)
	var generic genericv1.Config
	r.NoError(genericv1.Scheme.Decode(strings.NewReader(`
type: generic.config.ocm.software/v1
configurations:
  - type: nexus.uploader.transfer.config.ocm.software/v1alpha1
    match:
      accessType: Helm/v1
    url: https://nexus.example.com
    repository: helm-hosted
`), &generic))

	uploaders, err := spec.LookupUploaderConfigs(&generic)
	r.NoError(err)
	r.Len(uploaders, 1)
	u, ok := uploaders[0].(*spec.NexusUploaderConfig)
	r.True(ok, "entry must decode as NexusUploaderConfig, got %T", uploaders[0])
	r.Equal("https://nexus.example.com", u.URL)
	r.Equal("helm-hosted", u.Repository)
}
