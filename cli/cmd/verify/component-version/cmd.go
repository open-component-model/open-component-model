package componentversion

import (
	"bytes"
	"context"
	"crypto"
	"encoding/hex"
	"fmt"
	"log/slog"
	"maps"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/sync/errgroup"
	"ocm.software/open-component-model/bindings/go/descriptor/normalisation"
	"ocm.software/open-component-model/bindings/go/descriptor/normalisation/json/v4alpha1"
	descruntime "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/rsa/signing/v1alpha1"
	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/cli/internal/flags/log"

	genericv1 "ocm.software/open-component-model/bindings/go/configuration/generic/v1/spec"
	resolverruntime "ocm.software/open-component-model/bindings/go/configuration/ocm/v1/runtime"
	resolverv1 "ocm.software/open-component-model/bindings/go/configuration/ocm/v1/spec"
	"ocm.software/open-component-model/bindings/go/oci/spec/repository"
	ctfv1 "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/ctf"
	ociv1 "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/oci"
	ocmctx "ocm.software/open-component-model/cli/internal/context"
	"ocm.software/open-component-model/cli/internal/reference/compref"
	"ocm.software/open-component-model/cli/internal/repository/ocm"
)

const (
	FlagConcurrencyLimit        = "concurrency-limit"
	FlagSignature               = "signature"
	FlagVerifyDigestConsistency = "verify-digest-consistency"
	FlagVerifierSpec            = "verifier-spec"
)

