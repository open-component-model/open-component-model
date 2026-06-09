package configuration

import (
	"os"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	genericv1 "ocm.software/open-component-model/bindings/go/configuration/generic/v1/spec"
	"ocm.software/open-component-model/bindings/go/runtime"
)

func TestGetOCMConfigForCommand(t *testing.T) {
	t.Run("explicit config flag with non-existent file returns error", func(t *testing.T) {
		cmd := &cobra.Command{Use: "test"}
		RegisterConfigFlag(cmd)
		require.NoError(t, cmd.PersistentFlags().Set(OCMConfigCommandArgument, "/nonexistent/path/config.yaml"))

		_, err := GetOCMConfigForCommand(cmd)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "/nonexistent/path/config.yaml")
	})

	t.Run("explicit config flag with existing file succeeds", func(t *testing.T) {
		cmd := &cobra.Command{Use: "test"}
		RegisterConfigFlag(cmd)
		require.NoError(t, cmd.PersistentFlags().Set(OCMConfigCommandArgument, "testdata/.ocmconfig-1"))

		cfg, err := GetOCMConfigForCommand(cmd)
		require.NoError(t, err)
		assert.NotNil(t, cfg)
	})

	t.Run("no config flag uses default discovery", func(t *testing.T) {
		cmd := &cobra.Command{Use: "test"}
		RegisterConfigFlag(cmd)

		// Should not panic; may or may not error depending on whether
		// default config files exist on the test machine
		_, _ = GetOCMConfigForCommand(cmd)
	})
}

func testEnvironment(existing map[string]bool, envVars map[string]string, home, wd, exe string) *Environment {
	return &Environment{
		Stat: func(path string) (os.FileInfo, error) {
			if existing == nil || existing[path] {
				return nil, nil
			}
			return nil, os.ErrNotExist
		},
		Getenv: func(key string) string {
			return envVars[key]
		},
		UserHomeDir: func() (string, error) {
			return home, nil
		},
		Getwd: func() (string, error) {
			return wd, nil
		},
		Executable: func() (string, error) {
			return exe, nil
		},
	}
}

func TestEnvironmentGetOCMConfigPaths(t *testing.T) {
	tests := []struct {
		name     string
		existing map[string]bool
		envVars  map[string]string
		home     string
		wd       string
		exe      string
		want     []string
		wantErr  bool
	}{
		{
			name:     "env var set and file exists",
			existing: map[string]bool{"/custom/config": true},
			envVars:  map[string]string{"OCM_CONFIG": "/custom/config"},
			home:     "/home/user",
			wd:       "/workspace",
			exe:      "/usr/bin/ocm",
			want:     []string{"/custom/config"},
		},
		{
			name:     "env var set but file does not exist",
			existing: map[string]bool{"/home/user/.ocm/config": true},
			envVars:  map[string]string{"OCM_CONFIG": "/missing/config"},
			home:     "/home/user",
			wd:       "/workspace",
			exe:      "/usr/bin/ocm",
			want:     []string{"/home/user/.ocm/config"},
		},
		{
			name:     "all files found across all locations in documented order",
			existing: nil, // all paths exist
			envVars: map[string]string{
				"OCM_CONFIG":      "/ocm-config",
				"XDG_CONFIG_HOME": "/xdg",
				"HOME":            "/home",
				"PWD":             "/pwd",
			},
			home: "/home/user",
			wd:   "/workspace",
			exe:  "/usr/bin/ocm",
			want: []string{
				"/ocm-config",
				"/xdg/.ocm/config",
				"/xdg/.ocmconfig",
				"/home/user/.config/.ocm/config",
				"/home/user/.config/.ocmconfig",
				"/home/user/.ocm/config",
				"/home/user/.ocmconfig",
				"/workspace/.ocm/config",
				"/workspace/.ocmconfig",
				"/usr/bin/.ocm/config",
				"/usr/bin/.ocmconfig",
			},
		},
		{
			name:     "no files found returns error",
			existing: map[string]bool{},
			envVars:  map[string]string{},
			home:     "/home/user",
			wd:       "/workspace",
			exe:      "/usr/bin/ocm",
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := testEnvironment(tt.existing, tt.envVars, tt.home, tt.wd, tt.exe)
			got, err := env.GetOCMConfigPaths()
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
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
