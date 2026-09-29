// Package artifactory uploads resources into repositories of a JFrog Artifactory server.
package artifactory

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
	"ocm.software/open-component-model/bindings/go/transfer/internal/repositoryupload"
	wgetaccess "ocm.software/open-component-model/bindings/go/wget/spec/access"
	wgetaccessv1 "ocm.software/open-component-model/bindings/go/wget/spec/access/v1"
)

const (
	Type    = "ArtifactoryUpload"
	Version = "v1alpha1"
)

// VersionedType is the versioned type identifier for ArtifactoryUpload transformations.
var VersionedType = runtime.NewVersionedType(Type, Version)

// Transformation uploads a resource into a repository of a JFrog Artifactory server and
// publishes it with an access on that repository, see [Transformer].
// +k8s:deepcopy-gen=true
// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +ocm:typegen=true
// +ocm:jsonschema-gen=true
type Transformation struct {
	// +ocm:jsonschema-gen:enum=ArtifactoryUpload/v1alpha1
	Type   runtime.Type             `json:"type"`
	ID     string                   `json:"id"`
	Spec   *repositoryupload.Spec   `json:"spec"`
	Output *repositoryupload.Output `json:"output,omitempty"`
}

// Transformer uploads a resource into a local repository of a JFrog Artifactory server.
// The package type of the repository decides what is uploaded and how the resource is
// published: a Helm chart with a Helm/v1 access (helm), or the resource content with a Wget/v1
// access (generic, maven, npm). See the ArtifactoryUploaderConfig transfer config.
type Transformer struct {
	repositoryupload.Uploader
}

