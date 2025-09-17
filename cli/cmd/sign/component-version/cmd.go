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

	"github.com/spf13/cobra"
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
	FlagSignerSpec              = "signer-spec"
	FlagSignature               = "signature"
	FlagNormalisationAlgorithm  = "normalisation"
	FlagHashAlgorithm           = "hash"
	FlagVerifyDigestConsistency = "verify-digest-consistency"
	FlagDryRun                  = "dry-run"
	FlagForce                   = "force"
)

func New() *cobra.Command {
	cmd := &cobra.Command{
		Use:        "component-version {reference}",
		Aliases:    []string{"cv", "component-versions", "cvs", "componentversion", "componentversions", "component", "components", "comp", "comps", "c"},
		SuggestFor: []string{"version", "versions"},
		Short:      "Sign component version(s) inside an OCM repository",
		Args:       cobra.MatchAll(cobra.ExactArgs(1), ComponentReferenceAsFirstPositional),
		Long: fmt.Sprintf(`Sign component version(s) inside an OCM repository.

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
Signing a single component version:

sign component-version ghcr.io/open-component-model/ocm//ocm.software/ocmcli:0.23.0 --signature my-signature
`),
		RunE:              VerifyComponentVersion,
		DisableAutoGenTag: true,
	}

	cmd.Flags().Int(FlagConcurrencyLimit, 4, "maximum amount of parallel requests to the repository for resolving component versions")
	cmd.Flags().String(FlagSignature, "", "name of the signature to verify. if not set, all signatures are verified")
	cmd.Flags().Bool(FlagVerifyDigestConsistency, true, "if enabled, all signature digests are verified before the signature itself is verified")
	cmd.Flags().String(FlagSignerSpec, "", "path to an optional signer specification file")
	cmd.Flags().Bool(FlagDryRun, false, "if enabled, the signature is not actually written to the repository")

	// TODO ensure this is an enum
	cmd.Flags().String(FlagNormalisationAlgorithm, v4alpha1.Algorithm, "algorithm to use for normalising the component version")
	// TODO ensure this is an enum
	cmd.Flags().String(FlagHashAlgorithm, crypto.SHA256.String(), "algorithm to use for hashing the normalised component version")
	cmd.Flags().Bool(FlagForce, false, "if enabled, existing signatures under the attempted name are overwritten")

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

	verifyDigestConsistency, err := cmd.Flags().GetBool(FlagVerifyDigestConsistency)
	if err != nil {
		return fmt.Errorf("getting verify-digest-consistency flag failed: %w", err)
	} else if !verifyDigestConsistency {
		logger.WarnContext(ctx, "digest consistency verification is disabled")
	}

	signerSpecPath, err := cmd.Flags().GetString(FlagSignerSpec)
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
	repo, err := ocm.NewFromRefWithFallbackRepo(ctx, pluginManager, credentialGraph, resolvers, reference, compref.WithCTFAccessMode(ctfv1.AccessModeReadWrite))
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
		// TODO(jakobmoellerdev): fully recursive reference validation is required here to be truly safe.
	}

	var signerSpec runtime.Typed
	if signerSpecPath == "" {
		logger.InfoContext(ctx, "no verifier specification file given, attempting to use default configuration (RSASSA-PSS)")
		signerSpec = &v1alpha1.Config{}
		_, _ = v1alpha1.Scheme.DefaultType(signerSpec)
	} else {
		genericScheme := runtime.NewScheme(runtime.WithAllowUnknown())
		verifierSpecBytes, err := os.ReadFile(signerSpecPath)
		if err != nil {
			return fmt.Errorf("reading verifier specification file %q failed: %w", signerSpecPath, err)
		}
		signerSpec = &runtime.Raw{}
		if err := genericScheme.Decode(bytes.NewReader(verifierSpecBytes), signerSpec); err != nil {
			return fmt.Errorf("decoding verifier specification file %q failed: %w", signerSpecPath, err)
		}
	}

	handler, err := pluginManager.SigningRegistry.GetPlugin(ctx, signerSpec)
	if err != nil {
		return fmt.Errorf("getting signature handler plugin failed: %w", err)
	}

	if signatureName == "" {
		logger.InfoContext(ctx, "no signature name given, using default name", "name", "default")
		signatureName = "default"
	}
	sigExists := func(signature descruntime.Signature) bool {
		return signature.Name == signatureName
	}

	if slices.ContainsFunc(desc.Signatures, sigExists) {
		if force, _ := cmd.Flags().GetBool(FlagForce); force {
			logger.InfoContext(ctx, "signature with name already exists, but force flag is set, overwriting", "name", signatureName)
		} else {
			return fmt.Errorf("signature with name %q already exists", signatureName)
		}
	}

	unsignedDigestSpec, err := generateDigest(ctx, desc, logger, cmd.Flag(FlagNormalisationAlgorithm).Value.String(), cmd.Flag(FlagHashAlgorithm).Value.String())
	if err != nil {
		return fmt.Errorf("generating digest failed: %w", err)
	}

	var credentials map[string]string
	if credentialConsumerIdentity, err := handler.GetSigningCredentialConsumerIdentity(ctx, signatureName, *unsignedDigestSpec, signerSpec); err == nil {
		if credentials, err = credentialGraph.Resolve(ctx, credentialConsumerIdentity); err != nil {
			logger.DebugContext(ctx, "could not resolve credentials for signature verification handler", "error", err.Error())
		}
	}

	if len(credentials) > 0 {
		logger.DebugContext(ctx, "discovered credentials from graph for signing", "attributes", slices.Collect(maps.Keys(credentials)))
	}

	signature, err := handler.Sign(ctx, *unsignedDigestSpec, signerSpec, credentials)
	if err != nil {
		return fmt.Errorf("creating signature from digest failed: %w", err)
	}

	if dryRun, _ := cmd.Flags().GetBool(FlagDryRun); dryRun {
		logger.InfoContext(ctx, "signature update skipped due to dry run")
		return nil
	}

	if idx := slices.IndexFunc(desc.Signatures, sigExists); idx >= 0 {
		desc.Signatures[idx].Signature = signature
	} else {
		desc.Signatures = append(desc.Signatures, descruntime.Signature{
			Name:      signatureName,
			Digest:    *unsignedDigestSpec,
			Signature: signature,
		})
	}

	if err := repo.ComponentVersionRepository().AddComponentVersion(ctx, desc); err != nil {
		return fmt.Errorf("updating component version with generated signature failed: %w", err)
	}

	logger.InfoContext(ctx, "signed successfully", "name", signatureName, "digest", unsignedDigestSpec.Value, "hashAlgorithm", unsignedDigestSpec.HashAlgorithm, "normalisationAlgorithm", unsignedDigestSpec.NormalisationAlgorithm)

	return nil
}

