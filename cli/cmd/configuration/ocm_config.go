package configuration

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	genericv1 "ocm.software/open-component-model/bindings/go/configuration/generic/v1/spec"
)

// OCM Configuration file and directory constants
const (
	OCMConfigDirectoryName   = ".ocm"
	OCMConfigFileName        = OCMConfigDirectoryName + "/config"
	NestedOCMConfigFileName  = ".ocmconfig"
	OCMConfigEnvironmentKey  = "OCM_CONFIG"
	OCMConfigCommandArgument = "config"
)

// statFunc is the file stat function used for config discovery. Overridden in tests.
var statFunc = os.Stat

func RegisterConfigFlag(cmd *cobra.Command) {
	cmd.PersistentFlags().StringArray(OCMConfigCommandArgument, nil, `supply configuration by a given configuration file.
By default (without specifying custom locations with this flag), the file will be read from one of the well known locations:
1. The path specified in the OCM_CONFIG environment variable
2. The XDG_CONFIG_HOME directory (if set), or the default XDG home ($HOME/.config), or the user's home directory
- $XDG_CONFIG_HOME/ocm/config
- $XDG_CONFIG_HOME/.ocmconfig
- $HOME/.config/ocm/config
- $HOME/.config/.ocmconfig
- $HOME/.ocm/config
- $HOME/.ocmconfig
3. The current working directory:
- $PWD/ocm/config
- $PWD/.ocmconfig
4. The directory of the current executable:
- $EXE_DIR/ocm/config
- $EXE_DIR/.ocmconfig
If multiple configuration files are found, they will be merged in the order they are discovered.
Using the option, the specified configuration file(s) will be used instead of the lookup above.`)
}

func GetFlattenedOCMConfigForCommand(cmd *cobra.Command) (*genericv1.Config, error) {
	cfg, err := GetOCMConfigForCommand(cmd)
	if err != nil {
		return nil, err
	}
	return genericv1.FlatMap(cfg), nil
}

func GetOCMConfigForCommand(cmd *cobra.Command) (*genericv1.Config, error) {
	flag := cmd.Flag(OCMConfigCommandArgument)
	if flag != nil && flag.Changed {
		paths := flag.Value.(pflag.SliceValue).GetSlice()
		return loadAndMergeConfigs(paths, true)
	}
	return GetOCMConfig()
}

// GetOCMConfig loads the OCM configuration file from multiple locations and returns the parsed configuration.
//
// It first determines the correct configuration file path using `GetOCMConfigPaths`.
// If a valid configuration file is found, it attempts to decode it into a `v1.Config` struct.
// If the file cannot be opened or decoded, an error is returned.
// One can specify additional paths to search for the configuration file in addition to the default locations.
//
// Returns:
//   - *v1.Config: The parsed configuration file.
//   - error: An error if no valid configuration file is found or if decoding fails.
func GetOCMConfig(additional ...string) (*genericv1.Config, error) {
	paths, err := GetOCMConfigPaths()
	paths = append(paths, additional...)
	if err != nil && len(additional) == 0 {
		return nil, err
	}
	return loadAndMergeConfigs(paths, false)
}

func loadAndMergeConfigs(paths []string, strict bool) (*genericv1.Config, error) {
	cfgs := make([]*genericv1.Config, 0, len(paths))
	for _, path := range paths {
		cfg, err := GetConfigFromPath(path)
		if err != nil {
			if strict {
				return nil, err
			}
			slog.Error("ocm config path was skipped due to an error loading it",
				slog.String("path", path),
				slog.String("error", err.Error()),
			)
			continue
		}
		slog.Debug("ocm config was loaded successfully", slog.String("path", path))
		cfgs = append(cfgs, cfg)
	}
	return genericv1.FlatMap(cfgs...), nil
}

// GetConfigFromPath reads and decodes the YAML configuration file from the specified path.
//
// Parameters:
//   - path (string): The file path of the configuration file.
//
// Returns:
//   - *v1.Config: The decoded configuration struct.
//   - error: An error if the file cannot be opened or decoded.
func GetConfigFromPath(path string) (_ *genericv1.Config, err error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() {
		err = errors.Join(err, file.Close())
	}()

	var instance genericv1.Config
	if err := genericv1.Scheme.Decode(file, &instance); err != nil {
		return nil, err
	}
	return &instance, nil
}

