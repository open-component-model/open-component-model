// Package download contains the npm registry access used by the npm bindings:
// it resolves the version metadata of a package, downloads the tarball it points
// to, and verifies the tarball against the checksums the registry published.
package download

import (
	"context"
	//nolint:gosec // G505: SHA-1 only verifies the dist.shasum the registry published; not a security primitive.
	"crypto/sha1"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"hash"
	"io"
	"log/slog"
	"maps"
	"net/http"
	"net/url"
	"os"
	"slices"
	"strings"

	accessv1 "ocm.software/open-component-model/bindings/go/npm/spec/access/v1"
	credv1 "ocm.software/open-component-model/bindings/go/npm/spec/credentials/v1"
)

const (
	tempFilePattern = "ocm-npm-download-*"

	// MediaTypeTGZ is the media type of an npm package tarball, as in OCM v1.
	MediaTypeTGZ = "application/x-tgz"
)

// Download resolves the version metadata of the requested package, downloads
// its tarball and returns it as a blob backed by a file on disk. The tarball is
// streamed rather than buffered, so memory use stays flat regardless of package
// size; the file is created under [Options.TempDir], outlives this call and is
// owned by the returned blob. Close the blob to remove the file promptly; an
// abandoned blob also reclaims its file once it becomes unreachable.
//
// Published checksums are verified before the blob is returned. A mismatch fails
// the download and removes the file; missing checksums produce a warning.
//
// An explicitly file://-backed registry may read local metadata and file://
// tarballs. Paths are literal strings after the file:// prefix (including relative
// paths), not percent-decoded URLs. HTTP metadata cannot authorize local reads.
func Download(ctx context.Context, access *accessv1.NPM, creds *credv1.NPMCredentials, opts Options) (*Blob, error) {
	if err := access.Validate(); err != nil {
		return nil, redact(err)
	}

	if opts.MaxMetadataSize == 0 {
		opts.MaxMetadataSize = DefaultMaxMetadataSize
	}

	meta, err := resolveVersion(ctx, access, creds, opts)
	if err != nil {
		return nil, err
	}

	return downloadTarball(ctx, access, meta, creds, opts)
}

// downloadTarball streams dist.tarball into a temporary file, hashing it on the
// way, and fails if a published checksum does not match what was written.
func downloadTarball(ctx context.Context, access *accessv1.NPM, meta Version, creds *credv1.NPMCredentials, opts Options) (_ *Blob, err error) {
	registry, err := registryURL(access.Registry)
	if err != nil {
		return nil, err
	}

	// Resolving against the registry leaves an absolute URL untouched and turns a
	// relative one, which some registries publish, into the intended absolute URL.
	rawTarball := meta.Dist.Tarball
	var tarball *url.URL
	if strings.HasPrefix(rawTarball, "file://") {
		if !strings.HasPrefix(access.Registry, "file://") {
			return nil, fmt.Errorf("file tarballs require an explicitly file-backed registry")
		}
		tarball = &url.URL{Scheme: "file", Path: strings.TrimPrefix(rawTarball, "file://")}
	} else {
		tarball, err = registry.Parse(rawTarball)
		if err != nil {
			return nil, fmt.Errorf("invalid tarball url %q: %w", safeURLString(rawTarball), redact(err))
		}
		rawTarball = tarball.String()
	}
	if tarball.Scheme != "http" && tarball.Scheme != "https" && !strings.HasPrefix(meta.Dist.Tarball, "file://") {
		return nil, fmt.Errorf("unsupported tarball url scheme %q: only http and https are allowed", tarball.Scheme)
	}

	if meta.Deprecated != "" {
		slog.WarnContext(ctx, "npm registry reports this package version as deprecated",
			"package", access.Package, "version", access.Version, "message", meta.Deprecated)
	}

	check, err := verifierFor(ctx, meta.Dist)
	if err != nil {
		return nil, err
	}

	resp, err := getTarball(ctx, rawTarball, access.Registry, creds, opts)
	if err != nil {
		return nil, err
	}
	defer closeBody(ctx, resp)

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("tarball request to %s returned status %d", safeURL(tarball), resp.StatusCode)
	}

	maxDownloadSize := opts.MaxDownloadSize

	// When the server announces the size up front, an oversized tarball is
	// rejected before any of it is transferred. ContentLength is negative when unknown.
	if maxDownloadSize > 0 && resp.ContentLength > maxDownloadSize {
		return nil, fmt.Errorf("tarball from %s exceeds maximum allowed size of %d bytes", safeURL(tarball), maxDownloadSize)
	}

	body := io.Reader(resp.Body)
	if maxDownloadSize > 0 {
		body = io.LimitReader(resp.Body, maxDownloadSize+1)
	}

	file, err := os.CreateTemp(opts.TempDir, tempFilePattern)
	if err != nil {
		return nil, fmt.Errorf("error creating temporary file for %s: %w", safeURL(tarball), err)
	}
	path := file.Name()

	defer func() {
		if err != nil {
			_ = os.Remove(path)
		}
	}()

	// The hash is fed from the same stream that is written to disk, so the content
	// is verified exactly as it was stored.
	writers := []io.Writer{file}
	if check != nil {
		writers = append(writers, check.hash)
	}

	written, err := io.Copy(io.MultiWriter(writers...), body)
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return nil, fmt.Errorf("error writing tarball from %s to %s: %w", safeURL(tarball), path, redact(err))
	}

	if maxDownloadSize > 0 && written > maxDownloadSize {
		return nil, fmt.Errorf("tarball from %s exceeds maximum allowed size of %d bytes", safeURL(tarball), maxDownloadSize)
	}

	if check != nil {
		if got := hex.EncodeToString(check.hash.Sum(nil)); !slices.Contains(check.expected, got) {
			return nil, fmt.Errorf("%s mismatch for %s@%s: registry published %s, downloaded tarball has %s",
				check.name, access.Package, access.Version, strings.Join(check.expected, " or "), got)
		}
	} else {
		slog.WarnContext(ctx, "npm registry published no checksum for package, tarball content is unverified",
			"package", access.Package, "version", access.Version, "registry", safeURLString(access.Registry))
	}

	b, err := newBlob(path)
	if err != nil {
		return nil, fmt.Errorf("error creating blob for %s from %s: %w", safeURL(tarball), path, err)
	}
	b.SetMediaType(MediaTypeTGZ)

	return b, nil
}

