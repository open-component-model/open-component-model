// Package nexus uploads resources into hosted repositories of a Sonatype Nexus Repository 3 server.
package nexus

import (
	"bytes"
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/xml"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"slices"
	"strings"
	"time"

	"ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/blob/inmemory"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/bindings/go/transfer/internal/repositoryupload"
	uploadv1alpha1 "ocm.software/open-component-model/bindings/go/transfer/transformation/spec/v1alpha1"
	wgetaccess "ocm.software/open-component-model/bindings/go/wget/spec/access"
	wgetaccessv1 "ocm.software/open-component-model/bindings/go/wget/spec/access/v1"
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

// backend stores resources in Nexus: charts with [helmStore], raw and maven2 files with
// [fileStore], npm packages with [npmStore].
type backend struct{}

func (backend) Name() string { return "nexus" }

func (backend) CredentialURLs(spec *uploadv1alpha1.RepositoryUploadSpec) (string, string, error) {
	repoURL, err := url.JoinPath(spec.URL, "repository", spec.Repository)
	if err != nil {
		return "", "", fmt.Errorf("invalid nexus url: %w", err)
	}
	return repoURL, repoURL, nil
}

func (backend) Store(ctx context.Context, c *repositoryupload.Client, spec *uploadv1alpha1.RepositoryUploadSpec, src *descriptor.Resource, interval time.Duration) (repositoryupload.Store, error) {
	typ, err := repositoryType(ctx, c, spec)
	if err != nil {
		return nil, err
	}
	repoURL, err := url.JoinPath(spec.URL, "repository", spec.Repository)
	if err != nil {
		return nil, fmt.Errorf("invalid nexus url: %w", err)
	}
	switch typ {
	case "helm":
		if spec.Path != "" {
			return nil, fmt.Errorf("path is not supported for nexus helm repositories: nexus stores charts under <name>-<version>.tgz")
		}
		file, err := repositoryupload.ResourceFile(src, ".tgz")
		if err != nil {
			return nil, err
		}
		return newHelmStore(spec, repoURL, file, interval)
	case "raw", "maven2":
		format, path := rawFormat, ""
		if typ == "raw" {
			path, err = repositoryupload.UploadPath(spec, src, "")
		} else {
			format = mavenFormat
			path, err = mavenPath(spec)
		}
		if err != nil {
			return nil, err
		}
		return &fileStore{format: format, spec: spec, path: path, target: repoURL + "/" + path, interval: interval}, nil
	case "npm":
		if spec.Path != "" {
			return nil, fmt.Errorf("path is not supported for nexus npm repositories: nexus stores packages under <name>/-/<name>-<version>.tgz")
		}
		file, err := repositoryupload.ResourceFile(src, ".tgz")
		if err != nil {
			return nil, err
		}
		return &npmStore{spec: spec, repoURL: repoURL, file: file, interval: interval}, nil
	default:
		return nil, fmt.Errorf("nexus repository %q has format %q; supported: helm, raw, maven2, npm", spec.Repository, typ)
	}
}

// repositoryType reads the format of the repository from its settings. Only hosted repositories
// accept uploads.
func repositoryType(ctx context.Context, c *repositoryupload.Client, spec *uploadv1alpha1.RepositoryUploadSpec) (string, error) {
	target, err := url.JoinPath(spec.URL, "service", "rest", "v1", "repositories", spec.Repository)
	if err != nil {
		return "", fmt.Errorf("invalid nexus url: %w", err)
	}
	var settings struct {
		Format string `json:"format"`
		Type   string `json:"type"`
	}
	if err := c.Send(ctx, http.MethodGet, target, nil, -1, nil, &settings); err != nil {
		return "", fmt.Errorf("failed detecting the type of nexus repository %q: %w", spec.Repository, err)
	}
	if settings.Type != "hosted" {
		return "", fmt.Errorf("nexus repository %q is a %s repository; uploads need a hosted repository", spec.Repository, settings.Type)
	}
	return settings.Format, nil
}

// fileStore stores the resource content as is at path. Nexus records no owner of a file, so a
// file already stored at the path is never overwritten: it is reused when it has the content,
// otherwise the upload fails.
type fileStore struct {
	format       fileFormat
	spec         *uploadv1alpha1.RepositoryUploadSpec
	path, target string
	interval     time.Duration
}

func (s *fileStore) Chart() bool { return false }

func (s *fileStore) URL() string { return s.target }

// Stored reports whether target already holds content with sha256Hex. It fails when target
// holds other content, or content whose digest is unknown up front. Nexus reports the checksum
// of a stored file shortly after storing it (raw assets are indexed for search), so a stored file
// without a reported checksum yet is polled for.
func (s *fileStore) Stored(ctx context.Context, c *repositoryupload.Client, sha256Hex string) (bool, error) {
	resp, err := c.Do(ctx, http.MethodHead, s.target, nil, -1, nil)
	if err != nil {
		return false, err
	}
	_ = resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusNotFound:
		return false, nil
	case http.StatusOK:
	default:
		return false, fmt.Errorf("HEAD %s returned status %d", repositoryupload.RedactURL(s.target), resp.StatusCode)
	}
	if sha256Hex != "" {
		var checksums []string
		if _, err := repositoryupload.Poll(ctx, s.interval, func() (bool, error) {
			checksums, err = s.format.checksums(ctx, c, s.spec, s.path)
			return len(checksums) > 0, err
		}); err != nil {
			return false, err
		}
		if slices.Contains(checksums, sha256Hex) {
			return true, nil
		}
	}
	return false, fmt.Errorf("nexus repository %q already stores a different file at %s; the uploader never overwrites files in %s repositories, configure a different path",
		s.spec.Repository, repositoryupload.RedactURL(s.target), s.format.name)
}