func (t *Transformer) Transform(ctx context.Context, step runtime.Typed) (runtime.Typed, error) {
	var transformation Transformation
	if err := t.Scheme.Convert(step, &transformation); err != nil {
		return nil, fmt.Errorf("failed converting generic transformation to ArtifactoryUpload transformation: %w", err)
	}
	spec := transformation.Spec
	if err := repositoryupload.ValidateSpec(spec); err != nil {
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
	c, err := t.Target(ctx, helmRepo, repoURL)
	if err != nil {
		return nil, err
	}
	typ, err := repositoryType(ctx, c, spec)
	if err != nil {
		return nil, err
	}

	src := descriptor.ConvertFromV2Resource(spec.Resource)
	var out *descriptor.Resource
	switch typ {
	case "helm":
		var srv *server
		if srv, err = t.serverFor(spec, src, ".tgz"); err != nil {
			return nil, err
		}
		out, err = t.UploadHelm(ctx, c, spec, src, srv)
	case "generic", "maven":
		out, err = t.uploadFile(ctx, c, spec, src, "", nil)
	case "npm":
		out, err = t.uploadFile(ctx, c, spec, src, ".tgz", npmPackage)
	default:
		return nil, fmt.Errorf("artifactory repository %q has package type %q; supported: helm, generic, maven, npm", spec.Repository, typ)
	}
	if err != nil {
		return nil, err
	}
	if transformation.Output, err = t.Output(out); err != nil {
		return nil, err
	}
	return &transformation, nil
}

// serverFor returns the server storing the resource at its upload path with owner
// properties; ext is the extension of the default file name.
func (t *Transformer) serverFor(spec *repositoryupload.Spec, src *descriptor.Resource, ext string) (*server, error) {
	path, err := repositoryupload.UploadPath(spec, src, ext)
	if err != nil {
		return nil, err
	}
	return newServer(spec, path, ownerProperties(spec.ComponentVersion, src), t.Interval())
}

// repositoryType reads the package type of the
// repository from its configuration. Only local and federated repositories accept uploads.
func repositoryType(ctx context.Context, c *repositoryupload.Client, spec *repositoryupload.Spec) (string, error) {
	target, err := url.JoinPath(spec.URL, "artifactory", "api", "repositories", spec.Repository)
	if err != nil {
		return "", fmt.Errorf("invalid artifactory url: %w", err)
	}
	resp, err := c.Do(ctx, http.MethodGet, target, nil, -1, nil)
	if err != nil {
		return "", fmt.Errorf("failed detecting the type of artifactory repository %q: %w", spec.Repository, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("failed detecting the type of artifactory repository %q: GET %s returned status %d",
			spec.Repository, repositoryupload.RedactURL(target), resp.StatusCode)
	}
	var config struct {
		PackageType string `json:"packageType"`
		RClass      string `json:"rclass"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, repositoryupload.MaxErrorBodyBytes)).Decode(&config); err != nil {
		return "", fmt.Errorf("failed decoding the configuration of artifactory repository %q: %w", spec.Repository, err)
	}
	if rclass := strings.ToLower(config.RClass); rclass != "local" && rclass != "federated" {
		return "", fmt.Errorf("artifactory repository %q is a %s repository; uploads need a local repository", spec.Repository, config.RClass)
	}
	return strings.ToLower(config.PackageType), nil
}

// packageKind names the properties Artifactory records for a package it recognizes in
// a stored file.
type packageKind struct {
	kind, nameKey, versionKey string
}

// npmPackage is an npm package tarball; Artifactory reads its package.json.
var npmPackage = &packageKind{kind: "npm package", nameKey: "npm.name", versionKey: "npm.version"}

// uploadFile deploys the resource content as is and returns the resource with a Wget/v1 access
// on the stored file. Like charts, the file carries the owner properties and an existing file
// is only replaced when they name this resource. ext is the extension of the default file name.
// With pkg set, the content must be a package Artifactory recognizes: otherwise the stored file
// is removed again and the upload fails.
func (t *Transformer) uploadFile(ctx context.Context, c *repositoryupload.Client, spec *repositoryupload.Spec, src *descriptor.Resource, ext string, pkg *packageKind) (*descriptor.Resource, error) {
	srv, err := t.serverFor(spec, src, ext)
	if err != nil {
		return nil, err
	}
	req, err := t.Open(ctx, spec, src)
	if err != nil {
		return nil, err
	}
	content, err := t.Charts.OpenContent(ctx, req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = content.Close() }()

	expected, known, err := repositoryupload.KnownDigest(src.Digest, content.Blob, content.FromOCI)
	if err != nil {
		return nil, err
	}
	mediaType := repositoryupload.ContentType(content, spec.Resource)
	reused, err := srv.Claim(ctx, c, known)
	if err != nil {
		return nil, err
	}
	if !reused && known != "" {
		if reused, err = srv.Reuse(ctx, c, known); err != nil {
			return nil, err
		}
	}
	digestHex := known
	uploadURL := repositoryupload.RedactURL(srv.UploadURL())
	if reused {
		slog.InfoContext(ctx, "reused content already stored in the artifactory repository", "resource", src.ToIdentity(), "url", repositoryupload.RedactURL(srv.storedURL()))
	} else {
		header := http.Header{"Content-Type": {mediaType}}
		if known != "" {
			// Artifactory rejects the upload when the bytes do not match the checksum.
			header.Set("X-Checksum-Sha256", known)
		}
		computed, _, err := repositoryupload.UploadBlob(ctx, c, content.Blob, srv.DeployURL(), header, &srv.deployed)
		if err != nil {
			return nil, err
		}
		if known != "" && computed != known {
			if err := srv.Discard(ctx, c, computed); err != nil {
				slog.WarnContext(ctx, "failed removing uploaded content that must not be published", "url", uploadURL, "error", err)
			}
			return nil, fmt.Errorf("digest mismatch: expected %s, got %s", known, computed)
		}
		slog.InfoContext(ctx, "uploaded resource content", "server", srv.Name(), "resource", src.ToIdentity(), "url", repositoryupload.RedactURL(srv.storedURL()))
		digestHex = computed
	}

	if pkg != nil {
		name, version, found, err := srv.packageInfo(ctx, c, pkg.nameKey, pkg.versionKey)
		if err != nil {
			return nil, err
		}
		if !found {
			if err := srv.Discard(ctx, c, digestHex); err != nil {
				slog.WarnContext(ctx, "failed removing uploaded content that must not be published", "url", uploadURL, "error", err)
			}
			return nil, fmt.Errorf("content of resource %s is not an %s: artifactory recorded no %s and %s for %s",
				src.ToIdentity(), pkg.kind, pkg.nameKey, pkg.versionKey, repositoryupload.RedactURL(srv.storedURL()))
		}
		slog.InfoContext(ctx, "artifactory indexed the "+pkg.kind, "resource", src.ToIdentity(), "package", name+"@"+version)
	}

	out := src.DeepCopy()
	out.Access = &wgetaccessv1.Wget{
		Type:      wgetaccess.V1VersionedType,
		URL:       srv.storedURL(),
		MediaType: mediaType,
	}
	out.Digest = repositoryupload.UploadedDigest(src.Digest, expected, digestHex)
	return out, nil
}
