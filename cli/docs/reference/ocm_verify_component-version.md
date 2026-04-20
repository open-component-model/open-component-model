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

Verify component version(s) inside an OCM repository based on signatures.

## Reference Format

	[type::]{repository}/[valid-prefix]/{component}[:version]

- Prefixes: {component-descriptors|none} (default: "component-descriptors")  
- Repo types: {OCIRepository|CommonTransportFormat} (short: {OCI|oci|CTF|ctf})

## OCM Verification explained in simple steps

- Resolve OCM repository  
- Fetch component version 
- Normalise descriptor (algorithm from signature)  
- Recompute hash and compare with signature digest  
- Verify signature (--verifier-spec, default RSASSA-PSS verifier)  

## Behavior

- --signature: verify only the named signature  
- Without --signature: verify all signatures  
- Fail fast on first invalid signature  
- Default verifier: RSASSA-PSS plugin  
  - Uses public key from discovered credentials (config-less)
- For Sigstore keyless verification, pass --verifier-spec with a SigstoreVerificationConfiguration/v1alpha1 config
  - Identity constraints (certificateOIDCIssuer + certificateIdentity, or regexp variants) are REQUIRED
  - Without them, verification cannot assert who produced the signature

Use to validate component versions before promotion, deployment, or further usage to ensure integrity and provenance.

```
ocm verify component-version {reference} [flags]
```

### Examples

```
# Verify all component version signatures found in a component version
verify component-version ghcr.io/open-component-model/ocm//ocm.software/ocmcli:0.23.0

## Example Credential Config (Plain encoding — bare public key)
#
# Used when the signature was created with signatureEncodingPolicy: Plain (the default).
# Supply the matching RSA public key.

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
            public_key_pem: <PEM>

## Example Credential Config (PEM encoding — certificate chain trust anchor)
#
# Used when the signature was created with signatureEncodingPolicy: PEM.
# The signature already embeds the leaf and intermediate certificates.
# Supply only the root CA certificate as the trust anchor; it must be self-signed.
# The verifier isolates the provided root from system roots, so only this CA is trusted.

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
            public_key_pem_file: /path/to/root-ca.pem

## Example Verifier Spec — Sigstore keyless (SigstoreVerificationConfiguration/v1alpha1)
#
# Identity constraints are REQUIRED: (certificateOIDCIssuer or certificateOIDCIssuerRegexp)
# AND (certificateIdentity or certificateIdentityRegexp) must be set.
# See https://docs.sigstore.dev/cosign/verifying/verify/

    type: SigstoreVerificationConfiguration/v1alpha1
    certificateOIDCIssuer: https://accounts.google.com
    certificateIdentity: jane.doe@example.com

# With regexp identity constraints:

    type: SigstoreVerificationConfiguration/v1alpha1
    certificateOIDCIssuerRegexp: https://github.com/.*
    certificateIdentityRegexp: https://github.com/my-org/my-repo/.*

# For private Sigstore infrastructure (skips public transparency log verification):

    type: SigstoreVerificationConfiguration/v1alpha1
    certificateOIDCIssuer: https://fulcio.example.com
    certificateIdentity: ci-user@example.com
    trustedRoot: /path/to/trusted_root.json
    privateInfrastructure: true

# Verify with Sigstore verifier spec:
verify component-version ghcr.io/open-component-model/ocm//ocm.software/ocmcli:0.23.0 --verifier-spec ./sigstore-verify.yaml

# Verify a specific signature
verify component-version ghcr.io/open-component-model/ocm//ocm.software/ocmcli:0.23.0 --signature my-signature

# Use a verifier specification file
verify component-version ghcr.io/open-component-model/ocm//ocm.software/ocmcli:0.23.0 --verifier-spec ./rsassa-pss.yaml
```

### Options

```
      --concurrency-limit int   maximum amount of parallel requests to the repository for resolving component versions (default 4)
  -h, --help                    help for component-version
      --signature string        name of the signature to verify. If not set, all signatures are verified.
      --verifier-spec string    path to a verifier specification file. If empty, defaults to RSASSA-PSS. For Sigstore keyless verification, use type SigstoreVerificationConfiguration/v1alpha1 (identity constraints required).
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
                                           If multiple configuration files are found, they will be merged in the order they are discovered.
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

* [ocm verify]({{< relref "ocm_verify.md" >}})	 - verify digests and signatures of component versions in OCM

