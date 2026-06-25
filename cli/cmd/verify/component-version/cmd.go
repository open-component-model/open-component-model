package componentversion

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/sync/errgroup"

	"ocm.software/open-component-model/bindings/go/credentials"
	descruntime "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/oci/compref"
	ctfv1 "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/ctf"
	ociv1 "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/oci"
	"ocm.software/open-component-model/bindings/go/rsa/signing/v1alpha1"
	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/bindings/go/signing"
	ocmctx "ocm.software/open-component-model/cli/internal/context"
	"ocm.software/open-component-model/cli/internal/flags/log"
	"ocm.software/open-component-model/cli/internal/repository/ocm"
)

const (
	FlagConcurrencyLimit = "concurrency-limit"
	FlagSignature        = "signature"
	FlagVerifierSpec     = "verifier-spec"
)

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
- Verify signature (--verifier-spec, default RSASSA-PSS verifier)  

## Behavior

- --signature selects a single signature by name; without it, every signature on the descriptor is verified
- Signatures are verified concurrently (--concurrency-limit); the command exits non-zero on the first failure
- Default verifier: RSASSA-PSS, resolves the public key from credentials in .ocmconfig
- For Sigstore keyless verification, pass --verifier-spec with a SigstoreVerificationConfiguration/v1alpha1 config

Use to validate component versions before promotion, deployment, or further usage to ensure integrity and provenance.`,
			compref.DefaultPrefix,
			strings.Join([]string{ociv1.Type, ctfv1.Type}, "|"),
			strings.Join([]string{ociv1.ShortType, ociv1.ShortType2, ctfv1.ShortType, ctfv1.ShortType2}, "|"),
		),
		Example: strings.TrimSpace(`
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
#
# certificateOIDCIssuer must match the issuer that Fulcio recorded in the cert.
# On public Sigstore (Dex federation), Fulcio passes through the upstream IdP issuer:
#   - Google login   -> https://accounts.google.com
#   - GitHub login   -> https://github.com/login/oauth
#   - Microsoft login -> https://login.microsoftonline.com
# It is NOT the Dex URL (https://oauth2.sigstore.dev/auth).
# See https://docs.sigstore.dev/cosign/verifying/verify/

    type: SigstoreVerificationConfiguration/v1alpha1
    certificateOIDCIssuer: https://accounts.google.com
    certificateIdentity: jane.doe@example.com

# With regexp identity constraints:

    type: SigstoreVerificationConfiguration/v1alpha1
    certificateOIDCIssuerRegexp: https://github.com/.*
    certificateIdentityRegexp: https://github.com/my-org/my-repo/.*

# For private Sigstore infrastructure (skips public transparency log verification).
# The trusted root is NOT a verifier-spec field. It is supplied via credentials
# under a SigstoreVerifier/v1alpha1 consumer (see Example Credential Config below):

    type: SigstoreVerificationConfiguration/v1alpha1
    certificateOIDCIssuer: https://login.example.com
    certificateIdentity: ci-user@example.com
    privateInfrastructure: true

## Example Credential Config (.ocmconfig) — Sigstore trusted root (private deployments)
#
# Required for private Sigstore infrastructure (privateInfrastructure: true on the
# verifier spec). Use trusted_root_json_file (path) or trusted_root_json (inline JSON).
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

# Verify with Sigstore verifier spec:
verify component-version ghcr.io/open-component-model/ocm//ocm.software/ocmcli:0.23.0 --verifier-spec ./sigstore-verify.yaml

# Verify a specific signature
verify component-version ghcr.io/open-component-model/ocm//ocm.software/ocmcli:0.23.0 --signature my-signature

# Use a verifier specification file
verify component-version ghcr.io/open-component-model/ocm//ocm.software/ocmcli:0.23.0 --verifier-spec ./rsassa-pss.yaml
`),
		RunE:              VerifyComponentVersion,
		DisableAutoGenTag: true,
	}

	cmd.Flags().Int(FlagConcurrencyLimit, 4, "maximum amount of parallel requests to the repository for resolving component versions")
	cmd.Flags().String(FlagSignature, "", "name of the signature to verify. If not set, all signatures are verified.")
	cmd.Flags().String(FlagVerifierSpec, "", "path to a verifier specification file. If empty, defaults to RSASSA-PSS.")

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

	signatureName, err := cmd.Flags().GetString(FlagSignature)
	if err != nil {
		return fmt.Errorf("getting signature name flag failed: %w", err)
	}

	concurrencyLimit, err := cmd.Flags().GetInt(FlagConcurrencyLimit)
	if err != nil {
		return fmt.Errorf("getting concurrency limit flag failed: %w", err)
	}

	verifierSpecPath, err := cmd.Flags().GetString(FlagVerifierSpec)
	if err != nil {
		return fmt.Errorf("getting verifier-spec flag failed: %w", err)
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

	var verifierSpec runtime.Typed
	if verifierSpecPath == "" {
		logger.InfoContext(ctx, "no verifier specification file given, using default RSASSA-PSS")
		verifierSpec = &v1alpha1.Config{}
		_, _ = v1alpha1.Scheme.DefaultType(verifierSpec)
	} else {
		genericScheme := runtime.NewScheme(runtime.WithAllowUnknown())
		verifierSpecBytes, err := os.ReadFile(verifierSpecPath)
		if err != nil {
			return fmt.Errorf("reading verifier specification file %q failed: %w", verifierSpecPath, err)
		}
		verifierSpec = &runtime.Raw{}
		if err := genericScheme.Decode(bytes.NewReader(verifierSpecBytes), verifierSpec); err != nil {
			return fmt.Errorf("decoding verifier specification file %q failed: %w", verifierSpecPath, err)
		}
	}

	handler, err := pluginManager.SigningRegistry.GetPlugin(ctx, verifierSpec)
	if err != nil {
		return fmt.Errorf("getting signature handler plugin failed: %w", err)
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
