package componentversion

import (
	"context"
	"crypto"
	"crypto/x509"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/sync/errgroup"

	ocmctx "ocm.software/open-component-model/bindings/go/cli/internal/context"
	"ocm.software/open-component-model/bindings/go/cli/internal/flags/log"
	"ocm.software/open-component-model/bindings/go/cli/internal/repository/ocm"
	genericv1 "ocm.software/open-component-model/bindings/go/configuration/generic/v1/spec"
	"ocm.software/open-component-model/bindings/go/credentials"
	"ocm.software/open-component-model/bindings/go/descriptor/normalisation/algorithms"
	descruntime "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/oci/compref"
	ctfv1 "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/ctf"
	ociv1 "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/oci"
	"ocm.software/open-component-model/bindings/go/rsa/signing/v1alpha1"
	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/bindings/go/signing"
	"ocm.software/open-component-model/bindings/go/signing/tsa"
	signingv1alpha1 "ocm.software/open-component-model/bindings/go/signing/v1alpha1/spec"
	sigstorev1alpha1 "ocm.software/open-component-model/bindings/go/sigstore/signing/v1alpha1"
)

const (
	FlagConcurrencyLimit = "concurrency-limit"
	FlagSignature        = "signature"
	FlagVerifierSpec     = "verifier-spec"

	FlagInsecureTrustLegacyTimestamps = "insecure-trust-legacy-timestamps"
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

	[type::]{repository}/[valid-prefix]/{component}[:version][@digest]

- Prefixes: {%[1]s|none} (default: %[1]q)  
- Repo types: {%[2]s} (short: {%[3]s})
- Digest: optional pin (see "ocm get cv --help"), "@sha256:<hex>" (OCI digest of the artifact the component version is stored as; the version may be omitted) or "@jsonNormalisation.v4alpha3+sha256:<hex>" (normalized: the digest signatures sign); the command fails unless the component version matches it

## OCM Verification explained in simple steps

- Resolve OCM repository  
- Fetch component version 
- Normalise descriptor (algorithm from signature)
- Recompute hash and compare with signature digest
- Verify signature (verifier from the OCM configuration, otherwise derived from the signature algorithm)

## Behavior

- --signature selects a single signature by name; without it, every signature on the descriptor is verified
- Credentials are resolved per signature under that signature's name, so every signature needs its own consumer entry
- Signatures are verified concurrently (--concurrency-limit); the command exits non-zero on the first failure
- Without a configured verifier, the signature algorithm selects the default: RSA signatures (RSASSA-PSS, RSASSA-PKCS1-V1_5) use the RSA verifier, which resolves the public key from credentials in .ocmconfig
- Sigstore signatures default to a Sigstore verifier that accepts any signer identity and issuer and logs a warning: the bundle, certificate chain and transparency log are checked, but not who signed. Configure a SigstoreVerificationConfiguration/v1alpha1 verifier with identity constraints to pin the signer
- The verifier is configured in the OCM configuration (%[4]s), not on the command line
- An entry with a "signature" field only applies to that signature, one without applies to all
- The verifier is resolved per signature, so a component carrying several signatures can be verified with a different handler for each
- --verifier-spec is no longer supported and fails with an error
- In an OCI registry or CTF, the version tag is resolved once and the component version is read from the artifact it resolved to; on success the log field "reference" holds the reference pinned to that artifact's OCI digest. Passing it to a subsequent command (e.g. transfer, download resource) reads exactly the verified content, even if the tag moves in between

## Timestamp Verification (RFC 3161 TSA)

- If a signature carries an RFC 3161 timestamp, the verifier checks it automatically
- Full certificate-chain verification of the timestamp token requires the TSA's root CA certificate, supplied via the credential graph under a TSA/v1alpha1 identity
- Without TSA root certificates the token structure and imprint are still checked, but the certificate chain is not verified and a warning is logged
- Only a timestamp validated against trusted TSA roots is used to validate an (RSA/PEM) signing certificate as of the signing time; a merely structural token never relaxes certificate validity
- The TSA URL label (url.tsa.ocm.software/<signature>) selects a URL-specific TSA/v1alpha1 entry; an entry without URL attributes applies to any TSA
- Timestamps created by the OCM v1 CLI are verified as well, but their time is not used to validate certificates unless --%[5]s is set: they cover only the descriptor digest, which is known before signing, so they do not prove the signature existed at that time

Use to validate component versions before promotion, deployment, or further usage to ensure integrity and provenance.`,
			compref.DefaultPrefix,
			strings.Join([]string{ociv1.Type, ctfv1.Type}, "|"),
			strings.Join([]string{ociv1.ShortType, ociv1.ShortType2, ctfv1.ShortType, ctfv1.ShortType2}, "|"),
			signingConfigType,
			FlagInsecureTrustLegacyTimestamps,
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
# resolved via .ocmconfig credentials. If omitted, the signature algorithm selects
# the verifier: RSA signatures use the RSA verifier, Sigstore signatures a Sigstore
# verifier that accepts any signer identity (with a warning).
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

## Example Credential Config (TSA timestamp verification)
# TSA/v1alpha1 identity supplying the TSA root CA for full timestamp chain verification
# (see "Timestamp Verification (RFC 3161 TSA)" above):

    type: generic.config.ocm.software/v1
    configurations:
    - type: credentials.config.ocm.software
      consumers:
      - identity:
          type: TSA/v1alpha1
          hostname: timestamp.digicert.com
          scheme: http
        credentials:
        - type: TSACredentials/v1alpha1
          rootCertsPEMFile: /path/to/digicert-tsa-root.pem
`),
		RunE:              VerifyComponentVersion,
		DisableAutoGenTag: true,
	}

	cmd.Flags().Int(FlagConcurrencyLimit, 4, "maximum amount of parallel requests to the repository for resolving component versions")
	cmd.Flags().String(FlagSignature, "", "name of the signature to verify. If not set, all signatures are verified.")
	cmd.Flags().Bool(FlagInsecureTrustLegacyTimestamps, false, "use the time of trusted timestamps created by the OCM v1 CLI to validate expired signing certificates; these timestamps cover only the descriptor digest, so a signature made after the certificate expired can reuse one")
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

	trustLegacyTimestamps, err := cmd.Flags().GetBool(FlagInsecureTrustLegacyTimestamps)
	if err != nil {
		return fmt.Errorf("getting flag %q failed: %w", FlagInsecureTrustLegacyTimestamps, err)
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

	// Read from the artifact the version tag resolves to, so the pinned reference reported on
	// success names exactly the verified content.
	_, desc, pinnedRef, err := ocm.GetComponentVersionByArtifact(ctx, repo, ref)
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

	// A signature covers resources and references only through their digests.
	// With GODEBUG=fips140=only, a binding that rests on a weak hash such as MD5
	// or SHA-1 does not count as verified; otherwise warn.
	if err := signing.ValidateDigestHashAlgorithms(&desc.Component); err != nil {
		if signing.DigestHashAlgorithmsEnforced() {
			return fmt.Errorf("refusing to verify component version with GODEBUG=fips140=only: %w", err)
		}
		logger.WarnContext(ctx, "component version uses a weak digest hash algorithm (GODEBUG=fips140=only rejects it)", "error", err.Error())
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
			verifierSpec, err := loadVerifierConfig(config, signature, logger)
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

			// Verify an optional RFC 3161 timestamp attached to the signature.
			verifyCtx := egctx
			if signature.Timestamp != nil {
				verifiedTime, trusted, err := verifyTSATimestamp(egctx, logger, credentialGraph, desc, signature, trustLegacyTimestamps)
				if err != nil {
					return err
				}
				// Only a timestamp validated against trusted TSA roots may relax
				// certificate validity. A merely structural token proves nothing
				// about the signing time, so it must never override the handler's
				// current-time certificate validation.
				if trusted {
					verifyCtx = signing.WithTrustedSigningTime(egctx, verifiedTime)
				}
			}

			if err := handler.Verify(verifyCtx, signature, verifierSpec, creds); err != nil {
				return err
			}
			algorithms.WarnNonDefaultDigest(egctx, logger, signature.Digest.NormalisationAlgorithm, "name", signature.Name)
			return nil
		})
	}

	if err := eg.Wait(); err != nil {
		return fmt.Errorf("SIGNATURE VERIFICATION FAILED: %w", err)
	}

	if pinnedRef == nil {
		logger.InfoContext(ctx, "SIGNATURE VERIFICATION SUCCESSFUL")
		return nil
	}
	// Feeding this reference to a subsequent command reads exactly the verified artifact.
	logger.InfoContext(ctx, "SIGNATURE VERIFICATION SUCCESSFUL", "reference", pinnedRef.String())
	return nil
}

