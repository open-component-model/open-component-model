---
title: ocm version
description: Retrieve the build version of the OCM CLI.
suppressTitle: true
toc: true
sidebar:
  collapsed: true
---

## ocm version

Retrieve the build version of the OCM CLI

### Synopsis

The version command retrieves the build version of the OCM CLI.

The build version can be formatted in different ways depending on the specified format flag.
The default format is "legacyjson", which outputs the version in a format compatible with OCM v1 specifications,
with slight modifications:

- "gitTreeState" is removed in favor of "meta" field, which contains the git tree state.
- "buildDate" and "gitCommit" are derived from the input version string, and are parsed according to the go module version specification.

When the format is set to "gobuildinfo", it outputs the Go build information as a string. The format is standardized
and unified across all golang applications.

When the format is set to "gobuildinfojson", it outputs the Go build information in JSON format.
This is equivalent to "gobuildinfo", but in a structured JSON format.

The build info by default is drawn from the go module build information, which is set at build time of the CLI.
When officially built, it is possibly overwritten with the released version of the OCM CLI.

```
ocm version [flags]
```

### Examples

```
ocm version --format legacyjson
```

### Options

```
  -f, --format string   format of the generated documentation (default "legacyjson")
  -h, --help            help for version
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
      --idle-conn-timeout duration         HTTP idle connection timeout (e.g. "90s"). Overrides config file value.
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
      --response-header-timeout duration   HTTP response header timeout (e.g. "10s"). Overrides config file value.
      --tcp-dial-timeout duration          TCP dial timeout for establishing connections (e.g. "30s"). Overrides config file value.
      --tcp-keep-alive duration            TCP keep-alive interval (e.g. "30s"). Overrides config file value.
      --temp-folder string                 Specify a custom temporary folder path for filesystem operations.
      --timeout duration                   HTTP client timeout, overriding the config file value (e.g. "30s", "5m"). Use "0" to disable the timeout.
      --tls-handshake-timeout duration     TLS handshake timeout (e.g. "10s"). Overrides config file value.
      --working-directory string           Specify a custom working directory path to load resources from.
```

### SEE ALSO

* [ocm]({{< relref "ocm.md" >}})	 - The official Open Component Model (OCM) CLI

