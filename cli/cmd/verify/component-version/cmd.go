package componentversion

import (
	"bytes"
	"errors"
	"fmt"
	"maps"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/sync/errgroup"

	"ocm.software/open-component-model/bindings/go/credentials"
	descruntime "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	ctfv1 "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/ctf"
	ociv1 "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/oci"
	"ocm.software/open-component-model/bindings/go/rsa/signing/v1alpha1"
	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/bindings/go/signing"
	ocmctx "ocm.software/open-component-model/cli/internal/context"
	"ocm.software/open-component-model/cli/internal/flags/log"
	"ocm.software/open-component-model/cli/internal/reference/compref"
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

- --signature: verify only the named signature  
- Without --signature: verify all signatures  
- Fail fast on first invalid signature  
- Default verifier: RSASSA-PSS plugin  
  - Supports config-less verification  
  - Uses discovered credentials or PEM certificates when possible  

Use to validate component versions before promotion, deployment, or further usage to ensure integrity and provenance.`,
			compref.DefaultPrefix,
			strings.Join([]string{ociv1.Type, ctfv1.Type}, "|"),
			strings.Join([]string{ociv1.ShortType, ociv1.ShortType2, ctfv1.ShortType, ctfv1.ShortType2}, "|"),
		),
		Example: strings.TrimSpace(`
# Verify all component version signatures found in a component version
verify component-version ghcr.io/open-component-model/ocm//ocm.software/ocmcli:0.23.0

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
            public_key_pem: <PEM>

# Verify a specific signature
sign component-version ghcr.io/open-component-model/ocm//ocm.software/ocmcli:0.23.0 --signature my-signature

# Use a verifier specification file
sign component-version ghcr.io/open-component-model/ocm//ocm.software/ocmcli:0.23.0 --verifier-spec ./rsassa-pss.yaml
`),
		RunE:              VerifyComponentVersion,
		DisableAutoGenTag: true,
	}

	cmd.Flags().Int(FlagConcurrencyLimit, 4, "maximum amount of parallel requests to the repository for resolving component versions")
	cmd.Flags().String(FlagSignature, "", "name of the signature to verify. If not set, all signatures are verified.")
	cmd.Flags().String(FlagVerifierSpec, "", "path to an optional verifier specification file. If empty, defaults to an empty RSASSA-PSS configuration.")

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

			var creds map[string]string
			if consumerID, err := handler.GetVerifyingCredentialConsumerIdentity(egctx, signature, verifierSpec); err == nil {
				if creds, err = credentialGraph.Resolve(egctx, consumerID); err != nil {
					if errors.Is(err, credentials.ErrNotFound) {
						logger.DebugContext(egctx, "could not resolve credentials for verification", "error", err.Error())
					} else {
						return fmt.Errorf("resolving credentials for verification failed: %w", err)
					}
				}
			}

			if len(creds) > 0 {
				logger.DebugContext(egctx, "using discovered credentials for verification", "attributes", slices.Collect(maps.Keys(creds)))
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