func (s *fileStore) Put(ctx context.Context, c *repositoryupload.Client, content blob.ReadOnlyBlob, mediaType, _ string) (string, error) {
	return s.format.put(ctx, c, s.spec, content, s.target, mediaType)
}

func (s *fileStore) Discard(context.Context, *repositoryupload.Client, string) error {
	return fmt.Errorf("nexus keeps the uploaded file at %s", repositoryupload.RedactURL(s.target))
}

func (s *fileStore) Publish(_ context.Context, _ *repositoryupload.Client, _, mediaType string) (runtime.Typed, error) {
	return &wgetaccessv1.Wget{Type: wgetaccess.V1VersionedType, URL: s.target, MediaType: mediaType}, nil
}

// fileFormat is how a repository format stores a file and reports what it stores.
type fileFormat struct {
	name string
	// checksums returns the SHA-256 Nexus reports for the file stored at path; empty when it
	// reports none yet.
	checksums func(ctx context.Context, c *repositoryupload.Client, spec *uploadv1alpha1.RepositoryUploadSpec, path string) ([]string, error)
	// put stores content at target and returns the hex SHA-256 of the bytes sent.
	put func(ctx context.Context, c *repositoryupload.Client, spec *uploadv1alpha1.RepositoryUploadSpec, content blob.ReadOnlyBlob, target, mediaType string) (string, error)
}

// rawFormat stores files with a plain PUT and finds them by the asset search.
var rawFormat = fileFormat{
	name: "raw",
	checksums: func(ctx context.Context, c *repositoryupload.Client, spec *uploadv1alpha1.RepositoryUploadSpec, path string) ([]string, error) {
		return assetChecksums(ctx, c, spec, path, nil)
	},
	put: func(ctx context.Context, c *repositoryupload.Client, _ *uploadv1alpha1.RepositoryUploadSpec, content blob.ReadOnlyBlob, target, mediaType string) (string, error) {
		computed, _, err := repositoryupload.UploadBlob(ctx, c, content, target, http.Header{"Content-Type": {mediaType}}, nil)
		return computed, err
	},
}

// mavenFormat stores files through the components API, which, unlike a plain PUT, updates
// maven-metadata.xml, so version ranges and latest/release see the upload. The asset search
// finds maven assets by their coordinates, not by name.
var mavenFormat = fileFormat{
	name: "maven2",
	checksums: func(ctx context.Context, c *repositoryupload.Client, spec *uploadv1alpha1.RepositoryUploadSpec, path string) ([]string, error) {
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
		return assetChecksums(ctx, c, spec, path, query)
	},
	put: mavenUpload,
}

// assetChecksums returns the SHA-256 of the assets Nexus's search finds at path. query
// selects the assets by format-specific attributes; without it, raw assets are selected by name.
func assetChecksums(ctx context.Context, c *repositoryupload.Client, spec *uploadv1alpha1.RepositoryUploadSpec, path string, query url.Values) ([]string, error) {
	name, err := url.PathUnescape(path)
	if err != nil {
		return nil, fmt.Errorf("invalid path %q: %w", path, err)
	}
	// Nexus reports asset paths with a leading slash (e.g. /a/b/c.txt); path never has one.
	assetPath := "/" + name
	if query == nil {
		query = url.Values{"name": {assetPath}}
	}
	items, err := searchAssets(ctx, c, spec, query)
	if err != nil {
		return nil, err
	}
	var checksums []string
	for _, item := range items {
		if item.Path == assetPath {
			checksums = append(checksums, item.Checksum.SHA256)
		}
	}
	return checksums, nil
}

