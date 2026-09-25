package configuration

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	ocmctx "ocm.software/open-component-model/bindings/go/cli/internal/context"
	genericv1 "ocm.software/open-component-model/bindings/go/configuration/generic/v1/spec"
)

// OCM Configuration file and directory constants
const (
	OCMConfigDirectoryName   = ".ocm"
	OCMConfigFileName        = OCMConfigDirectoryName + "/config"
	XDGOCMConfigFileName     = "ocm/config"
	NestedOCMConfigFileName  = ".ocmconfig"
	OCMConfigEnvironmentKey  = "OCM_CONFIG"
	OCMConfigCommandArgument = "config"
	// StdinConfigPath is the --config value that reads the configuration from stdin.
	StdinConfigPath = "-"
)

type OCMConfigOptions struct {
	Stat        func(string) (os.FileInfo, error)
	Getenv      func(string) string
	UserHomeDir func() (string, error)
	Getwd       func() (string, error)
	Executable  func() (string, error)
}

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
- $PWD/.ocm/config
- $PWD/.ocmconfig
4. The directory of the current executable:
- $EXE_DIR/.ocm/config
- $EXE_DIR/.ocmconfig
If multiple configuration files are found, they will be merged in the order they are discovered.
Later entries have higher priority.
Using the option, the specified configuration file(s) will be used instead of the lookup above.
Use "-" to read the configuration from stdin.`)
}

func GetFlattenedOCMConfigForCommand(cmd *cobra.Command) (*genericv1.Config, error) {
	cfg, err := GetOCMConfigForCommand(cmd)
	if err != nil {
		return nil, err
	}
	return genericv1.FlatMap(cfg), nil
}

// GetOCMConfigForCommand returns the configuration for a command. With --config set, only
// the given values are loaded, in command line order; "-" reads stdin and may be given
// once. Without the flag the well known locations are searched and stdin is never read.
func GetOCMConfigForCommand(cmd *cobra.Command) (*genericv1.Config, error) {
	flag := cmd.Flag(OCMConfigCommandArgument)
	if flag != nil && flag.Changed {
		paths := flag.Value.(pflag.SliceValue).GetSlice()
		return loadAndMergeConfigs(paths, true, stdinConfigReader(cmd))
	}
	syscalls := ocmctx.FromContext(cmd.Context()).Syscalls()
	options := OCMConfigOptions{
		Stat:        syscalls.Stat,
		Getenv:      syscalls.Getenv,
		UserHomeDir: syscalls.UserHomeDir,
		Getwd:       syscalls.Getwd,
		Executable:  syscalls.Executable,
	}
	return GetOCMConfig(options)
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
func GetOCMConfig(options OCMConfigOptions, additional ...string) (*genericv1.Config, error) {
	paths, err := GetOCMConfigPaths(options)
	paths = append(paths, additional...)
	if err != nil && len(additional) == 0 {
		return nil, err
	}
	return loadAndMergeConfigs(paths, false, nil)
}

// stdinConfigReader loads the configuration from the command's stdin and puts every other
// document of the stream back, so the command still finds them in cmd.InOrStdin().
func stdinConfigReader(cmd *cobra.Command) configReader {
	return func() (*genericv1.Config, error) {
		cfg, rest, err := readConfigStream(cmd.InOrStdin())
		if err != nil {
			return nil, err
		}
		cmd.SetIn(bytes.NewReader(rest))
		return cfg, nil
	}
}

type configReader func() (*genericv1.Config, error)

// loadAndMergeConfigs merges the configurations at the given paths in order. The path "-"
// is read through readStdin, which may be nil when stdin is not available.
func loadAndMergeConfigs(paths []string, strict bool, readStdin configReader) (*genericv1.Config, error) {
	if countStdinPaths(paths) > 1 {
		return nil, fmt.Errorf("configuration from stdin (%q) can only be given once", StdinConfigPath)
	}
	cfgs := make([]*genericv1.Config, 0, len(paths))
	for _, path := range paths {
		cfg, err := loadConfig(path, readStdin)
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

func countStdinPaths(paths []string) int {
	n := 0
	for _, path := range paths {
		if path == StdinConfigPath {
			n++
		}
	}
	return n
}

func loadConfig(path string, readStdin configReader) (*genericv1.Config, error) {
	if path != StdinConfigPath {
		return GetConfigFromPath(path)
	}
	if readStdin == nil {
		return nil, errors.New("could not load configuration from stdin: stdin is not available")
	}
	cfg, err := readStdin()
	if err != nil {
		return nil, fmt.Errorf("could not load configuration from stdin: %w", err)
	}
	return cfg, nil
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
	return decodeConfig(file)
}

func decodeConfig(r io.Reader) (*genericv1.Config, error) {
	var instance genericv1.Config
	if err := genericv1.Scheme.Decode(r, &instance); err != nil {
		return nil, err
	}
	return &instance, nil
}

// GetOCMConfigPaths searches for the OCM configuration file in the following locations (in order):
// 1. The path specified in the OCM_CONFIG environment variable
// 2. The XDG_CONFIG_HOME directory (if set), or the default XDG home ($HOME/.config), or the user's home directory
//   - $XDG_CONFIG_HOME/ocm/config
//   - $XDG_CONFIG_HOME/.ocmconfig
//   - $HOME/.config/ocm/config
//   - $HOME/.config/.ocmconfig
//   - $HOME/.ocm/config
//   - $HOME/.ocmconfig
//
// 3. The current working directory:
//   - $PWD/.ocm/config
//   - $PWD/.ocmconfig
//
// 4. The directory of the current executable:
//   - $EXE_DIR/.ocm/config
//   - $EXE_DIR/.ocmconfig
//
// Returns:
//   - []string: A slice of valid config file paths found; otherwise, an empty slice.
//   - error: An error if no configuration file is found.
func GetOCMConfigPaths(options OCMConfigOptions) ([]string, error) {
	var paths []string
	if path := getFromEnvironment(options); path != "" {
		paths = append(paths, path)
	}
	if subPaths := getFromXDGOrHomeDir(options); len(subPaths) > 0 {
		paths = append(paths, subPaths...)
	}
	if subPaths := getFromWorkingDir(options); len(subPaths) > 0 {
		paths = append(paths, subPaths...)
	}
	if subPaths := getFromExecutableDir(options); len(subPaths) > 0 {
		paths = append(paths, subPaths...)
	}

	if len(paths) > 0 {
		return paths, nil
	}

	return nil, fmt.Errorf("ocm config not found in any known locations, see --help for details on how to supply configuration files")
}

func getFromEnvironment(options OCMConfigOptions) string {
	if env := options.Getenv(OCMConfigEnvironmentKey); env != "" {
		if _, err := options.Stat(filepath.Clean(env)); err == nil {
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
func getFromXDGOrHomeDir(o OCMConfigOptions) []string {
	paths := []string{}
	if xdg := o.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		if subPaths := checkXDGConfigPaths(o, xdg); len(subPaths) > 0 {
			paths = append(paths, subPaths...)
		}
	}

	if home, err := o.UserHomeDir(); err == nil {
		if subPaths := checkXDGConfigPaths(o, filepath.Join(home, ".config")); len(subPaths) > 0 {
			paths = append(paths, subPaths...)
		}
		if subPaths := checkConfigPaths(o, home); len(subPaths) > 0 {
			paths = append(paths, subPaths...)
		}
	}

	return paths
}

// getFromWorkingDir checks the current working directory for the configuration file.
//
// Returns:
//   - []string: A slice of valid config file paths found; otherwise, an empty slice.
func getFromWorkingDir(o OCMConfigOptions) []string {
	if wd, err := o.Getwd(); err == nil {
		return checkConfigPaths(o, wd)
	}
	return []string{}
}

// getFromExecutableDir checks the directory of the running executable for the configuration file.
//
// Returns:
//   - []string: A slice of valid config file paths found; otherwise, an empty slice.
func getFromExecutableDir(o OCMConfigOptions) []string {
	if ex, err := o.Executable(); err == nil {
		base := filepath.Dir(ex)
		return checkConfigPaths(o, base)
	}
	return []string{}
}

// checkConfigPaths searches for config file variations in a given base directory
// using dotfile names (.ocm/config, .ocmconfig). This is used for directories where
// hidden files are the convention (e.g. $HOME, $PWD, $EXE_DIR).
func checkConfigPaths(o OCMConfigOptions, base string) []string {
	return checkPaths(o, base, []string{OCMConfigFileName, NestedOCMConfigFileName})
}

// checkXDGConfigPaths searches for config file variations in a given XDG config
// directory using non-dot names (ocm/config, .ocmconfig). This is used for directories
// that are already config directories (e.g. $XDG_CONFIG_HOME, $HOME/.config) following the
// convention of not prefixing with another '.'
func checkXDGConfigPaths(o OCMConfigOptions, base string) []string {
	return checkPaths(o, base, []string{XDGOCMConfigFileName, NestedOCMConfigFileName})
}

func checkPaths(o OCMConfigOptions, base string, names []string) []string {
	paths := []string{}
	for _, name := range names {
		path := filepath.Clean(filepath.Join(base, name))
		if _, err := o.Stat(path); err == nil {
			paths = append(paths, path)
		}
	}
	return paths
}
