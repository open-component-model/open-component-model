---
title: ocm download attestation
description: Download the attestation documents describing a component version, its resources, or one resource.
suppressTitle: true
toc: true
sidebar:
  collapsed: true
---

## ocm download attestation

Download the attestation documents describing a component version, its resources, or one resource

### Synopsis

Download the attestation documents attached to a component version: SBOMs, VEX documents,
signatures, SLSA provenance, vulnerability reports, SARIF and anything else attached.

By default only the attestations attached to the component version artifact itself are downloaded.
Widen or narrow the scope with:

  --identity name=X   only the attestations describing resource X.
  --all-resources     every resource of the component plus the component's own referrers.
  --recursive         also follow componentReferences and download their attestations (-1 = unlimited).

Per resource, attestations are merged from every source:

  1. Resources stored in the component itself that declare, through the
     "ocm.software/artifact-references" label, that they describe the resource.
  2. OCI referrers attached to the resource's artifact (cosign, vexctl, oras, syft, notary, ...).
  3. BuildKit in-toto attestations embedded in the artifact's image index.

Each attestation's primary document is written to its own file in the output directory, byte for
byte as published, so digests and signatures over it still apply. The file name is a friendly label
derived from the type, a short id and a media-type extension; the paths written are printed to
standard output, one per line.

This is EXPERIMENTAL: what is discovered, how it is written out and the flags themselves may change
in a future release. Use --type to filter: it matches a substring of any raw type signal, plus
friendly aliases (for example "sbom" matches spdx and cyclonedx, "signature" matches cosign and
notation).

```
ocm download attestation {reference} [flags]
```

### Examples

```
  # The component's own referrers, into ./attestations
  ocm download attestation ghcr.io/acme//acme.org/app:1.0.0

  # Every resource plus the component's own referrers, into a chosen directory
  ocm download attestation ghcr.io/acme//acme.org/app:1.0.0 --all-resources --output ./atts

  # Only the "image" resource
  ocm download attestation ghcr.io/acme//acme.org/app:1.0.0 --identity name=image

  # Follow componentReferences and download their attestations too
  ocm download attestation ghcr.io/acme//acme.org/app:1.0.0 --all-resources --recursive

  # Only SBOMs
  ocm download attestation ghcr.io/acme//acme.org/app:1.0.0 --all-resources --type sbom
```

### Options

```
      --all-resources[=true|false]   download the attestations of every resource of the component plus the component's own referrers
      --concurrency-limit int        maximum number of resources inspected in parallel when downloading the whole component (--all-resources) (default 4)
  -h, --help[=true|false]            help for attestation
      --identity string              download only the attestations describing this one resource
  -o, --output string                output directory for the downloaded attestation documents (default "attestations")
      --recursive int[=-1]           recursively follow componentReferences and download their attestations too; depth of recursion (-1 for unlimited)
      --type strings                 filter to attestations whose type contains any of these substrings or matches a friendly alias (sbom, vex, signature, provenance, spdx, ...); repeatable; empty downloads all
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
                                           Use "-" to read configuration documents from stdin. It is an entry like any other: on its own it
                                           replaces the lookup above, so add --config <file> to keep a file, and a repeated "-" is ignored.
                                           Other documents on stdin stay there for commands that read it, such as transfer with --transfer-spec -.
      --logformat enum                     set the log output format that is used to print individual logs
                                              json: Output logs in JSON format, suitable for machine processing
                                              text: Output logs in human-readable text format, suitable for console output
                                              the interactive progress display needs a terminal and the text format; any other format prints its records in full instead
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

