package componentversion

import (
	"context"
	"crypto"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"

	"github.com/spf13/cobra"
	"sigs.k8s.io/yaml"

	ocmctx "ocm.software/open-component-model/bindings/go/cli/internal/context"
	"ocm.software/open-component-model/bindings/go/cli/internal/flags/enum"
	"ocm.software/open-component-model/bindings/go/cli/internal/flags/log"
	"ocm.software/open-component-model/bindings/go/cli/internal/render"
	"ocm.software/open-component-model/bindings/go/cli/internal/repository/ocm"
	genericv1 "ocm.software/open-component-model/bindings/go/configuration/generic/v1/spec"
	"ocm.software/open-component-model/bindings/go/credentials"
	"ocm.software/open-component-model/bindings/go/descriptor/normalisation/json/v4alpha1"
	descruntime "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	ocmhttp "ocm.software/open-component-model/bindings/go/http"
	httpv1alpha1 "ocm.software/open-component-model/bindings/go/http/spec/config/v1alpha1"
	"ocm.software/open-component-model/bindings/go/oci/compref"
	ctfv1 "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/ctf"
	ociv1 "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/oci"
	"ocm.software/open-component-model/bindings/go/rsa/signing/v1alpha1"
	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/bindings/go/signing"
	"ocm.software/open-component-model/bindings/go/signing/tsa"
	signingv1alpha1 "ocm.software/open-component-model/bindings/go/signing/v1alpha1/spec"
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
	FlagTSA                    = "tsa"
	FlagTSAURL                 = "tsa-url"
)

const (
	// DefaultTSAURL is a well-known public TSA server used when --tsa is set
	// without an explicit --tsa-url. DigiCert's TSA is widely used in the
	// software supply-chain ecosystem (e.g. by sigstore, Authenticode, Java
	// jarsigner) and offers free, unauthenticated RFC 3161 timestamps. HTTPS is
	// used so the timestamp exchange is protected against on-path tampering.
	DefaultTSAURL = "https://timestamp.digicert.com"

	// DefaultSignatureName is the default name of the signature to create or update if not provided by FlagSignature.
	DefaultSignatureName = "default"
)

// signingConfigType is the configuration entry that replaced FlagSignerSpec.
var signingConfigType = runtime.NewVersionedType(signingv1alpha1.ConfigType, signingv1alpha1.Version)

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
- Sign hash (signer from the OCM configuration)

## Behavior

- Conflicting signatures cause failure unless --force is set (then overwrite)
- --dry-run: compute only, do not persist signature
- Default signature name: default
- Default signer: RSASSA-PSS plugin (needs private key)
- The signer is configured in the OCM configuration (%[4]s), not on the command line
- An entry with a "signature" field only applies to that signature, one without applies to all
- --signer-spec is no longer supported and fails with an error

Use this command to establish provenance of component versions.`,
			compref.DefaultPrefix,
			strings.Join([]string{ociv1.Type, ctfv1.Type}, "|"),
			strings.Join([]string{ociv1.ShortType, ociv1.ShortType2, ctfv1.ShortType, ctfv1.ShortType2}, "|"),
			signingConfigType,
		),
		Example: strings.TrimSpace(`
# Sign a component version with default algorithms
sign component-version ghcr.io/open-component-model//ocm.software/cli:0.12.0

## Example Credential Config (.ocmconfig) — Plain encoding (default)
#
# Credentials (private/public keys) are always resolved via .ocmconfig.
# The "signature" field must match the --signature flag (default: "default").

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

## Example Credential Config (.ocmconfig) — PEM encoding with certificate chain
#
# Required when signatureEncodingPolicy: PEM is set in the signer spec.
# private_key_pem_file: leaf private key (PKCS#1 or PKCS#8)
# public_key_pem_file:  PEM file containing [leaf, intermediate] certificates
#                       Do NOT include the root CA here — it must not be embedded
#                       in the signature (the verifier rejects self-signed embedded certs).

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
            private_key_pem_file: /path/to/leaf.key
            public_key_pem_file: /path/to/leaf-and-intermediate-chain.pem

