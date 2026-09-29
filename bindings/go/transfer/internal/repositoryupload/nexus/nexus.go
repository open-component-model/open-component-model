// Package nexus uploads resources into hosted repositories of a Sonatype Nexus Repository 3 server.
// Each repository format has its own store package under internal/.
package nexus

import (
	"context"
	"fmt"
	"net/url"
	"time"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/bindings/go/transfer/internal/repositoryupload"
	"ocm.software/open-component-model/bindings/go/transfer/internal/repositoryupload/client"
	"ocm.software/open-component-model/bindings/go/transfer/internal/repositoryupload/nexus/internal/api"
	"ocm.software/open-component-model/bindings/go/transfer/internal/repositoryupload/nexus/internal/helm"
	"ocm.software/open-component-model/bindings/go/transfer/internal/repositoryupload/nexus/internal/maven"
	"ocm.software/open-component-model/bindings/go/transfer/internal/repositoryupload/nexus/internal/npm"
	"ocm.software/open-component-model/bindings/go/transfer/internal/repositoryupload/nexus/internal/raw"
	"ocm.software/open-component-model/bindings/go/transfer/internal/repositoryupload/uploadpath"
	uploadv1alpha1 "ocm.software/open-component-model/bindings/go/transfer/transformation/spec/v1alpha1"
)

// Transformer uploads a resource into a hosted repository of a Sonatype Nexus Repository 3
// server. The format of the repository decides what is uploaded and how the resource is
// published: a Helm chart with a Helm/v1 access (helm), or the resource content with a Wget/v1
// access (raw, maven2, npm). It runs [uploadv1alpha1.NexusUpload] transformations.
type Transformer struct {
	repositoryupload.Uploader
}

func (t *Transformer) Transform(ctx context.Context, step runtime.Typed) (runtime.Typed, error) {
	var tr uploadv1alpha1.NexusUpload
	if err := t.Scheme.Convert(step, &tr); err != nil {
		return nil, fmt.Errorf("failed converting generic transformation to NexusUpload transformation: %w", err)
	}
	out, err := t.Upload(ctx, tr.Spec, backend{})
	if err != nil {
		return nil, err
	}
	tr.Output = out
	return &tr, nil
}

// backend picks the store of the repository format: [helm.Store], [raw.Store], [maven.Store] or
// [npm.Store].
type backend struct{}

func (backend) Name() string { return "nexus" }

func (backend) CredentialURLs(spec *uploadv1alpha1.RepositoryUploadSpec) (string, string, error) {
	repoURL, err := url.JoinPath(spec.URL, "repository", spec.Repository)
	if err != nil {
		return "", "", fmt.Errorf("invalid nexus url: %w", err)
	}
	return repoURL, repoURL, nil
}

// Store reads the settings of the repository; only hosted repositories accept uploads.
func (backend) Store(ctx context.Context, c *client.Client, spec *uploadv1alpha1.RepositoryUploadSpec, src *descriptor.Resource, interval time.Duration) (repositoryupload.Store, error) {
	repo, err := api.New(c, spec.URL, spec.Repository)
	if err != nil {
		return nil, err
	}
	settings, err := repo.Settings(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed detecting the type of nexus repository %q: %w", spec.Repository, err)
	}
	if settings.Type != "hosted" {
		return nil, fmt.Errorf("nexus repository %q is a %s repository; uploads need a hosted repository", spec.Repository, settings.Type)
	}
	switch settings.Format {
	case "helm":
		if spec.Path != "" {
			return nil, fmt.Errorf("path is not supported for nexus helm repositories: nexus stores charts under <name>-<version>.tgz")
		}
		file, err := uploadpath.File(src, ".tgz")
		if err != nil {
			return nil, err
		}
		return helm.New(repo, file, interval), nil
	case "raw":
		path, err := uploadpath.Resolve(spec, src, "")
		if err != nil {
			return nil, err
		}
		return raw.New(repo, path, interval), nil
	case "maven2":
		store, err := maven.New(repo, spec.Path, interval)
		if err != nil {
			return nil, err
		}
		return store, nil
	case "npm":
		if spec.Path != "" {
			return nil, fmt.Errorf("path is not supported for nexus npm repositories: nexus stores packages under <name>/-/<name>-<version>.tgz")
		}
		file, err := uploadpath.File(src, ".tgz")
		if err != nil {
			return nil, err
		}
		return npm.New(repo, file, interval), nil
	default:
		return nil, fmt.Errorf("nexus repository %q has format %q; supported: helm, raw, maven2, npm", spec.Repository, settings.Format)
	}
}
