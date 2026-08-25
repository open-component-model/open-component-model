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
- Verify signature (verifier from the OCM configuration, default RSASSA-PSS verifier)

## Behavior

- --signature selects a single signature by name; without it, every signature on the descriptor is verified
- Signatures are verified concurrently (--concurrency-limit); the command exits non-zero on the first failure
- Default verifier: RSASSA-PSS, resolves the public key from credentials in .ocmconfig
- The verifier is configured in the OCM configuration (signing.config.ocm.software/v1alpha1), not on the command line
- An entry with a "signature" field only applies to that signature, one without applies to all
- The verifier is resolved per signature, so a component carrying several signatures can be verified with a different handler for each
- --verifier-spec is no longer supported and fails with an error

Use to validate component versions before promotion, deployment, or further usage to ensure integrity and provenance.

```
ocm verify component-version {reference} [flags]
```

### Examples

```
# Verify all component version signatures found in a component version
verify component-version ghcr.io/open-component-model//ocm.software/cli:0.12.0

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

## Example Verifier Config (.ocmconfig)
#
# The verifier selects the verification handler and configures it.
# It does NOT contain credentials - public keys and trust material are always
# resolved via .ocmconfig credentials. If omitted, defaults to RSASSA-PSS.
# Add a "signature" field to scope an entry to the signature of that name
# (see the per-signature example below); without it the entry applies to all.

    type: generic.config.ocm.software/v1
    configurations:
    - type: signing.config.ocm.software/v1alpha1
      verifier:
        type: RSASigningConfiguration/v1alpha1

## Example Verifier Config - one verifier per signature
#
# The entry whose "signature" matches the signature being verified wins; the
# entry without one is the fallback for every other signature. The credentials
# for each signature are matched the same way, by the "signature" field of the
# consumer identity. Without --signature every signature is verified, each with
# the verifier that its own name resolves to.

    type: generic.config.ocm.software/v1
    configurations:
    - type: signing.config.ocm.software/v1alpha1
      signature: release
      verifier:
        type: SigstoreVerificationConfiguration/v1alpha1
        certificateOIDCIssuer: https://accounts.google.com
        certificateIdentity: jane.doe@example.com
    - type: signing.config.ocm.software/v1alpha1
      verifier:
        type: RSASigningConfiguration/v1alpha1

## Example Verifier Config - Sigstore keyless (SigstoreVerificationConfiguration/v1alpha1)
#
# Identity constraints are REQUIRED: (certificateOIDCIssuer or certificateOIDCIssuerRegexp)
# AND (certificateIdentity or certificateIdentityRegexp) must be set.
#
# certificateOIDCIssuer must match the issuer that Fulcio recorded in the cert.
# On public Sigstore (Dex federation), Fulcio passes through the upstream IdP issuer:
#   - Google login   -> https://accounts.google.com
#   - GitHub login   -> https://github.com/login/oauth
#   - Microsoft login -> https://login.microsoftonline.com
# It is NOT the Dex URL (https://oauth2.sigstore.dev/auth).
# See https://docs.sigstore.dev/cosign/verifying/verify/

    type: generic.config.ocm.software/v1
    configurations:
    - type: signing.config.ocm.software/v1alpha1
      verifier:
        type: SigstoreVerificationConfiguration/v1alpha1
        certificateOIDCIssuer: https://accounts.google.com
        certificateIdentity: jane.doe@example.com

# With regexp identity constraints:

    type: generic.config.ocm.software/v1
    configurations:
    - type: signing.config.ocm.software/v1alpha1
      verifier:
        type: SigstoreVerificationConfiguration/v1alpha1
        certificateOIDCIssuerRegexp: https://github.com/.*
        certificateIdentityRegexp: https://github.com/my-org/my-repo/.*

# For private Sigstore infrastructure (skips public transparency log verification).
# The trusted root is NOT a verifier field. It is supplied via credentials
# under a SigstoreVerifier/v1alpha1 consumer (see Example Credential Config below):

    type: generic.config.ocm.software/v1
    configurations:
    - type: signing.config.ocm.software/v1alpha1
      verifier:
        type: SigstoreVerificationConfiguration/v1alpha1
        certificateOIDCIssuer: https://login.example.com
        certificateIdentity: ci-user@example.com
        privateInfrastructure: true

## Example Credential Config (.ocmconfig) — Sigstore trusted root (private deployments)
#
# Required for private Sigstore infrastructure (privateInfrastructure: true on the
# verifier). Use trusted_root_json_file (path) or trusted_root_json (inline JSON).
# Public-good Sigstore does not need this credential.

    type: generic.config.ocm.software/v1
    configurations:
    - type: credentials.config.ocm.software
      consumers:
      - identity:
          type: SigstoreVerifier/v1alpha1
          signature: default
        credentials:
        - type: Credentials/v1
          properties:
            trusted_root_json_file: /path/to/trusted_root.json

# Verify using the default .ocmconfig file
#
# In this case, the verifier configuration AND the credentials are all configured in the main ocm configuration
# file.
verify component-version ghcr.io/open-component-model//ocm.software/cli:0.12.0

# Optionally, providing a --config flag on the CLI will overwrite all configurations and use this instead.
# Multiple configuration flags can be combined this way. Either have everything (verifier config and credentials) or
# have multiple --config flags strung together.
verify component-version ./repo//ocm.software/cli:0.12.0 --config ./sigstore-verify.ocmconfig --config ~/.ocmconfig

# Verify a specific signature
verify component-version ghcr.io/open-component-model//ocm.software/cli:0.12.0 --signature my-signature
```

### Options

```
      --concurrency-limit int   maximum amount of parallel requests to the repository for resolving component versions (default 4)
  -h, --help                    help for component-version
      --signature string        name of the signature to verify. If not set, all signatures are verified.
      --verifier-spec string    DEPRECATED: no longer supported, configure the verifier in the OCM configuration instead (signing.config.ocm.software/v1alpha1, field "verifier")
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

* [ocm verify]({{< relref "ocm_verify.md" >}})	 - verify digests and signatures of component versions in OCM

