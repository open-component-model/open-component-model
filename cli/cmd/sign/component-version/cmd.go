package componentversion

import (
	"crypto"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"os"
	"slices"
	"strings"

	"github.com/spf13/cobra"
	"sigs.k8s.io/yaml"

	resolverruntime "ocm.software/open-component-model/bindings/go/configuration/ocm/v1/runtime"
	"ocm.software/open-component-model/bindings/go/descriptor/normalisation/json/v4alpha1"
	descruntime "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	ctfv1 "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/ctf"
	ociv1 "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/oci"
	"ocm.software/open-component-model/bindings/go/rsa/signing/v1alpha1"
	"ocm.software/open-component-model/bindings/go/runtime"
	ocmctx "ocm.software/open-component-model/cli/internal/context"
	"ocm.software/open-component-model/cli/internal/flags/enum"
	"ocm.software/open-component-model/cli/internal/flags/log"
	"ocm.software/open-component-model/cli/internal/reference/compref"
	"ocm.software/open-component-model/cli/internal/render"
	"ocm.software/open-component-model/cli/internal/repository/ocm"
	"ocm.software/open-component-model/cli/internal/signing"
)

const (
	FlagConcurrencyLimit        = "concurrency-limit"
	FlagSignerSpec              = "signer-spec"
	FlagSignature               = "signature"
	FlagOutput                  = "output"
	FlagNormalisationAlgorithm  = "normalisation"
	FlagHashAlgorithm           = "hash"
	FlagVerifyDigestConsistency = "verify-digest-consistency"
	FlagDryRun                  = "dry-run"
	FlagForce                   = "force"
)

const (
	// DefaultSignatureName is the default name of the signature to create or update if not provided by FlagSignature.
	DefaultSignatureName = "default"
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
		RunE:              SignComponentVersion,
		DisableAutoGenTag: true,
	}

	enum.VarP(cmd.Flags(), FlagOutput, "o", []string{render.OutputFormatYAML.String(), render.OutputFormatJSON.String()}, "output format of the resulting signature")

	cmd.Flags().Int(FlagConcurrencyLimit, 4, "maximum amount of parallel requests to the repository for resolving component versions")
	cmd.Flags().String(FlagSignature, DefaultSignatureName, "name of the signature to create or update. defaults to \"default\"")
	cmd.Flags().Bool(FlagVerifyDigestConsistency, true, "verify that all digests are complete and valid before signing")
	cmd.Flags().String(FlagSignerSpec, "", "path to a signer specification file. If empty, defaults to an empty RSASSA-PSS configuration.")
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

