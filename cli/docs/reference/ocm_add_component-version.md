---
title: ocm add component-version
description: Add component version(s) to an OCM Repository stored as Common Transport Format Archive (CTF) based on a "component-constructor" file.
suppressTitle: true
toc: true
sidebar:
  collapsed: true
---

## ocm add component-version

Add component version(s) to an OCM Repository stored as Common Transport Format Archive (CTF) based on a "component-constructor" file

### Synopsis

Add component version(s) to an OCM Common Transport Format Archive (CTF) that can be reused
for transfers.

A "component-constructor" file is used to specify the component version(s) to be added. It can contain both a single component or many
components. The component reference is used to determine the repository to add the components to.

By default, the command will look for a file named ""component-constructor".yaml" or ""component-constructor".yml" in the current directory.
If given a path to a directory, the command will look for a file named "component-constructor.yaml" or "component-constructor.yml" in that directory.
If given a path to a file, the command will attempt to use that file as the "component-constructor" file.

In case the component archive does not exist, it will be created by default.


```
ocm add component-version [flags]
```

### Examples

```
Adding component versions to a non-default CTF named "transport-archive" based on a non-default default "component-constructor" file:

add component-version  --repository ./path/to/transport-archive --constructor ./path/to/component-constructor.yaml
```

### Options

```
      --blob-cache-directory string              path to the blob cache directory (default ".ocm/cache")
      --component-version-conflict-policy enum   policy to apply when a component version already exists in the repository
                                                 (must be one of [abort-and-fail replace skip]) (default abort-and-fail)
      --concurrency-limit int                    maximum number of component versions that can be constructed concurrently. (default 4)
  -c, --constructor path                         path to the component constructor file (default component-constructor.yaml)
  -h, --help                                     help for component-version
  -r, --repository path                          path to the repository (default transport-archive)
      --skip-reference-digest-processing         skip digest processing for resources and sources. Any resource referenced via access type will not have their digest updated.
```

### Options inherited from parent commands

```
      --config string        supply configuration by a given configuration file.
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
      --logformat enum       set the log output format that is used to print individual logs
                                json: Output logs in JSON format, suitable for machine processing
                                text: Output logs in human-readable text format, suitable for console output
                             (must be one of [json text]) (default text)
      --loglevel enum        sets the logging level
                                debug: Show all logs including detailed debugging information
                                info:  Show informational messages and above
                                warn:  Show warnings and errors only (default)
                                error: Show errors only
                             (must be one of [debug error info warn]) (default info)
      --logoutput enum       set the log output destination
                                stdout: Write logs to standard output (default)
                                stderr: Write logs to standard error, useful for separating logs from normal output
                             (must be one of [stderr stdout]) (default stdout)
      --temp-folder string   Specify a custom temporary folder path for filesystem operations.
```

### SEE ALSO

* [ocm add]({{< relref "ocm_add.md" >}})	 - Add anything to OCM

