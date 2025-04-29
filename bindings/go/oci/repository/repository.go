package repository

import (
	"context"
	"fmt"
	"net/url"
	"path/filepath"

	"oras.land/oras-go/v2/registry/remote"

	"ocm.software/open-component-model/bindings/go/ctf"
	"ocm.software/open-component-model/bindings/go/oci"
	ocictf "ocm.software/open-component-model/bindings/go/oci/ctf"
	urlresolver "ocm.software/open-component-model/bindings/go/oci/resolver/url"
	ctfrepospecv1 "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/ctf"
	ocirepospecv1 "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/oci"
)

// NewFromCTFRepoV1 creates a new [*oci.Repository] instance from a CTF repository v1 specification.
// It opens the CTF archive specified in the repository path and returns the instance.
// Based on the underlying format, write operations may be limited. (e.g. for archived ctfs, editing the CTF may
// work on a extracted filesystem version.
// The path is cleaned to ensure it is a valid file path.
// The access mode is converted to a bitmask for use with the CTF archive.
func NewFromCTFRepoV1(ctx context.Context, repository *ctfrepospecv1.Repository, options ...oci.RepositoryOption) (*oci.Repository, error) {
	path := repository.Path
	if path == "" {
		return nil, fmt.Errorf("a path is required")
	}

	path = filepath.Clean(path)
	mask := repository.AccessMode.ToAccessBitmask()
	archive, _, err := ctf.OpenCTFByFileExtension(ctx, path, mask)
	if err != nil {
		return nil, fmt.Errorf("unable to open ctf archive %q: %w", path, err)
	}
	store := ocictf.NewFromCTF(archive)

	return oci.NewRepository(append(options, ocictf.WithCTF(store))...)
}

// NewFromOCIRepoV1 creates a new [*oci.Repository] instance from an OCI repository v1 specification.
// It configures the repository with the provided base URL and client, and sets up the appropriate
// resolver for handling OCI registry operations.
func NewFromOCIRepoV1(_ context.Context, repository *ocirepospecv1.Repository, client remote.Client, options ...oci.RepositoryOption) (*oci.Repository, error) {
	if repository.BaseUrl == "" {
		return nil, fmt.Errorf("a base url is required")
	}

	resolver := urlresolver.New(repository.BaseUrl)

	if purl, err := url.Parse(repository.BaseUrl); err == nil && purl.Scheme == "http" {
		resolver.PlainHTTP = true
	}

	resolver.BaseClient = client

	return oci.NewRepository(append(options, oci.WithResolver(resolver))...)
}
