package v1

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"ocm.software/open-component-model/bindings/go/runtime"
)

func TestGetFlattenedGetConfigFromPath(t *testing.T) {
	type args struct {
		path string
	}
	tests := []struct {
		name    string
		args    args
		want    *Config
		wantErr bool
	}{
		{
			name: "parse config from file",
			args: args{
				path: "testdata/.ocmconfig-1",
			},
			want: &Config{
				Type: runtime.Type{
					Group:   "generic.config.ocm",
					Version: "v1",
					Name:    "software",
				},
				Configurations: []Configuration{
					{
						Raw: &runtime.Raw{
							Type: runtime.Type{
								Group: "credentials.config.ocm",
								Name:  "software",
							},
							Data: []byte(`{"repositories":[{"repository":{"dockerConfigFile":"~/.docker/config.json","propagateConsumerIdentity":true,"type":"DockerConfig/v1"}}],"type":"credentials.config.ocm.software"}`),
						},
					},
					{
						Raw: &runtime.Raw{
							Type: runtime.Type{
								Group: "attributes.config.ocm",
								Name:  "software",
							},
							Data: []byte(`{"attributes":{"cache":"~/.ocm/cache"},"type":"attributes.config.ocm.software"}`),
						},
					},
					{
						Raw: &runtime.Raw{
							Type: runtime.Type{
								Group: "credentials.config.ocm",
								Name:  "software",
							},
							Data: []byte(`{"consumers":[{"credentials":[{"properties":{"password":"password","username":"username"},"type":"Credentials/v1"}],"identity":{"hostname":"common.repositories.cloud.sap","type":"HelmChartRepository"}}],"type":"credentials.config.ocm.software"}`),
						},
					},
					{
						Raw: &runtime.Raw{
							Type: runtime.Type{
								Group: "credentials.config.ocm",
								Name:  "software",
							},
							Data: []byte(`{"consumers":[{"credentials":[{"properties":{"password":"password","username":"username"},"type":"Credentials/v1"}],"identity":{"hostname":"common.repositories.cloud.sap","type":"JFrogHelm"}}],"type":"credentials.config.ocm.software"}`),
						},
					},
					{
						Raw: &runtime.Raw{
							Type: runtime.Type{
								Group: "uploader.ocm.config.ocm",
								Name:  "software",
							},
							Data: []byte(`{"registrations":[{"artifactType":"helmChart","config":{"repository":"test-ocm","type":"JFrogHelm/v1alpha1","url":"common.repositories.cloud.sap"},"name":"plugin/jfrog/JFrogHelm","priority":200}],"type":"uploader.ocm.config.ocm.software"}`),
						},
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
