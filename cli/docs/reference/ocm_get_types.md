---
title: ocm get types
description: Describe OCM types and their configuration schema.
suppressTitle: true
toc: true
sidebar:
  collapsed: true
---

## ocm get types

Describe OCM types and their configuration schema

### Synopsis

Describe OCM types registered in various subsystems.

WHAT ARE SUBSYSTEMS?
  OCM uses a plugin-based architecture where different types of functionality are organized
  into subsystems. Each subsystem is a collection of related type implementations. When you
  use OCM commands or configure OCM resources, you specify types from these subsystems.

  Common subsystems:
    - ocm-repository:          Where component versions are stored (OCI registries, CTF archives)
    - ocm-resource-repository: Where resources within components are stored
    - input:                   How content is sourced (from files, directories, etc.) in component constructors
    - credential-repository:   Where credentials are stored and retrieved
    - signing:                 How component versions are signed and verified

HOW TO USE SUBSYSTEMS:
  When creating OCM configurations (YAML files) or using CLI commands, you'll specify a 'type'
  field. This type comes from one of the subsystems. For example:

  In a repository configuration:
    type: OCIRepository/v1        # Type from ocm-repository subsystem
    spec:
      baseUrl: ghcr.io

  In an input specification:
    type: dir/v1                  # Type from input subsystem
    spec:
      path: ./my-content

  Use this command to:
    1. Discover what subsystems exist
    2. See what types are available in each subsystem
    3. Learn what fields each type requires

EXPLORATION WORKFLOW:
  1. List all subsystems (no arguments)
  2. Pick a subsystem and list its types (one argument: subsystem name)
  3. View field details for a specific type (two arguments: subsystem and type name)

FIELD PATH NAVIGATION:
  You can drill into nested object fields using dot notation as an optional third argument.
  This shows only the fields within the specified nested structure, making it easier to explore
  complex schemas.

  Examples:
    ocm describe types ocm-repository oci baseUrl
    ocm describe types input file spec.file

OUTPUT FORMATS:
  Use -o/--output to control the format:
    - text:       Human-readable table format (default, best for terminal)
    - markdown:   Markdown tables (good for documentation)
    - html:       HTML tables (good for web publishing)
    - jsonschema: Raw JSON Schema (only for type descriptions, not lists)
    - examples:   Generate example YAML configuration (only for type descriptions)

```
ocm get types [subsystem [type [field-path]]] [flags]
```

### Examples

```
  # Workflow: Setting up an OCI repository
  # Step 1: Discover available repository types
  ocm describe types ocm-repository

  # Step 2: Learn about the OCI repository type
  ocm describe types ocm-repository oci/v1

  # Workflow: Configuring input methods for component creation
  # Step 1: See what input methods are available
  ocm describe types input

  # Step 2: Learn about the directory input type
  ocm describe types input dir/v1

  # Other useful commands:
  # List all subsystems to see what's available
  ocm describe types

  # Navigate into nested configuration fields
  ocm describe types ocm-repository oci/v1 baseUrl

  # List all available field paths for navigation
  ocm describe types input file/v1 --show-paths

  # Export documentation as markdown for your team
  ocm describe types input file -o markdown > signing-docs.md
```

### Options

```
  -h, --help               help for types
  -o, --output enum        Output format (text, markdown, html are supported for all command combinations, jsonschema is only supported for type descriptions).
                           (must be one of [examples html jsonschema markdown text]) (default text)
      --show-paths         List all available field paths for the type (useful for navigation)
      --table-style enum   table output style
                           (must be one of [StyleColoredBright StyleColoredDark StyleDefault]) (default StyleDefault)
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

