// Package artifactory uploads resources into repositories of a JFrog Artifactory server.
// Each package type has its own store package under internal/.
package artifactory

import (
	"context"
	"fmt"
	"strings"
	"time"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/bindings/go/transfer/internal/repositoryupload"
	"ocm.software/open-component-model/bindings/go/transfer/internal/repositoryupload/artifactory/internal/api"
	"ocm.software/open-component-model/bindings/go/transfer/internal/repositoryupload/artifactory/internal/generic"
	"ocm.software/open-component-model/bindings/go/transfer/internal/repositoryupload/artifactory/internal/helm"
	"ocm.software/open-component-model/bindings/go/transfer/internal/repositoryupload/artifactory/internal/npm"
	"ocm.software/open-component-model/bindings/go/transfer/internal/repositoryupload/client"
	"ocm.software/open-component-model/bindings/go/transfer/internal/repositoryupload/uploadpath"
	uploadv1alpha1 "ocm.software/open-component-model/bindings/go/transfer/transformation/spec/v1alpha1"
)

// Transformer uploads a resource into a local repository of a JFrog Artifactory server.
// The package type of the repository decides what is uploaded and how the resource is
// published: a Helm chart with a Helm/v1 access (helm), or the resource content with a Wget/v1
// access (generic, maven, npm). It runs [uploadv1alpha1.ArtifactoryUpload] transformations.
type Transformer struct {
	Uploader *repositoryupload.Uploader
}

func (t *Transformer) Transform(ctx context.Context, step runtime.Typed) (runtime.Typed, error) {
	var tr uploadv1alpha1.ArtifactoryUpload
	if err := t.Uploader.Scheme.Convert(step, &tr); err != nil {
		return nil, fmt.Errorf("failed converting generic transformation to ArtifactoryUpload transformation: %w", err)
	}
	out, err := t.Uploader.Upload(ctx, tr.Spec, backend{})
	if err != nil {
		return nil, err
	}
	tr.Output = out
	return &tr, nil
}

// backend picks the store of the package type: [helm.Store], [npm.Store] or [generic.Store].
type backend struct{}

func (backend) Name() string { return "artifactory" }

func (backend) CredentialURLs(spec *uploadv1alpha1.RepositoryUploadSpec) (string, string, error) {
	repoURL, helmURL, err := api.URLs(spec.URL, spec.Repository)
	return helmURL, repoURL, err
}

// Store reads the configuration of the repository; only local and federated repositories accept
// uploads. The resource is stored at its upload path with owner properties; helm and npm files
// get the .tgz extension in the default file name.
func (backend) Store(ctx context.Context, c *client.Client, spec *uploadv1alpha1.RepositoryUploadSpec, src *descriptor.Resource, interval time.Duration) (repositoryupload.Store, error) {
	repo, err := api.New(c, spec.URL, spec.Repository)
	if err != nil {
		return nil, err
	}
	config, err := repo.Configuration(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed detecting the type of artifactory repository %q: %w", spec.Repository, err)
	}
	if rclass := strings.ToLower(config.RClass); rclass != "local" && rclass != "federated" {
		return nil, fmt.Errorf("artifactory repository %q is a %s repository; uploads need a local repository", spec.Repository, config.RClass)
	}
	packageType := strings.ToLower(config.PackageType)
	var ext string
	switch packageType {
	case "helm", "npm":
		ext = ".tgz"
	case "generic", "maven":
	default:
		return nil, fmt.Errorf("artifactory repository %q has package type %q; supported: helm, generic, maven, npm", spec.Repository, packageType)
	}
	path, err := uploadpath.Resolve(spec, src, ext)
	if err != nil {
		return nil, err
	}
	file := repo.File(path, api.Owner(spec.ComponentVersion, src), interval)
	switch packageType {
	case "helm":
		return helm.New(file, repo.HelmURL), nil
	case "npm":
		return npm.New(file), nil
	default:
		return generic.New(file), nil
	}
}
