package componentversion

import (
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/sync/errgroup"

	genericv1 "ocm.software/open-component-model/bindings/go/configuration/generic/v1/spec"
	"ocm.software/open-component-model/bindings/go/credentials"
	descruntime "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/oci/compref"
	ctfv1 "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/ctf"
	ociv1 "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/oci"
	"ocm.software/open-component-model/bindings/go/rsa/signing/v1alpha1"
	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/bindings/go/signing"
	signingv1alpha1 "ocm.software/open-component-model/bindings/go/signing/v1alpha1/spec"
	ocmctx "ocm.software/open-component-model/bindings/go/cli/internal/context"
	"ocm.software/open-component-model/bindings/go/cli/internal/flags/log"
	"ocm.software/open-component-model/bindings/go/cli/internal/repository/ocm"
)

const (
	FlagConcurrencyLimit = "concurrency-limit"
	FlagSignature        = "signature"
	FlagVerifierSpec     = "verifier-spec"
)

// signingConfigType is the configuration entry that replaced FlagVerifierSpec.
var signingConfigType = runtime.NewVersionedType(signingv1alpha1.ConfigType, signingv1alpha1.Version)

func New() *cobra.Command {
	cmd := &cobra.Command{
		Use:        "component-version {reference}",
		Aliases:    []string{"cv", "component-versions", "cvs", "componentversion", "componentversions", "component", "components", "comp", "comps", "c"},
		SuggestFor: []string{"version", "versions"},
		Short:      "Verify component version(s) inside an OCM repository",
		Args:       cobra.MatchAll(cobra.ExactArgs(1), ComponentReferenceAsFirstPositional),
		Long: fmt.Sprintf(`Verify component version(s) inside an OCM repository based on signatures.

## Reference Format

	[type::]{repository}/[valid-prefix]/{component}[:version]

- Prefixes: {%[1]s|none} (default: %[1]q)  
- Repo types: {%[2]s} (short: {%[3]s})

## OCM Verification explained in simple steps

- Resolve OCM repository  
- Fetch component version 
- Normalise descriptor (algorithm from signature)
- Recompute hash and compare with signature digest
- Verify signature (verifier from the OCM configuration, default RSASSA-PSS verifier)

## Behavior

- --signature selects a single signature by name; without it, every signature on the descriptor is verified
- Credentials are resolved per signature under that signature's name, so every signature needs its own consumer entry
- Signatures are verified concurrently (--concurrency-limit); the command exits non-zero on the first failure
- Default verifier: RSASSA-PSS, resolves the public key from credentials in .ocmconfig
- The verifier is configured in the OCM configuration (%[4]s), not on the command line
- An entry with a "signature" field only applies to that signature, one without applies to all
- The verifier is resolved per signature, so a component carrying several signatures can be verified with a different handler for each
- --verifier-spec is no longer supported and fails with an error

Use to validate component versions before promotion, deployment, or further usage to ensure integrity and provenance.`,
			compref.DefaultPrefix,
			strings.Join([]string{ociv1.Type, ctfv1.Type}, "|"),
			strings.Join([]string{ociv1.ShortType, ociv1.ShortType2, ctfv1.ShortType, ctfv1.ShortType2}, "|"),
			signingConfigType,
		),
		Example: strings.TrimSpace(`
# Verify all component version signatures found in a component version
verify component-version ghcr.io/open-component-model//ocm.software/cli:0.12.0

## Example Credential Config (Plain encoding — bare public key)
#
# Used when the signature was created with signatureEncodingPolicy: Plain (the default).
# Supply the matching RSA public key.
#
# The consumer identity is looked up with "signature" set to the name of the
# signature being verified, and identities are matched exactly. A "signature:
# default" entry therefore does NOT serve a signature named "prod", and an entry
# with no "signature" field at all matches nothing. Add one consumer entry per
# signature name; without --signature every signature on the descriptor is
# verified and each resolves its own credentials.

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
`),
		RunE:              VerifyComponentVersion,
		DisableAutoGenTag: true,
	}

	cmd.Flags().Int(FlagConcurrencyLimit, 4, "maximum amount of parallel requests to the repository for resolving component versions")
	cmd.Flags().String(FlagSignature, "", "name of the signature to verify. If not set, all signatures are verified.")
	cmd.Flags().String(FlagVerifierSpec, "", fmt.Sprintf("DEPRECATED: no longer supported, configure the verifier in the OCM configuration instead (%s, field \"verifier\")", signingConfigType))

	return cmd
}

func ComponentReferenceAsFirstPositional(_ *cobra.Command, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("missing component reference as first positional argument")
	}
	if _, err := compref.Parse(args[0]); err != nil {
		return fmt.Errorf("parsing component reference from first position argument %q failed: %w", args[0], err)
	}
	return nil
}

