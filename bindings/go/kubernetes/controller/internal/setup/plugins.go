package setup

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	filesystemv1alpha1 "ocm.software/open-component-model/bindings/go/configuration/filesystem/v1alpha1/spec"
	genericv1 "ocm.software/open-component-model/bindings/go/configuration/generic/v1/spec"
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
)

// creator controller user-agent.
const creator = "ocm.software/open-component-model/bindings/go/kubernetes/controller"

// PluginOptions since TempDir is dependent on the Pod and mounted temp folder
// we set it separately from the ocm config. Also, filesystem config is not allowed.
type PluginOptions struct {
	// TempDir used to write temporary files into.
	TempDir string
}

// PluginOption configures [NewPluginManager].
type PluginOption func(*PluginOptions)

// WithTempDir sets the root for ephemeral files written by plugins.
func WithTempDir(dir string) PluginOption {
	return func(o *PluginOptions) {
		o.TempDir = dir
	}
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

	repositoryProvider := provider.NewComponentVersionRepositoryProvider(
		provider.WithScheme(ocirepository.Scheme),
		provider.WithUserAgent(creator),
		provider.WithTempDir(options.TempDir),
		provider.WithHTTPConfig(httpCfg),
		provider.WithBlobCacheOptions(&ocicache.Options{RemotePolicy: ocicache.RemotePolicyAlways}),
		provider.WithReferenceCacheOptions(&ocicache.Options{RemotePolicy: ocicache.RemotePolicyAlways}),
	)

	signingHandler, err := handler.New(signingv1alpha1.Scheme, true)
	if err != nil {
		return nil, fmt.Errorf("failed to create signing handler: %w", err)
	}

	ociResourceRepoPlugin := ocires.NewResourceRepository(
		fsCfg,
		ocires.WithUserAgent(creator),
		ocires.WithHTTPConfig(httpCfg),
	)

	helmDigestProcessor := helmdigest.NewDigestProcessor(options.TempDir)

	if err := errors.Join(
		pm.ComponentVersionRepositoryRegistry.RegisterInternalComponentVersionRepositoryPlugin(repositoryProvider),
		pm.SigningRegistry.RegisterInternalComponentSignatureHandler(signingHandler),
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
		pm.CredentialTypeRegistry.RegisterInternalCredentialTypeSchemeProvider(ociResourceRepoPlugin),
		pm.CredentialTypeRegistry.RegisterInternalCredentialTypeSchemeProvider(helmDigestProcessor),
	); err != nil {
		return nil, fmt.Errorf("failed to register credential types: %w", err)
	}

	return pm, nil
}