// verifier is the checksum a tarball is verified against. Several digests can be
// acceptable for one algorithm, in which case any of them passes.
type verifier struct {
	name     string
	hash     hash.Hash
	expected []string
}

// integrityAlgorithms are the Subresource Integrity algorithms npm publishes,
// strongest first. The order decides which one a multi-entry dist.integrity is
// verified with.
var integrityAlgorithms = []struct {
	name string
	new  func() hash.Hash
}{
	{"sha512", sha512.New},
	{"sha384", sha512.New384},
	{"sha256", sha256.New},
	// sha1 is referenced as a constructor here, so the import comment covers it.
	{"sha1", sha1.New},
}

// verifierFor builds the checksum verifier for a dist section.
//
// dist.integrity is a Subresource Integrity string that may list digests in
// several algorithms. npm verifies the strongest algorithm present and accepts
// any digest given for it, so a stale weak digest published beside a strong one
// does not fail an install; this does the same. dist.shasum, the hex SHA-1 of
// older packages, is only used when dist.integrity is absent or empty.
//
// A dist.integrity that is present but unusable is an error rather than a skip:
// silently downloading unverified content is worse than failing.
func verifierFor(ctx context.Context, dist Dist) (*verifier, error) {
	digests := map[string][]string{}

	for _, entry := range strings.Fields(dist.Integrity) {
		algorithm, encoded, ok := strings.Cut(entry, "-")
		if !ok {
			return nil, fmt.Errorf("malformed dist.integrity entry %q: expected <algorithm>-<base64>", entry)
		}
		// An entry may carry "?"-separated metadata options, which are not part of
		// the digest.
		encoded, _, _ = strings.Cut(encoded, "?")

		raw, err := base64.StdEncoding.DecodeString(encoded)
		if err != nil {
			return nil, fmt.Errorf("cannot decode dist.integrity entry %q: %w", entry, err)
		}

		algorithm = strings.ToLower(algorithm)
		digests[algorithm] = append(digests[algorithm], hex.EncodeToString(raw))
	}

	for _, algorithm := range integrityAlgorithms {
		if expected := digests[algorithm.name]; len(expected) > 0 {
			return &verifier{
				name:     "dist.integrity (" + algorithm.name + ")",
				hash:     algorithm.new(),
				expected: expected,
			}, nil
		}
	}

	if len(digests) > 0 {
		algorithms := slices.Sorted(maps.Keys(digests))
		return nil, fmt.Errorf("dist.integrity carries no known algorithm, only %s", strings.Join(algorithms, ", "))
	}

	if dist.Shasum != "" {
		// A SHA-1 is 40 hex characters; anything else is not the digest this field
		// is defined to carry, and comparing it would report a content mismatch.
		if len(dist.Shasum) != sha1.Size*2 {
			return nil, fmt.Errorf("dist.shasum %q is not a 40-character SHA-1", dist.Shasum)
		}
		if _, err := hex.DecodeString(dist.Shasum); err != nil {
			return nil, fmt.Errorf("cannot decode dist.shasum %q: %w", dist.Shasum, err)
		}
		return &verifier{
			name:     "dist.shasum (sha1)",
			hash:     sha1.New(), //nolint:gosec // G401: checksum published by the registry, see the import comment
			expected: []string{strings.ToLower(dist.Shasum)},
		}, nil
	}

	return nil, nil
}

// registryURL parses and validates the registry base URL.
func registryURL(registry string) (*url.URL, error) {
	if strings.HasPrefix(registry, "file://") {
		return &url.URL{Scheme: "file", Path: strings.TrimPrefix(registry, "file://")}, nil
	}
	parsed, err := url.Parse(registry)
	if err != nil {
		return nil, fmt.Errorf("invalid registry url %q: %w", safeURLString(registry), redact(err))
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, fmt.Errorf("unsupported registry url scheme %q: only http and https are allowed", parsed.Scheme)
	}
	if parsed.Host == "" {
		return nil, fmt.Errorf("registry url %q has no host", safeURLString(registry))
	}
	return parsed, nil
}

// safeURL strips userinfo and query params so presigned URLs and credentials are
// never leaked into error messages or logs.
func safeURL(u *url.URL) string {
	safe := *u
	safe.User = nil
	safe.RawQuery = ""
	safe.Fragment = ""
	return safe.String()
}

func closeBody(ctx context.Context, resp *http.Response) {
	if err := resp.Body.Close(); err != nil {
		slog.WarnContext(ctx, "failed to close HTTP response body", "error", redact(err))
	}
}
