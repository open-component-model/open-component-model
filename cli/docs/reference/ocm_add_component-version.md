---
title: ocm add component-version
description: Add component version(s) to an OCM Repository based on a "component-constructor" file.
suppressTitle: true
toc: true
sidebar:
  collapsed: true
---

## ocm add component-version

Add component version(s) to an OCM Repository based on a "component-constructor" file

### Synopsis

Add component version(s) to an OCM repository that can be reused for transfers.

A "component-constructor" file is used to specify the component version(s) to be added. It can contain both a single component or many components.

By default, the command will look for a file named "component-constructor.yaml" or "component-constructor.yml" in the current directory.
If given a path to a directory, the command will look for a file named "component-constructor.yaml" or "component-constructor.yml" in that directory.
If given a path to a file, the command will attempt to use that file as the "component-constructor" file.

If you provide a working directory, all paths in the "component-constructor" file will be resolved relative to that directory.
Otherwise the path to the "component-constructor" file will be used as the working directory.
You are only allowed to reference files within the working directory or sub-directories of the working directory.

Environment Variable Substitution:

The "component-constructor" file supports environment variable substitution using Go template syntax.
Variables can be referenced using ${VAR_NAME} or $VAR_NAME format.
All environment variables are expanded before the file is processed, allowing for dynamic
configuration of component versions, resource paths, image references, and other values.

Example:
  components:
    - name: ${COMPONENT_NAME}
      version: ${COMPONENT_VERSION}
      provider:
        name: ${PROVIDER_NAME}
      resources:
        - name: my-image
          type: ociImage
          version: ${COMPONENT_VERSION}
          access:
            type: ociArtifact
            imageReference: ${REGISTRY_URL}/my-app:${IMAGE_TAG}

Repository Reference Format:
	[type::]{repository}

For known types, currently only {OCIRepository|CommonTransportFormat} are supported, which can be shortened to {OCI|oci|CTF|ctf} respectively for convenience.

If no type is given, the repository specification is interpreted based on introspection and heuristics:

- URL schemes or domain patterns -> OCI registry
- Local paths -> CTF archive

In case the CTF archive does not exist, it will be created by default.
If not specified, it will be created with the name "transport-archive".


```
ocm add component-version [flags]
```

### Examples

```
Adding component versions to a CTF archive:

add component-version --repository ./path/to/transport-archive --constructor ./path/to/component-constructor.yaml
add component-version --repository /tmp/my-archive --constructor constructor.yaml

Adding component versions to an OCI registry:

add component-version --repository ghcr.io/my-org/my-repo --constructor component-constructor.yaml
add component-version --repository https://my-registry.com/my-repo --constructor component-constructor.yaml
add component-version --repository localhost:5000/my-repo --constructor component-constructor.yaml

Specifying repository types explicitly:

add component-version --repository ctf::./local/archive --constructor component-constructor.yaml
add component-version --repository oci::http://localhost:8080/my-repo --constructor component-constructor.yaml

Using environment variables in "component-constructor" files:

export COMPONENT_NAME="github.com/my-org/my-app"
export COMPONENT_VERSION="1.2.3"
export REGISTRY_URL="ghcr.io/my-org"
add component-version --repository ./archive --constructor component-constructor.yaml
```

### Options

```
      --blob-cache-directory string                   path to the blob cache directory (default ".ocm/cache")
      --component-version-conflict-policy enum        policy to apply when a component version already exists in the repository
                                                      (must be one of [abort-and-fail replace skip]) (default abort-and-fail)
      --concurrency-limit int                         maximum number of component versions that can be constructed concurrently. (default 4)
  -c, --constructor path                              path to the component constructor file (default component-constructor.yaml)
      --display-mode enum                             display mode can be used in combination with --recursive
                                                        static: print the output once the complete component graph is discovered
                                                        live (experimental): continuously updates the output to represent the current construction state of the component graph
                                                      (must be one of [live static]) (default static)
      --external-component-version-copy-policy enum   policy to apply when a component reference to a component version outside of the constructor or target repository is encountered
                                                      (must be one of [copy-or-fail skip]) (default skip)
  -h, --help                                          help for component-version
  -o, --output enum                                   output format of the component descriptors
                                                      (must be one of [json ndjson table tree yaml]) (default table)
  -r, --repository string                             repository ref (default "transport-archive")
      --skip-reference-digest-processing              skip digest processing for resources and sources. Any resource referenced via access type will not have their digest updated.
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

* [ocm add]({{< relref "ocm_add.md" >}})	 - Add anything to OCM