## Example Signer Config (.ocmconfig)
#
# The signer configures the signing algorithm and encoding policy.
# It does NOT contain credentials - keys are always resolved via .ocmconfig.
# If omitted, defaults to RSASSA-PSS with Plain encoding.
# Add a "signature" field to scope an entry to the signature of that name
# (see the per-signature example below); without it the entry applies to all.
#
# Supported signer fields:
#   type:                    RSASigningConfiguration/v1alpha1
#   signatureAlgorithm:      RSASSA-PSS (default) | RSASSA-PKCS1-V1_5
#   signatureEncodingPolicy: Plain (default) | PEM
#
# signatureEncodingPolicy controls the *signature output* format:
#   Plain - signature stored as hex string; verification needs an external public key
#   PEM   - signature wrapped in a PEM SIGNATURE block with embedded certificate chain
#           (experimental; credentials must provide certificates, not bare public keys)

    type: generic.config.ocm.software/v1
    configurations:
    - type: signing.config.ocm.software/v1alpha1
      signer:
        type: RSASigningConfiguration/v1alpha1
        signatureAlgorithm: RSASSA-PSS
        signatureEncodingPolicy: Plain

# Example signer for PEM encoding (requires certificate chain in credentials):

    type: generic.config.ocm.software/v1
    configurations:
    - type: signing.config.ocm.software/v1alpha1
      signer:
        type: RSASigningConfiguration/v1alpha1
        signatureAlgorithm: RSASSA-PSS
        signatureEncodingPolicy: PEM

## Example Signer Config - one signer per signature
#
# The entry whose "signature" matches --signature wins; the entry without one
# is the fallback for every other signature. The credentials for each signature
# are matched the same way, by the "signature" field of the consumer identity.

    type: generic.config.ocm.software/v1
    configurations:
    - type: signing.config.ocm.software/v1alpha1
      signature: release
      signer:
        type: SigstoreSigningConfiguration/v1alpha1
    - type: signing.config.ocm.software/v1alpha1
      signer:
        type: RSASigningConfiguration/v1alpha1

## Example Signer Config - Sigstore keyless (SigstoreSigningConfiguration/v1alpha1)
#
# Use when signing without private keys via Sigstore/Fulcio OIDC.
# Endpoint discovery precedence:
#   1. signingConfig - local signing_config.json
#   2. Not set - public-good Sigstore TUF (default)

    type: generic.config.ocm.software/v1
    configurations:
    - type: signing.config.ocm.software/v1alpha1
      signer:
        type: SigstoreSigningConfiguration/v1alpha1

# With a local signing config file (private infrastructure):

    type: generic.config.ocm.software/v1
    configurations:
    - type: signing.config.ocm.software/v1alpha1
      signer:
        type: SigstoreSigningConfiguration/v1alpha1
        signingConfig: /path/to/signing_config.json

## Example Credential Config (.ocmconfig) — Sigstore OIDC token
#
# The OIDCIdentityTokenProvider plugin acquires an OIDC token via an interactive browser flow.

    type: generic.config.ocm.software/v1
    configurations:
    - type: credentials.config.ocm.software
      consumers:
      - identity:
          type: SigstoreSigner/v1alpha1
          signature: default
        credentials:
        - type: OIDCIdentityTokenProvider/v1alpha1

## Note on the OIDC issuer recorded in the Fulcio certificate
#
# On public Sigstore (Dex federation), Fulcio passes the upstream IdP issuer through
# into the certificate (OID 1.3.6.1.4.1.57264.1.8) — NOT the Dex URL:
#   - Google login   -> https://accounts.google.com
#   - GitHub login   -> https://github.com/login/oauth
#   - Microsoft login -> https://login.microsoftonline.com
# Verifiers must use the upstream issuer in certificateOIDCIssuer.
# OCM also stores this value in signatures[].signature.issuer for convenience.

# Sign with Sigtore using default .ocmconfig file
#
# In this case, the signer configuration AND the OIDC credentials are all configured in the main ocm configugration
# file.
sign component-version ghcr.io/open-component-model//ocm.software/cli:0.12.0

# Optionally, providing a --config flag on the CLI will overwrite all configurations and use this instead.
# Multiple configuration flags can be combined this way. Either have everything (signer config and credentials) or
# have multiple --config flags string. 
sign component-version ./repo//ocm.software/cli:0.12.0 --config ./rsassa-pss.ocmconfig --config ~/.ocmconfig

