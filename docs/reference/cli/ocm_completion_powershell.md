---
title: ocm completion powershell
description: Generate the autocompletion script for powershell.
suppressTitle: true
toc: true
sidebar:
  collapsed: true
---

## ocm completion powershell

Generate the autocompletion script for powershell

### Synopsis

Generate the autocompletion script for powershell.

To load completions in your current shell session:

	ocm completion powershell | Out-String | Invoke-Expression

To load completions for every new session, add the output of the above command
to your powershell profile.


```
ocm completion powershell [flags]
```

### Options

```
  -h, --help              help for powershell
      --no-descriptions   disable completion descriptions
```

### Options inherited from parent commands

```
      --config string    supply configuration by a given configuration file.
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
      --logformat enum   set the log output format that is used to print individual logs
                            json: Output logs in JSON format, suitable for machine processing
                            text: Output logs in human-readable text format, suitable for console output
                         (must be one of [json text]) (default text)
      --loglevel enum    sets the logging level
                            debug: Show all logs including detailed debugging information
                            info:  Show informational messages and above
                            warn:  Show warnings and errors only (default)
                            error: Show errors only
                         (must be one of [debug error info warn]) (default info)
      --logoutput enum   set the log output destination
                            stdout: Write logs to standard output (default)
                            stderr: Write logs to standard error, useful for separating logs from normal output
                         (must be one of [stderr stdout]) (default stdout)
```

### SEE ALSO

* [ocm completion](ocm_completion.md)	 - Generate the autocompletion script for the specified shell

