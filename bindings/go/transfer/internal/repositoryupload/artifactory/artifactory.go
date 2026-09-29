// Package artifactory uploads resources into repositories of a JFrog Artifactory server.
package artifactory

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/bindings/go/transfer/internal/repositoryupload"
	uploadv1alpha1 "ocm.software/open-component-model/bindings/go/transfer/transformation/spec/v1alpha1"
)

// Transformer uploads a resource into a local repository of a JFrog Artifactory server.
// The package type of the repository decides what is uploaded and how the resource is
// published: a Helm chart with a Helm/v1 access (helm), or the resource content with a Wget/v1
// access (generic, maven, npm). It runs [uploadv1alpha1.ArtifactoryUpload] transformations.
type Transformer struct {
	repositoryupload.Uploader
}

func (t *Transformer) Transform(ctx context.Context, step runtime.Typed) (runtime.Typed, error) {
	var tr uploadv1alpha1.ArtifactoryUpload
	if err := t.Scheme.Convert(step, &tr); err != nil {
		return nil, fmt.Errorf("failed converting generic transformation to ArtifactoryUpload transformation: %w", err)
	}
	out, err := t.Upload(ctx, tr.Spec, backend{})
	if err != nil {
		return nil, err
	}
	tr.Output = out
	return &tr, nil
}

// backend stores resources in Artifactory, see [server].
type backend struct{}

func (backend) Name() string { return "artifactory" }

func (backend) CredentialURLs(spec *uploadv1alpha1.RepositoryUploadSpec) (string, string, error) {
	helmRepo, err := url.JoinPath(spec.URL, "artifactory", "api", "helm", spec.Repository)
	if err != nil {
		return "", "", fmt.Errorf("invalid artifactory url: %w", err)
	}
	repoURL, err := url.JoinPath(spec.URL, "artifactory", spec.Repository)
	if err != nil {
		return "", "", fmt.Errorf("invalid artifactory url: %w", err)
	}
	return helmRepo, repoURL, nil
}

// Store stores the resource at its upload path with owner properties. Helm and npm files get
// the .tgz extension in the default file name.
func (backend) Store(ctx context.Context, c *repositoryupload.Client, spec *uploadv1alpha1.RepositoryUploadSpec, src *descriptor.Resource, interval time.Duration) (repositoryupload.Store, error) {
	typ, err := repositoryType(ctx, c, spec)
	if err != nil {
		return nil, err
	}
	var ext string
	switch typ {
	case "helm", "npm":
		ext = ".tgz"
	case "generic", "maven":
	default:
		return nil, fmt.Errorf("artifactory repository %q has package type %q; supported: helm, generic, maven, npm", spec.Repository, typ)
	}
	path, err := repositoryupload.UploadPath(spec, src, ext)
	if err != nil {
		return nil, err
	}
	return newServer(spec, typ, path, ownerProperties(spec.ComponentVersion, src), interval)
}

// repositoryType reads the package type of the repository from its configuration. Only local
// and federated repositories accept uploads.
func repositoryType(ctx context.Context, c *repositoryupload.Client, spec *uploadv1alpha1.RepositoryUploadSpec) (string, error) {
	target, err := url.JoinPath(spec.URL, "artifactory", "api", "repositories", spec.Repository)
	if err != nil {
		return "", fmt.Errorf("invalid artifactory url: %w", err)
	}
	var config struct {
		PackageType string `json:"packageType"`
		RClass      string `json:"rclass"`
	}
	if err := c.Send(ctx, http.MethodGet, target, nil, -1, nil, &config); err != nil {
		return "", fmt.Errorf("failed detecting the type of artifactory repository %q: %w", spec.Repository, err)
	}
	if rclass := strings.ToLower(config.RClass); rclass != "local" && rclass != "federated" {
		return "", fmt.Errorf("artifactory repository %q is a %s repository; uploads need a local repository", spec.Repository, config.RClass)
	}
	return strings.ToLower(config.PackageType), nil
}
