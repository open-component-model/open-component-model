package internal

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/runtime"
	wgetaccess "ocm.software/open-component-model/bindings/go/wget/spec/access"
	wgetaccessv1 "ocm.software/open-component-model/bindings/go/wget/spec/access/v1"
)

// NexusUpload uploads a resource into a hosted repository of a Sonatype Nexus Repository 3
// server. The format of the repository decides what is uploaded and how the resource is
// published: a Helm chart with a Helm/v1 access (helm), or the resource content with a Wget/v1
// access (raw). See the NexusUploaderConfig transfer config.
type NexusUpload struct {
	repositoryUploader
}

func (t *NexusUpload) Transform(ctx context.Context, step runtime.Typed) (runtime.Typed, error) {
	var transformation NexusUploadTransformation
	if err := t.Scheme.Convert(step, &transformation); err != nil {
		return nil, fmt.Errorf("failed converting generic transformation to NexusUpload transformation: %w", err)
	}
	spec := transformation.Spec
	if err := validateSpec(spec); err != nil {
		return nil, fmt.Errorf("invalid NexusUpload transformation: %w", err)
	}
	repoURL, err := url.JoinPath(spec.URL, "repository", spec.Repository)
	if err != nil {
		return nil, fmt.Errorf("invalid nexus url: %w", err)
	}
	c, err := t.target(ctx, repoURL, repoURL)
	if err != nil {
		return nil, err
	}
	typ, err := nexusRepositoryType(ctx, c, spec)
	if err != nil {
		return nil, err
	}

	src := descriptor.ConvertFromV2Resource(spec.Resource)
	var out *descriptor.Resource
	switch typ {
	case "helm":
		if spec.Path != "" {
			return nil, fmt.Errorf("path is not supported for nexus helm repositories: nexus stores charts under <name>-<version>.tgz")
		}
		file, err := resourceFile(src, ".tgz")
		if err != nil {
			return nil, err
		}
		srv, err := newNexusServer(spec, file, t.interval())
		if err != nil {
			return nil, err
		}
		if out, err = t.uploadHelm(ctx, c, spec, src, srv); err != nil {
			return nil, err
		}
	case "raw":
		if out, err = t.uploadRaw(ctx, c, spec, src, repoURL); err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("nexus repository %q has format %q; supported: helm, raw", spec.Repository, typ)
	}
	if transformation.Output, err = t.output(out); err != nil {
		return nil, err
	}
	return &transformation, nil
}

