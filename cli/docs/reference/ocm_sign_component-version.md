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
- Sign hash (signer from the OCM configuration)

## Behavior

- Conflicting signatures cause failure unless --force is set (then overwrite)
- --dry-run: compute only, do not persist signature
- Default signature name: default
- Default signer: RSASSA-PSS plugin (needs private key)
- The signer is configured in the OCM configuration (signing.config.ocm.software/v1alpha1), not on the command line
- An entry with a "signature" field only applies to that signature, one without applies to all
- --signer-spec is no longer supported and fails with an error

Use this command to establish provenance of component versions.

```
ocm sign component-version {reference} [flags]
```

### Examples

```
# Sign a component version with default algorithms
sign component-version ghcr.io/open-component-model//ocm.software/cli:0.12.0

## Example Credential Config (.ocmconfig) — Plain encoding (default)
#
# Credentials (private/public keys) are always resolved via .ocmconfig.
# The "signature" field must match the --signature flag (default: "default").

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

## Example Credential Config (.ocmconfig) — PEM encoding with certificate chain
#
# Required when signatureEncodingPolicy: PEM is set in the signer spec.
# private_key_pem_file: leaf private key (PKCS#1 or PKCS#8)
# public_key_pem_file:  PEM file containing [leaf, intermediate] certificates
#                       Do NOT include the root CA here — it must not be embedded
#                       in the signature (the verifier rejects self-signed embedded certs).

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
            private_key_pem_file: /path/to/leaf.key
            public_key_pem_file: /path/to/leaf-and-intermediate-chain.pem

## Example Signer Config (.ocmconfig)
#
# The signer configures the signing algorithm and encoding policy.
# It does NOT contain credentials - keys are always resolved via .ocmconfig.
# If omitted, defaults to RSASSA-PSS with Plain encoding.
# Add a "signature" field to scope an entry to the signature of that name
# (see the per-signature example below); without it the entry applies to all.
#
# Supported signer fields:
#   type:                    RSASigningConfiguration/v1alpha1
#   signatureAlgorithm:      RSASSA-PSS (default) | RSASSA-PKCS1-V1_5
#   signatureEncodingPolicy: Plain (default) | PEM
#
# signatureEncodingPolicy controls the *signature output* format:
#   Plain - signature stored as hex string; verification needs an external public key
#   PEM   - signature wrapped in a PEM SIGNATURE block with embedded certificate chain
#           (experimental; credentials must provide certificates, not bare public keys)

    type: generic.config.ocm.software/v1
    configurations:
    - type: signing.config.ocm.software/v1alpha1
      signer:
        type: RSASigningConfiguration/v1alpha1
        signatureAlgorithm: RSASSA-PSS
        signatureEncodingPolicy: Plain

# Example signer for PEM encoding (requires certificate chain in credentials):

    type: generic.config.ocm.software/v1
    configurations:
    - type: signing.config.ocm.software/v1alpha1
      signer:
        type: RSASigningConfiguration/v1alpha1
        signatureAlgorithm: RSASSA-PSS
        signatureEncodingPolicy: PEM

## Example Signer Config - one signer per signature
#
# The entry whose "signature" matches --signature wins; the entry without one
# is the fallback for every other signature. The credentials for each signature
# are matched the same way, by the "signature" field of the consumer identity.

    type: generic.config.ocm.software/v1
    configurations:
    - type: signing.config.ocm.software/v1alpha1
      signature: release
      signer:
        type: SigstoreSigningConfiguration/v1alpha1
    - type: signing.config.ocm.software/v1alpha1
      signer:
        type: RSASigningConfiguration/v1alpha1

## Example Signer Config - Sigstore keyless (SigstoreSigningConfiguration/v1alpha1)
#
# Use when signing without private keys via Sigstore/Fulcio OIDC.
# Endpoint discovery precedence:
#   1. signingConfig - local signing_config.json
#   2. Not set - public-good Sigstore TUF (default)

    type: generic.config.ocm.software/v1
    configurations:
    - type: signing.config.ocm.software/v1alpha1
      signer:
        type: SigstoreSigningConfiguration/v1alpha1

# With a local signing config file (private infrastructure):

    type: generic.config.ocm.software/v1
    configurations:
    - type: signing.config.ocm.software/v1alpha1
      signer:
        type: SigstoreSigningConfiguration/v1alpha1
        signingConfig: /path/to/signing_config.json

## Example Credential Config (.ocmconfig) — Sigstore OIDC token
#
# The OIDCIdentityTokenProvider plugin acquires an OIDC token via an interactive browser flow.

    type: generic.config.ocm.software/v1
    configurations:
    - type: credentials.config.ocm.software
      consumers:
      - identity:
          type: SigstoreSigner/v1alpha1
          signature: default
        credentials:
        - type: OIDCIdentityTokenProvider/v1alpha1

## Note on the OIDC issuer recorded in the Fulcio certificate
#
# On public Sigstore (Dex federation), Fulcio passes the upstream IdP issuer through
# into the certificate (OID 1.3.6.1.4.1.57264.1.8) — NOT the Dex URL:
#   - Google login   -> https://accounts.google.com
#   - GitHub login   -> https://github.com/login/oauth
#   - Microsoft login -> https://login.microsoftonline.com
# Verifiers must use the upstream issuer in certificateOIDCIssuer.
# OCM also stores this value in signatures[].signature.issuer for convenience.

# Sign with Sigtore using default .ocmconfig file
#
# In this case, the signer configuration AND the OIDC credentials are all configured in the main ocm configugration
# file.
sign component-version ghcr.io/open-component-model//ocm.software/cli:0.12.0

# Optionally, providing a --config flag on the CLI will overwrite all configurations and use this instead.
# Multiple configuration flags can be combined this way. Either have everything (signer config and credentials) or
# have multiple --config flags string. 
sign component-version ./repo//ocm.software/cli:0.12.0 --config ./rsassa-pss.ocmconfig --config ~/.ocmconfig

# Sign with custom signature name
sign component-version ghcr.io/open-component-model//ocm.software/cli:0.12.0 --signature my-signature

# Dry-run signing
sign component-version ghcr.io/open-component-model//ocm.software/cli:0.12.0 --signature test --dry-run

# Force overwrite an existing signature
sign component-version ghcr.io/open-component-model//ocm.software/cli:0.12.0 --signature my-signature --force
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
      --signer-spec string      DEPRECATED: no longer supported, configure the signer in the OCM configuration instead (signing.config.ocm.software/v1alpha1, field "signer")
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

* [ocm sign]({{< relref "ocm_sign.md" >}})	 - create signatures for component versions in OCM

