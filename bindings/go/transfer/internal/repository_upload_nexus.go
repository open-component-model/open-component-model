package internal

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"slices"
	"strings"
	"time"

	"ocm.software/open-component-model/bindings/go/blob"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/runtime"
	wgetaccess "ocm.software/open-component-model/bindings/go/wget/spec/access"
	wgetaccessv1 "ocm.software/open-component-model/bindings/go/wget/spec/access/v1"
)

// NexusUpload uploads a resource into a hosted repository of a Sonatype Nexus Repository 3
// server. The format of the repository decides what is uploaded and how the resource is
// published: a Helm chart with a Helm/v1 access (helm), or the resource content with a Wget/v1
// access (raw, maven2). See the NexusUploaderConfig transfer config.
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
		path, err := uploadPath(spec, src, "")
		if err != nil {
			return nil, err
		}
		if out, err = t.uploadFile(ctx, c, spec, src, repoURL, path, nexusRaw); err != nil {
			return nil, err
		}
	case "maven2":
		path, err := mavenPath(spec)
		if err != nil {
			return nil, err
		}
		if out, err = t.uploadFile(ctx, c, spec, src, repoURL, path, nexusMaven); err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("nexus repository %q has format %q; supported: helm, raw, maven2", spec.Repository, typ)
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

// nexusFileFormat is how a repository format stores a file and reports what it stores.
type nexusFileFormat struct {
	name string
	// checksums returns the SHA-256 Nexus reports for the file stored at path; empty when it
	// reports none yet.
	checksums func(ctx context.Context, c *repositoryClient, spec *RepositoryUploadSpec, path, target string) ([]string, error)
	// put stores content at path and returns the hex SHA-256 of the bytes sent.
	put func(ctx context.Context, c *repositoryClient, spec *RepositoryUploadSpec, content blob.ReadOnlyBlob, path, target, mediaType string) (string, error)
}

// nexusRaw stores files with a plain PUT and finds them by the asset search.
var nexusRaw = nexusFileFormat{
	name: "raw",
	checksums: func(ctx context.Context, c *repositoryClient, spec *RepositoryUploadSpec, path, _ string) ([]string, error) {
		return nexusAssetChecksums(ctx, c, spec, path, nil)
	},
	put: func(ctx context.Context, c *repositoryClient, _ *RepositoryUploadSpec, content blob.ReadOnlyBlob, _, target, mediaType string) (string, error) {
		computed, _, err := uploadBlob(ctx, c, content, target, http.Header{"Content-Type": {mediaType}}, nil)
		return computed, err
	},
}

// nexusMaven stores files through the components API, which, unlike a plain PUT, updates
// maven-metadata.xml, so version ranges and latest/release see the upload. The asset search
// finds maven assets by their coordinates, not by name.
var nexusMaven = nexusFileFormat{
	name: "maven2",
	checksums: func(ctx context.Context, c *repositoryClient, spec *RepositoryUploadSpec, path, _ string) ([]string, error) {
		coords, err := parseMavenPath(spec.Path)
		if err != nil {
			return nil, err
		}
		query := url.Values{
			"maven.groupId":     {coords.groupID},
			"maven.artifactId":  {coords.artifactID},
			"maven.baseVersion": {coords.version},
			"maven.extension":   {coords.extension},
		}
		if coords.classifier != "" {
			query.Set("maven.classifier", coords.classifier)
		}
		return nexusAssetChecksums(ctx, c, spec, path, query)
	},
	put: nexusMavenUpload,
}