func VerifyComponentVersion(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	logger, err := log.GetBaseLogger(cmd)
	if err != nil {
		return fmt.Errorf("getting base logger failed: %w", err)
	}

	ocmContext := ocmctx.FromContext(ctx)
	if ocmContext == nil {
		return fmt.Errorf("no OCM context found")
	}

	pluginManager := ocmContext.PluginManager()
	if pluginManager == nil {
		return fmt.Errorf("could not retrieve plugin manager from context")
	}

	credentialGraph := ocmContext.CredentialGraph()
	if credentialGraph == nil {
		return fmt.Errorf("could not retrieve credential graph from context")
	}

	if cmd.Flags().Changed(FlagVerifierSpec) {
		return fmt.Errorf("--%s is no longer supported: move the verifier specification into the OCM configuration as an entry of type %s (field %q) and pass it with --config", FlagVerifierSpec, signingConfigType, "verifier")
	}

	signatureName, err := cmd.Flags().GetString(FlagSignature)
	if err != nil {
		return fmt.Errorf("getting signature name flag failed: %w", err)
	}

	concurrencyLimit, err := cmd.Flags().GetInt(FlagConcurrencyLimit)
	if err != nil {
		return fmt.Errorf("getting concurrency limit flag failed: %w", err)
	}

	reference := args[0]

	config := ocmContext.Configuration()
	ref, err := compref.Parse(reference)
	if err != nil {
		return fmt.Errorf("parsing component reference %q failed: %w", reference, err)
	}
	repoProvider, err := ocm.NewComponentVersionRepositoryForComponentProvider(cmd.Context(), pluginManager.ComponentVersionRepositoryRegistry, credentialGraph, config, ref)
	if err != nil {
		return fmt.Errorf("could not initialize ocm repository: %w", err)
	}

	repo, err := repoProvider.GetComponentVersionRepositoryForComponent(cmd.Context(), ref.Component, ref.Version)
	if err != nil {
		return fmt.Errorf("could not access ocm repository: %w", err)
	}

	desc, err := repo.GetComponentVersion(ctx, ref.Component, ref.Version)
	if err != nil {
		return fmt.Errorf("getting component reference and versions failed: %w", err)
	}

	var sigs []descruntime.Signature
	if signatureName != "" {
		for _, sig := range desc.Signatures {
			if sig.Name == signatureName {
				sigs = append(sigs, sig)
				break
			}
		}
	} else {
		sigs = desc.Signatures
	}

	if len(sigs) == 0 {
		return fmt.Errorf("no signatures found to verify")
	}

	if err := signing.IsSafelyDigestible(&desc.Component); err != nil {
		logger.WarnContext(ctx, "component version is not considered safely digestable", "error", err.Error())
	}

	eg, egctx := errgroup.WithContext(ctx)
	eg.SetLimit(concurrencyLimit)
	for _, signature := range sigs {
		eg.Go(func() error {
			start := time.Now()
			logger.InfoContext(egctx, "verifying signature", "name", signature.Name)
			defer func() {
				logger.InfoContext(egctx, "signature verification completed", "name", signature.Name, "duration", time.Since(start).String())
			}()

			if err := signing.VerifyDigestMatchesDescriptor(egctx, desc, signature, logger); err != nil {
				return err
			}

			// The verifier is resolved per signature so that a component version
			// carrying several signatures can be verified with a different handler
			// for each of them.
			verifierSpec, err := loadVerifierConfig(config, signature.Name, logger)
			if err != nil {
				return err
			}

			handler, err := pluginManager.SigningRegistry.GetPlugin(egctx, verifierSpec)
			if err != nil {
				return fmt.Errorf("getting signature handler plugin failed: %w", err)
			}

			var creds runtime.Typed
			if consumerID, err := handler.GetVerifyingCredentialConsumerIdentity(egctx, signature, verifierSpec); err == nil {
				if creds, err = credentialGraph.Resolve(egctx, consumerID); err != nil {
					if errors.Is(err, credentials.ErrNotFound) {
						logger.DebugContext(egctx, "could not resolve credentials for verification", "error", err.Error())
					} else {
						return fmt.Errorf("resolving credentials for verification failed: %w", err)
					}
				}
			}

			if creds != nil {
				logger.DebugContext(egctx, "using discovered credentials for verification", "type", creds.GetType())
			}

			return handler.Verify(egctx, signature, verifierSpec, creds)
		})
	}

	if err := eg.Wait(); err != nil {
		return fmt.Errorf("SIGNATURE VERIFICATION FAILED: %w", err)
	}

	logger.InfoContext(ctx, "SIGNATURE VERIFICATION SUCCESSFUL")
	return nil
}

// loadVerifierConfig resolves the verifier configuration for the given signature
// from the central OCM configuration, falling back to RSASSA-PSS if none is
// configured.
func loadVerifierConfig(config *genericv1.Config, signatureName string, logger *slog.Logger) (runtime.Typed, error) {
	signingConfig, err := signingv1alpha1.LookupConfigForSignature(config, signatureName)
	if err != nil {
		return nil, fmt.Errorf("getting signing configuration failed: %w", err)
	}
	if signingConfig != nil && signingConfig.Verifier != nil {
		logger.Debug("using verifier from configuration", "type", signingConfig.Verifier.GetType(), "signature", signatureName)
		return signingConfig.Verifier, nil
	}

	spec := &v1alpha1.Config{}
	logger.Info("no verifier configured, using default RSASSA-PSS", "signature", signatureName)
	_, _ = v1alpha1.Scheme.DefaultType(spec)
	return spec, nil
}
