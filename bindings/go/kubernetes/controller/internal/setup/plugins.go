package setup

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	filesystemv1alpha1 "ocm.software/open-component-model/bindings/go/configuration/filesystem/v1alpha1/spec"
	genericv1 "ocm.software/open-component-model/bindings/go/configuration/generic/v1/spec"
	gitrepo "ocm.software/open-component-model/bindings/go/git/repository"
	ghdigest "ocm.software/open-component-model/bindings/go/github/digest"
	ghresource "ocm.software/open-component-model/bindings/go/github/repository/resource"
	gpghandler "ocm.software/open-component-model/bindings/go/gpg/signing/handler"
	helmdigest "ocm.software/open-component-model/bindings/go/helm/digest"
	ocmhttp "ocm.software/open-component-model/bindings/go/http"
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
	s3repo "ocm.software/open-component-model/bindings/go/s3/repository"
	sigstorehandler "ocm.software/open-component-model/bindings/go/sigstore/signing/handler"
	wgetrepo "ocm.software/open-component-model/bindings/go/wget/repository"
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

	gpgSigningHandler, err := gpghandler.New(nil, gpghandler.WithTempDir(options.TempDir))
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

	s3DigestProcessor := s3repo.NewResourceRepository(fsCfg, s3repo.WithHTTPConfig(httpCfg))
	wgetDigestProcessor := wgetrepo.NewResourceRepository(fsCfg, wgetrepo.WithHTTPClient(ocmhttp.New(ocmhttp.WithConfig(httpCfg))))
	gitDigestProcessor := gitrepo.NewResourceRepository(fsCfg, gitrepo.WithHTTPClient(ocmhttp.New(ocmhttp.WithConfig(httpCfg))))
	ghDigestProcessor := ghdigest.NewDigestProcessor(ghresource.WithHTTPConfig(httpCfg))

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
		pm.DigestProcessorRegistry.RegisterInternalDigestProcessorPlugin(s3DigestProcessor),
		pm.DigestProcessorRegistry.RegisterInternalDigestProcessorPlugin(wgetDigestProcessor),
		pm.DigestProcessorRegistry.RegisterInternalDigestProcessorPlugin(gitDigestProcessor),
		pm.DigestProcessorRegistry.RegisterInternalDigestProcessorPlugin(ghDigestProcessor),
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
		pm.CredentialTypeRegistry.RegisterInternalCredentialTypeSchemeProvider(s3DigestProcessor),
		pm.CredentialTypeRegistry.RegisterInternalCredentialTypeSchemeProvider(wgetDigestProcessor),
		pm.CredentialTypeRegistry.RegisterInternalCredentialTypeSchemeProvider(gitDigestProcessor),
		pm.CredentialTypeRegistry.RegisterInternalCredentialTypeSchemeProvider(ghDigestProcessor),
	); err != nil {
		return nil, fmt.Errorf("failed to register credential types: %w", err)
	}

	return pm, nil
}