func SignComponentVersion(cmd *cobra.Command, args []string) error {
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
		return fmt.Errorf("plugin manager not available in context")
	}

	credentialGraph := ocmContext.CredentialGraph()
	if credentialGraph == nil {
		return fmt.Errorf("credential graph not available in context")
	}

	// flags
	signatureName, _ := cmd.Flags().GetString(FlagSignature)
	if signatureName == "" {
		signatureName = DefaultSignatureName
	}
	verifyDigestConsistency, _ := cmd.Flags().GetBool(FlagVerifyDigestConsistency)
	signerSpecPath, _ := cmd.Flags().GetString(FlagSignerSpec)
	force, _ := cmd.Flags().GetBool(FlagForce)
	dryRun, _ := cmd.Flags().GetBool(FlagDryRun)

	if !verifyDigestConsistency {
		logger.WarnContext(ctx, "digest consistency verification is disabled")
	}

	ref := args[0]
	var resolvers []*resolverruntime.Resolver //nolint:staticcheck // no replacement for resolvers available yet https://github.com/open-component-model/ocm-project/issues/575
	if cfg := ocmContext.Configuration(); cfg != nil {
		if resolvers, err = ocm.ResolversFromConfig(cfg); err != nil {
			return fmt.Errorf("resolvers from configuration failed: %w", err)
		}
	}

	repo, err := ocm.NewFromRefWithFallbackRepo(
		ctx, pluginManager, credentialGraph, resolvers, ref,
		compref.WithCTFAccessMode(ctfv1.AccessModeReadWrite),
	)
	if err != nil {
		return fmt.Errorf("initializing repository failed: %w", err)
	}

	desc, err := repo.GetComponentVersion(ctx)
	if err != nil {
		return fmt.Errorf("getting component version failed: %w", err)
	}

	if verifyDigestConsistency {
		if err := signing.IsSafelyDigestible(&desc.Component); err != nil {
			logger.WarnContext(ctx, "component version not safely digestible", "error", err.Error())
		}
	}

	// signer spec
	signerSpec, err := loadSignerSpec(signerSpecPath, logger)
	if err != nil {
		return err
	}

	handler, err := pluginManager.SigningRegistry.GetPlugin(ctx, signerSpec)
	if err != nil {
		return fmt.Errorf("getting signature handler failed: %w", err)
	}

	// existing signature check
	sigExists := func(sig descruntime.Signature) bool { return sig.Name == signatureName }
	if slices.ContainsFunc(desc.Signatures, sigExists) {
		if !force {
			return fmt.Errorf("signature %q already exists", signatureName)
		}
		logger.InfoContext(ctx, "overwriting existing signature", "name", signatureName)
	}

	// digest
	unsignedDigest, err := signing.GenerateDigest(
		ctx, desc, logger,
		cmd.Flag(FlagNormalisationAlgorithm).Value.String(),
		cmd.Flag(FlagHashAlgorithm).Value.String(),
	)
	if err != nil {
		return fmt.Errorf("generating digest failed: %w", err)
	}

	// credentials
	credentials := map[string]string{}
	if consumerID, err := handler.GetSigningCredentialConsumerIdentity(ctx, signatureName, *unsignedDigest, signerSpec); err == nil {
		if creds, err := credentialGraph.Resolve(ctx, consumerID); err == nil {
			credentials = creds
			logger.DebugContext(ctx, "using discovered credentials", "attributes", slices.Collect(maps.Keys(credentials)))
		} else {
			logger.DebugContext(ctx, "could not resolve credentials", "error", err.Error())
		}
	}

	// sign
	sigBytes, err := handler.Sign(ctx, *unsignedDigest, signerSpec, credentials)
	if err != nil {
		return fmt.Errorf("signing failed: %w", err)
	}

	out := descruntime.Signature{
		Name:      signatureName,
		Digest:    *unsignedDigest,
		Signature: sigBytes,
	}

	if err := printSignature(cmd, out); err != nil {
		return err
	}

	if dryRun {
		logger.InfoContext(ctx, "dry run: signature not persisted")
		return nil
	}

	// persist signature
	if idx := slices.IndexFunc(desc.Signatures, sigExists); idx >= 0 {
		desc.Signatures[idx] = out
	} else {
		desc.Signatures = append(desc.Signatures, out)
	}

	if err := repo.ComponentVersionRepository().AddComponentVersion(ctx, desc); err != nil {
		return fmt.Errorf("updating component version failed: %w", err)
	}

	logger.InfoContext(ctx, "signed successfully",
		"name", signatureName,
		"digest", unsignedDigest.Value,
		"hashAlgorithm", unsignedDigest.HashAlgorithm,
		"normalisationAlgorithm", unsignedDigest.NormalisationAlgorithm,
	)
	return nil
}

func loadSignerSpec(path string, logger *slog.Logger) (_ runtime.Typed, err error) {
	if path == "" {
		logger.Info("no signer spec file provided, using default RSASSA-PSS")
		spec := &v1alpha1.Config{}
		_, _ = v1alpha1.Scheme.DefaultType(spec)
		return spec, nil
	}

	data, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("reading signer spec %q failed: %w", path, err)
	}
	defer func() {
		err = errors.Join(err, data.Close())
	}()

	scheme := runtime.NewScheme(runtime.WithAllowUnknown())
	raw := &runtime.Raw{}
	if err := scheme.Decode(data, raw); err != nil {
		return nil, fmt.Errorf("decoding signer spec %q failed: %w", path, err)
	}
	return raw, nil
}

func printSignature(cmd *cobra.Command, sig descruntime.Signature) error {
	output, err := enum.Get(cmd.Flags(), FlagOutput)
	if err != nil {
		return fmt.Errorf("getting output flag failed: %w", err)
	}

	v2sig := descruntime.ConvertToV2Signature(&sig)

	switch strings.ToLower(output) {
	case render.OutputFormatJSON.String():
		b, err := json.MarshalIndent(v2sig, "", "  ")
		if err != nil {
			return fmt.Errorf("marshalling signature to json failed: %w", err)
		}
		fmt.Fprintln(cmd.OutOrStdout(), string(b))
	case render.OutputFormatYAML.String():
		b, err := yaml.Marshal(v2sig)
		if err != nil {
			return fmt.Errorf("marshalling signature to yaml failed: %w", err)
		}
		fmt.Fprintln(cmd.OutOrStdout(), string(b))
	default:
		return fmt.Errorf("unsupported output format %q (supported: json|yaml|text)", output)
	}

	return nil
}