// loadVerifierConfig resolves the verifier configuration for the given signature
// from the central OCM configuration. Without one, the verifier is derived from
// the algorithm the signature declares.
func loadVerifierConfig(config *genericv1.Config, signature descruntime.Signature, logger *slog.Logger) (runtime.Typed, error) {
	signingConfig, err := signingv1alpha1.LookupConfigForSignature(config, signature.Name)
	if err != nil {
		return nil, fmt.Errorf("getting signing configuration failed: %w", err)
	}
	if signingConfig != nil && signingConfig.Verifier != nil {
		logger.Debug("using verifier from configuration", "type", signingConfig.Verifier.GetType(), "signature", signature.Name)
		return signingConfig.Verifier, nil
	}

	switch sigstorev1alpha1.SignatureAlgorithm(signature.Signature.Algorithm) {
	case sigstorev1alpha1.AlgorithmSigstoreV1Alpha1, sigstorev1alpha1.AlgorithmSigstoreLegacy:
		// Without configured constraints any Fulcio-issued identity is accepted: the
		// bundle is still checked cryptographically, but not bound to a signer.
		spec := &sigstorev1alpha1.VerifyConfig{
			Type:                        runtime.NewVersionedType(sigstorev1alpha1.VerifyConfigType, sigstorev1alpha1.Version),
			CertificateOIDCIssuerRegexp: ".*",
			CertificateIdentityRegexp:   ".*",
		}
		logger.Warn("no verifier configured, using default Sigstore verifier that accepts ANY signer identity and issuer; "+
			"configure a "+spec.Type.String()+" verifier with certificateOIDCIssuer and certificateIdentity to pin the signer",
			"signature", signature.Name, "algorithm", signature.Signature.Algorithm)
		return spec, nil
	}

	spec := &v1alpha1.Config{}
	logger.Info("no verifier configured, using default RSA verifier", "signature", signature.Name, "algorithm", signature.Signature.Algorithm)
	_, _ = v1alpha1.Scheme.DefaultType(spec)
	return spec, nil
}

