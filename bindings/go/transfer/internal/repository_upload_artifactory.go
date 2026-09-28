package internal

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/runtime"
	wgetaccess "ocm.software/open-component-model/bindings/go/wget/spec/access"
	wgetaccessv1 "ocm.software/open-component-model/bindings/go/wget/spec/access/v1"
)

// ArtifactoryUpload uploads a resource into a local repository of a JFrog Artifactory server.
// The package type of the repository decides what is uploaded and how the resource is
// published: a Helm chart with a Helm/v1 access (helm), or the resource content with a Wget/v1
// access (any other package type). See the ArtifactoryUploaderConfig transfer config.
type ArtifactoryUpload struct {
	repositoryUploader
}

func (t *ArtifactoryUpload) Transform(ctx context.Context, step runtime.Typed) (runtime.Typed, error) {
	var transformation ArtifactoryUploadTransformation
	if err := t.Scheme.Convert(step, &transformation); err != nil {
		return nil, fmt.Errorf("failed converting generic transformation to ArtifactoryUpload transformation: %w", err)
	}
	spec := transformation.Spec
	if err := validateSpec(spec); err != nil {
		return nil, fmt.Errorf("invalid ArtifactoryUpload transformation: %w", err)
	}
	helmRepo, err := url.JoinPath(spec.URL, "artifactory", "api", "helm", spec.Repository)
	if err != nil {
		return nil, fmt.Errorf("invalid artifactory url: %w", err)
	}
	repoURL, err := url.JoinPath(spec.URL, "artifactory", spec.Repository)
	if err != nil {
		return nil, fmt.Errorf("invalid artifactory url: %w", err)
	}
	c, err := t.target(ctx, helmRepo, repoURL)
	if err != nil {
		return nil, err
	}
	typ, err := artifactoryRepositoryType(ctx, c, spec)
	if err != nil {
		return nil, err
	}

	src := descriptor.ConvertFromV2Resource(spec.Resource)
	var out *descriptor.Resource
	if typ == "helm" {
		var srv *artifactoryServer
		if srv, err = t.artifactoryServer(spec, src, ".tgz"); err != nil {
			return nil, err
		}
		out, err = t.uploadHelm(ctx, c, spec, src, srv)
	} else {
		out, err = t.uploadFile(ctx, c, spec, src)
	}
	if err != nil {
		return nil, err
	}
	if transformation.Output, err = t.output(out); err != nil {
		return nil, err
	}
	return &transformation, nil
}

// artifactoryServer returns the server storing the resource at its upload path with owner
// properties; ext is the extension of the default file name.
func (t *ArtifactoryUpload) artifactoryServer(spec *RepositoryUploadSpec, src *descriptor.Resource, ext string) (*artifactoryServer, error) {
	path, err := uploadPath(spec, src, ext)
	if err != nil {
		return nil, err
	}
	return newArtifactoryServer(spec, path, chartOwner(spec.ComponentVersion, src), t.interval())
}

// artifactoryRepositoryType reads the package type of the
// repository from its configuration. Only local and federated repositories accept uploads.
func artifactoryRepositoryType(ctx context.Context, c *repositoryClient, spec *RepositoryUploadSpec) (string, error) {
	target, err := url.JoinPath(spec.URL, "artifactory", "api", "repositories", spec.Repository)
	if err != nil {
		return "", fmt.Errorf("invalid artifactory url: %w", err)
	}
	resp, err := c.do(ctx, http.MethodGet, target, nil, -1, nil)
	if err != nil {
		return "", fmt.Errorf("failed detecting the type of artifactory repository %q: %w", spec.Repository, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("failed detecting the type of artifactory repository %q: GET %s returned status %d",
			spec.Repository, redactURL(target), resp.StatusCode)
	}
	var config struct {
		PackageType string `json:"packageType"`
		RClass      string `json:"rclass"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxErrorBodyBytes)).Decode(&config); err != nil {
		return "", fmt.Errorf("failed decoding the configuration of artifactory repository %q: %w", spec.Repository, err)
	}
	if rclass := strings.ToLower(config.RClass); rclass != "local" && rclass != "federated" {
		return "", fmt.Errorf("artifactory repository %q is a %s repository; uploads need a local repository", spec.Repository, config.RClass)
	}
	return strings.ToLower(config.PackageType), nil
}

// uploadFile deploys the resource content as is into a repository of any package type other
// than helm and returns the resource with a Wget/v1 access on the stored file. Artifactory
// deploys files the same way into every local repository; the package type only decides how it
// indexes them. Like charts, the file carries the owner properties and an existing file is only
// replaced when they name this resource.
func (t *ArtifactoryUpload) uploadFile(ctx context.Context, c *repositoryClient, spec *RepositoryUploadSpec, src *descriptor.Resource) (*descriptor.Resource, error) {
	srv, err := t.artifactoryServer(spec, src, "")
	if err != nil {
		return nil, err
	}
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
	reused, err := srv.claim(ctx, c, known)
	if err != nil {
		return nil, err
	}
	if !reused && known != "" {
		if reused, err = srv.reuse(ctx, c, known); err != nil {
			return nil, err
		}
	}
	digestHex := known
	uploadURL := redactURL(srv.uploadURL())
	if reused {
		slog.InfoContext(ctx, "reused content already stored in the artifactory repository", "resource", src.ToIdentity(), "url", redactURL(srv.storedURL()))
	} else {
		header := http.Header{"Content-Type": {mediaType}}
		if known != "" {
			// Artifactory rejects the upload when the bytes do not match the checksum.
			header.Set("X-Checksum-Sha256", known)
		}
		computed, _, err := uploadBlob(ctx, c, content.Blob, srv.deployURL(), header, &srv.deployed)
		if err != nil {
			return nil, err
		}
		if known != "" && computed != known {
			if err := srv.discard(ctx, c, computed); err != nil {
				slog.WarnContext(ctx, "failed removing uploaded content that must not be published", "url", uploadURL, "error", err)
			}
			return nil, fmt.Errorf("digest mismatch: expected %s, got %s", known, computed)
		}
		slog.InfoContext(ctx, "uploaded resource content", "server", srv.name(), "resource", src.ToIdentity(), "url", redactURL(srv.storedURL()))
		digestHex = computed
	}

	out := src.DeepCopy()
	out.Access = &wgetaccessv1.Wget{
		Type:      wgetaccess.V1VersionedType,
		URL:       srv.storedURL(),
		MediaType: mediaType,
	}
	out.Digest = uploadedDigest(src.Digest, expected, digestHex)
	return out, nil
}
