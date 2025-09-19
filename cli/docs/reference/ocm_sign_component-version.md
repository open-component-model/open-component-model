---
title: ocm sign component-version
description: Sign component version(s) inside an OCM repository.
suppressTitle: true
toc: true
sidebar:
  collapsed: true
---

## ocm sign component-version

Sign component version(s) inside an OCM repository

### Synopsis

Sign component version(s) inside an OCM repository.

The format of a component reference is:
	[type::]{repository}/[valid-prefix]/{component}[:version]

For valid prefixes {component-descriptors|none} are available. If <none> is used, it defaults to "component-descriptors". This is because by default,
OCM components are stored within a specific sub-repository.

For known types, currently only {OCIRepository|CommonTransportFormat} are supported, which can be shortened to {OCI|oci|CTF|ctf} respectively for convenience.

If no type is given, the repository path is interpreted based on introspection and heuristics.


```
ocm sign component-version {reference} [flags]
```

### Examples

```
Signing a single component version:

sign component-version ghcr.io/open-component-model/ocm//ocm.software/ocmcli:0.23.0 --signature my-signature
```

### Options

```
      --concurrency-limit int       maximum amount of parallel requests to the repository for resolving component versions (default 4)
      --dry-run                     if enabled, the signature is not actually written to the repository
      --force                       if enabled, existing signatures under the attempted name are overwritten
      --hash string                 algorithm to use for hashing the normalised component version (default "SHA-256")
  -h, --help                        help for component-version
      --normalisation string        algorithm to use for normalising the component version (default "jsonNormalisation/v4alpha1")
      --signature string            name of the signature to verify. if not set, all signatures are verified
      --signer-spec string          path to an optional signer specification file
      --verify-digest-consistency   if enabled, all signature digests are verified before the signature itself is verified (default true)
```

### Options inherited from parent commands

```
      --config string                      supply configuration by a given configuration file.
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

* [ocm sign]({{< relref "ocm_sign.md" >}})	 - sign anything in OCM

