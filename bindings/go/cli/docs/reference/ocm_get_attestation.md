---
title: ocm get attestation
description: Get the attestations describing a component version, its resources, or one resource.
suppressTitle: true
toc: true
sidebar:
  collapsed: true
---

## ocm get attestation

Get the attestations describing a component version, its resources, or one resource

### Synopsis

Discover and list the attestations attached to a component version: SBOMs, VEX documents,
signatures, SLSA provenance, vulnerability reports, SARIF and anything else attached.

By default every attestation of the component version is listed: the component's own
referrers plus one group per resource of the component. Narrow the scope with:

  --identity name=X        only the attestations describing resource X.
  --content-digest DIGEST  only the attestation whose document bytes have this digest.
  --type TYPE              only attestations matching a friendly type or raw-type substring.
  --recursive              also follow componentReferences and list theirs too (-1 = unlimited).

Per resource, attestations are merged from every source:

  1. Resources stored in the component itself that declare, through the
     "ocm.software/artifact-references" label, that they describe the resource.
  2. OCI referrers attached to the resource's artifact (cosign, vexctl, oras, syft, notary, ...).
  3. BuildKit in-toto attestations embedded in the artifact's image index.

This is EXPERIMENTAL and read-only. The table and tree views list what exists from metadata
only, without downloading documents or verifying signatures. The json and yaml views instead
emit the documents themselves: each matching attestation is fetched and reduced to its decoded
inner payload (the real SBOM, VEX, provenance or other predicate, unwrapped from any in-toto
statement or Sigstore bundle), ready to pipe into another tool.

The tree nests the raw structure: the component or a resource, then the discovery source
(ocm-local, oci-referrer, oci-index), then each document. An enveloped document (an in-toto
statement or a Sigstore bundle) is a node named by its envelope media type, with its
predicate type(s) beneath it; a bare document is a leaf named by its media type. Each leaf
also carries a friendly TYPE tag (sbom, vex, signature, provenance, sarif) and the digest of
the document bytes. Documents with identical bytes attached in more than one place share that
digest and are tagged with the same letter, so
duplicates are easy to spot.

json emits an array of the decoded payloads (a bare object when a single document matches,
for example under --content-digest); yaml emits a stream separated by "---". Byte-identical
documents are emitted once; a document whose bytes are not JSON (a binary signature) is skipped.

Use --type to filter: it matches a substring of any raw type signal, plus friendly aliases
(for example "sbom" matches spdx and cyclonedx, "signature" matches cosign and notation).

```
ocm get attestation {reference} [flags]
```

### Examples

```
  # Every attestation of the component version and its resources
  ocm get attestations ghcr.io/acme//acme.org/app:1.0.0

  # As a provenance tree
  ocm get attestations ghcr.io/acme//acme.org/app:1.0.0 -o tree

  # Only the "image" resource
  ocm get attestations ghcr.io/acme//acme.org/app:1.0.0 --identity name=image

  # Print one document, by its content digest (the decoded payload as json)
  ocm get attestations ghcr.io/acme//acme.org/app:1.0.0 --content-digest sha256:abc123... -o json

  # Follow componentReferences and list their attestations too
  ocm get attestations ghcr.io/acme//acme.org/app:1.0.0 --recursive

  # Every SBOM and VEX document, as a yaml stream
  ocm get attestations ghcr.io/acme//acme.org/app:1.0.0 --type sbom --type vex -o yaml
```

### Options

```
      --concurrency-limit int   maximum number of resources inspected in parallel when listing the whole component (default 4)
      --content-digest string   filter to the attestation whose document content digest equals this value (for example sha256:...)
  -h, --help[=true|false]       help for attestation
      --identity string         list only the attestations describing this one resource
      --no-color[=true|false]   disable coloured output (also honoured via the NO_COLOR environment variable)
  -o, --output enum             output format
                                (must be one of [json table tree yaml]) (default table)
      --recursive int[=-1]      recursively follow componentReferences and list their attestations too; depth of recursion (-1 for unlimited)
      --type strings            filter to attestations whose type contains any of these substrings or matches a friendly alias (sbom, vex, signature, provenance, spdx, ...); repeatable; empty lists all
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

* [ocm get]({{< relref "ocm_get.md" >}})	 - Get anything from OCM

