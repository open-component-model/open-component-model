## ocm generate

Generate documentation for the OCM CLI

### Synopsis

A longer description that spans multiple lines and likely contains examples
and usage of using your command. For example:

Cobra is a CLI library for Go that empowers applications.
This application is a tool to generate the needed files
to quickly create a Cobra application.

### Options

```
  -h, --help   help for generate
```

### Options inherited from parent commands

```
      --config string      supply configuration by a given configuration file.
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
  -f, --logformat string   set the log format (text, json) (default "text")
      --loglevel enum      set the log level (debug, info, warn, error, fatal) (must be one of [debug error info warn]) (default warn)
```

### SEE ALSO

* [ocm](ocm.md)	 - The official Open Component Model (OCM) CLI
* [ocm generate docs](ocm_generate_docs.md)	 - Generation Documentation for the CLI

