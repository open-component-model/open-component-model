package spec_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	genericv1 "ocm.software/open-component-model/bindings/go/configuration/generic/v1/spec"
	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/bindings/go/transfer/v1alpha1/spec"
)

func TestLookupUploaderConfigs_Git(t *testing.T) {
	tests := []struct {
		name    string
		entry   string
		want    *spec.GitUploaderConfig
		wantErr string
	}{
		{
			name: "versioned type with repository and ref",
			entry: `type: git.uploader.transfer.config.ocm.software/v1alpha1
    repository: https://git.example.com/mirror/repo.git
    ref: refs/heads/main`,
			want: &spec.GitUploaderConfig{Repository: "https://git.example.com/mirror/repo.git", Ref: "refs/heads/main"},
		},
		{
			name: "unversioned type with templated repository, ref and match",
			entry: `type: git.uploader.transfer.config.ocm.software
    match: resource.name == "sources"
    repository: '${"https://git.example.com" + url(resource.access.repository).path}'
    ref: refs/heads/main`,
			want: &spec.GitUploaderConfig{Match: `resource.name == "sources"`, Repository: `${"https://git.example.com" + url(resource.access.repository).path}`, Ref: "refs/heads/main"},
		},
		{
			name: "missing repository",
			entry: `type: git.uploader.transfer.config.ocm.software/v1alpha1
    ref: refs/heads/main`,
			wantErr: "repository is required",
		},
		{
			name: "missing ref",
			entry: `type: git.uploader.transfer.config.ocm.software/v1alpha1
    repository: https://git.example.com/repo.git`,
			wantErr: "ref is required",
		},
		{
			name: "baseUrl is no longer a field",
			entry: `type: git.uploader.transfer.config.ocm.software/v1alpha1
    repository: https://git.example.com/repo.git
    ref: refs/heads/main
    baseUrl: https://git.example.com`,
			wantErr: `unknown field "baseUrl"`,
		},
		{
			name: "unknown field",
			entry: `type: git.uploader.transfer.config.ocm.software/v1alpha1
    repository: https://git.example.com/repo.git
    ref: refs/heads/main
    depth: 1`,
			wantErr: `unknown field "depth"`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := require.New(t)
			var generic genericv1.Config
			r.NoError(genericv1.Scheme.Decode(strings.NewReader("type: generic.config.ocm.software/v1\nconfigurations:\n  - "+tt.entry+"\n"), &generic))

			uploaders, err := spec.LookupUploaderConfigs(&generic)
			if tt.wantErr != "" {
				r.ErrorContains(err, tt.wantErr)
				return
			}
			r.NoError(err)
			r.Len(uploaders, 1)
			got, ok := uploaders[0].(*spec.GitUploaderConfig)
			r.True(ok)
			got.Type = tt.want.Type
			r.Equal(tt.want, got)
		})
	}
}

func TestGitUploaderConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		typ     runtime.Type
		wantErr string
	}{
		{name: "empty type for programmatic configs"},
		{name: "versioned type", typ: runtime.NewVersionedType(spec.GitUploaderConfigType, spec.Version)},
		{name: "type of another uploader", typ: runtime.NewVersionedType(spec.HTTPUploaderConfigType, spec.Version), wantErr: "invalid type"},
		{name: "unknown version", typ: runtime.NewVersionedType(spec.GitUploaderConfigType, "v2"), wantErr: "invalid type"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := require.New(t)
			err := (&spec.GitUploaderConfig{Type: tt.typ, Repository: "https://git.example.com/repo.git", Ref: "refs/heads/main"}).Validate()
			if tt.wantErr != "" {
				r.ErrorContains(err, tt.wantErr)
				return
			}
			r.NoError(err)
		})
	}
}
