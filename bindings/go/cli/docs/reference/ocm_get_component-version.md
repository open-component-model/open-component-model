---
title: ocm get component-version
description: Get component version(s) from an OCM repository.
suppressTitle: true
toc: true
sidebar:
  collapsed: true
---

## ocm get component-version

Get component version(s) from an OCM repository

### Synopsis

Get component version(s) from an OCM repository.

The format of a component reference is:

	[type::]{repository}/[valid-prefix]/{component}[:version][@digest]

For valid prefixes {component-descriptors|none} are available. If <none> is used, it defaults to "component-descriptors". This is because by default,
OCM components are stored within a specific sub-repository.

For known types, currently only {OCIRepository|CommonTransportFormat} are supported, which can be shortened to {OCI|oci|CTF|ctf} respectively for convenience.

A digest pins the component version: the command fails unless the component version matches it.
Two forms are accepted, each with sha256 or sha512:

- "@sha256:<hex>" (OCI digest) is the digest of the OCI image index or manifest the component version is
  stored as, the digest its version tag resolves to (for example "oras resolve
  {repository}/component-descriptors/{component}:{version}"). The component version is then read from that
  artifact instead of through the tag, like an OCI image pinned by digest. The digest covers the descriptor
  and all local blobs, so the version may be omitted; if given, the artifact must hold it. Only OCI
  registries and CTF archives support it, and it changes when the version is transferred or signed.
- "@jsonNormalisation.v4alpha3+sha256:<hex>" (normalized) is the hash of the descriptor normalised with
  jsonNormalisation/v4alpha3, the digest "ocm sign" signs and logs. It stays the same across transfer and
  signing, but does not cover access information or labels that are excluded from signing. A reference
  pinned to it must name the version.

If no type is given, the repository path is interpreted based on introspection and heuristics.

When the reference is a repository (no component), every component in the repository is listed. Components
that cannot be read (for example missing pull permission or an unsupported descriptor schema) are reported
and skipped; the command still succeeds unless no component can be read.

The --detail level controls how much is fetched for a repository reference:
  names (default): component names only, from the catalog (one request, fast);
  versions: also list the versions of each component (one request per component);
  full: fetch descriptors, needed for --recursive and the tree outputs.
When --detail is not set, the cheapest level that satisfies the other flags is used: --latest or
--constraint select versions; --recursive or -o tree|widetree select full. Listing a CTF archive now
defaults to names too; pass --detail full for the previous descriptor output.

For a component reference without a version (repo//component), --detail versions lists its versions
without fetching descriptors, and full (the default) fetches them. A pinned reference
(repo//component:version or @digest) is always full.


```
ocm get component-version {reference} [flags]
```

### Examples

```
Getting a single component version:

get component-version ghcr.io/open-component-model//ocm.software/cli:0.12.0
get cv ./path/to/ctf//ocm.software/cli:0.12.0
get cv ./path/to/ctf/component-descriptors/ocm.software/cli:0.12.0
get cv ghcr.io/open-component-model//ocm.software/cli@sha256:<oci-digest>
get cv ghcr.io/open-component-model//ocm.software/cli:0.12.0@jsonNormalisation.v4alpha3+sha256:<signed-digest>

Listing components of a repository (no component in the reference):

get cvs ghcr.io/open-component-model                         # component names (fast, default)
get cvs ghcr.io/open-component-model --detail versions       # every version of every component
get cvs ghcr.io/open-component-model --detail versions --latest
get cvs ghcr.io/open-component-model --detail full -o yaml   # descriptors
get cv ghcr.io/open-component-model//ocm.software/cli --detail versions   # versions of one component

Listing many component versions:

get component-versions ghcr.io/open-component-model//ocm.software/cli
get cvs ghcr.io/open-component-model//ocm.software/cli --output json
get cvs ghcr.io/open-component-model//ocm.software/cli -oyaml

Showing the resources of each component version in the tree output:

get cv ./path/to/ctf//ocm.software/cli:0.12.0 -o widetree
get cvs ghcr.io/open-component-model//ocm.software/cli -o widetree --recursive

Specifying types and schemes:

get cv ctf::github.com/locally-checked-out-repo//ocm.software/cli:0.12.0
get cvs oci::localhost:8080//ocm.software/cli

A registry without TLS needs plainHTTP in the HTTP client configuration; an http:// prefix on the
repository is deprecated and fails without it (see https://ocm.software/docs/changelog/migrate-from-http-prefixes/).
```

### Options

```
      --constraint string     version constraint restricting which versions to output, evaluated by each version's configured scheme; versions with no applicable scheme are retained (default "> 0.0.0-0")
      --detail enum           detail level when listing (a repository reference, or a component reference without a version):
                                names (default for a repository): component names only, from the catalog (cheapest)
                                versions: also list the versions of each component (one request per component)
                                full: fetch descriptors; required for --recursive and the tree outputs; the default for a component reference
                              When unset, the cheapest level that satisfies the other flags is used.
                              (must be one of [full names versions]) (default names)
      --display-mode enum     display mode can be used in combination with --recursive
                                static: print the output once the complete component graph is discovered
                                live (experimental): continuously updates the output to represent the current discovery state of the component graph
                              (must be one of [live static]) (default static)
  -h, --help[=true|false]     help for component-version
      --latest[=true|false]   if set, only the latest version of the component is returned
  -o, --output enum           output format of the component descriptors
                              (must be one of [json ndjson table tree widetree yaml]) (default tree)
      --recursive int[=-1]    depth of recursion for resolving referenced component versions (0=none, -1=unlimited, >0=levels (not implemented yet))
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

