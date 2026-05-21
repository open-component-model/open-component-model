---
title: ocm get owner
description: Get owning component version(s) of an artifact.
suppressTitle: true
toc: true
sidebar:
  collapsed: true
---

## ocm get owner

Get owning component version(s) of an artifact

### Synopsis

Get the owning component version(s) of an artifact.

<!-- OCM-REVIEW-last-commit-2: [blocking] [since 2026-05-27] this synopsis (and the example below at
     line 35) describes auto-detect dispatch the command does not do. cmd.go:32's Long says
     "Today only 'oci::' is recognized" and cmd.go:77 rejects everything else. Regenerate the doc
     against the current Long/Example.
     (see tmp/ocm-review-last-commit.md) -->
The artifact's access type is auto-detected from the reference by asking each
registered resource backend; today that means OCI image references, with Helm
chart and other backends opt-in as they implement the ownership-lookup
capability. The matched backend is then queried for ownership referrers and
the owning component versions are resolved from the OCM repository and
rendered the same way as 'ocm get component-version'.

'-o json' short-circuits the component-version resolution step and emits the
raw owner-lookup payload directly.

```
ocm get owner {ref} [flags]
```

### Examples

```
  ocm get owner ghcr.io/acme/ocm/component-descriptors/ocm.software/app@sha256:abc...
```

### Options

```
  -h, --help          help for owner
  -o, --output enum   output format
                      (must be one of [json table yaml]) (default table)
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

* [ocm get]({{< relref "ocm_get.md" >}})	 - Get anything from OCM

