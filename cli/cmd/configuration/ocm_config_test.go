package configuration

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	genericv1 "ocm.software/open-component-model/bindings/go/configuration/generic/v1/spec"
	"ocm.software/open-component-model/bindings/go/runtime"
)

func TestGetOCMConfigPaths(t *testing.T) {
	tests := []struct {
		name     string
		existing map[string]bool
		envVars  map[string]string
		want     func(workingDirectory, executableDirectory string) []string
		wantErr  bool
	}{
		{
			name:     "env var set and file exists",
			existing: map[string]bool{"/custom/config": true},
			envVars:  map[string]string{"OCM_CONFIG": "/custom/config"},
			want:     func(string, string) []string { return []string{"/custom/config"} },
		},
		{
			name:     "env var set but file does not exist",
			existing: map[string]bool{},
			envVars:  map[string]string{"OCM_CONFIG": "/missing/config"},
			wantErr:  true,
		},
		{
			name:     "all files found across all locations in documented order",
			existing: nil, // all paths exist
			envVars: map[string]string{
				"OCM_CONFIG":      "/ocm-config",
				"XDG_CONFIG_HOME": "/xdg",
			},
			want: func(workingDirectory, executableDirectory string) []string {
				return []string{
					"/ocm-config",
					"/xdg/.ocm/config",
					"/xdg/.ocmconfig",
					"/home/user/.config/.ocm/config",
					"/home/user/.config/.ocmconfig",
					"/home/user/.ocm/config",
					"/home/user/.ocmconfig",
					filepath.Join(workingDirectory, ".ocm/config"),
					filepath.Join(workingDirectory, ".ocmconfig"),
					filepath.Join(executableDirectory, ".ocm/config"),
					filepath.Join(executableDirectory, ".ocmconfig"),
				}
			},
		},
		{
			name:     "no files found returns error",
			existing: map[string]bool{},
			envVars:  map[string]string{},
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			workingDirectory := t.TempDir()
			ex, err := os.Executable()
			require.NoError(t, err)
			executableDirectory := filepath.Dir(ex)
			t.Chdir(workingDirectory)

			options := OCMConfigOptions{
				Stat: func(path string) (os.FileInfo, error) {
					if tt.existing == nil || tt.existing[path] {
						return nil, nil
					}
					return nil, os.ErrNotExist
				},
				Getenv: func(key string) string {
					return tt.envVars[key]
				},
				UserHomeDir: func() (string, error) { return "/home/user", nil },
				Getwd:       func() (string, error) { return workingDirectory, nil },
				Executable:  func() (string, error) { return ex, nil },
			}

			got, err := GetOCMConfigPaths(options)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want(workingDirectory, executableDirectory), got)
		})
	}
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
