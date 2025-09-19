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

	resolverruntime "ocm.software/open-component-model/bindings/go/configuration/ocm/v1/runtime"
	"ocm.software/open-component-model/bindings/go/descriptor/normalisation"
	"ocm.software/open-component-model/bindings/go/descriptor/normalisation/json/v4alpha1"
	descruntime "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	ctfv1 "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/ctf"
	ociv1 "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/oci"
	"ocm.software/open-component-model/bindings/go/rsa/signing/v1alpha1"
	"ocm.software/open-component-model/bindings/go/runtime"
	ocmctx "ocm.software/open-component-model/cli/internal/context"
	"ocm.software/open-component-model/cli/internal/flags/log"
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

If this command succeeds on a trusted signature, it can be trusted.

This command checks cryptographic signatures stored on component versions
to ensure integrity, authenticity, and provenance. Each signature covers a
normalised and hashed form of the component descriptor, which is compared
against the expected digest and verified with the configured verifier.

The format of a component reference is:
	[type::]{repository}/[valid-prefix]/{component}[:version]

Valid prefixes: {%[1]s|none}. If <none> is used, it defaults to %[1]q.
Supported repository types: {%[2]s} (short forms: {%[3]s}).
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
* If --verifier-spec is not provided, the default RSASSA-PSS verifier plugin is used.
    This default plugin supports verifying signatures without a configuration file,
    and uses either discovered credentials or performs keyless verification through encoded PEM certificates 
    when possible.

Use this command in automated pipelines or audits to validate the
authenticity of component versions before promotion, deployment,
or further processing.`,
			compref.DefaultPrefix,
			strings.Join([]string{ociv1.Type, ctfv1.Type}, "|"),
			strings.Join([]string{ociv1.ShortType, ociv1.ShortType2, ctfv1.ShortType, ctfv1.ShortType2}, "|"),
		),
		RunE:              VerifyComponentVersion,
		DisableAutoGenTag: true,
	}

	cmd.Flags().Int(FlagConcurrencyLimit, 4, "maximum amount of parallel requests to the repository for resolving component versions")
	cmd.Flags().String(FlagSignature, "", "name of the signature to verify. if not set, all signatures are verified")
	cmd.Flags().Bool(FlagVerifyDigestConsistency, true, "verify that all digests match the descriptor before verifying the signature itself")
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
		return fmt.Errorf("getting verifier-spec flag failed: %w", err)
	}

	reference := args[0]
	config := ocmctx.FromContext(ctx).Configuration()

	//nolint:staticcheck // no replacement for resolvers available yet
	var resolvers []*resolverruntime.Resolver
	if config != nil {
		resolvers, err = ocm.ResolversFromConfig(config)
		if err != nil {
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
			logger.WarnContext(ctx, "component version is not considered safely digestable", "error", err.Error())
		}
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
		s := signature
		eg.Go(func() error {
			start := time.Now()
			logger.InfoContext(egctx, "verifying signature", "name", s.Name)
			defer func() {
				logger.InfoContext(egctx, "signature verification completed", "name", s.Name, "duration", time.Since(start).String())
			}()

			if verifyDigestConsistency {
				if err := verifyDigestMatchesDescriptor(egctx, desc, s, logger); err != nil {
					return err
				}
			}

			var credentials map[string]string
			if consumerID, err := handler.GetVerifyingCredentialConsumerIdentity(egctx, s, verifierSpec); err == nil {
				if credentials, err = credentialGraph.Resolve(egctx, consumerID); err != nil {
					logger.DebugContext(egctx, "could not resolve credentials for verification", "error", err.Error())
				}
			}

			if len(credentials) > 0 {
				logger.DebugContext(egctx, "using discovered credentials for verification", "attributes", slices.Collect(maps.Keys(credentials)))
			}

			return handler.Verify(egctx, s, verifierSpec, credentials)
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
		logger.WarnContext(ctx, "legacy normalisation algorithm detected, using v4alpha1", "legacy", legacyAlgo, "new", v4alpha1.Algorithm)
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
	freshDigest := instance.Sum(nil)
	digestFromSignature, err := hex.DecodeString(signature.Digest.Value)
	if err != nil {
		return fmt.Errorf("decoding digest from signature failed: %w", err)
	}
	if !bytes.Equal(freshDigest, digestFromSignature) {
		return fmt.Errorf("digest from signature does not match descriptor digest")
	}
	return nil
}

func isSafelyDigestible(cd *descruntime.Component) error {
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
			return fmt.Errorf("digest for resource with empty access not allowed %s:%s", res.Name, res.Version)
		}
	}
	return nil
}
