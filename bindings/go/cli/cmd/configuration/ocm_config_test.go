package configuration

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
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
					"/xdg/ocm/config",
					"/xdg/.ocmconfig",
					"/home/user/.config/ocm/config",
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

func TestLoadAndMergeConfigsWithStdin(t *testing.T) {
	const stdinConfig = `type: generic.config.ocm.software/v1
configurations:
- type: attributes.config.ocm.software
  attributes:
    source: stdin
`
	fileConfigData := []byte(`{"consumers":[{"credentials":[{"properties":{"password":"ghcr-token","username":"ghcr-user"},"type":"Credentials/v1"}],"identity":{"hostname":"ghcr.io","type":"OCIRegistry"}}],"type":"credentials.config.ocm.software"}`)
	stdinConfigData := []byte(`{"attributes":{"source":"stdin"},"type":"attributes.config.ocm.software"}`)

	tests := []struct {
		name     string
		paths    []string
		stdin    string
		wantData [][]byte
		wantErr  string
	}{
		{
			name:     "stdin only",
			paths:    []string{StdinConfigPath},
			stdin:    stdinConfig,
			wantData: [][]byte{stdinConfigData},
		},
		{
			name:     "stdin before file keeps command line order",
			paths:    []string{StdinConfigPath, "testdata/.ocmconfig-2"},
			stdin:    stdinConfig,
			wantData: [][]byte{stdinConfigData, fileConfigData},
		},
		{
			name:     "file before stdin keeps command line order",
			paths:    []string{"testdata/.ocmconfig-2", StdinConfigPath},
			stdin:    stdinConfig,
			wantData: [][]byte{fileConfigData, stdinConfigData},
		},
		{
			name:    "stdin given twice is rejected",
			paths:   []string{StdinConfigPath, StdinConfigPath},
			wantErr: "can only be given once",
		},
		{
			name:    "stdin given twice after a file is rejected before the file is read",
			paths:   []string{"testdata/.ocmconfig-2", StdinConfigPath, StdinConfigPath},
			wantErr: "can only be given once",
		},
		{
			name:    "empty stdin is rejected",
			paths:   []string{StdinConfigPath},
			stdin:   "",
			wantErr: "no configuration document",
		},
		{
			name:    "missing file is still rejected",
			paths:   []string{"testdata/does-not-exist"},
			wantErr: "does-not-exist",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := require.New(t)
			cmd := &cobra.Command{}
			cmd.SetIn(strings.NewReader(tt.stdin))
			got, err := loadAndMergeConfigs(tt.paths, true, stdinConfigReader(cmd))
			if tt.wantErr != "" {
				r.ErrorContains(err, tt.wantErr)
				return
			}
			r.NoError(err)
			gotData := make([][]byte, 0, len(got.Configurations))
			for _, cfg := range got.Configurations {
				gotData = append(gotData, cfg.Data)
			}
			r.Equal(tt.wantData, gotData)
		})
	}
}

func TestLoadAndMergeConfigsWithoutStdin(t *testing.T) {
	_, err := loadAndMergeConfigs([]string{StdinConfigPath}, true, nil)
	require.ErrorContains(t, err, "stdin is not available")
}

// streamTransferSpec carries nested "type" fields, which must not make it look like a
// configuration document.
const (
	streamCredentials = `---
type: generic.config.ocm.software/v1
configurations:
  - type: credentials.config.ocm.software
    consumers: []
`
	streamTransferSpec = `---
environment: {}
transformations:
- id: upload
  spec:
    repository:
      type: OCIRepository/v1
  type: OCIAddComponentVersion/v1alpha1
`
	streamTransferConfig = `---
type: generic.config.ocm.software/v1
configurations:
  - type: transfer.config.ocm.software/v1alpha1
    copyMode: localBlob
`
)

func TestGetOCMConfigForCommandWithStdinStream(t *testing.T) {
	tests := []struct {
		name      string
		stdin     string
		wantTypes []string
	}{
		{
			name:      "credentials then transfer spec",
			stdin:     streamCredentials + streamTransferSpec,
			wantTypes: []string{"credentials.config.ocm.software"},
		},
		{
			name:      "transfer spec then credentials",
			stdin:     streamTransferSpec + streamCredentials,
			wantTypes: []string{"credentials.config.ocm.software"},
		},
		{
			name:      "two configurations around the transfer spec are merged in order",
			stdin:     streamTransferConfig + streamTransferSpec + streamCredentials,
			wantTypes: []string{"transfer.config.ocm.software/v1alpha1", "credentials.config.ocm.software"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := require.New(t)
			cmd := &cobra.Command{Use: "test"}
			RegisterConfigFlag(cmd)
			r.NoError(cmd.PersistentFlags().Set(OCMConfigCommandArgument, StdinConfigPath))
			cmd.SetIn(strings.NewReader(tt.stdin))

			cfg, err := GetOCMConfigForCommand(cmd)
			r.NoError(err)
			types := make([]string, 0, len(cfg.Configurations))
			for _, c := range cfg.Configurations {
				types = append(types, c.Type.String())
			}
			r.Equal(tt.wantTypes, types)

			rest, err := io.ReadAll(cmd.InOrStdin())
			r.NoError(err)
			r.Equal(strings.TrimSpace(strings.TrimPrefix(streamTransferSpec, "---\n")), strings.TrimSpace(string(rest)),
				"the transfer spec must stay on stdin for the command")
		})
	}
}
