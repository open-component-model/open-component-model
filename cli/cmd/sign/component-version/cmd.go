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

This command creates or updates cryptographic signatures on component versions
stored in an OCM repository. The signature covers a normalised and hashed form
of the component descriptor, ensuring integrity and authenticity of the
component and its resources, no matter where and how they are stored.

The format of a component reference is:
	[type::]{repository}/[valid-prefix]/{component}[:version]

Valid prefixes: {%[1]s|none}. If <none> is used, it defaults to %[1]q.
Supported repository types: {%[2]s} (short forms: {%[3]s}).
If no type is given, the repository path is inferred by heuristics.

Verification steps performed before signing:

* Resolve the repository and fetch the target component version.
* Check digest consistency if not disabled (--verify-digest-consistency).
* Normalise the descriptor using the chosen algorithm (--normalisation).
* Hash the normalised form with the given algorithm (--hash).
* Produce a signature with the configured signer specification (--signer-spec).

Behavior:

* If a signature with the same name exists and --force is not set, the command fails.
* With --force, an existing signature is overwritten.
* With --dry-run, a signature is computed but not persisted to the repository.
* If --signature is omitted, the default signature name "default" is used.
* If --signer-spec is omitted, the default RSASSA-PSS plugin is used (without auto-generated key material)

Use this command in automated pipelines or interactive workflows to
establish provenance of component versions and prepare them for downstream
verification. Also use it for testing integrity workflows.`,
			compref.DefaultPrefix,
			strings.Join([]string{ociv1.Type, ctfv1.Type}, "|"),
			strings.Join([]string{ociv1.ShortType, ociv1.ShortType2, ctfv1.ShortType, ctfv1.ShortType2}, "|"),
		),
		Example: strings.TrimSpace(`
# Sign a component version with default algorithms
sign component-version ghcr.io/open-component-model/ocm//ocm.software/ocmcli:0.23.0

# Sign with custom signature name
sign component-version ghcr.io/open-component-model/ocm//ocm.software/ocmcli:0.23.0 --signature my-signature

# Use a signer specification file
sign component-version ./repo/ocm//ocm.software/ocmcli:0.23.0 --signer-spec ./rsassa-pss.yaml

# Dry-run signing
sign component-version ghcr.io/open-component-model/ocm//ocm.software/ocmcli:0.23.0 --signature test --dry-run

# Force overwrite an existing signature
sign component-version ghcr.io/open-component-model/ocm//ocm.software/ocmcli:0.23.0 --signature my-signature --force
`),
		RunE:              VerifyComponentVersion,
		DisableAutoGenTag: true,
	}

	cmd.Flags().Int(FlagConcurrencyLimit, 4, "maximum amount of parallel requests to the repository for resolving component versions")
	cmd.Flags().String(FlagSignature, "", "name of the signature to create or update. defaults to \"default\"")
	cmd.Flags().Bool(FlagVerifyDigestConsistency, true, "verify that all digests are complete and valid before signing")
	cmd.Flags().String(FlagSignerSpec, "", "path to a signer specification file; defaults to RSASSA-PSS if not provided")
	cmd.Flags().Bool(FlagDryRun, false, "compute signature but do not persist it to the repository")
	cmd.Flags().String(FlagNormalisationAlgorithm, v4alpha1.Algorithm, "normalisation algorithm to use (default jsonNormalisation/v4alpha1)")
	cmd.Flags().String(FlagHashAlgorithm, crypto.SHA256.String(), "hash algorithm to use (SHA256, SHA512)")
	cmd.Flags().Bool(FlagForce, false, "overwrite existing signatures under the same name")

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
		return fmt.Errorf("getting signer-spec flag failed: %w", err)
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
			logger.WarnContext(ctx, "component version is not considered safely digestable", "error", err.Error())
		}
	}

	var signerSpec runtime.Typed
	if signerSpecPath == "" {
		logger.InfoContext(ctx, "no signer specification file given, using default RSASSA-PSS")
		signerSpec = &v1alpha1.Config{}
		_, _ = v1alpha1.Scheme.DefaultType(signerSpec)
	} else {
		genericScheme := runtime.NewScheme(runtime.WithAllowUnknown())
		specBytes, err := os.ReadFile(signerSpecPath)
		if err != nil {
			return fmt.Errorf("reading signer specification file %q failed: %w", signerSpecPath, err)
		}
		signerSpec = &runtime.Raw{}
		if err := genericScheme.Decode(bytes.NewReader(specBytes), signerSpec); err != nil {
			return fmt.Errorf("decoding signer specification file %q failed: %w", signerSpecPath, err)
		}
	}

	handler, err := pluginManager.SigningRegistry.GetPlugin(ctx, signerSpec)
	if err != nil {
		return fmt.Errorf("getting signature handler plugin failed: %w", err)
	}

	if signatureName == "" {
		logger.InfoContext(ctx, "no signature name given, using default", "name", "default")
		signatureName = "default"
	}
	sigExists := func(signature descruntime.Signature) bool {
		return signature.Name == signatureName
	}

	if slices.ContainsFunc(desc.Signatures, sigExists) {
		if force, _ := cmd.Flags().GetBool(FlagForce); force {
			logger.InfoContext(ctx, "overwriting existing signature due to --force", "name", signatureName)
		} else {
			return fmt.Errorf("signature with name %q already exists", signatureName)
		}
	}

	unsignedDigestSpec, err := generateDigest(ctx, desc, logger, cmd.Flag(FlagNormalisationAlgorithm).Value.String(), cmd.Flag(FlagHashAlgorithm).Value.String())
	if err != nil {
		return fmt.Errorf("generating digest failed: %w", err)
	}

	var credentials map[string]string
	if consumerID, err := handler.GetSigningCredentialConsumerIdentity(ctx, signatureName, *unsignedDigestSpec, signerSpec); err == nil {
		if credentials, err = credentialGraph.Resolve(ctx, consumerID); err != nil {
			logger.DebugContext(ctx, "could not resolve credentials", "error", err.Error())
		}
	}

	if len(credentials) > 0 {
		logger.DebugContext(ctx, "using discovered credentials for signing", "attributes", slices.Collect(maps.Keys(credentials)))
	}

	signature, err := handler.Sign(ctx, *unsignedDigestSpec, signerSpec, credentials)
	if err != nil {
		return fmt.Errorf("creating signature failed: %w", err)
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
		return fmt.Errorf("updating component version with signature failed: %w", err)
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
		logger.WarnContext(ctx, "legacy normalisation algorithm detected, using v4alpha1", "legacy", legacyAlgo, "new", v4alpha1.Algorithm)
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
	freshDigest := instance.Sum(nil)
	return &descruntime.Digest{
		HashAlgorithm:          hash.HashFunc().String(),
		NormalisationAlgorithm: normalisationAlgorithm,
		Value:                  hex.EncodeToString(freshDigest),
	}, nil
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