// verifyTSATimestamp verifies the timestamp attached to a signature. trusted is
// true only when the token chained to TSA roots from the credential graph as of
// GenTime; without roots only structural validity is checked.
func verifyTSATimestamp(
	ctx context.Context,
	logger *slog.Logger,
	credentialGraph credentials.Resolver,
	desc *descruntime.Descriptor,
	signature descruntime.Signature,
	trustLegacy bool,
) (time.Time, bool, error) {
	var tsaURL string
	labelName := tsa.TSAURLLabelPrefix + signature.Name
	if idx := slices.IndexFunc(desc.Component.Labels, func(l descruntime.Label) bool { return l.Name == labelName }); idx >= 0 {
		if err := desc.Component.Labels[idx].GetValue(&tsaURL); err != nil {
			logger.WarnContext(ctx, "ignoring unparsable TSA URL label for credential lookup", "label", labelName, "error", err.Error())
		}
	}

	tsaRootPool, err := tsaRootPoolFromGraph(ctx, logger, credentialGraph, tsaURL)
	if err != nil {
		return time.Time{}, false, err
	}
	if tsaRootPool != nil {
		logger.DebugContext(ctx, "TSA root certificates resolved from credential graph", "name", signature.Name)
	} else {
		logger.WarnContext(ctx, "verifying TSA timestamp without root certificates; only structural validity is checked. Configure TSA root certs in the credential graph (type: TSA/v1alpha1) for full trust verification.", "name", signature.Name)
	}
	logger.InfoContext(ctx, "verifying TSA timestamp", "name", signature.Name)

	tsaDER, hash, imprint, err := timestampTokenAndImprint(signature)
	if err != nil {
		return time.Time{}, false, err
	}

	verifiedTime, trusted, err := tsa.Verify(tsaDER, hash, imprint, tsaRootPool)
	if err != nil {
		return time.Time{}, false, fmt.Errorf("TSA timestamp verification failed: %w", err)
	}

	// A legacy token covers the descriptor digest, which exists before the
	// signature, so its time does not prove when the signature was made.
	if trusted && !trustLegacy && tsa.IsLegacyPEM([]byte(signature.Timestamp.Value)) {
		logger.WarnContext(ctx, "not using the time of a legacy OCM timestamp for certificate validation; it covers only the descriptor digest", "name", signature.Name, "flag", "--"+FlagInsecureTrustLegacyTimestamps)
		trusted = false
	}

	logger.InfoContext(ctx, "TSA timestamp verified", "name", signature.Name, "time", verifiedTime, "trusted", trusted)
	return verifiedTime, trusted, nil
}