// uploadFile uploads the resource content as is to path and returns the resource with a Wget/v1
// access on the stored file. Nexus records no owner of a file, so a file already stored at the
// path is never overwritten: it is reused when it has the content, otherwise the upload fails.
func (t *NexusUpload) uploadFile(ctx context.Context, c *repositoryClient, spec *RepositoryUploadSpec, src *descriptor.Resource, repoURL, path string, format nexusFileFormat) (*descriptor.Resource, error) {
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
	stored, err := nexusFileStored(ctx, c, spec, format, path, target, known, t.interval())
	if err != nil {
		return nil, err
	}
	digestHex := known
	if stored {
		slog.InfoContext(ctx, "reused content already stored in the nexus repository", "resource", src.ToIdentity(), "url", safe)
	} else {
		computed, err := format.put(ctx, c, spec, content.Blob, path, target, mediaType)
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

// nexusFileStored reports whether target already holds content with sha256Hex. It fails when
// target holds other content, or content whose digest is unknown up front. Nexus reports the
// checksum of a stored file shortly after storing it (raw assets are indexed for search), so a
// stored file without a reported checksum yet is polled like chart metadata.
func nexusFileStored(ctx context.Context, c *repositoryClient, spec *RepositoryUploadSpec, format nexusFileFormat, path, target, sha256Hex string, interval time.Duration) (bool, error) {
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
	for attempt := 1; sha256Hex != ""; attempt++ {
		checksums, err := format.checksums(ctx, c, spec, path, target)
		if err != nil {
			return false, err
		}
		if slices.Contains(checksums, sha256Hex) {
			return true, nil
		}
		if len(checksums) > 0 || attempt == chartMetadataAttempts {
			break
		}
		select {
		case <-ctx.Done():
			return false, ctx.Err()
		case <-time.After(interval):
		}
	}
	return false, fmt.Errorf("nexus repository %q already stores a different file at %s; the uploader never overwrites files in %s repositories, configure a different path",
		spec.Repository, redactURL(target), format.name)
}

// nexusAssetChecksums returns the SHA-256 of the assets Nexus's search finds at path. query
// selects the assets by format-specific attributes; without it, raw assets are selected by name.
func nexusAssetChecksums(ctx context.Context, c *repositoryClient, spec *RepositoryUploadSpec, path string, query url.Values) ([]string, error) {
	base, err := url.JoinPath(spec.URL, "service", "rest", "v1", "search", "assets")
	if err != nil {
		return nil, fmt.Errorf("invalid nexus url: %w", err)
	}
	name, err := url.PathUnescape(path)
	if err != nil {
		return nil, fmt.Errorf("invalid path %q: %w", path, err)
	}
	// Nexus reports asset paths with a leading slash (e.g. /a/b/c.txt); path never has one.
	assetPath := "/" + name
	if query == nil {
		query = url.Values{"name": {assetPath}}
	}
	query.Set("repository", spec.Repository)
	target := base + "?" + query.Encode()
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
			Path     string `json:"path"`
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
		if item.Path == assetPath {
			checksums = append(checksums, item.Checksum.SHA256)
		}
	}
	return checksums, nil
}

// mavenCoordinates are the Maven coordinates of a single file.
type mavenCoordinates struct {
	groupID, artifactID, version, classifier, extension string
}

// mavenPath validates the configured path as a Maven repository layout path,
// <group path>/<artifactId>/<version>/<artifactId>-<version>[-<classifier>].<extension>, and
// returns it path-escaped. Nexus maven2 repositories store files only at such paths.
func mavenPath(spec *RepositoryUploadSpec) (string, error) {
	if spec.Path == "" {
		return "", fmt.Errorf("nexus maven2 repositories need a path in the Maven repository layout, e.g. " +
			`${"com/example/" + resource.name + "/" + resource.version + "/" + resource.name + "-" + resource.version + ".jar"}`)
	}
	path, err := customPath(spec.Path, "")
	if err != nil {
		return "", err
	}
	if _, err := parseMavenPath(spec.Path); err != nil {
		return "", err
	}
	return path, nil
}

// parseMavenPath returns the coordinates of a Maven repository layout path.
func parseMavenPath(path string) (mavenCoordinates, error) {
	invalid := fmt.Errorf("path %q is not in the Maven repository layout <group path>/<artifactId>/<version>/<artifactId>-<version>[-<classifier>].<extension>", path)
	segments := strings.Split(path, "/")
	if len(segments) < 4 {
		return mavenCoordinates{}, invalid
	}
	n := len(segments)
	coords := mavenCoordinates{
		groupID:    strings.Join(segments[:n-3], "."),
		artifactID: segments[n-3],
		version:    segments[n-2],
	}
	rest, ok := strings.CutPrefix(segments[n-1], coords.artifactID+"-"+coords.version)
	if !ok {
		return mavenCoordinates{}, invalid
	}
	if classified, ok := strings.CutPrefix(rest, "-"); ok {
		coords.classifier, rest, ok = strings.Cut(classified, ".")
		if !ok || coords.classifier == "" {
			return mavenCoordinates{}, invalid
		}
		rest = "." + rest
	}
	if coords.extension, ok = strings.CutPrefix(rest, "."); !ok || coords.extension == "" {
		return mavenCoordinates{}, invalid
	}
	return coords, nil
}

// nexusMavenUpload streams content as a single asset of a maven2 component to the components
// API and returns the hex SHA-256 of the bytes sent. The components API refuses snapshot
// versions, so those are stored with a plain PUT, which Nexus accepts at Maven layout paths but
// does not record in maven-metadata.xml.
func nexusMavenUpload(ctx context.Context, c *repositoryClient, spec *RepositoryUploadSpec, content blob.ReadOnlyBlob, _, target, mediaType string) (string, error) {
	coords, err := parseMavenPath(spec.Path)
	if err != nil {
		return "", err
	}
	if strings.HasSuffix(coords.version, "-SNAPSHOT") {
		computed, _, err := uploadBlob(ctx, c, content, target, http.Header{"Content-Type": {mediaType}}, nil)
		return computed, err
	}
	base, err := url.JoinPath(spec.URL, "service", "rest", "v1", "components")
	if err != nil {
		return "", fmt.Errorf("invalid nexus url: %w", err)
	}
	componentsURL := base + "?" + url.Values{"repository": {spec.Repository}}.Encode()
	rc, err := content.ReadCloser()
	if err != nil {
		return "", fmt.Errorf("failed opening content: %w", err)
	}
	defer func() { _ = rc.Close() }()

	hasher := sha256.New()
	body, pw := io.Pipe()
	form := multipart.NewWriter(pw)
	written := make(chan struct{})
	go func() {
		defer close(written)
		pw.CloseWithError(writeMavenForm(form, coords, io.TeeReader(rc, hasher), mediaType))
	}()
	err = c.send(ctx, http.MethodPost, componentsURL, body, -1, http.Header{"Content-Type": {form.FormDataContentType()}}, nil)
	// Unblock the writer if the request stopped reading, then wait until it stopped hashing.
	_ = body.CloseWithError(io.ErrClosedPipe)
	<-written
	return hex.EncodeToString(hasher.Sum(nil)), err
}

// writeMavenForm writes the components API form of a single maven2 asset.
func writeMavenForm(form *multipart.Writer, coords mavenCoordinates, content io.Reader, mediaType string) error {
	fields := [][2]string{
		{"maven2.groupId", coords.groupID},
		{"maven2.artifactId", coords.artifactID},
		{"maven2.version", coords.version},
		{"maven2.generate-pom", "false"},
		{"maven2.asset1.extension", coords.extension},
	}
	if coords.classifier != "" {
		fields = append(fields, [2]string{"maven2.asset1.classifier", coords.classifier})
	}
	for _, field := range fields {
		if err := form.WriteField(field[0], field[1]); err != nil {
			return err
		}
	}
	header := textproto.MIMEHeader{}
	header.Set("Content-Disposition", fmt.Sprintf(`form-data; name="maven2.asset1"; filename=%q`, coords.artifactID+"."+coords.extension))
	header.Set("Content-Type", mediaType)
	part, err := form.CreatePart(header)
	if err != nil {
		return err
	}
	if _, err := io.Copy(part, content); err != nil {
		return err
	}
	return form.Close()
}
