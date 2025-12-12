---
title: ocm get component-version
description: Get component version(s) from an OCM repository.
suppressTitle: true
toc: true
sidebar:
  collapsed: true
---

## ocm get component-version

Get component version(s) from an OCM repository

### Synopsis

Get component version(s) from an OCM repository.

The format of a component reference is:
	[type::]{repository}/[valid-prefix]/{component}[:version]

For valid prefixes {component-descriptors|none} are available. If <none> is used, it defaults to "component-descriptors". This is because by default,
OCM components are stored within a specific sub-repository.

For known types, currently only {OCIRepository|CommonTransportFormat} are supported, which can be shortened to {OCI|oci|CTF|ctf} respectively for convenience.

If no type is given, the repository path is interpreted based on introspection and heuristics.


```
ocm get component-version {reference} [flags]
```

### Examples

```
Getting a single component version:

get component-version ghcr.io/open-component-model/ocm//ocm.software/ocmcli:0.23.0
get cv ./path/to/ctf//ocm.software/ocmcli:0.23.0
get cv ./path/to/ctf/component-descriptors/ocm.software/ocmcli:0.23.0

Listing many component versions:

get component-versions ghcr.io/open-component-model/ocm//ocm.software/ocmcli
get cvs ghcr.io/open-component-model/ocm//ocm.software/ocmcli --output json
get cvs ghcr.io/open-component-model/ocm//ocm.software/ocmcli -oyaml

Specifying types and schemes:

get cv ctf::github.com/locally-checked-out-repo//ocm.software/ocmcli:0.23.0
get cvs oci::http://localhost:8080//ocm.software/ocmcli
```

### Options

```
      --display-mode enum          display mode can be used in combination with --recursive
                                     static: print the output once the complete component graph is discovered
                                     live (experimental): continuously updates the output to represent the current discovery state of the component graph
                                   (must be one of [live static]) (default static)
  -h, --help                       help for component-version
      --latest                     if set, only the latest version of the component is returned
  -o, --output enum                output format of the component descriptors
                                   (must be one of [json ndjson table tree yaml]) (default table)
      --recursive int[=-1]         depth of recursion for resolving referenced component versions (0=none, -1=unlimited, >0=levels (not implemented yet))
      --semver-constraint string   semantic version constraint restricting which versions to output (default "> 0.0.0-0")
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

* [ocm get]({{< relref "ocm_get.md" >}})	 - Get anything from OCM