func New() *cobra.Command {
	cmd := &cobra.Command{
		Use:        "component-version {reference}",
		Aliases:    []string{"cv", "component-versions", "cvs", "componentversion", "componentversions", "component", "components", "comp", "comps", "c"},
		SuggestFor: []string{"version", "versions"},
		Short:      "Verify component version(s) inside an OCM repository",
		Args:       cobra.MatchAll(cobra.ExactArgs(1), ComponentReferenceAsFirstPositional),
		Long: fmt.Sprintf(`Verify component version(s) inside an OCM repository.

The format of a component reference is:
	[type::]{repository}/[valid-prefix]/{component}[:version]

For valid prefixes {%[1]s|none} are available. If <none> is used, it defaults to %[1]q. This is because by default,
OCM components are stored within a specific sub-repository.

For known types, currently only {%[2]s} are supported, which can be shortened to {%[3]s} respectively for convenience.

If no type is given, the repository path is interpreted based on introspection and heuristics.
`,
			compref.DefaultPrefix,
			strings.Join([]string{ociv1.Type, ctfv1.Type}, "|"),
			strings.Join([]string{ociv1.ShortType, ociv1.ShortType2, ctfv1.ShortType, ctfv1.ShortType2}, "|"),
		),
		Example: strings.TrimSpace(`
Verifying a single component version:

verify component-version ghcr.io/open-component-model/ocm//ocm.software/ocmcli:0.23.0
`),
		RunE:              VerifyComponentVersion,
		DisableAutoGenTag: true,
	}

	cmd.Flags().Int(FlagConcurrencyLimit, 4, "maximum amount of parallel requests to the repository for resolving component versions")
	cmd.Flags().String(FlagSignature, "", "name of the signature to verify. if not set, all signatures are verified")
	cmd.Flags().Bool(FlagVerifyDigestConsistency, true, "if enabled, all signature digests are verified before the signature itself is verified")
	cmd.Flags().String(FlagVerifierSpec, "", "path to an optional verifier specification file")

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

	pluginManager := ocmctx.FromContext(ctx).PluginManager()
	if pluginManager == nil {
		return fmt.Errorf("could not retrieve plugin manager from context")
	}

	credentialGraph := ocmctx.FromContext(ctx).CredentialGraph()
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

	verifyDigestConsistency, err := cmd.Flags().GetBool(FlagVerifyDigestConsistency)
	if err != nil {
		return fmt.Errorf("getting verify-digest-consistency flag failed: %w", err)
	} else if !verifyDigestConsistency {
		logger.WarnContext(ctx, "digest consistency verification is disabled")
	}

	verifierSpecPath, err := cmd.Flags().GetString(FlagVerifierSpec)
	if err != nil {
		return fmt.Errorf("getting verifier spec flag failed: %w", err)
	}

	reference := args[0]
	config := ocmctx.FromContext(ctx).Configuration()

	//nolint:staticcheck // no replacement for resolvers available yet (https://github.com/open-component-model/ocm-project/issues/575)
	var resolvers []resolverruntime.Resolver
	if config != nil {
		var err error
		if resolvers, err = resolversFromConfig(config, err); err != nil {
			return fmt.Errorf("getting resolvers from configuration failed: %w", err)
		}
	}
	repo, err := ocm.NewFromRefWithFallbackRepo(ctx, pluginManager, credentialGraph, resolvers, reference)
	if err != nil {
		return fmt.Errorf("could not initialize ocm repository: %w", err)
	}

	desc, err := repo.GetComponentVersion(ctx)
	if err != nil {
		return fmt.Errorf("getting component reference and versions failed: %w", err)
	}

	if verifyDigestConsistency {
		if err := isSafelyDigestible(&desc.Component); err != nil {
			logger.WarnContext(ctx, "component version is not considered safely digestable, proceed at your own risk", "error", err.Error())
		}
	}

	var verifierSpec runtime.Typed
	if verifierSpecPath == "" {
		logger.InfoContext(ctx, "no verifier specification file given, attempting to use default configuration (RSASSA-PSS)")
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

	eg, egctx := errgroup.WithContext(ctx)
	eg.SetLimit(concurrencyLimit)
	for _, signature := range sigs {
		eg.Go(func() error {
			start := time.Now()
			logger.InfoContext(egctx, "verifying signature", "name", signature.Name)
			defer logger.InfoContext(egctx, "signature verification completed", "name", signature.Name, "duration", time.Since(start).String())

			// verify digest consistency first, if enabled
			if verifyDigestConsistency {
				if err := verifyDigestMatchesDescriptor(egctx, desc, signature, logger); err != nil {
					return err
				}
			}

			var credentials map[string]string
			if credentialConsumerIdentity, err := handler.GetVerifyingCredentialConsumerIdentity(egctx, signature, verifierSpec); err == nil {
				if credentials, err = credentialGraph.Resolve(egctx, credentialConsumerIdentity); err != nil {
					logger.DebugContext(egctx, "could not resolve credentials for signature verification handler", "error", err.Error())
				}
			}

			if len(credentials) > 0 {
				logger.DebugContext(egctx, "discovered credentials from graph for signature verification", "attributes", slices.Collect(maps.Keys(credentials)))
			}

			return handler.Verify(egctx, signature, verifierSpec, credentials)
		})
	}

	if err := eg.Wait(); err != nil {
		return fmt.Errorf("SIGNATURE VERIFICATION FAILED: %w", err)
	}

	logger.InfoContext(ctx, "SIGNATURE VERIFICATION SUCCESSFUL")
	return nil
}

func verifyDigestMatchesDescriptor(ctx context.Context, desc *descruntime.Descriptor, signature descruntime.Signature, logger *slog.Logger) error {
	if legacyAlgo := "jsonNormalisation/v3"; signature.Digest.NormalisationAlgorithm == legacyAlgo {
		signature.Digest.NormalisationAlgorithm = v4alpha1.Algorithm
		logger.WarnContext(ctx, "detected legacy signature algorithm, enabling best effort compatibility. consider updating your signature specification", "algorithm", signature.Digest.NormalisationAlgorithm, "legacy", legacyAlgo, "new", "hint")
	}

	normalised, err := normalisation.Normalise(desc, signature.Digest.NormalisationAlgorithm)
	if err != nil {
		return fmt.Errorf("normalising component version failed: %w", err)
	}
	var hash crypto.Hash
	switch signature.Digest.HashAlgorithm {
	case crypto.SHA256.String():
		hash = crypto.SHA256
	case crypto.SHA512.String():
		hash = crypto.SHA512
	default:
		return fmt.Errorf("unsupported hash algorithm %q", signature.Digest.HashAlgorithm)
	}
	instance := hash.New()
	if _, err := instance.Write(normalised); err != nil {
		return fmt.Errorf("hashing component version failed: %w", err)
	}
	freshlyCalculatedDigest := instance.Sum(nil)
	digestFromSignature, err := hex.DecodeString(signature.Digest.Value)
	if err != nil {
		return fmt.Errorf("decoding digest from signature failed: %w", err)
	}
	if !bytes.Equal(freshlyCalculatedDigest, digestFromSignature) {
		return fmt.Errorf("digest from signature does not match calculated digest from descriptor")
	}
	return nil
}

//nolint:staticcheck // no replacement for resolvers available yet (https://github.com/open-component-model/ocm-project/issues/575)
func resolversFromConfig(config *genericv1.Config, err error) ([]resolverruntime.Resolver, error) {
	filtered, err := genericv1.FilterForType[*resolverv1.Config](resolverv1.Scheme, config)
	if err != nil {
		return nil, fmt.Errorf("filtering configuration for resolver config failed: %w", err)
	}
	resolverConfigV1 := resolverv1.Merge(filtered...)

	resolverConfig, err := resolverruntime.ConvertFromV1(repository.Scheme, resolverConfigV1)
	if err != nil {
		return nil, fmt.Errorf("converting resolver configuration from v1 to runtime failed: %w", err)
	}
	var resolvers []resolverruntime.Resolver
	if resolverConfig != nil && len(resolverConfig.Resolvers) > 0 {
		resolvers = resolverConfig.Resolvers
	}
	return resolvers, nil
}

// isSafelyDigestible checks if a component version is considered safely digestible.
// This means that all digests are present and valid and that all resources have a digest.
// if this is not the case normalisation and digesting is possible, but can lead to non-unique values when passed
// to digesting algorithms.
func isSafelyDigestible(cd *descruntime.Component) error {
	// check for digests on component references
	for _, reference := range cd.References {
		if reference.Digest.HashAlgorithm == "" || reference.Digest.NormalisationAlgorithm == "" || reference.Digest.Value == "" {
			return fmt.Errorf("missing digest in componentReference for %s:%s", reference.Name, reference.Version)
		}
	}

	for _, res := range cd.Resources {
		if (res.Access != nil && res.Access.GetType().String() != "None") &&
			(res.Digest == nil || res.Digest.HashAlgorithm == "" || res.Digest.NormalisationAlgorithm == "" || res.Digest.Value == "") {
			return fmt.Errorf("missing digest in resource for %s:%s", res.Name, res.Version)
		}
		if (res.Access == nil || res.Access.GetType().String() == "None") && res.Digest != nil {
			return fmt.Errorf("digest for resource with empty (None) access not allowed %s:%s", res.Name, res.Version)
		}
	}

	return nil
}