// asset is an asset item of Nexus's asset search API.
type asset struct {
	Path     string `json:"path"`
	Checksum struct {
		SHA256 string `json:"sha256"`
	} `json:"checksum"`
}

// searchAssets returns all assets of the repository Nexus's asset search finds with query,
// following continuation tokens: a name with search wildcards (e.g. *) can match more assets
// than fit on the first page.
func searchAssets(ctx context.Context, c *repositoryupload.Client, spec *uploadv1alpha1.RepositoryUploadSpec, query url.Values) ([]asset, error) {
	base, err := url.JoinPath(spec.URL, "service", "rest", "v1", "search", "assets")
	if err != nil {
		return nil, fmt.Errorf("invalid nexus url: %w", err)
	}
	query.Set("repository", spec.Repository)
	var items []asset
	for {
		var page struct {
			Items             []asset `json:"items"`
			ContinuationToken string  `json:"continuationToken"`
		}
		if err := c.Send(ctx, http.MethodGet, base+"?"+query.Encode(), nil, -1, nil, &page); err != nil {
			return nil, err
		}
		items = append(items, page.Items...)
		if page.ContinuationToken == "" {
			return items, nil
		}
		query.Set("continuationToken", page.ContinuationToken)
	}
}

// mavenCoordinates are the Maven coordinates of a single file.
type mavenCoordinates struct {
	groupID, artifactID, version, classifier, extension string
}

// mavenPath validates the configured path as a Maven repository layout path,
// <group path>/<artifactId>/<version>/<artifactId>-<version>[-<classifier>].<extension>, and
// returns it path-escaped. Nexus maven2 repositories store files only at such paths.
func mavenPath(spec *uploadv1alpha1.RepositoryUploadSpec) (string, error) {
	if spec.Path == "" {
		return "", fmt.Errorf("nexus maven2 repositories need a path in the Maven repository layout, e.g. " +
			`${"com/example/" + resource.name + "/" + resource.version + "/" + resource.name + "-" + resource.version + ".jar"}`)
	}
	path, err := repositoryupload.CustomPath(spec.Path, "")
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

// mavenUpload streams content as a single asset of a maven2 component to the components
// API and returns the hex SHA-256 of the bytes sent. The components API refuses snapshot
// versions, so those are stored with a plain PUT, which Nexus accepts at Maven layout paths but
// does not record in maven-metadata.xml.
func mavenUpload(ctx context.Context, c *repositoryupload.Client, spec *uploadv1alpha1.RepositoryUploadSpec, content blob.ReadOnlyBlob, target, mediaType string) (string, error) {
	coords, err := parseMavenPath(spec.Path)
	if err != nil {
		return "", err
	}
	if coords.extension == "pom" {
		if content, err = checkPOM(content, coords, spec.Path); err != nil {
			return "", err
		}
	}
	if strings.HasSuffix(coords.version, "-SNAPSHOT") {
		return rawFormat.put(ctx, c, spec, content, target, mediaType)
	}
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
	return componentUpload(ctx, c, spec, fields, "maven2.asset1", coords.artifactID+"."+coords.extension, content, mediaType)
}

// componentUpload streams content as the single asset assetField of a component, with
// the form fields, to the components API and returns the hex SHA-256 of the bytes sent.
func componentUpload(ctx context.Context, c *repositoryupload.Client, spec *uploadv1alpha1.RepositoryUploadSpec, fields [][2]string, assetField, filename string, content blob.ReadOnlyBlob, mediaType string) (string, error) {
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
		pw.CloseWithError(writeComponentForm(form, fields, assetField, filename, io.TeeReader(rc, hasher), mediaType))
	}()
	err = c.Send(ctx, http.MethodPost, componentsURL, body, -1, http.Header{"Content-Type": {form.FormDataContentType()}}, nil)
	// Unblock the writer if the request stopped reading, then wait until it stopped hashing.
	_ = body.CloseWithError(io.ErrClosedPipe)
	<-written
	return hex.EncodeToString(hasher.Sum(nil)), err
}

