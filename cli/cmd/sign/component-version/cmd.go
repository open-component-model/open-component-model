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

	"ocm.software/open-component-model/bindings/go/credentials"
	"ocm.software/open-component-model/bindings/go/descriptor/normalisation/json/v4alpha1"
	descruntime "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	ctfv1 "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/ctf"
	ociv1 "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/oci"
	"ocm.software/open-component-model/bindings/go/rsa/signing/v1alpha1"
	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/bindings/go/signing"
	ocmctx "ocm.software/open-component-model/cli/internal/context"
	"ocm.software/open-component-model/cli/internal/flags/enum"
	"ocm.software/open-component-model/cli/internal/flags/log"
	"ocm.software/open-component-model/cli/internal/reference/compref"
	"ocm.software/open-component-model/cli/internal/render"
	"ocm.software/open-component-model/cli/internal/repository/ocm"
)

const (
	FlagConcurrencyLimit       = "concurrency-limit"
	FlagSignerSpec             = "signer-spec"
	FlagSignature              = "signature"
	FlagOutput                 = "output"
	FlagNormalisationAlgorithm = "normalisation"
	FlagHashAlgorithm          = "hash"
	FlagDryRun                 = "dry-run"
	FlagForce                  = "force"
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
		Long: fmt.Sprintf(`Creates or update cryptographic signatures on component descriptors.

## Reference Format

	[type::]{repository}/[valid-prefix]/{component}[:version]

- Prefixes: {%[1]s|none} (default: %[1]q)  
- Repo types: {%[2]s} (short: {%[3]s})  

## OCM Signing explained in simple steps

- Resolve OCM repository
- Fetch component version  
- Verify digests (--verify-digest-consistency)
- Normalise descriptor (--normalisation)
- Hash normalised descriptor (--hash)
- Sign hash (--signer-spec)

## Behavior

- Conflicting signatures cause failure unless --force is set (then overwrite)
- --dry-run: compute only, do not persist signature
- Default signature name: default
- Default signer: RSASSA-PSS plugin (needs private key)

Use this command to establish provenance of component versions.`,
			compref.DefaultPrefix,
			strings.Join([]string{ociv1.Type, ctfv1.Type}, "|"),
			strings.Join([]string{ociv1.ShortType, ociv1.ShortType2, ctfv1.ShortType, ctfv1.ShortType2}, "|"),
		),
		Example: strings.TrimSpace(`
# Sign a component version with default algorithms
sign component-version ghcr.io/open-component-model/ocm//ocm.software/ocmcli:0.23.0

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
            private_key_pem: <PEM>

# Sign with custom signature name
sign component-version ghcr.io/open-component-model/ocm//ocm.software/ocmcli:0.23.0 --signature my-signature

# Use a signer specification file
sign component-version ./repo/ocm//ocm.software/ocmcli:0.23.0 --signer-spec ./rsassa-pss.yaml

# Dry-run signing
sign component-version ghcr.io/open-component-model/ocm//ocm.software/ocmcli:0.23.0 --signature test --dry-run

# Force overwrite an existing signature
sign component-version ghcr.io/open-component-model/ocm//ocm.software/ocmcli:0.23.0 --signature my-signature --force`),
		RunE:              SignComponentVersion,
		DisableAutoGenTag: true,
	}

	enum.VarP(cmd.Flags(), FlagOutput, "o", []string{render.OutputFormatYAML.String(), render.OutputFormatJSON.String()}, "output format of the resulting signature")

	cmd.Flags().Int(FlagConcurrencyLimit, 4, "maximum amount of parallel requests to the repository for resolving component versions")
	cmd.Flags().String(FlagSignature, DefaultSignatureName, "name of the signature to create or update. defaults to \"default\"")
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
	signerSpecPath, _ := cmd.Flags().GetString(FlagSignerSpec)
	force, _ := cmd.Flags().GetBool(FlagForce)
	dryRun, _ := cmd.Flags().GetBool(FlagDryRun)

	reference := args[0]
	ref, err := compref.Parse(reference, compref.WithCTFAccessMode(ctfv1.AccessModeReadWrite))
	if err != nil {
		return fmt.Errorf("parsing component reference %q failed: %w", reference, err)
	}
	config := ocmContext.Configuration()
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
		return fmt.Errorf("getting component version failed: %w", err)
	}

	if err := signing.IsSafelyDigestible(&desc.Component); err != nil {
		logger.WarnContext(ctx, "component version not safely digestible", "error", err.Error())
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
	credMap := map[string]string{}
	if consumerID, err := handler.GetSigningCredentialConsumerIdentity(ctx, signatureName, *unsignedDigest, signerSpec); err == nil {
		if creds, err := credentialGraph.Resolve(ctx, consumerID); err == nil {
			credMap = creds
			logger.DebugContext(ctx, "using discovered credentials", "attributes", slices.Collect(maps.Keys(credMap)))
		} else {
			if errors.Is(err, credentials.ErrNotFound) {
				logger.DebugContext(ctx, "could not resolve credentials", "error", err.Error())
			} else {
				return fmt.Errorf("resolving signing credentials failed: %w", err)
			}
		}
	}

	// sign
	sigBytes, err := handler.Sign(ctx, *unsignedDigest, signerSpec, credMap)
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

	if err := repo.AddComponentVersion(ctx, desc); err != nil {
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
		spec := &v1alpha1.Config{
			SignatureAlgorithm:      v1alpha1.AlgorithmRSASSAPSS,
			SignatureEncodingPolicy: v1alpha1.SignatureEncodingPolicyPlain,
		}
		logger.Info("no signer spec file provided, using default", "algorithm", spec.SignatureAlgorithm, "encodingPolicy", spec.SignatureEncodingPolicy)
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

	var b []byte
	switch strings.ToLower(output) {
	case render.OutputFormatJSON.String():
		if b, err = json.MarshalIndent(v2sig, "", "  "); err != nil {
			return fmt.Errorf("marshalling signature to json failed: %w", err)
		}
		_, err = fmt.Fprintln(cmd.OutOrStdout(), string(b))
	case render.OutputFormatYAML.String():
		if b, err = yaml.Marshal(v2sig); err != nil {
			return fmt.Errorf("marshalling signature to yaml failed: %w", err)
		}
		_, err = fmt.Fprintln(cmd.OutOrStdout(), string(b))
	default:
		return fmt.Errorf("unsupported output format %q (supported: json|yaml|text)", output)
	}

	return err
}
