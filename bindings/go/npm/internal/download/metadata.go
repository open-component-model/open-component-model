package download

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"path"
	"strings"
)

// Dist is the distribution section of an npm version document. It carries the
// tarball location and the checksums the registry published with it.
type Dist struct {
	// Integrity is a Subresource Integrity string, e.g. "sha512-<base64>".
	Integrity string `json:"integrity"`
	// Shasum is the hex-encoded SHA-1 of the tarball, present on older packages.
	Shasum string `json:"shasum"`
	// Tarball is the absolute URL of the package tarball.
	Tarball string `json:"tarball"`
}

// Version is an npm version document.
type Version struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	Dist    Dist   `json:"dist"`
	// Deprecated carries the message npm shows for a deprecated version.
	Deprecated string `json:"deprecated"`
}

// document covers both registry replies in one shape: a version document has
// dist set, a packument has versions set. Decoding into one struct means the
// body is parsed once, whichever of the two the registry returned.
type document struct {
	Version
	Versions map[string]Version `json:"versions"`
}

// version returns the requested version out of the document, whether the
// registry answered with a version document or with a full packument.
//
// The version endpoint resolves dist-tags, so "<registry>/<pkg>/latest" answers
// with whatever latest points at. The checksums come out of that same document,
// so a tarball for the wrong version would verify perfectly; the answer is only
// accepted when it describes the version that was asked for.
func (d *document) version(want string) (Version, bool) {
	if d.Dist.Tarball != "" {
		return d.Version, d.Version.Version == want
	}
	v, ok := d.Versions[want]
	return v, ok && v.Dist.Tarball != ""
}

// packageURL is "<registry>/<package>", the packument of a package.
func packageURL(registry, pkg string) string {
	// The scope of a package name stays literal, as in OCM v1: registries serve
	// "@scope/name" unescaped.
	return strings.TrimSuffix(registry, "/") + path.Join("/", pkg)
}

// packageVersionURL is "<registry>/<package>/<version>", the version document.
func packageVersionURL(registry, pkg, version string) string {
	return strings.TrimSuffix(registry, "/") + path.Join("/", pkg, version)
}

// resolveVersion looks up the version document of a package.
//
// It asks for "<registry>/<package>/<version>" first and falls back to the full
// packument at "<registry>/<package>". The packument is the only path the npm
// client itself uses and the only one Nexus serves: Nexus answers the version
// URL with 404 for a plain name and with 400 for a scoped one
// (https://github.com/sonatype/nexus-public/issues/224). Any client error on the
// version URL therefore falls back rather than failing, except one saying the
// caller is not allowed in, which the packument would answer the same way.
func resolveVersion(ctx context.Context, req Request, o *option) (Version, error) {
	versionURL := packageVersionURL(req.Registry, req.Package, req.Version)

	doc, status, err := getDocument(ctx, versionURL, o)
	switch {
	case errors.Is(err, errMetadataTooLarge):
		return Version{}, err
	case err != nil:
		// An undecodable body is not fatal: a registry behind an auth proxy answers
		// 200 with an HTML login page, and the packument is still worth a try.
		slog.DebugContext(ctx, "version metadata unusable, falling back to the packument", "url", versionURL, "err", err)
	case status == http.StatusOK:
		if v, ok := doc.version(req.Version); ok {
			return v, nil
		}
	case status == http.StatusUnauthorized || status == http.StatusForbidden:
		return Version{}, fmt.Errorf("version metadata request to %s returned status %d", versionURL, status)
	case status < 400 || status >= 500:
		return Version{}, fmt.Errorf("version metadata request to %s returned status %d", versionURL, status)
	}

	packumentURL := packageURL(req.Registry, req.Package)

	doc, status, err = getDocument(ctx, packumentURL, o)
	if err != nil {
		return Version{}, err
	}
	if status != http.StatusOK {
		return Version{}, fmt.Errorf("package metadata request to %s returned status %d", packumentURL, status)
	}

	v, ok := doc.version(req.Version)
	if !ok {
		return Version{}, fmt.Errorf("version %q of package %q not found in registry %s", req.Version, req.Package, req.Registry)
	}

	return v, nil
}

// getDocument performs a GET and decodes the body as a registry document.
// It returns the status code so the caller can decide about a fallback. A
// syntactically valid body that carries no dist.tarball decodes into an empty
// document, which the caller treats like a missing version.
func getDocument(ctx context.Context, url string, o *option) (document, int, error) {
	resp, err := get(ctx, url, o)
	if err != nil {
		return document{}, 0, err
	}
	defer closeBody(ctx, resp)

	if resp.StatusCode != http.StatusOK {
		return document{}, resp.StatusCode, nil
	}

	body := io.Reader(resp.Body)
	if limit := o.maxMetadataSize(); limit > 0 {
		if resp.ContentLength > limit {
			return document{}, resp.StatusCode, fmt.Errorf("%w: %s is %d bytes, limit is %d", errMetadataTooLarge, url, resp.ContentLength, limit)
		}
		body = &limitedReader{r: io.LimitReader(resp.Body, limit+1), limit: limit, url: url}
	}

	var doc document
	if err := json.NewDecoder(body).Decode(&doc); err != nil {
		return document{}, resp.StatusCode, fmt.Errorf("cannot decode metadata document at %s: %w", url, err)
	}

	return doc, resp.StatusCode, nil
}

// errMetadataTooLarge marks a metadata document over the configured limit. It is
// fatal rather than a reason to fall back, because the packument the fallback
// asks for is the larger of the two documents.
var errMetadataTooLarge = errors.New("metadata document exceeds the maximum allowed size")

// limitedReader turns the extra byte read past the limit into an error, so an
// oversized document is reported as such instead of as a JSON syntax error.
type limitedReader struct {
	r     io.Reader
	limit int64
	read  int64
	url   string
}

func (l *limitedReader) Read(p []byte) (int, error) {
	n, err := l.r.Read(p)
	l.read += int64(n)
	if l.read > l.limit {
		return n, fmt.Errorf("%w: %s, limit is %d bytes", errMetadataTooLarge, l.url, l.limit)
	}
	return n, err
}
