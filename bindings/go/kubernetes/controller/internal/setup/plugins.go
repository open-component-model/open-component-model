package setup

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	filesystemv1alpha1 "ocm.software/open-component-model/bindings/go/configuration/filesystem/v1alpha1/spec"
	genericv1 "ocm.software/open-component-model/bindings/go/configuration/generic/v1/spec"
	gpghandler "ocm.software/open-component-model/bindings/go/gpg/signing/handler"
	helmdigest "ocm.software/open-component-model/bindings/go/helm/digest"
	httpv1alpha1 "ocm.software/open-component-model/bindings/go/http/spec/config/v1alpha1"
	ocicache "ocm.software/open-component-model/bindings/go/oci/cache"
	ocicredentials "ocm.software/open-component-model/bindings/go/oci/credentials"
	"ocm.software/open-component-model/bindings/go/oci/repository/provider"
	ocires "ocm.software/open-component-model/bindings/go/oci/repository/resource"
	ociidentityv1 "ocm.software/open-component-model/bindings/go/oci/spec/identity/v1"
	ocirepository "ocm.software/open-component-model/bindings/go/oci/spec/repository"
	"ocm.software/open-component-model/bindings/go/oci/transformer"
	"ocm.software/open-component-model/bindings/go/plugin/manager"
	"ocm.software/open-component-model/bindings/go/rsa/signing/handler"
	signingv1alpha1 "ocm.software/open-component-model/bindings/go/rsa/signing/v1alpha1"
	ocmruntime "ocm.software/open-component-model/bindings/go/runtime"
	sigstorehandler "ocm.software/open-component-model/bindings/go/sigstore/signing/handler"
)

// creator controller user-agent.
const creator = "ocm.software/open-component-model/bindings/go/kubernetes/controller"

// blobCacheMaxEntries sizes the one blob cache all reconciles share; a component version takes about three entries.
const blobCacheMaxEntries = 10_000

// PluginOptions since TempDir is dependent on the Pod and mounted temp folder
// we set it separately from the ocm config. Also, filesystem config is not allowed.
type PluginOptions struct {
	// TempDir used to write temporary files into.
	TempDir string
	// BlobCache is the OCI blob cache shared by every plugin manager of the process.
	BlobCache *ocicache.BlobCache
}

// PluginOption configures [NewPluginManager].
type PluginOption func(*PluginOptions)

// WithTempDir sets the root for ephemeral files written by plugins.
func WithTempDir(dir string) PluginOption {
	return func(o *PluginOptions) {
		o.TempDir = dir
	}
}

// WithBlobCache shares the OCI blob cache between plugin managers. Without it, OCI caching is disabled,
// because the cache owns its directory and per-request plugin managers must not each build their own.
func WithBlobCache(bc *ocicache.BlobCache) PluginOption {
	return func(o *PluginOptions) {
		o.BlobCache = bc
	}
}

// NewBlobCache creates the blob cache to pass to [WithBlobCache]. There is no reference cache:
// under RemotePolicyAlways it never serves a hit, so it would only cost disk writes.
func NewBlobCache(tempDir string) (*ocicache.BlobCache, error) {
	if tempDir == "" {
		tempDir = os.TempDir()
	}
	return ocicache.NewBlobCache(ocicache.Options{
		Dir:          filepath.Join(tempDir, "ocm-oci-cas"),
		RemotePolicy: ocicache.RemotePolicyAlways,
		MaxEntries:   blobCacheMaxEntries,
	})
}

// NewPluginManager build a per-request plugin manager.
func NewPluginManager(ctx context.Context, cfg *genericv1.Config, logger *slog.Logger, opts ...PluginOption) (*manager.PluginManager, error) {
	options := &PluginOptions{}
	for _, opt := range opts {
		opt(options)
	}

	httpCfg, err := httpv1alpha1.LookupConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to look up http configuration: %w", err)
	}

	fsCfg := &filesystemv1alpha1.Config{}
	if options.TempDir != "" {
		fsCfg.TempFolder = &options.TempDir
	}

	pm := manager.NewPluginManager(ctx)

	providerOpts := []provider.Option{
		provider.WithScheme(ocirepository.Scheme),
		provider.WithUserAgent(creator),
		provider.WithTempDir(options.TempDir),
		provider.WithHTTPConfig(httpCfg),
	}
	if options.BlobCache != nil {
		providerOpts = append(providerOpts, provider.WithBlobCache(options.BlobCache))
	}
	repositoryProvider := provider.NewComponentVersionRepositoryProvider(providerOpts...)

	signingHandler, err := handler.New(signingv1alpha1.Scheme, true)
	if err != nil {
		return nil, fmt.Errorf("failed to create signing handler: %w", err)
	}

	gpgSigningHandler, err := gpghandler.New(nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create GPG signing handler: %w", err)
	}

	sigstoreSigningHandler := sigstorehandler.New(sigstorehandler.WithTempDir(options.TempDir))

	ociResourceRepoPlugin := ocires.NewResourceRepository(
		fsCfg,
		ocires.WithUserAgent(creator),
		ocires.WithHTTPConfig(httpCfg),
	)

	helmDigestProcessor := helmdigest.NewDigestProcessor(options.TempDir)

	if err := errors.Join(
		pm.ComponentVersionRepositoryRegistry.RegisterInternalComponentVersionRepositoryPlugin(repositoryProvider),
		pm.SigningRegistry.RegisterInternalComponentSignatureHandler(signingHandler),
		pm.SigningRegistry.RegisterInternalComponentSignatureHandler(gpgSigningHandler),
		pm.SigningRegistry.RegisterInternalComponentSignatureHandler(sigstoreSigningHandler),
		pm.CredentialRepositoryRegistry.RegisterInternalCredentialRepositoryPlugin(
			&ocicredentials.OCICredentialRepository{},
			[]ocmruntime.Type{ociidentityv1.Type},
		),
		pm.ResourcePluginRegistry.RegisterInternalResourcePlugin(ociResourceRepoPlugin),
		pm.DigestProcessorRegistry.RegisterInternalDigestProcessorPlugin(ociResourceRepoPlugin),
		pm.DigestProcessorRegistry.RegisterInternalDigestProcessorPlugin(helmDigestProcessor),
		pm.BlobTransformerRegistry.RegisterInternalBlobTransformerPlugin(transformer.New(logger)),
	); err != nil {
		return nil, fmt.Errorf("failed to register internal plugins: %w", err)
	}

	// Each internal plugin declares the credential types it consumes.
	if err := errors.Join(
		pm.CredentialTypeRegistry.RegisterInternalCredentialTypeSchemeProvider(signingHandler),
		pm.CredentialTypeRegistry.RegisterInternalCredentialTypeSchemeProvider(gpgSigningHandler),
		pm.CredentialTypeRegistry.RegisterInternalCredentialTypeSchemeProvider(sigstoreSigningHandler),
		pm.CredentialTypeRegistry.RegisterInternalCredentialTypeSchemeProvider(ociResourceRepoPlugin),
		pm.CredentialTypeRegistry.RegisterInternalCredentialTypeSchemeProvider(helmDigestProcessor),
	); err != nil {
		return nil, fmt.Errorf("failed to register credential types: %w", err)
	}

	return pm, nil
}