func generateDigest(
	ctx context.Context,
	desc *descruntime.Descriptor,
	logger *slog.Logger,
	normalisationAlgorithm string,
	hashAlgorithm string,
) (*descruntime.Digest, error) {
	if legacyAlgo := "jsonNormalisation/v3"; normalisationAlgorithm == legacyAlgo {
		normalisationAlgorithm = v4alpha1.Algorithm
		logger.WarnContext(ctx, "detected legacy signature algorithm, enabling best effort compatibility. consider updating your signature specification", "algorithm", normalisationAlgorithm, "legacy", legacyAlgo, "new", "hint")
	}

	normalised, err := normalisation.Normalise(desc, normalisationAlgorithm)
	if err != nil {
		return nil, fmt.Errorf("normalising component version failed: %w", err)
	}
	var hash crypto.Hash
	switch hashAlgorithm {
	case crypto.SHA256.String():
		hash = crypto.SHA256
	case crypto.SHA512.String():
		hash = crypto.SHA512
	default:
		return nil, fmt.Errorf("unsupported hash algorithm %q", hashAlgorithm)
	}
	instance := hash.New()
	if _, err := instance.Write(normalised); err != nil {
		return nil, fmt.Errorf("hashing component version failed: %w", err)
	}
	freshlyCalculatedDigest := instance.Sum(nil)
	return &descruntime.Digest{
		HashAlgorithm:          hash.HashFunc().String(),
		NormalisationAlgorithm: normalisationAlgorithm,
		Value:                  hex.EncodeToString(freshlyCalculatedDigest),
	}, nil
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
// TODO deduplicate with verify
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
