---
title: ocm skill push
description: Generate an OCM component-constructor for a directory of AI skills.
suppressTitle: true
toc: true
sidebar:
  collapsed: true
---

## ocm skill push

Generate an OCM component-constructor for a directory of AI skills

### Synopsis

Generate a component-constructor.yaml that packages all SKILL.md files found
in <skills-dir> as ai.skill/v1 resources inside a single OCM component.

Prints the constructor YAML to stdout by default. Use --output to write to a file.

After generation, run:
  ocm add component-version --repository <ref> --constructor <output-file>

```
ocm skill push <skills-dir> --component <name> --version <v> [flags]
```

### Examples

```
  # Print constructor to stdout
  ocm skill push ~/.claude/skills --component jakob.io/ai-skill-catalogue --version 1.0.0

  # Write constructor to file
  ocm skill push ~/.claude/skills --component jakob.io/ai-skill-catalogue --version 1.0.0 --output constructor.yaml
  ocm add component-version --repository ./catalogue --constructor constructor.yaml
```

### Options

```
      --component string    component name (required, e.g. jakob.io/ai-skill-catalogue)
  -h, --help                help for push
      --output string       write constructor YAML to this file instead of stdout
      --provider string     provider name (defaults to the domain part of the component name)
      --repository string   target repository reference (printed in usage hint) (default "transport-archive")
      --version string      component version (default "1.0.0")
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
                                           If multiple configuration files are found, they will be merged in the order they are discovered.
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

* [ocm skill]({{< relref "ocm_skill.md" >}})	 - Manage AI skills distributed via OCM component catalogues