# Sign with custom signature name
sign component-version ghcr.io/open-component-model//ocm.software/cli:0.12.0 --signature my-signature

# Dry-run signing
sign component-version ghcr.io/open-component-model//ocm.software/cli:0.12.0 --signature test --dry-run

# Force overwrite an existing signature
sign component-version ghcr.io/open-component-model//ocm.software/cli:0.12.0 --signature my-signature --force`),
		RunE:              SignComponentVersion,
		DisableAutoGenTag: true,
	}

	enum.VarP(cmd.Flags(), FlagOutput, "o", []string{render.OutputFormatYAML.String(), render.OutputFormatJSON.String()}, "output format of the resulting signature")

	cmd.Flags().Int(FlagConcurrencyLimit, 4, "maximum amount of parallel requests to the repository for resolving component versions")
	cmd.Flags().String(FlagSignature, DefaultSignatureName, "name of the signature to create or update. defaults to \"default\"")
	cmd.Flags().String(FlagSignerSpec, "", fmt.Sprintf("DEPRECATED: no longer supported, configure the signer in the OCM configuration instead (%s, field \"signer\")", signingConfigType))
	cmd.Flags().Bool(FlagDryRun, false, "compute signature but do not persist it to the repository")
	cmd.Flags().String(FlagNormalisationAlgorithm, v4alpha1.Algorithm, "normalisation algorithm to use (default jsonNormalisation/v4alpha1)")
	cmd.Flags().String(FlagHashAlgorithm, crypto.SHA256.String(), "hash algorithm to use (SHA256, SHA512)")
	cmd.Flags().Bool(FlagForce, false, "overwrite existing signatures under the same name")
	cmd.Flags().Bool(FlagTSA, false, fmt.Sprintf("request an RFC 3161 timestamp from a TSA server (default: %s)", DefaultTSAURL))
	cmd.Flags().String(FlagTSAURL, "", "custom TSA server URL (implies --tsa)")

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
	if cmd.Flags().Changed(FlagSignerSpec) {
		return fmt.Errorf("--%s is no longer supported: move the signer specification into the OCM configuration as an entry of type %s (field %q) and pass it with --config", FlagSignerSpec, signingConfigType, "signer")
	}

	signatureName, _ := cmd.Flags().GetString(FlagSignature)
	if signatureName == "" {
		signatureName = DefaultSignatureName
	}
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
	signerConfig, err := loadSignerConfig(config, signatureName, logger)
	if err != nil {
		return err
	}

	handler, err := pluginManager.SigningRegistry.GetPlugin(ctx, signerConfig)
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

	// Resolve the TSA URL from flags. --tsa uses the default server; --tsa-url
	// selects a custom one and implies --tsa. A dry run never contacts a TSA.
	tsaURL := tsaURLFromFlags(cmd)
	useTSA := tsaURL != "" && !dryRun
	if useTSA {
		if err := addSignedTSALabel(desc, signatureName, tsaURL); err != nil {
			return err
		}
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
	var foundCreds runtime.Typed
	if consumerID, err := handler.GetSigningCredentialConsumerIdentity(ctx, signatureName, *unsignedDigest, signerConfig); err == nil {
		if creds, err := credentialGraph.Resolve(ctx, consumerID); err == nil {
			foundCreds = creds
			logger.DebugContext(ctx, "using discovered credentials", "type", foundCreds.GetType())
		} else {
			if errors.Is(err, credentials.ErrNotFound) {
				logger.DebugContext(ctx, "could not resolve credentials", "error", err.Error())
			} else {
				return fmt.Errorf("resolving signing credentials failed: %w", err)
			}
		}
	}

	// sign
	sigBytes, err := handler.Sign(ctx, *unsignedDigest, signerConfig, foundCreds)
	if err != nil {
		return fmt.Errorf("signing failed: %w", err)
	}

	// TSA timestamp (optional). Requested after signing so it can cover the same
	// digest. Never performed on a dry run (useTSA already excludes dryRun).
	var tsSpec *descruntime.TimestampSpec
	if useTSA {
		httpConfig, err := httpv1alpha1.ResolveHTTPConfig(config)
		if err != nil {
			return fmt.Errorf("resolving HTTP configuration for TSA request failed: %w", err)
		}
		tsaClient := ocmhttp.New(ocmhttp.WithConfig(httpConfig))
		tsSpec, err = requestTSATimestamp(ctx, logger, tsaClient, tsaURL, unsignedDigest)
		if err != nil {
			return err
		}
	}

	out := descruntime.Signature{
		Name:      signatureName,
		Digest:    *unsignedDigest,
		Signature: sigBytes,
		Timestamp: tsSpec,
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

// loadSignerConfig resolves the signer configuration for the given signature from
// the central OCM configuration, falling back to RSASSA-PSS with Plain encoding
// if none is configured.
func loadSignerConfig(config *genericv1.Config, signatureName string, logger *slog.Logger) (runtime.Typed, error) {
	signingConfig, err := signingv1alpha1.LookupConfigForSignature(config, signatureName)
	if err != nil {
		return nil, fmt.Errorf("getting signing configuration failed: %w", err)
	}
	if signingConfig != nil && signingConfig.Signer != nil {
		logger.Debug("using signer from configuration", "type", signingConfig.Signer.GetType(), "signature", signatureName)
		return signingConfig.Signer, nil
	}

	spec := &v1alpha1.Config{
		SignatureAlgorithm:      v1alpha1.AlgorithmRSASSAPSS,
		SignatureEncodingPolicy: v1alpha1.SignatureEncodingPolicyPlain,
	}
	logger.Info("no signer configured, using default", "algorithm", spec.SignatureAlgorithm, "encodingPolicy", spec.SignatureEncodingPolicy)
	_, _ = v1alpha1.Scheme.DefaultType(spec)
	return spec, nil
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

// tsaURLFromFlags resolves the effective TSA URL from the --tsa / --tsa-url
// flags. --tsa selects the default server; --tsa-url overrides it and implies
// --tsa. It returns an empty string when timestamping was not requested.
func tsaURLFromFlags(cmd *cobra.Command) string {
	tsaURL := ""
	if tsaEnabled, _ := cmd.Flags().GetBool(FlagTSA); tsaEnabled {
		tsaURL = DefaultTSAURL
	}
	if customTSAURL, _ := cmd.Flags().GetString(FlagTSAURL); customTSAURL != "" {
		tsaURL = customTSAURL
	}
	return tsaURL
}

// addSignedTSALabel records the TSA URL as a signed (signing-relevant) label on
// the component so it is covered by the digest and therefore tamper-evident.
// Verifiers use it for URL-specific credential lookup of the TSA root certs.
func addSignedTSALabel(desc *descruntime.Descriptor, signatureName, tsaURL string) error {
	tsaURLJSON, err := json.Marshal(tsaURL)
	if err != nil {
		return fmt.Errorf("marshalling TSA URL label: %w", err)
	}
	desc.Component.Labels = append(desc.Component.Labels, descruntime.Label{
		Name:    tsa.TSAURLLabelPrefix + signatureName,
		Value:   tsaURLJSON,
		Signing: true,
		Version: "v1alpha1",
	})
	return nil
}

// requestTSATimestamp obtains an RFC 3161 timestamp for the given digest from
// the TSA at tsaURL and returns it as a TimestampSpec ready to attach to the
// signature.
func requestTSATimestamp(ctx context.Context, logger *slog.Logger, client tsa.HTTPClient, tsaURL string, digest *descruntime.Digest) (*descruntime.TimestampSpec, error) {
	hash, err := signing.GetSupportedHash(digest.HashAlgorithm)
	if err != nil {
		return nil, fmt.Errorf("preparing TSA request: %w", err)
	}
	digestBytes, err := hex.DecodeString(digest.Value)
	if err != nil {
		return nil, fmt.Errorf("decoding digest for TSA request: %w", err)
	}

	token, err := tsa.RequestTimestamp(ctx, client, tsaURL, hash, digestBytes)
	if err != nil {
		return nil, fmt.Errorf("TSA timestamp request failed: %w", err)
	}

	logger.InfoContext(ctx, "obtained TSA timestamp", "time", token.Time, "server", tsaURL)
	return &descruntime.TimestampSpec{
		Value: string(tsa.ToPEM(token.Raw)),
		Time:  descruntime.CreationTime(token.Time),
	}, nil
}
