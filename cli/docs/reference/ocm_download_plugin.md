---
title: ocm download plugin
description: Download plugin binaries from a component version.
suppressTitle: true
toc: true
sidebar:
  collapsed: true
---

## ocm download plugin

Download plugin binaries from a component version.

### Synopsis

Download a plugin binary from a component version located in a component version.

This command fetches a specific plugin resource from the given OCM component version and stores it at the specified output location.
The plugin binary can be identified by resource name and version, with optional extra identity parameters for platform-specific binaries.

Resources can be accessed either locally or via a plugin that supports remote fetching, with optional credential resolution.

```
ocm download plugin [flags]
```

### Examples

```
 # Download a plugin binary with resource name 'ocm-plugin' and version 'v1.0.0'
  ocm download plugin ghcr.io/org/component:v1 --resource-name ocm-plugin --resource-version v1.0.0 --output ./plugins/ocm-plugin

  # Download a platform-specific plugin binary with extra identity parameters
  ocm download plugin ghcr.io/org/component:v1 --resource-name ocm-plugin --resource-version v1.0.0 --extra-identity os=linux,arch=amd64 --output ./plugins/ocm-plugin-linux-amd64

  # Download plugin using only resource name (uses component version if resource version not specified)
  ocm download plugin ghcr.io/org/component:v1 --resource-name ocm-plugin --output ./plugins/ocm-plugin
```

### Options

```
      --extra-identity strings    extra identity parameters for resource matching (e.g., os=linux,arch=amd64)
  -h, --help                      help for plugin
      --output string             output location to download the plugin binary to (required) (default ".")
  -f, --output-format enum        output format of the plugin information
                                  (must be one of [json table yaml]) (default table)
      --resource-name string      name of the plugin resource to download (required)
      --resource-version string   version of the plugin resource to download (optional, defaults to component version)
      --skip-validation           skip validation of the downloaded plugin binary
```

### Options inherited from parent commands

```
      --config string             supply configuration by a given configuration file.
                                  By default (without specifying custom locations with this flag), the file will be read from one of the well known locations:
                                  1. The path specified in the OCM_CONFIG_PATH environment variable
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
                                  Using the option, this configuration file be used instead of the lookup above.
      --logformat enum            set the log output format that is used to print individual logs
                                     json: Output logs in JSON format, suitable for machine processing
                                     text: Output logs in human-readable text format, suitable for console output
                                  (must be one of [json text]) (default text)
      --loglevel enum             sets the logging level
                                     debug: Show all logs including detailed debugging information
                                     info:  Show informational messages and above
                                     warn:  Show warnings and errors only (default)
                                     error: Show errors only
                                  (must be one of [debug error info warn]) (default info)
      --logoutput enum            set the log output destination
                                     stdout: Write logs to standard output (default)
                                     stderr: Write logs to standard error, useful for separating logs from normal output
                                  (must be one of [stderr stdout]) (default stdout)
      --plugin-directory string   default directory path for ocm plugins. (default "$HOME/.config/ocm/plugins")
      --temp-folder string        Specify a custom temporary folder path for filesystem operations.
```

### SEE ALSO

* [ocm download]({{< relref "ocm_download.md" >}})	 - Download anything from OCM

