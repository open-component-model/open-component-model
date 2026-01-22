---
title: ocm transfer component-version
description: Transfer a component version between OCM repositories.
suppressTitle: true
toc: true
sidebar:
  collapsed: true
---

## ocm transfer component-version

Transfer a component version between OCM repositories

### Synopsis

Transfer a single component version from a source repository to
a target repository using an internally generated transformation graph.

This command constructs a TransformationGraphDefinition consisting of:
  1. CTFGetComponentVersion / OCIGetComponentVersion
  2. CTFAddComponentVersion / OCIAddComponentVersion

The graph is validated, and then executed unless --dry-run is set.

```
ocm transfer component-version {reference} {target} [flags]
```

### Options

```
      --dry-run       build and validate the graph but do not execute
  -h, --help          help for component-version
  -o, --output enum   output format of the component descriptors
                      (must be one of [json ndjson yaml]) (default yaml)
  -r, --recursive     recursively discover and transfer component versions
```

### Options inherited from parent commands

```
      --config string                      supply configuration by a given configuration file.
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
      --logformat enum                     set the log output format that is used to print individual logs
                                              json: Output logs in JSON format, suitable for machine processing
                                              text: Output logs in human-readable text format, suitable for console output
                                           (must be one of [json text]) (default text)
      --loglevel enum                      sets the logging level
                                              debug: Show all logs including detailed debugging information
                                              info:  Show informational messages and above
                                              warn:  Show warnings and errors only (default)
                                              error: Show errors only
                                           (must be one of [debug error info warn]) (default info)
      --logoutput enum                     set the log output destination
                                              stdout: Write logs to standard output
                                              stderr: Write logs to standard error, useful for separating logs from normal output
                                           (must be one of [stderr stdout]) (default stderr)
      --plugin-directory string            default directory path for ocm plugins. (default "$HOME/.config/ocm/plugins")
      --plugin-shutdown-timeout duration   Timeout for plugin shutdown. If a plugin does not shut down within this time, it is forcefully killed (default 10s)
      --temp-folder string                 Specify a custom temporary folder path for filesystem operations.
      --working-directory string           Specify a custom working directory path to load resources from.
```

### SEE ALSO

* [ocm transfer]({{< relref "ocm_transfer.md" >}})	 - Transfer anything in OCM