// timestampTokenAndImprint returns the DER timestamp token of a signature and
// the imprint it must cover. Tokens from this CLI cover the signature value.
// Tokens from the legacy OCM CLI ("TIMESTAMP INFO") cover the descriptor digest;
// they are verified with that imprint so v1 data verifies as it did with v1.
func timestampTokenAndImprint(signature descruntime.Signature) ([]byte, crypto.Hash, []byte, error) {
	value := []byte(signature.Timestamp.Value)
	if tsa.IsLegacyPEM(value) {
		hash, err := signing.GetSupportedHash(signature.Digest.HashAlgorithm)
		if err != nil {
			return nil, 0, nil, fmt.Errorf("preparing TSA verification: %w", err)
		}
		der, err := tsa.FromLegacyPEM(value)
		if err != nil {
			return nil, 0, nil, fmt.Errorf("parsing legacy OCM timestamp failed: %w", err)
		}
		digest, err := hex.DecodeString(signature.Digest.Value)
		if err != nil {
			return nil, 0, nil, fmt.Errorf("decoding signature digest for legacy OCM timestamp failed: %w", err)
		}
		return der, hash, digest, nil
	}
	der, err := tsa.FromPEM(value)
	if err != nil {
		return nil, 0, nil, fmt.Errorf("parsing TSA timestamp PEM failed: %w", err)
	}
	hash, imprint, err := tsa.SignatureImprint(signature.Digest.HashAlgorithm, []byte(signature.Signature.Value))
	if err != nil {
		return nil, 0, nil, fmt.Errorf("preparing TSA verification: %w", err)
	}
	return der, hash, imprint, nil
}

// tsaRootPoolFromGraph resolves TSA root certificates from the credential graph.
// The identity derived from the TSA URL label is tried first; the generic
// TSA/v1alpha1 identity follows, so a consumer entry without URL attributes
// matches any TSA. URL matching requires equal hostnames, so a type-only entry
// never matches the URL-derived identity on its own. The label is not signed:
// it only selects among roots the verifier configured, and a token is trusted
// only when it chains to one of them.
func tsaRootPoolFromGraph(ctx context.Context, logger *slog.Logger, credentialGraph credentials.Resolver, tsaURL string) (*x509.CertPool, error) {
	candidates := []string{tsaURL}
	if tsaURL != "" {
		candidates = append(candidates, "")
	}
	for _, candidate := range candidates {
		tsaID, err := tsa.TSAConsumerIdentity(candidate)
		if err != nil {
			logger.WarnContext(ctx, "ignoring unparsable TSA URL for credential lookup", "error", err.Error())
			continue
		}
		tsaCreds, err := credentialGraph.Resolve(ctx, tsaID)
		if err != nil {
			if errors.Is(err, credentials.ErrNotFound) {
				logger.DebugContext(ctx, "no TSA credentials for identity", "identity", tsaID.String())
				continue
			}
			return nil, fmt.Errorf("resolving TSA credentials failed: %w", err)
		}
		pool, err := tsa.RootCertPoolFromCredentials(tsaCreds)
		if err != nil {
			return nil, fmt.Errorf("loading TSA root certificates from credential graph: %w", err)
		}
		if pool != nil {
			return pool, nil
		}
	}
	return nil, nil
}
