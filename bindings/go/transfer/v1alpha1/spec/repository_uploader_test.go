package spec_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	genericv1 "ocm.software/open-component-model/bindings/go/configuration/generic/v1/spec"
	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/bindings/go/transfer/v1alpha1/spec"
)

// repositoryUploader holds the fields the Artifactory and Nexus uploader configs share.
type repositoryUploader struct {
	typ                   runtime.Type
	match                 spec.UploaderMatch
	url, repository, path string
}

var repositoryUploaderKinds = []struct {
	typ string
	new func(u repositoryUploader) spec.UploaderConfig
}{
	{spec.ArtifactoryUploaderConfigType, func(u repositoryUploader) spec.UploaderConfig {
		return &spec.ArtifactoryUploaderConfig{Type: u.typ, MatchSpec: u.match, URL: u.url, Repository: u.repository, Path: u.path}
	}},
	{spec.NexusUploaderConfigType, func(u repositoryUploader) spec.UploaderConfig {
		return &spec.NexusUploaderConfig{Type: u.typ, MatchSpec: u.match, URL: u.url, Repository: u.repository, Path: u.path}
	}},
}

func TestRepositoryUploaderConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(u *repositoryUploader)
		wantErr string
	}{
		{name: "valid minimal config", mutate: func(*repositoryUploader) {}},
		{name: "wrong type", mutate: func(u *repositoryUploader) {
			u.typ = runtime.NewVersionedType(spec.HTTPUploaderConfigType, spec.Version)
		}, wantErr: "invalid type"},
		{name: "missing accessType", mutate: func(u *repositoryUploader) { u.match.AccessType = runtime.Type{} }, wantErr: "match.accessType is required"},
		{name: "missing url", mutate: func(u *repositoryUploader) { u.url = "" }, wantErr: "url is required"},
		{name: "scheme-less url", mutate: func(u *repositoryUploader) { u.url = "repo.example.com" }, wantErr: "url must be an absolute http or https URL"},
		{name: "url with query", mutate: func(u *repositoryUploader) { u.url = "https://repo.example.com?x=1" }, wantErr: "url must not carry a query or fragment"},
		{name: "empty repository", mutate: func(u *repositoryUploader) { u.repository = "" }, wantErr: "repository is required"},
		{name: "nested repository", mutate: func(u *repositoryUploader) { u.repository = "a/b" }, wantErr: "repository must be a single repository key"},
		{name: "path is valid", mutate: func(u *repositoryUploader) { u.path = "my/custom/path" }},
	}
	for _, kind := range repositoryUploaderKinds {
		for _, tt := range tests {
			t.Run(kind.typ+"/"+tt.name, func(t *testing.T) {
				r := require.New(t)
				u := repositoryUploader{
					typ:        runtime.NewVersionedType(kind.typ, spec.Version),
					match:      spec.UploaderMatch{AccessType: runtime.NewVersionedType("Helm", "v1")},
					url:        "https://repo.example.com",
					repository: "helm-local",
				}
				tt.mutate(&u)
				err := kind.new(u).Validate()
				if tt.wantErr == "" {
					r.NoError(err)
					return
				}
				r.ErrorContains(err, tt.wantErr)
			})
		}
	}
}

func TestLookupUploaderConfigs_RepositoryUploaders(t *testing.T) {
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
  - type: nexus.uploader.transfer.config.ocm.software/v1alpha1
    match:
      accessType: Helm/v1
    url: https://nexus.example.com
    repository: helm-hosted
`), &generic))

	uploaders, err := spec.LookupUploaderConfigs(&generic)
	r.NoError(err)
	r.Len(uploaders, 3)
	r.IsType(&spec.HTTPUploaderConfig{}, uploaders[0])
	r.Equal(&spec.ArtifactoryUploaderConfig{
		Type:       runtime.NewVersionedType(spec.ArtifactoryUploaderConfigType, spec.Version),
		MatchSpec:  spec.UploaderMatch{AccessType: runtime.NewVersionedType("Helm", "v1")},
		URL:        "https://artifactory.example.com",
		Repository: "helm-local",
	}, uploaders[1])
	r.Equal(&spec.NexusUploaderConfig{
		Type:       runtime.NewVersionedType(spec.NexusUploaderConfigType, spec.Version),
		MatchSpec:  spec.UploaderMatch{AccessType: runtime.NewVersionedType("Helm", "v1")},
		URL:        "https://nexus.example.com",
		Repository: "helm-hosted",
	}, uploaders[2])
}
