---
title: ocm transfer component-version
description: Transfer one or more component versions between OCM repositories.
suppressTitle: true
toc: true
sidebar:
  collapsed: true
---

## ocm transfer component-version

Transfer one or more component versions between OCM repositories

### Synopsis

Transfer component version(s) from a source repository to
a target repository using an internally generated transformation graph.

When a version is included in the source reference, exactly that version is transferred.
When the version is omitted, all versions of the component are discovered and transferred.
Use --constraint to restrict which versions are selected, and --latest to transfer
only the newest matching version.

OCI, CTF, and Helm repositories are supported as transfer sources.
OCI and CTF repositories are supported as transfer targets, while Helm repositories are not supported.

By default, local blobs are copied and all other resources stay by reference (their access
is unchanged in the target). Uploaders decide what happens to a resource; they come from the
OCM configuration and from --uploader, which adds entries after the configured ones. Each
uploader selects resources with a CEL match expression over resource and target (test access
types with resource.access.isType("OCIImage"), which resolves aliases and versions);
the first uploader whose match is true handles the resource. Available uploaders: oci
(separate OCI artifacts), http (custom HTTP targets), localblob (copy as local blobs) and
reference (keep by reference). --uploader localblob copies every resource no other uploader
selects. The copy mode setting is replaced by uploaders (see the "Migrate from --upload-as to
Uploader Configurations" guide on ocm.software). The deprecated --copy-resources and
--upload-as flags still work: they are translated into uploaders appended after all others
(--copy-resources as localblob, --upload-as ociArtifact as oci, limited to OCI-manifest local
blobs without --copy-resources). --recursive walks the component's references and transfers
them too.

Driving defaults from the OCM configuration:
  A transfer.config.ocm.software/v1alpha1 entry inside the central OCM configuration
  (passed via --config) sets defaults for --recursive.
  Explicit command-line flags always override the values from the configuration.

Two-step workflow (generate, review, replay):
  --dry-run builds and validates the graph without executing it, and with -o yaml|json prints
  the resulting TransformationGraphDefinition. --transfer-spec then replays a saved definition
  from a file (or stdin with "-"):
    1. Generate the spec:  transfer cv --dry-run -o yaml --uploader localblob -r {reference} {target} > spec.yaml
    2. Review/edit spec.yaml, then execute: transfer cv --transfer-spec spec.yaml
  All graph-shaping flags (--recursive, --uploader) and any transfer or uploader
  configuration entry are baked into the spec during step 1 and are therefore ignored in
  step 2 - the spec is the full graph definition. Only --dry-run, --output, and
  --concurrency-limit remain meaningful when replaying a spec.

How the graph is built:
  Internally the command assembles a TransformationGraphDefinition from these node types,
  selected based on the source/target references:
    1. CTFGetComponentVersion -> OCIGetComponentVersion
    2. CTFAddComponentVersion -> OCIAddComponentVersion
    3. GetOCIArtifact -> OCIAddLocalResource, or TransferOCIArtifact (OCI uploader)
    4. GetHelmChart -> ConvertHelmToOCI -> OCIAddLocalResource / AddOCIArtifact (OCI uploader)

```
ocm transfer component-version {reference} {target} [flags]
```

### Examples

```
# Transfer a component version from a CTF archive to an OCI registry
transfer component-version ctf::./my-archive//ocm.software/mycomponent:1.0.0 ghcr.io/my-org/ocm

# Transfer from one OCI registry to another
transfer component-version ghcr.io/source-org/ocm//ocm.software/mycomponent:1.0.0 ghcr.io/target-org/ocm

# Transfer all versions of a component (omit version from reference)
transfer component-version ctf::./my-archive//ocm.software/mycomponent ghcr.io/my-org/ocm

# Transfer all versions matching a version constraint
transfer component-version ctf::./my-archive//ocm.software/mycomponent ghcr.io/my-org/ocm --constraint ">= 1.0.0, < 2.0.0"

# Transfer only the latest version
transfer component-version ctf::./my-archive//ocm.software/mycomponent ghcr.io/my-org/ocm --latest

# Upload OCI images, Helm charts and OCI-manifest local blobs as separate OCI artifacts and copy
# every other resource as a local blob. Equivalent OCM configuration:
#   type: generic.config.ocm.software/v1
#   configurations:
#   - type: oci.uploader.transfer.config.ocm.software/v1alpha1
#   - type: localblob.uploader.transfer.config.ocm.software/v1alpha1
transfer component-version --uploader oci --uploader localblob ghcr.io/source-org/ocm//ocm.software/mycomponent:1.0.0 ghcr.io/target-org/ocm

# Keep one resource by reference and copy all others as local blobs
transfer component-version --uploader 'reference=resource.name == "base-os-image"' --uploader localblob ghcr.io/source-org/ocm//ocm.software/mycomponent:1.0.0 ghcr.io/target-org/ocm

# Transfer including all resources (e.g. OCI artifacts)
transfer component-version ctf::./my-archive//ocm.software/mycomponent:1.0.0 ghcr.io/my-org/ocm --uploader localblob

# Recursively transfer a component version and all its references
transfer component-version ghcr.io/source-org/ocm//ocm.software/mycomponent:1.0.0 ghcr.io/target-org/ocm -r --uploader localblob

# Drive defaults from the OCM configuration. With --config ./ocmconfig.yaml containing:
#   type: generic.config.ocm.software/v1
#   configurations:
#   - type: transfer.config.ocm.software/v1alpha1
#     recursive: -1
#   - type: localblob.uploader.transfer.config.ocm.software/v1alpha1
# the following invocation transfers recursively with all resources copied.
# Any explicit flag still overrides the corresponding configuration value.
transfer component-version --config ./ocmconfig.yaml ghcr.io/source-org/ocm//ocm.software/mycomponent:1.0.0 ghcr.io/target-org/ocm

# Two-step transfer: generate a spec with all desired flags, then review and execute
transfer component-version --dry-run -o yaml --uploader localblob -r ghcr.io/source-org/ocm//ocm.software/mycomponent:1.0.0 ghcr.io/target-org/ocm > spec.yaml
# (review/edit spec.yaml as needed, e.g. change the target registry)
transfer component-version --transfer-spec spec.yaml
```

### Options

```
      --concurrency-limit int   maximum number of transformation nodes processed in parallel; independent nodes run concurrently while dependency ordering is preserved. Increase it to speed up large graphs, decrease it to reduce load on the registry (default 4)
      --constraint string       version constraint evaluated by each version's configured scheme; versions with no applicable scheme are retained (e.g. ">= 1.0.0, < 2.0.0"); only used when no version is specified in the reference
      --dry-run                 build and validate the graph but do not execute
  -h, --help                    help for component-version
      --latest                  if set, only the latest version of the component is transferred; only used when no version is specified in the reference
  -o, --output enum             output format of the component descriptors
                                (must be one of [json ndjson yaml]) (default yaml)
  -r, --recursive               recursively discover and transfer component versions
      --transfer-spec string    path to a transfer specification file (use "-" for stdin). The input must hold exactly one transfer spec document; with "-", OCM configuration documents in stdin are applied as configuration
      --uploader stringArray    add an uploader configuration entry after those from the OCM configuration (repeatable, in the given order): an uploader name (http, localblob, oci, reference), <name>=<CEL match expression> (e.g. 'reference=resource.name == "my-image"'), or a YAML/JSON mapping of the entry whose "type" is a name. "localblob" copies every resource no other uploader selects
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
                                           Configuration documents piped into stdin are applied last, on top of these files.
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

* [ocm transfer]({{< relref "ocm_transfer.md" >}})	 - Transfer anything in OCM

