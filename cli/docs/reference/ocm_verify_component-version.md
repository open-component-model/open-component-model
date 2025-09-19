---
title: ocm verify component-version
description: Verify component version(s) inside an OCM repository.
suppressTitle: true
toc: true
sidebar:
  collapsed: true
---

## ocm verify component-version

Verify component version(s) inside an OCM repository

### Synopsis

Verify component version(s) inside an OCM repository.

If this command succeeds on a trusted signature, it can be trusted.

This command checks cryptographic signatures stored on component versions
to ensure integrity, authenticity, and provenance. Each signature covers a
normalised and hashed form of the component descriptor, which is compared
against the expected digest and verified with the configured verifier.

The format of a component reference is:
	[type::]{repository}/[valid-prefix]/{component}[:version]

Valid prefixes: {component-descriptors|none}. If <none> is used, it defaults to "component-descriptors".
Supported repository types: {OCIRepository|CommonTransportFormat} (short forms: {OCI|oci|CTF|ctf}).
If no type is given, the repository path is inferred by heuristics.

Verification steps performed:
  * Resolve the repository and fetch the target component version.
  * Verify digest consistency if not disabled (--verify-digest-consistency).
  * Normalise the descriptor with the algorithm recorded in the signature.
  * Recompute the hash and compare with the signature digest.
  * Verify the signature against the provided verifier specification (--verifier-spec),
    or fall back to the default RSASSA-PSS verifier if not specified.

Behavior:
  * If --signature is set, only the named signature is verified.
  * Without --signature, all available signatures are verified.
  * Verification fails fast on the first invalid signature.

Use this command in automated pipelines or audits to validate the
authenticity of component versions before promotion, deployment,
or further processing.

```
ocm verify component-version {reference} [flags]
```

### Options

```
      --concurrency-limit int       maximum amount of parallel requests to the repository for resolving component versions (default 4)
  -h, --help                        help for component-version
      --signature string            name of the signature to verify. if not set, all signatures are verified
      --verifier-spec string        path to an optional verifier specification file
      --verify-digest-consistency   verify that all digests match the descriptor before verifying the signature itself (default true)
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

* [ocm verify]({{< relref "ocm_verify.md" >}})	 - verify anything in OCM