// GetOCMConfigPaths searches for the OCM configuration file in the following locations (in order):
// 1. The path specified in the OCM_CONFIG_PATH environment variable
// 2. The XDG_CONFIG_HOME directory (if set), or the default XDG home ($HOME/.config), or the user's home directory
//   - $XDG_CONFIG_HOME/ocm/config
//   - $XDG_CONFIG_HOME/.ocmconfig
//   - $HOME/.config/ocm/config
//   - $HOME/.config/.ocmconfig
//   - $HOME/.ocm/config
//   - $HOME/.ocmconfig
//
// 3. The current working directory:
//   - $PWD/ocm/config
//   - $PWD/.ocmconfig
//
// 4. The directory of the current executable:
//   - $EXE_DIR/ocm/config
//   - $EXE_DIR/.ocmconfig
//
// Returns:
//   - []string: A slice of valid config file paths found; otherwise, an empty slice.
//   - error: An error if no configuration file is found.
func GetOCMConfigPaths() ([]string, error) {
	var paths []string
	if path := getFromEnvironment(); path != "" {
		paths = append(paths, path)
	}
	if subPaths := getFromXDGOrHomeDir(); len(subPaths) > 0 {
		paths = append(paths, subPaths...)
	}
	if subPaths := getFromWorkingDir(); len(subPaths) > 0 {
		paths = append(paths, subPaths...)
	}
	if subPaths := getFromExecutableDir(); len(subPaths) > 0 {
		paths = append(paths, subPaths...)
	}

	if len(paths) > 0 {
		return paths, nil
	}

	return nil, fmt.Errorf("ocm config not found in any known locations, see --help for details on how to supply configuration files")
}

func getFromEnvironment() string {
	if env := os.Getenv(OCMConfigEnvironmentKey); env != "" {
		if _, err := statFunc(filepath.Clean(env)); err == nil {
			return env
		}
	}
	return ""
}

// getFromXDGOrHomeDir checks for the configuration file in the XDG_CONFIG_HOME or the user's home directory.
//
// XDG_CONFIG_HOME is checked first if set, followed by the default XDG home (~/.config).
// If both are unavailable, it falls back to the user's home directory.
//
// Returns:
//   - []string: A slice of valid config file paths found; otherwise, an empty slice.
func getFromXDGOrHomeDir() []string {
	paths := []string{}
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		if subPaths := checkConfigPaths(xdg); len(subPaths) > 0 {
			paths = append(paths, subPaths...)
		}
	}

	// Check default XDG home ($HOME/.config)
	if home, err := os.UserHomeDir(); err == nil {
		if subPaths := checkConfigPaths(filepath.Join(home, ".config")); len(subPaths) > 0 {
			paths = append(paths, subPaths...)
		}
		if subPaths := checkConfigPaths(home); len(subPaths) > 0 {
			paths = append(paths, subPaths...)
		}
	}

	return paths
}

// getFromWorkingDir checks the current working directory for the configuration file.
//
// Returns:
//   - []string: A slice of valid config file paths found; otherwise, an empty slice.
func getFromWorkingDir() []string {
	if wd, err := os.Getwd(); err == nil {
		return checkConfigPaths(wd)
	}
	return []string{}
}

// getFromExecutableDir checks the directory of the running executable for the configuration file.
//
// Returns:
//   - []string: A slice of valid config file paths found; otherwise, an empty slice.
func getFromExecutableDir() []string {
	if ex, err := os.Executable(); err == nil {
		base := filepath.Dir(ex)
		return checkConfigPaths(base)
	}
	return []string{}
}

// checkConfigPaths searches for both config file variations in a given base directory.
//
// Parameters:
//   - base (string): The directory to search in.
//
// Returns:
//   - []string: A slice of valid config file paths found; otherwise, an empty slice.
func checkConfigPaths(base string) []string {
	paths := []string{}
	for _, name := range []string{OCMConfigFileName, NestedOCMConfigFileName} {
		path := filepath.Clean(filepath.Join(base, name))
		if _, err := statFunc(path); err == nil {
			paths = append(paths, path)
		}
	}
	return paths
}
