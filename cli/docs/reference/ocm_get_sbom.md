---
title: ocm get sbom
description: Get the SBOM describing a resource of a component version.
suppressTitle: true
toc: true
sidebar:
  collapsed: true
---

## ocm get sbom

Get the SBOM describing a resource of a component version

### Synopsis

Get the Software Bill of Materials describing a resource of a component version.

The resource is selected with --identity. Its SBOM is then looked for in two ways, in order:

  1. Another resource of the same component version declaring, through the
     "ocm.software/artefact-references" label, that it describes the selected resource.
  2. For a resource backed by an OCI artifact, an SBOM attached to that artifact by
     "docker buildx build --sbom=true". SBOMs attached by other tooling, such as cosign
     or the OCI referrers API, are not discovered.

The SBOM document is written to standard output as it was published, so it can be piped
straight into a scanner.

```
ocm get sbom {reference} [flags]
```

### Examples

```
Getting the SBOM of a resource:

ocm get sbom ghcr.io/open-component-model//ocm.software/tutorial-sbom:1.0.0 --identity name=cli
ocm get sbom ./path/to/ctf//ocm.software/tutorial-sbom:1.0.0 --identity name=cli

Selecting one of several builds of the same resource:

ocm get sbom ghcr.io/org//ocm.software/product:1.0.0 --identity name=cli,architecture=arm64

Piping into a scanner:

ocm get sbom ghcr.io/org//ocm.software/product:1.0.0 --identity name=image -o raw > sbom.json
```

### Options

```
  -h, --help              help for sbom
      --identity string   identity of the resource to get the SBOM for
  -o, --output enum       output format of the SBOM document. 'raw' writes the document byte for byte, which is what any signature or digest over it covers
                          (must be one of [json raw]) (default json)
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

* [ocm get]({{< relref "ocm_get.md" >}})	 - Get anything from OCM

