package configuration

import (
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/assert"

	genericv1 "ocm.software/open-component-model/bindings/go/configuration/generic/v1/spec"
	"ocm.software/open-component-model/bindings/go/runtime"
)

const envSubstConfig = `type: generic.config.ocm.software/v1
configurations:
- type: credentials.config.ocm.software
  consumers:
  - identity:
      type: HelmChartRepository
      hostname: ${OCM_TEST_HOSTNAME}
    credentials:
    - type: Credentials/v1
      properties:
        username: "${OCM_TEST_USERNAME}"
        password: "${OCM_TEST_PASSWORD}"
`

func TestGetConfigFromFS_EnvSubstitution(t *testing.T) {
	t.Setenv("OCM_TEST_HOSTNAME", "my.registry.example.com")
	t.Setenv("OCM_TEST_USERNAME", "testuser")
	t.Setenv("OCM_TEST_PASSWORD", "s3cret")

	fsys := fstest.MapFS{
		"config": &fstest.MapFile{Data: []byte(envSubstConfig)},
	}

	got, err := GetConfigFromFS(fsys, "config")
	if err != nil {
		t.Fatalf("GetConfigFromFS() error = %v", err)
	}

	assert.Len(t, got.Configurations, 1)
	raw := got.Configurations[0]
	assert.Equal(t, "credentials.config.ocm.software", raw.Type.Name)
	assert.Contains(t, string(raw.Data), `"hostname":"my.registry.example.com"`)
	assert.Contains(t, string(raw.Data), `"username":"testuser"`)
	assert.Contains(t, string(raw.Data), `"password":"s3cret"`)
}

func TestGetConfigFromFS_EnvSubstitution_Unset(t *testing.T) {
	t.Setenv("OCM_TEST_HOSTNAME", "")
	t.Setenv("OCM_TEST_USERNAME", "")
	t.Setenv("OCM_TEST_PASSWORD", "")

	fsys := fstest.MapFS{
		"config": &fstest.MapFile{Data: []byte(envSubstConfig)},
	}

	got, err := GetConfigFromFS(fsys, "config")
	if err != nil {
		t.Fatalf("GetConfigFromFS() error = %v", err)
	}

	assert.Len(t, got.Configurations, 1)
	raw := got.Configurations[0]
	// Unquoted empty YAML value becomes null, quoted empty stays ""
	assert.Contains(t, string(raw.Data), `"hostname":null`)
	assert.Contains(t, string(raw.Data), `"username":""`)
	assert.Contains(t, string(raw.Data), `"password":""`)
}

func TestGetFlattenedGetConfigFromPath(t *testing.T) {
	type args struct {
		path string
	}
	tests := []struct {
		name    string
		args    args
		want    *genericv1.Config
		wantErr bool
	}{
		{
			name: "parse config from file",
			args: args{
				path: "testdata/.ocmconfig-1",
			},
			want: &genericv1.Config{
				Type: runtime.Type{
					Version: "v1",
					Name:    "generic.config.ocm.software",
				},
				Configurations: []*runtime.Raw{
					{
						Type: runtime.Type{
							Name: "credentials.config.ocm.software",
						},
						Data: []byte(`{"repositories":[{"repository":{"dockerConfigFile":"~/.docker/config.json","propagateConsumerIdentity":true,"type":"DockerConfig/v1"}}],"type":"credentials.config.ocm.software"}`),
					},
					{
						Type: runtime.Type{
							Name: "attributes.config.ocm.software",
						},
						Data: []byte(`{"attributes":{"cache":"~/.ocm/cache"},"type":"attributes.config.ocm.software"}`),
					},
					{
						Type: runtime.Type{
							Name: "credentials.config.ocm.software",
						},
						Data: []byte(`{"consumers":[{"credentials":[{"properties":{"password":"password","username":"username"},"type":"Credentials/v1"}],"identity":{"hostname":"common.repositories.cloud.sap","type":"HelmChartRepository"}}],"type":"credentials.config.ocm.software"}`),
					},
					{
						Type: runtime.Type{
							Name: "credentials.config.ocm.software",
						},
						Data: []byte(`{"consumers":[{"credentials":[{"properties":{"password":"password","username":"username"},"type":"Credentials/v1"}],"identity":{"hostname":"common.repositories.cloud.sap","type":"JFrogHelm"}}],"type":"credentials.config.ocm.software"}`),
					},
					{
						Type: runtime.Type{
							Name: "uploader.ocm.config.ocm.software",
						},
						Data: []byte(`{"registrations":[{"artifactType":"helmChart","config":{"repository":"test-ocm","type":"JFrogHelm/v1alpha1","url":"common.repositories.cloud.sap"},"name":"plugin/jfrog/JFrogHelm","priority":200}],"type":"uploader.ocm.config.ocm.software"}`),
					},
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := GetConfigFromPath(tt.args.path)
			if (err != nil) != tt.wantErr {
				t.Errorf("GetConfigFromPath() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			assert.Equal(t, tt.want, got)
		})
	}
}
