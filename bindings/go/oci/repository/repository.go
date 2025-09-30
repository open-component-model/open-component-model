package repository

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"oras.land/oras-go/v2/registry/remote"

	"ocm.software/open-component-model/bindings/go/ctf"
	"ocm.software/open-component-model/bindings/go/oci"
	ocictf "ocm.software/open-component-model/bindings/go/oci/ctf"
	urlresolver "ocm.software/open-component-model/bindings/go/oci/resolver/url"
	ctfrepospecv1 "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/ctf"
	ocirepospecv1 "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/oci"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// NewFromCTFRepoV1 creates a new [*oci.Repository] instance from a CTF repository v1 specification.
// It opens the CTF archive specified in the repository path and returns the instance.
// Based on the underlying format, write operations may be limited. (e.g. for archived ctfs, editing the CTF may
// work on an extracted filesystem version.
// The path is cleaned to ensure it is a valid file path.
// The access mode is converted to a bitmask for use with the CTF archive.
func NewFromCTFRepoV1(ctx context.Context, repository *ctfrepospecv1.Repository, options ...oci.RepositoryOption) (*oci.Repository, *ocictf.Store, error) {
	path := repository.Path
	if path == "" {
		return nil, nil, fmt.Errorf("a path is required")
	}

	path = filepath.Clean(path)
	mask := repository.AccessMode.ToAccessBitmask()

	format := ctf.DiscoverCTFFormatFromPath(path)
	if mask&ctf.O_RDWR != 0 && (format == ctf.FormatTAR || format == ctf.FormatTGZ) {
		return nil, nil, fmt.Errorf("readwrite access is not supported for archive formats such as %s", format.String())
	}

	repoOpts := &oci.RepositoryOptions{}
	for _, opt := range options {
		opt(repoOpts)
	}

	ctfOpts := ctf.OpenCTFOptions{
		Path:    path,
		Flag:    mask,
		TempDir: repoOpts.TempDir,
	}

	archive, _, err := ctf.OpenCTFByFileExtension(ctx, ctfOpts)
	if err != nil {
		return nil, nil, fmt.Errorf("unable to open ctf archive %q: %w", path, err)
	}

	store := ocictf.NewFromCTF(archive)

	repo, err := oci.NewRepository(append(options, ocictf.WithCTF(store))...)
	if err != nil {
		return nil, nil, fmt.Errorf("unable to create new repository: %w", err)
	}
	return repo, store, nil
}

// NewFromOCIRepoV1 creates a new [*oci.Repository] instance from an OCI repository v1 specification.
// It configures the repository with the provided base URL and client, and sets up the appropriate
// resolver for handling OCI registry operations.
func NewFromOCIRepoV1(_ context.Context, repository *ocirepospecv1.Repository, client remote.Client, options ...oci.RepositoryOption) (*oci.Repository, error) {
	if repository.BaseUrl == "" {
		return nil, fmt.Errorf("a base url is required")
	}

	purl, err := runtime.ParseURLAndAllowNoScheme(repository.BaseUrl)
	if err != nil {
		return nil, fmt.Errorf("could not parse OCI repository URL %q: %w", repository.BaseUrl, err)
	}

	var opts []urlresolver.Option
	if purl.Scheme != "" {
		opts = append(opts, urlresolver.WithBaseURL(strings.TrimPrefix(purl.String(), purl.Scheme+"://")))
		if purl.Scheme == "http" {
			opts = append(opts, urlresolver.WithPlainHTTP(true))
		}
	} else {
		opts = append(opts, urlresolver.WithBaseURL(repository.BaseUrl))
	}

	opts = append(opts, urlresolver.WithBaseClient(client))

	resolver, err := urlresolver.New(opts...)
	if err != nil {
		return nil, fmt.Errorf("could not create URL resolver for OCI repository %q: %w", repository.BaseUrl, err)
	}

	return oci.NewRepository(append(options, oci.WithResolver(resolver))...)
}
