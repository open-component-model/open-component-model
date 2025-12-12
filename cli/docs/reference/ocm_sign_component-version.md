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

Creates or update cryptographic signatures on component descriptors.

## Reference Format

	[type::]{repository}/[valid-prefix]/{component}[:version]

- Prefixes: {component-descriptors|none} (default: "component-descriptors")  
- Repo types: {OCIRepository|CommonTransportFormat} (short: {OCI|oci|CTF|ctf})  

## OCM Signing explained in simple steps

- Resolve OCM repository
- Fetch component version  
- Verify digests (--verify-digest-consistency)
- Normalise descriptor (--normalisation)
- Hash normalised descriptor (--hash)
- Sign hash (--signer-spec)

## Behavior

- Conflicting signatures cause failure unless --force is set (then overwrite)
- --dry-run: compute only, do not persist signature
- Default signature name: default
- Default signer: RSASSA-PSS plugin (needs private key)

Use this command to establish provenance of component versions.

```
ocm sign component-version {reference} [flags]
```

### Examples

```
# Sign a component version with default algorithms
sign component-version ghcr.io/open-component-model/ocm//ocm.software/ocmcli:0.23.0

## Example Credential Config

    type: generic.config.ocm.software/v1
    configurations:
    - type: credentials.config.ocm.software
      consumers:
      - identity:
          type: RSA/v1alpha1
          algorithm: RSASSA-PSS
          signature: default
        credentials:
        - type: Credentials/v1
          properties:
            private_key_pem: <PEM>

# Sign with custom signature name
sign component-version ghcr.io/open-component-model/ocm//ocm.software/ocmcli:0.23.0 --signature my-signature

# Use a signer specification file
sign component-version ./repo/ocm//ocm.software/ocmcli:0.23.0 --signer-spec ./rsassa-pss.yaml

# Dry-run signing
sign component-version ghcr.io/open-component-model/ocm//ocm.software/ocmcli:0.23.0 --signature test --dry-run

# Force overwrite an existing signature
sign component-version ghcr.io/open-component-model/ocm//ocm.software/ocmcli:0.23.0 --signature my-signature --force
```

### Options

```
      --concurrency-limit int   maximum amount of parallel requests to the repository for resolving component versions (default 4)
      --dry-run                 compute signature but do not persist it to the repository
      --force                   overwrite existing signatures under the same name
      --hash string             hash algorithm to use (SHA256, SHA512) (default "SHA-256")
  -h, --help                    help for component-version
      --normalisation string    normalisation algorithm to use (default jsonNormalisation/v4alpha1) (default "jsonNormalisation/v4alpha1")
  -o, --output enum             output format of the resulting signature
                                (must be one of [json yaml]) (default yaml)
      --signature string        name of the signature to create or update. defaults to "default" (default "default")
      --signer-spec string      path to a signer specification file. If empty, defaults to an empty RSASSA-PSS configuration.
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

* [ocm sign]({{< relref "ocm_sign.md" >}})	 - create signatures for component versions in OCM

