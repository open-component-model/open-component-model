package spec_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	genericv1 "ocm.software/open-component-model/bindings/go/configuration/generic/v1/spec"
	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/bindings/go/transfer/v1alpha1/spec"
)

func TestJFrogHelmUploaderConfig_Validate(t *testing.T) {
	valid := func() *spec.JFrogHelmUploaderConfig {
		return &spec.JFrogHelmUploaderConfig{
			Type:       runtime.NewVersionedType(spec.JFrogHelmUploaderConfigType, spec.Version),
			MatchSpec:  spec.UploaderMatch{AccessType: runtime.NewVersionedType("Helm", "v1")},
			URL:        "https://common.repositories.cloud.sap",
			Repository: "open-component-model-helm-test",
		}
	}

	tests := []struct {
		name    string
		mutate  func(u *spec.JFrogHelmUploaderConfig)
		wantErr string
	}{
		{name: "valid minimal config", mutate: func(*spec.JFrogHelmUploaderConfig) {}},
		{name: "wrong type", mutate: func(u *spec.JFrogHelmUploaderConfig) {
			u.Type = runtime.NewVersionedType(spec.HTTPUploaderConfigType, spec.Version)
		}, wantErr: "invalid type"},
		{name: "missing accessType", mutate: func(u *spec.JFrogHelmUploaderConfig) {
			u.MatchSpec.AccessType = runtime.Type{}
		}, wantErr: "match.accessType is required"},
		{name: "missing url", mutate: func(u *spec.JFrogHelmUploaderConfig) { u.URL = "" }, wantErr: "url is required"},
		{name: "scheme-less url", mutate: func(u *spec.JFrogHelmUploaderConfig) {
			u.URL = "common.repositories.cloud.sap"
		}, wantErr: "url must be an absolute http or https URL"},
		{name: "url with query", mutate: func(u *spec.JFrogHelmUploaderConfig) {
			u.URL = "https://common.repositories.cloud.sap?x=1"
		}, wantErr: "url must not carry a query or fragment"},
		{name: "empty repository", mutate: func(u *spec.JFrogHelmUploaderConfig) { u.Repository = "" }, wantErr: "repository is required"},
		{name: "nested repository", mutate: func(u *spec.JFrogHelmUploaderConfig) { u.Repository = "a/b" }, wantErr: "repository must be a single repository key"},
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

func TestLookupUploaderConfigs_JFrogHelm(t *testing.T) {
	r := require.New(t)
	var generic genericv1.Config
	r.NoError(genericv1.Scheme.Decode(strings.NewReader(`
type: generic.config.ocm.software/v1
configurations:
  - type: http.uploader.transfer.config.ocm.software/v1alpha1
    match:
      accessType: Wget/v1
    targetURL: '${"https://target.example/" + resource.name}'
  - type: jfrog.helm.uploader.transfer.config.ocm.software/v1alpha1
    match:
      accessType: Helm/v1
    url: https://common.repositories.cloud.sap
    repository: open-component-model-helm-test
`), &generic))

	uploaders, err := spec.LookupUploaderConfigs(&generic)
	r.NoError(err)
	r.Len(uploaders, 2)
	r.IsType(&spec.HTTPUploaderConfig{}, uploaders[0])
	u, ok := uploaders[1].(*spec.JFrogHelmUploaderConfig)
	r.True(ok, "second entry must decode as JFrogHelmUploaderConfig, got %T", uploaders[1])
	r.Equal("https://common.repositories.cloud.sap", u.URL)
	r.Equal("open-component-model-helm-test", u.Repository)
}
