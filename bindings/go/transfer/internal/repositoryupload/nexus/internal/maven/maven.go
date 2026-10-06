// Package maven uploads files into Nexus Repository 3 maven2 hosted repositories.
package maven

import (
	"bytes"
	"cmp"
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/opencontainers/go-digest"

	"ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/blob/inmemory"
	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/bindings/go/transfer/internal/repositoryupload"
	"ocm.software/open-component-model/bindings/go/transfer/internal/repositoryupload/client"
	"ocm.software/open-component-model/bindings/go/transfer/internal/repositoryupload/nexus/internal/api"
	"ocm.software/open-component-model/bindings/go/transfer/internal/repositoryupload/uploadpath"
)

// maxPOMBytes bounds the POM read into memory, see checkPOM.
const maxPOMBytes = 1 << 20

// Store stores a single file at a Maven repository layout path through the components API,
// which, unlike a plain PUT, updates maven-metadata.xml, so version ranges and latest/release
// see the upload. A file already stored at the path is never overwritten, see
// [api.Repository.StoredFile].
type Store struct {
	repo         *api.Repository
	coordinates  Coordinates
	path, target string
	interval     time.Duration
}

var _ repositoryupload.Store = (*Store)(nil)

// New returns the store uploading to path, a Maven repository layout path, in repo.
func New(repo *api.Repository, path string, interval time.Duration) (*Store, error) {
	if path == "" {
		return nil, fmt.Errorf("nexus maven2 repositories need a path in the Maven repository layout, e.g. " +
			`${"com/example/" + resource.name + "/" + resource.version + "/" + resource.name + "-" + resource.version + ".jar"}`)
	}
	escaped, err := uploadpath.Custom(path, "")
	if err != nil {
		return nil, err
	}
	coordinates, err := ParsePath(path)
	if err != nil {
		return nil, err
	}
	return &Store{repo: repo, coordinates: coordinates, path: escaped, target: repo.URL + "/" + escaped, interval: interval}, nil
}

func (s *Store) Chart() bool { return false }

func (s *Store) URL() string { return s.target }

// Stored reports whether the file at the path has the content. The asset search finds maven
// assets by their coordinates, not by name.
func (s *Store) Stored(ctx context.Context, known digest.Digest) (bool, error) {
	return s.repo.StoredFile(ctx, "maven2", s.path, s.query(), known, s.interval)
}

// query selects the assets of the file by its coordinates.
func (s *Store) query() url.Values {
	c := s.coordinates
	query := url.Values{
		"maven.groupId":     {c.GroupID},
		"maven.artifactId":  {c.ArtifactID},
		"maven.baseVersion": {c.Version},
		"maven.extension":   {c.Extension},
	}
	if c.Classifier != "" {
		query.Set("maven.classifier", c.Classifier)
	}
	return query
}

// Put uploads the file as the single asset of a maven2 component. The components API refuses
// snapshot versions, so those are stored with a plain PUT, which Nexus accepts at Maven layout
// paths but does not record in maven-metadata.xml.
func (s *Store) Put(ctx context.Context, content blob.ReadOnlyBlob, mediaType string, known digest.Digest) (digest.Digest, error) {
	c := s.coordinates
	if c.Extension == "pom" {
		var err error
		if content, err = checkPOM(content, c, s.path); err != nil {
			return "", err
		}
	}
	if strings.HasSuffix(c.Version, "-SNAPSHOT") {
		computed, _, err := s.repo.Client.PutBlob(ctx, s.target, content, known, http.Header{"Content-Type": {mediaType}}, nil)
		return computed, err
	}
	fields := [][2]string{
		{"maven2.groupId", c.GroupID},
		{"maven2.artifactId", c.ArtifactID},
		{"maven2.version", c.Version},
		{"maven2.generate-pom", "false"},
		{"maven2.asset1.extension", c.Extension},
	}
	if c.Classifier != "" {
		fields = append(fields, [2]string{"maven2.asset1.classifier", c.Classifier})
	}
	return s.repo.UploadComponent(ctx, fields, "maven2.asset1", c.ArtifactID+"."+c.Extension, content, known, mediaType)
}

func (s *Store) Discard(context.Context, digest.Digest) error {
	return fmt.Errorf("nexus keeps the uploaded file at %s", client.RedactURL(s.target))
}

// Publish returns a Wget/v1 access on the asset search download URL that pins the stored content,
// see [api.Repository.DownloadURL]. Snapshots, whose coordinates select every timestamped
// deployment, and content with an unknown digest get an access on the file.
func (s *Store) Publish(ctx context.Context, stored digest.Digest, mediaType string) (runtime.Typed, error) {
	if strings.HasSuffix(s.coordinates.Version, "-SNAPSHOT") {
		return repositoryupload.FileAccess(s.target, mediaType), nil
	}
	download, ok, err := s.repo.DownloadURL(ctx, s.query(), stored, s.interval)
	if err != nil {
		return nil, err
	}
	if !ok {
		download = s.target
	}
	return repositoryupload.FileAccess(download, mediaType), nil
}

// Coordinates are the Maven coordinates of a single file.
type Coordinates struct {
	GroupID, ArtifactID, Version, Classifier, Extension string
}

// ParsePath returns the coordinates of a Maven repository layout path,
// <group path>/<artifactId>/<version>/<artifactId>-<version>[-<classifier>].<extension>.
func ParsePath(path string) (Coordinates, error) {
	invalid := fmt.Errorf("path %q is not in the Maven repository layout <group path>/<artifactId>/<version>/<artifactId>-<version>[-<classifier>].<extension>", path)
	segments := strings.Split(path, "/")
	if len(segments) < 4 {
		return Coordinates{}, invalid
	}
	n := len(segments)
	c := Coordinates{
		GroupID:    strings.Join(segments[:n-3], "."),
		ArtifactID: segments[n-3],
		Version:    segments[n-2],
	}
	rest, ok := strings.CutPrefix(segments[n-1], c.ArtifactID+"-"+c.Version)
	if !ok {
		return Coordinates{}, invalid
	}
	if classified, ok := strings.CutPrefix(rest, "-"); ok {
		c.Classifier, rest, ok = strings.Cut(classified, ".")
		if !ok || c.Classifier == "" {
			return Coordinates{}, invalid
		}
		rest = "." + rest
	}
	if c.Extension, ok = strings.CutPrefix(rest, "."); !ok || c.Extension == "" {
		return Coordinates{}, invalid
	}
	return c, nil
}

// checkPOM reads a POM and fails unless its coordinates are c: the components API stores a POM
// under the coordinates it declares, ignoring the form fields, so a mismatch would write to a
// location that was never checked. It returns the POM to upload.
func checkPOM(content blob.ReadOnlyBlob, c Coordinates, path string) (blob.ReadOnlyBlob, error) {
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
	if groupID != c.GroupID || pom.ArtifactID != c.ArtifactID || version != c.Version {
		return nil, fmt.Errorf("POM declares %s:%s:%s, but path %q is %s:%s:%s; nexus stores a POM under the coordinates it declares",
			groupID, pom.ArtifactID, version, path, c.GroupID, c.ArtifactID, c.Version)
	}
	return inmemory.New(bytes.NewReader(data), inmemory.WithSize(int64(len(data)))), nil
}
