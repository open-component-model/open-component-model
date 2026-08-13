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

If no transformer is specified, the resource is written directly in its original format.

Resources can be accessed either locally or via a plugin that supports remote fetching, with optional credential resolution.

When --output is not provided, the output filename is the resource name.

With --sbom, the Software Bills of Materials describing the resource are downloaded instead of the
resource itself. This is EXPERIMENTAL: what is discovered, how it is written out and the flags
themselves may change in a future release. They are looked for in two ways, in order:

  1. Another resource of the same component version declaring, through the
     "ocm.software/artifact-references" label, that it describes the selected resource.
  2. For a resource backed by an OCI artifact, SBOMs attached to that artifact by
     "docker buildx build --sbom=true". SBOMs attached by other tooling, such as cosign
     or the OCI referrers API, are not discovered.

Every SBOM found is written to its own file in a directory, byte for byte as published, so digests
and signatures over them still apply. The directory is --output, or the values of the resource
identity joined by "-" when that is not given, so --identity name=image,architecture=amd64 writes
into "image-amd64". The paths written are printed to standard output, one per line.

```
ocm download resource [flags]
```

### Examples

```
 # Download a resource with identity 'name=example' and write to default output
  ocm download resource ghcr.io/org/component:v1 --identity name=example

  # Download a resource with identity 'name=example' and 'architecture=amd64' and write to default output
  ocm download resource ghcr.io/org/component:v1 --identity name=example,architecture=amd64

  # Download a resource and specify an output file
  ocm download resource ghcr.io/org/component:v1 --identity name=example --output ./my-resource.tar.gz

  # Download a resource and apply a transformer
  ocm download resource ghcr.io/org/component:v1 --identity name=example --transformer my-transformer

  # Download every SBOM describing a resource into a directory
  ocm download resource ghcr.io/org/component:v1 --identity name=example --sbom --output ./sboms

  # Scan every SBOM found for a resource
  ocm download resource ghcr.io/org/component:v1 --identity name=example --sbom | xargs -n1 grype sbom:
```

### Options

```
      --extraction-policy enum   policy to apply when extracting a resource. If set to 'disable', the resource will not be extracted, even if they could be. If set to 'auto', the resource will be automatically extracted if the returned resource is a recognized archive format.
                                 (must be one of [auto disable]) (default auto)
  -h, --help                     help for resource
      --identity string          resource identity to download
      --output string            output path. With --extraction-policy auto, extractable archives are extracted into this directory; otherwise, the resource is saved as this file path. Intermediate directories are created automatically. If not provided, defaults to the resource name. With --sbom this is a single file, and standard output is used when it is not given.
      --sbom                     download the SBOMs describing the resource instead of the resource itself, combining every SBOM found into a single document
      --sbom-format enum         format to write the combined SBOM in. Only valid together with --sbom.
                                 (must be one of [cyclonedx spdx]) (default spdx)
      --transformer string       transformer to use for the output. If not specified, the resource will be written as is. 
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

* [ocm download]({{< relref "ocm_download.md" >}})	 - Download anything from OCM

