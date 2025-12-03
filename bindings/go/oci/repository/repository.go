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
func NewFromCTFRepoV1(ctx context.Context, repository *ctfrepospecv1.Repository, options ...oci.RepositoryOption) (*oci.Repository, error) {
	store, err := NewStoreFromCTFRepoV1(ctx, repository, options...)
	if err != nil {
		return nil, err
	}

	repo, err := oci.NewRepository(append(options, ocictf.WithCTF(store))...)
	if err != nil {
		return nil, fmt.Errorf("unable to create new repository: %w", err)
	}
	return repo, nil
}

func NewStoreFromCTFRepoV1(ctx context.Context, repository *ctfrepospecv1.Repository, options ...oci.RepositoryOption) (*ocictf.Store, error) {
	path := repository.FilePath
	if path == "" {
		return nil, fmt.Errorf("a path is required")
	}

	path = filepath.Clean(path)
	mask := repository.AccessMode.ToAccessBitmask()

	format := ctf.DiscoverCTFFormatFromPath(path)
	if mask&ctf.O_RDWR != 0 && (format == ctf.FormatTAR || format == ctf.FormatTGZ) {
		return nil, fmt.Errorf("readwrite access is not supported for archive formats such as %s", format.String())
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
		return nil, fmt.Errorf("unable to open ctf archive %q: %w", path, err)
	}

	return ocictf.NewFromCTF(archive), nil
}

// NewFromOCIRepoV1 creates a new [*oci.Repository] instance from an OCI repository v1 specification.
//
// # Path Handling vs Old OCM
//
// Old OCM path handling:
//   - ParseRef separates host from path: Host="ghcr.io", Repository="org/repo/artifact"
//     https://github.com/open-component-model/ocm/blob/2b819e6/api/oci/ref.go#L72
//   - MapReference builds BaseURL from Host+Scheme only, ignoring Repository path
//     https://github.com/open-component-model/ocm/blob/2b819e6/api/oci/extensions/repositories/ocireg/uniform.go#L16
//   - getInfo() re-parses BaseURL inconsistently: with scheme: extracts host only, without scheme: may embed path if manually created,
//     https://github.com/open-component-model/ocm/blob/2b819e6/api/oci/extensions/repositories/ocireg/type.go#L104
//   - Validate() uses HostInfo() to extract host, discarding any path
//     https://github.com/open-component-model/ocm/blob/2b819e6/api/oci/extensions/repositories/ocireg/type.go#L138
//
// New OCM: Explicit BaseUrl + SubPath fields, consistent parsing, auto-extraction support
func NewFromOCIRepoV1(_ context.Context, repository *ocirepospecv1.Repository, client remote.Client, options ...oci.RepositoryOption) (*oci.Repository, error) {
	if repository.BaseUrl == "" {
		return nil, fmt.Errorf("a base url is required")
	}

	resolver, err := buildResolver(repository.BaseUrl, repository.SubPath, client)
	if err != nil {
		return nil, err
	}

	return oci.NewRepository(append(options, oci.WithResolver(resolver))...)
}

// BuildResolver creates a URL resolver from a base URL and optional subPath.
// It handles URLs with or without schemes and extracts subPath from the URL path
// if not explicitly provided.
func buildResolver(baseUrl, subPath string, client remote.Client) (*urlresolver.CachingResolver, error) {
	purl, err := runtime.ParseURLAndAllowNoScheme(baseUrl)
	if err != nil {
		return nil, fmt.Errorf("could not parse OCI repository URL %q: %w", baseUrl, err)
	}

	// Extract SubPath from BaseUrl if not explicitly set
	if subPath == "" && purl.Path != "" && purl.Path != "/" {
		subPath = strings.TrimPrefix(purl.Path, "/")
	}

	var opts []urlresolver.Option
	opts = append(opts, urlresolver.WithBaseURL(purl.Host))
	if purl.Scheme == "http" {
		opts = append(opts, urlresolver.WithPlainHTTP(true))
	}

	if subPath != "" {
		opts = append(opts, urlresolver.WithSubPath(subPath))
	}

	if client != nil {
		opts = append(opts, urlresolver.WithBaseClient(client))
	}

	resolver, err := urlresolver.New(opts...)
	if err != nil {
		return nil, fmt.Errorf("could not create URL resolver for OCI repository %q: %w", baseUrl, err)
	}
	return resolver, nil
}
