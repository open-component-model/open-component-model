---
title: ocm download resource
description: Download resources described in a component version in an OCM Repository.
suppressTitle: true
toc: true
sidebar:
  collapsed: true
---

## ocm download resource

Download resources described in a component version in an OCM Repository

### Synopsis

Download a resource from a component version located in an Open Component Model (OCM) repository.

This command fetches a specific resource from the given OCM component version reference and stores it at the specified output location. 
It supports optional transformation of the resource using a registered transformer plugin.

If no transformer is specified, the resource is written directly in its original format. If the media type is known,
the appropriate file extension will be added to the output file name if no output location is given.

Resources can be accessed either locally or via a plugin that supports remote fetching, with optional credential resolution.

```
ocm download resource [flags]
```

### Examples

```
 # Download a resource with identity 'name=example' and write to default output
  ocm download resource ghcr.io/org/component:v1 --identity name=example

  # Download a resource and specify an output file
  ocm download resource ghcr.io/org/component:v1 --identity name=example --output ./my-resource.tar.gz

  # Download a resource and apply a transformer
  ocm download resource ghcr.io/org/component:v1 --identity name=example --transformer my-transformer
```

### Options

```
      --extraction-policy enum   policy to apply when extracting a resource. If set to 'disable', the resource will not be extracted, even if they could be. If set to 'auto', the resource will be automatically extracted if the returned resource is a recognized archive format.
                                 (must be one of [auto disable]) (default auto)
  -h, --help                     help for resource
      --identity string          resource identity to download
      --output string            output location to download to. If no transformer is specified, and no format was discovered that can be written to a directory, the resource will be written to a file.
      --transformer string       transformer to use for the output. If not specified, the resource will be written as is. 
```

### Options inherited from parent commands

```
      --config string                      supply configuration by a given configuration file.
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

* [ocm download]({{< relref "ocm_download.md" >}})	 - Download anything from OCM