// nexusRepositoryType reads the format of the repository from
// its settings. Only hosted repositories accept uploads.
func nexusRepositoryType(ctx context.Context, c *repositoryClient, spec *RepositoryUploadSpec) (string, error) {
	target, err := url.JoinPath(spec.URL, "service", "rest", "v1", "repositories", spec.Repository)
	if err != nil {
		return "", fmt.Errorf("invalid nexus url: %w", err)
	}
	resp, err := c.do(ctx, http.MethodGet, target, nil, -1, nil)
	if err != nil {
		return "", fmt.Errorf("failed detecting the type of nexus repository %q: %w", spec.Repository, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("failed detecting the type of nexus repository %q: GET %s returned status %d",
			spec.Repository, redactURL(target), resp.StatusCode)
	}
	var settings struct {
		Format string `json:"format"`
		Type   string `json:"type"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&settings); err != nil {
		return "", fmt.Errorf("failed decoding the settings of nexus repository %q: %w", spec.Repository, err)
	}
	if settings.Type != "hosted" {
		return "", fmt.Errorf("nexus repository %q is a %s repository; uploads need a hosted repository", spec.Repository, settings.Type)
	}
	return settings.Format, nil
}

// uploadRaw uploads the resource content as is into a raw repository and returns the resource
// with a Wget/v1 access on the uploaded file. Nexus records no owner of a file, so a file
// already stored at the path is never overwritten: it is reused when it has the content,
// otherwise the upload fails.
func (t *NexusUpload) uploadRaw(ctx context.Context, c *repositoryClient, spec *RepositoryUploadSpec, src *descriptor.Resource, repoURL string) (*descriptor.Resource, error) {
	path, err := uploadPath(spec, src, "")
	if err != nil {
		return nil, err
	}
	target := repoURL + "/" + path
	req, err := t.open(ctx, spec, src)
	if err != nil {
		return nil, err
	}
	content, err := t.Charts.OpenContent(ctx, req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = content.Close() }()

	expected, known, err := knownDigest(src.Digest, content.Blob, content.FromOCI)
	if err != nil {
		return nil, err
	}
	mediaType := contentType(content, spec.Resource)
	safe := redactURL(target)
	stored, err := nexusRawStored(ctx, c, spec, path, target, known)
	if err != nil {
		return nil, err
	}
	digestHex := known
	if stored {
		slog.InfoContext(ctx, "reused content already stored in the nexus repository", "resource", src.ToIdentity(), "url", safe)
	} else {
		computed, _, err := uploadBlob(ctx, c, content.Blob, target, http.Header{"Content-Type": {mediaType}}, nil)
		if err != nil {
			return nil, err
		}
		if known != "" && computed != known {
			return nil, fmt.Errorf("digest mismatch: expected %s, got %s (nexus keeps the uploaded file at %s)", known, computed, safe)
		}
		slog.InfoContext(ctx, "uploaded resource content", "server", "nexus", "resource", src.ToIdentity(), "url", safe)
		digestHex = computed
	}

	out := src.DeepCopy()
	out.Access = &wgetaccessv1.Wget{
		Type:      wgetaccess.V1VersionedType,
		URL:       target,
		MediaType: mediaType,
	}
	out.Digest = uploadedDigest(src.Digest, expected, digestHex)
	return out, nil
}

// nexusRawStored reports whether target already holds content with sha256Hex. It fails when
// target holds other content, or content whose digest is unknown up front.
func nexusRawStored(ctx context.Context, c *repositoryClient, spec *RepositoryUploadSpec, path, target, sha256Hex string) (bool, error) {
	resp, err := c.do(ctx, http.MethodHead, target, nil, -1, nil)
	if err != nil {
		return false, err
	}
	_ = resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusNotFound:
		return false, nil
	case http.StatusOK:
	default:
		return false, fmt.Errorf("HEAD %s returned status %d", redactURL(target), resp.StatusCode)
	}
	if sha256Hex != "" {
		checksums, err := nexusAssetChecksums(ctx, c, spec, path)
		if err != nil {
			return false, err
		}
		for _, checksum := range checksums {
			if checksum == sha256Hex {
				return true, nil
			}
		}
	}
	return false, fmt.Errorf("nexus repository %q already stores a different file at %s; the uploader never overwrites files in raw repositories, configure a different path",
		spec.Repository, redactURL(target))
}

// nexusAssetChecksums returns the SHA-256 of the assets Nexus's search finds at path.
func nexusAssetChecksums(ctx context.Context, c *repositoryClient, spec *RepositoryUploadSpec, path string) ([]string, error) {
	base, err := url.JoinPath(spec.URL, "service", "rest", "v1", "search", "assets")
	if err != nil {
		return nil, fmt.Errorf("invalid nexus url: %w", err)
	}
	name, err := url.PathUnescape(path)
	if err != nil {
		return nil, fmt.Errorf("invalid path %q: %w", path, err)
	}
	// Nexus names raw assets by their path with a leading slash (e.g. /a/b/c.txt); path never
	// has one.
	target := base + "?" + url.Values{"repository": {spec.Repository}, "name": {"/" + name}}.Encode()
	resp, err := c.do(ctx, http.MethodGet, target, nil, -1, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s returned status %d", redactURL(target), resp.StatusCode)
	}
	var page struct {
		Items []struct {
			Checksum struct {
				SHA256 string `json:"sha256"`
			} `json:"checksum"`
		} `json:"items"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&page); err != nil {
		return nil, fmt.Errorf("failed decoding nexus search result of %s: %w", redactURL(target), err)
	}
	checksums := make([]string, 0, len(page.Items))
	for _, item := range page.Items {
		checksums = append(checksums, item.Checksum.SHA256)
	}
	return checksums, nil
}
