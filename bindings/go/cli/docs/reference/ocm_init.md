---
title: ocm init
description: Scaffold an OCM component-constructor file by inspecting a local repository.
suppressTitle: true
toc: true
sidebar:
  collapsed: true
---

## ocm init

Scaffold an OCM component-constructor file by inspecting a local repository

### Synopsis

Inspect a local repository and write a component-constructor.yaml scaffold you
can build with "ocm add cv".

By default classification is fully deterministic and needs no configuration: it
reads detected manifests (go.mod, Chart.yaml, Dockerfile, package.json, ...) and
git signals (origin, tags, branch) to decide the component kind, version, and
source. It works offline and produces the same result every time.

For a monorepo with several independent deliverables it emits one component per
sub-deliverable plus a root aggregate that references them. A single component
that merely keeps build scaffolding or tooling in subdirectories stays one
component.

Pass --ai-assist to route genuinely ambiguous repositories through the TypeSafe
Jev decisions model (configured via init.cli.config.ocm.software/v1alpha1). If no
API key is available it transparently falls back to the deterministic rules, so
the command never blocks on a missing key.

The command only WRITES the file; run "ocm add cv --constructor <file>" to build
the component version.

```
ocm init [path] [flags]
```

### Options

```
      --ai-assist              use the Jev decisions model to refine ambiguous classifications (falls back to deterministic rules if no API key is configured)
      --dry-run                print the constructor YAML to stdout instead of writing a file
  -h, --help                   help for init
      --min-confidence float   with --ai-assist, minimum model confidence to act on a decision; below this the deterministic result is kept (default 0.75)
      --offline                skip the GitHub Releases network call; resolve versions from git tags only
  -o, --output string          path to write the component-constructor file to (default "component-constructor.yaml")
```

### Options inherited from parent commands

```
      --config stringArray                 supply configuration by a given configuration file.
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

* [ocm]({{< relref "ocm.md" >}})	 - The official Open Component Model (OCM) CLI