// writeComponentForm writes the components API form of a single asset.
func writeComponentForm(form *multipart.Writer, fields [][2]string, assetField, filename string, content io.Reader, mediaType string) error {
	for _, field := range fields {
		if err := form.WriteField(field[0], field[1]); err != nil {
			return err
		}
	}
	header := textproto.MIMEHeader{}
	header.Set("Content-Disposition", fmt.Sprintf(`form-data; name=%q; filename=%q`, assetField, filename))
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

// npmStore uploads the npm package tarball through the components API. Nexus reads the package
// name and version from package.json and stores the tarball under <name>/-/<name>-<version>.tgz,
// so the tarball is found by its SHA-256 afterwards. A tarball the repository already stores is
// reused without uploading it.
type npmStore struct {
	spec          *uploadv1alpha1.RepositoryUploadSpec
	repoURL, file string
	interval      time.Duration
	// path is the escaped path, with a leading slash, of the stored tarball once found.
	path string
}

func (s *npmStore) Chart() bool { return false }

func (s *npmStore) URL() string { return s.repoURL + s.path }

func (s *npmStore) Stored(ctx context.Context, c *repositoryupload.Client, sha256Hex string) (bool, error) {
	if sha256Hex == "" {
		return false, nil
	}
	return s.find(ctx, c, sha256Hex)
}

func (s *npmStore) Put(ctx context.Context, c *repositoryupload.Client, content blob.ReadOnlyBlob, mediaType, _ string) (string, error) {
	return componentUpload(ctx, c, s.spec, nil, "npm.asset", s.file, content, mediaType)
}

func (s *npmStore) Discard(context.Context, *repositoryupload.Client, string) error {
	return fmt.Errorf("nexus keeps the uploaded package in repository %s", s.spec.Repository)
}

// Publish returns a Wget/v1 access on the stored tarball. Nexus indexes it for search shortly
// after the upload, so it is polled for.
func (s *npmStore) Publish(ctx context.Context, c *repositoryupload.Client, sha256Hex, mediaType string) (runtime.Typed, error) {
	if s.path == "" {
		found, err := repositoryupload.Poll(ctx, s.interval, func() (bool, error) { return s.find(ctx, c, sha256Hex) })
		if err != nil {
			return nil, err
		}
		if !found {
			return nil, fmt.Errorf("nexus repository %q stored the npm package with SHA-256 %s, but its search does not find it", s.spec.Repository, sha256Hex)
		}
	}
	return &wgetaccessv1.Wget{Type: wgetaccess.V1VersionedType, URL: s.repoURL + s.path, MediaType: mediaType}, nil
}

// find looks up the first .tgz asset the repository stores with content sha256Hex and remembers
// its path.
func (s *npmStore) find(ctx context.Context, c *repositoryupload.Client, sha256Hex string) (bool, error) {
	items, err := searchAssets(ctx, c, s.spec, url.Values{"sha256": {sha256Hex}})
	if err != nil {
		return false, err
	}
	for _, item := range items {
		if !strings.HasSuffix(item.Path, ".tgz") {
			continue
		}
		segments := strings.Split(strings.TrimPrefix(item.Path, "/"), "/")
		for i, segment := range segments {
			segments[i] = url.PathEscape(segment)
		}
		s.path = "/" + strings.Join(segments, "/")
		return true, nil
	}
	return false, nil
}

// checkPOM reads a POM and fails unless its coordinates are those of path: the components API
// stores a POM under the coordinates it declares, ignoring the form fields, so a mismatch would
// write to a location that was never checked. It returns the POM to upload.
func checkPOM(content blob.ReadOnlyBlob, coords mavenCoordinates, path string) (blob.ReadOnlyBlob, error) {
	rc, err := content.ReadCloser()
	if err != nil {
		return nil, fmt.Errorf("failed opening content: %w", err)
	}
	defer func() { _ = rc.Close() }()
	data, err := io.ReadAll(io.LimitReader(rc, maxPOMBytes+1))
	if err != nil {
		return nil, fmt.Errorf("failed reading POM: %w", err)
	}
	if len(data) > maxPOMBytes {
		return nil, fmt.Errorf("POM at %q exceeds %d bytes", path, maxPOMBytes)
	}
	var pom struct {
		GroupID    string `xml:"groupId"`
		ArtifactID string `xml:"artifactId"`
		Version    string `xml:"version"`
		Parent     struct {
			GroupID string `xml:"groupId"`
			Version string `xml:"version"`
		} `xml:"parent"`
	}
	if err := xml.Unmarshal(data, &pom); err != nil {
		return nil, fmt.Errorf("content for %q is not a POM: %w", path, err)
	}
	groupID, version := cmp.Or(pom.GroupID, pom.Parent.GroupID), cmp.Or(pom.Version, pom.Parent.Version)
	if groupID != coords.groupID || pom.ArtifactID != coords.artifactID || version != coords.version {
		return nil, fmt.Errorf("POM declares %s:%s:%s, but path %q is %s:%s:%s; nexus stores a POM under the coordinates it declares",
			groupID, pom.ArtifactID, version, path, coords.groupID, coords.artifactID, coords.version)
	}
	return inmemory.New(bytes.NewReader(data), inmemory.WithSize(int64(len(data)))), nil
}

// maxPOMBytes bounds the POM read into memory for checkPOM.
const maxPOMBytes = 1 << 20
