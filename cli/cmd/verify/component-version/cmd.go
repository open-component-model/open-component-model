package componentversion

import (
	"bytes"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
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
	"ocm.software/open-component-model/bindings/go/oci/compref"
	ctfv1 "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/ctf"
	ociv1 "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/oci"
	"ocm.software/open-component-model/bindings/go/rsa/signing/v1alpha1"
	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/bindings/go/signing"
	"ocm.software/open-component-model/bindings/go/signing/tsa"
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

# Verify a specific signature
verify component-version ghcr.io/open-component-model/ocm//ocm.software/ocmcli:0.23.0 --signature my-signature

# Use a verifier specification file
verify component-version ghcr.io/open-component-model/ocm//ocm.software/ocmcli:0.23.0 --verifier-spec ./rsassa-pss.yaml

## Example Credential Config (TSA timestamp verification)
#
# If a signature includes an RFC 3161 timestamp, the verifier checks it automatically.
# To enable full PKCS#7 chain verification of the timestamp token, supply the TSA's
# root CA certificate via the credential graph with a TSA/v1alpha1 identity.
# Without root certificates, only structural validity is checked.
#
# The TSA URL stored in the signed descriptor is used as a hint for credential
# lookup, enabling URL-specific matching.

    type: generic.config.ocm.software/v1
    configurations:
    - type: credentials.config.ocm.software
      consumers:
      - identity:
          type: TSA/v1alpha1
          hostname: timestamp.digicert.com
          scheme: https
        credentials:
        - type: Credentials/v1
          properties:
            root_certs_pem_file: /path/to/digicert-tsa-root.pem
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

			// Verify TSA timestamp if present
			if signature.Timestamp != nil {
				// Look up TSA root certs from the credential graph.
				// If the descriptor contains a TSA URL label for this signature,
				// use it for a URL-specific identity lookup. Otherwise, fall back
				// to a generic TSA/v1alpha1 identity.
				var tsaURL string
				labelName := tsa.TSAURLLabelPrefix + signature.Name
				for _, lbl := range desc.Component.Labels {
					if lbl.Name == labelName {
						_ = json.Unmarshal(lbl.Value, &tsaURL)
						break
					}
				}

				var tsaRootPool *x509.CertPool
				if tsaID, err := tsa.TSAConsumerIdentity(tsaURL); err == nil {
					if tsaCreds, err := credentialGraph.Resolve(egctx, tsaID); err == nil {
						if pool, err := tsa.RootCertPoolFromCredentials(tsaCreds); err == nil && pool != nil {
							tsaRootPool = pool
							logger.DebugContext(egctx, "TSA root certificates resolved from credential graph", "name", signature.Name, "tsaURL", tsaURL)
						} else if err != nil {
							return fmt.Errorf("loading TSA root certificates from credential graph: %w", err)
						}
					}
				}

				if tsaRootPool == nil {
					logger.WarnContext(egctx, "verifying TSA timestamp without root certificates; only structural validity is checked. Configure TSA root certs in the credential graph (type: TSA/v1alpha1) for full trust verification.", "name", signature.Name)
				}
				logger.InfoContext(egctx, "verifying TSA timestamp", "name", signature.Name)

				tsaDER, err := tsa.FromPEM([]byte(signature.Timestamp.Value))
				if err != nil {
					return fmt.Errorf("parsing TSA timestamp PEM failed: %w", err)
				}

				hash, err := signing.GetSupportedHash(signature.Digest.HashAlgorithm)
				if err != nil {
					return fmt.Errorf("preparing TSA verification: %w", err)
				}
				digestBytes, err := hex.DecodeString(signature.Digest.Value)
				if err != nil {
					return fmt.Errorf("decoding digest for TSA verification: %w", err)
				}

				verifiedTime, err := tsa.Verify(tsaDER, hash, digestBytes, tsaRootPool)
				if err != nil {
					return fmt.Errorf("TSA timestamp verification failed: %w", err)
				}

				logger.InfoContext(egctx, "TSA timestamp verified", "name", signature.Name, "time", verifiedTime)

				// Pass verified TSA time to handler so it can use it for cert validity checks
				if creds == nil {
					creds = make(map[string]string)
				}
				creds[tsa.VerifiedTimeKey] = verifiedTime.Format(time.RFC3339)
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
