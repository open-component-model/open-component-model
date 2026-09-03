package externalartifact

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	fluxmeta "github.com/fluxcd/pkg/apis/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// UnlimitedArtifactSize disables the per-artifact size cap.
const UnlimitedArtifactSize int64 = 0

// ErrArtifactTooLarge is returned when packaging content that exceeds the
// configured maximum artifact size.
var ErrArtifactTooLarge = errors.New("artifact exceeds maximum allowed size")

// Storage packages OCM resource content into gzip-compressed tar archives on
// the local filesystem and serves them over an in-cluster HTTP endpoint, in the
// same shape Flux's source-controller uses. On-disk layout mirrors the reported
// artifact path: <basePath>/<kind>/<namespace>/<name>/<checksum>.tar.gz.
//
// Concurrency: mutations to one artifact directory are serialized by a per-key
// lock (kind/namespace/name), so unrelated artifacts pack in parallel. Serving
// needs no lock — archives are content-addressed and written via temp-file +
// atomic rename, so a reader can never observe a partial file.
type Storage struct {
	// basePath is the root directory on the local filesystem under which all
	// artifacts are stored.
	basePath string

	// hostname is the in-cluster host (and optional port) the artifact HTTP
	// server is reachable at, e.g.
	// "ocm-k8s-toolkit.ocm-system.svc.cluster.local.". It is used to build the
	// artifact URLs reported in the ExternalArtifact status.
	hostname string

	// maxArtifactSize caps the uncompressed bytes written into an archive.
	// UnlimitedArtifactSize (0) disables the cap.
	maxArtifactSize int64

	// metrics records storage operations; may be nil (recording is a no-op).
	metrics *Metrics

	// keyLocks serializes mutations per artifact directory.
	keyLocks sync.Map // map[string]*sync.Mutex

	// verifiedCache records the fingerprint (size + mtime) of artifacts whose
	// integrity was last confirmed, so VerifyIntegrity can skip re-hashing
	// unchanged files. Keyed by storage-relative path.
	verifiedCache sync.Map // map[string]fileFingerprint
}

// fileFingerprint is a cheap identity for an on-disk file used to decide whether
// a previously-verified artifact needs re-hashing.
type fileFingerprint struct {
	size      int64
	modTimeNs int64
}

// StorageOption configures a Storage.
type StorageOption func(*Storage)

// WithMaxArtifactSize sets the maximum uncompressed size (in bytes) of a single
// artifact. Zero (UnlimitedArtifactSize) disables the cap.
func WithMaxArtifactSize(max int64) StorageOption {
	return func(s *Storage) { s.maxArtifactSize = max }
}

// WithMetrics attaches a Metrics recorder to the Storage.
func WithMetrics(m *Metrics) StorageOption {
	return func(s *Storage) { s.metrics = m }
}

// NewStorage creates a Storage rooted at basePath, advertising artifacts under
// the given in-cluster hostname. The base path is created if it does not exist.
func NewStorage(basePath, hostname string, opts ...StorageOption) (*Storage, error) {
	if basePath == "" {
		return nil, fmt.Errorf("storage base path must not be empty")
	}
	if err := os.MkdirAll(basePath, 0o700); err != nil {
		return nil, fmt.Errorf("failed to create storage base path %q: %w", basePath, err)
	}

	s := &Storage{
		basePath: basePath,
		hostname: strings.TrimSuffix(hostname, "/"),
	}
	for _, opt := range opts {
		opt(s)
	}

	return s, nil
}

// BasePath returns the root directory of the storage.
func (s *Storage) BasePath() string {
	return s.basePath
}

// lockFor returns the per-key mutex for an artifact directory, creating it on
// first use.
func (s *Storage) lockFor(kind, namespace, name string) *sync.Mutex {
	key := ArtifactDir(kind, namespace, name)
	actual, _ := s.keyLocks.LoadOrStore(key, &sync.Mutex{})

	return actual.(*sync.Mutex)
}

// ArtifactDir returns the directory (relative to the storage base path) that
// holds the artifacts for a single object identified by kind/namespace/name.
func ArtifactDir(kind, namespace, name string) string {
	return filepath.Join(strings.ToLower(kind), namespace, name)
}

// artifactPath returns the storage-relative path of an artifact given its
// object coordinates and the content checksum.
func artifactPath(kind, namespace, name, checksum string) string {
	return filepath.Join(ArtifactDir(kind, namespace, name), checksum+".tar.gz")
}

// TarGzResult carries the metadata produced while packaging content into a
// gzip-compressed tar archive.
type TarGzResult struct {
	// Path is the storage-relative path of the archive.
	Path string
	// Digest is the digest of the archive in "<algorithm>:<checksum>" form.
	Digest string
	// Size is the number of bytes of the archive on disk.
	Size int64
}

// countingWriter counts bytes written and fails past a limit (0 = unlimited).
type countingWriter struct {
	w       io.Writer
	limit   int64
	written int64
}

func (c *countingWriter) Write(p []byte) (int, error) {
	if c.limit > 0 && c.written+int64(len(p)) > c.limit {
		return 0, ErrArtifactTooLarge
	}
	n, err := c.w.Write(p)
	c.written += int64(n)

	return n, err
}

// Archive packages the files under srcDir into a gzip-compressed tar archive
// stored under <kind>/<namespace>/<name>/<checksum>.tar.gz and returns its
// metadata. The write is atomic and durable: content is streamed to a temp
// file (digest computed on the fly), fsync'd, then renamed into its
// digest-addressed path with the parent dir fsync'd — so a reader never sees a
// partial file and a crash cannot leave a valid-named but truncated one.
func (s *Storage) Archive(ctx context.Context, kind, namespace, name, srcDir string) (result *TarGzResult, err error) {
	lock := s.lockFor(kind, namespace, name)
	lock.Lock()
	defer lock.Unlock()

	start := time.Now()
	defer func() {
		if s.metrics == nil {
			return
		}
		s.metrics.ArchiveDurationSeconds.Observe(time.Since(start).Seconds())
		if err != nil {
			s.metrics.ArchiveErrorsTotal.Inc()

			return
		}
		s.metrics.ArtifactsProducedTotal.Inc()
		if result != nil {
			s.metrics.ArtifactSizeBytes.Observe(float64(result.Size))
		}
	}()

	dir := filepath.Join(s.basePath, ArtifactDir(kind, namespace, name))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("failed to create artifact directory %q: %w", dir, err)
	}

	tmp, err := os.CreateTemp(dir, "artifact-*.tar.gz.tmp")
	if err != nil {
		return nil, fmt.Errorf("failed to create temporary artifact file: %w", err)
	}
	tmpName := tmp.Name()
	// Best-effort cleanup of the temp file if we fail before renaming it.
	defer func() { _ = os.Remove(tmpName) }()

	hasher := sha256.New()
	// Everything written to the archive is simultaneously fed to the hasher so
	// the digest covers the exact bytes stored on disk. The size cap is applied
	// to the compressed output stream.
	counter := &countingWriter{w: io.MultiWriter(tmp, hasher), limit: s.maxArtifactSize}
	gzw := gzip.NewWriter(counter)
	tw := tar.NewWriter(gzw)

	if err := writeTarball(ctx, tw, srcDir); err != nil {
		_ = tw.Close()
		_ = gzw.Close()
		_ = tmp.Close()

		return nil, fmt.Errorf("failed to write tarball: %w", err)
	}

	if err := tw.Close(); err != nil {
		_ = gzw.Close()
		_ = tmp.Close()

		return nil, fmt.Errorf("failed to close tar writer: %w", err)
	}
	if err := gzw.Close(); err != nil {
		_ = tmp.Close()

		return nil, fmt.Errorf("failed to close gzip writer: %w", err)
	}
	// Durability: flush the file's data to stable storage before the rename so a
	// crash cannot persist the rename while losing the bytes.
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()

		return nil, fmt.Errorf("failed to fsync temporary artifact file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return nil, fmt.Errorf("failed to close temporary artifact file: %w", err)
	}

	checksum := hex.EncodeToString(hasher.Sum(nil))
	relPath := artifactPath(kind, namespace, name, checksum)
	finalPath := filepath.Join(s.basePath, relPath)

	if err := os.Rename(tmpName, finalPath); err != nil {
		return nil, fmt.Errorf("failed to move artifact into place: %w", err)
	}
	// fsync the directory so the rename itself is durable.
	if err := fsyncDir(dir); err != nil {
		return nil, fmt.Errorf("failed to fsync artifact directory: %w", err)
	}

	info, err := os.Stat(finalPath)
	if err != nil {
		return nil, fmt.Errorf("failed to stat artifact: %w", err)
	}

	return &TarGzResult{
		Path:   relPath,
		Digest: "sha256:" + checksum,
		Size:   info.Size(),
	}, nil
}

// fsyncDir flushes a directory's metadata (e.g. a rename) to stable storage.
func fsyncDir(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer func() { _ = d.Close() }()

	if err := d.Sync(); err != nil {
		return err
	}

	return d.Close()
}

// writeTarball walks srcDir and writes every regular file and directory into
// the tar writer using paths relative to srcDir. Symlinks and other
// non-regular files are skipped to avoid path-traversal and dangling-link
// hazards on the consuming side. It aborts if ctx is cancelled (e.g. on
// shutdown) so a large package does not block graceful stop.
func writeTarball(ctx context.Context, tw *tar.Writer, srcDir string) error {
	return filepath.Walk(srcDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}

		// Skip anything that is neither a regular file nor a directory.
		if !info.Mode().IsRegular() && !info.IsDir() {
			return nil
		}

		relPath, err := filepath.Rel(srcDir, path)
		if err != nil {
			return err
		}
		if relPath == "." {
			return nil
		}

		header, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		header.Name = filepath.ToSlash(relPath)
		// Normalise metadata so identical content always yields an identical
		// archive (and therefore an identical digest).
		header.ModTime = time.Time{}
		header.AccessTime = time.Time{}
		header.ChangeTime = time.Time{}
		header.Uid = 0
		header.Gid = 0
		header.Uname = ""
		header.Gname = ""

		if err := tw.WriteHeader(header); err != nil {
			return err
		}

		if info.IsDir() {
			return nil
		}

		file, err := os.Open(path)
		if err != nil {
			return err
		}
		defer func() { _ = file.Close() }()

		if _, err := io.Copy(tw, file); err != nil {
			return err
		}

		return nil
	})
}

// Artifact builds the Flux Artifact status object for a packaged archive.
// revisionID is the human-readable identifier (e.g. the resource version) that
// prefixes the revision field.
func (s *Storage) Artifact(result *TarGzResult, revisionID string) *fluxmeta.Artifact {
	size := result.Size

	return &fluxmeta.Artifact{
		Path:           result.Path,
		URL:            s.url(result.Path),
		Revision:       fmt.Sprintf("%s@%s", revisionID, result.Digest),
		Digest:         result.Digest,
		LastUpdateTime: metav1.Now(),
		Size:           &size,
	}
}

// url builds the in-cluster HTTP URL for a storage-relative artifact path.
func (s *Storage) url(relPath string) string {
	return fmt.Sprintf("http://%s/%s", s.hostname, filepath.ToSlash(relPath))
}

// Exists reports whether the artifact at the given storage-relative path is
// present on disk.
func (s *Storage) Exists(relPath string) bool {
	if relPath == "" {
		return false
	}
	info, err := os.Stat(filepath.Join(s.basePath, relPath))

	return err == nil && info.Mode().IsRegular()
}

// Verify checks that the artifact at relPath exists and its content digest
// matches the expected "sha256:<hex>" digest. It is used to detect a wiped or
// corrupted store (e.g. after a pod restart on ephemeral storage) so the caller
// can repackage.
func (s *Storage) Verify(relPath, expectedDigest string) (bool, error) {
	if relPath == "" || expectedDigest == "" {
		return false, nil
	}
	f, err := os.Open(filepath.Join(s.basePath, relPath))
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}

		return false, fmt.Errorf("failed to open artifact for verification: %w", err)
	}
	defer func() { _ = f.Close() }()

	hasher := sha256.New()
	if _, err := io.Copy(hasher, f); err != nil {
		return false, fmt.Errorf("failed to hash artifact: %w", err)
	}

	return "sha256:"+hex.EncodeToString(hasher.Sum(nil)) == expectedDigest, nil
}

// SizeMatches reports whether the artifact at relPath exists and its on-disk
// size equals wantSize. It is a cheap first-gate for the reconcile fast path:
// artifacts are digest-addressed and immutable, so a present file of the
// expected size is overwhelmingly the correct content, and a full re-hash
// (Verify) is only needed when this cheap check is inconclusive. A wantSize <= 0
// forces the caller to fall back to Verify.
func (s *Storage) SizeMatches(relPath string, wantSize int64) bool {
	if relPath == "" || wantSize <= 0 {
		return false
	}
	info, err := os.Stat(filepath.Join(s.basePath, relPath))

	return err == nil && info.Mode().IsRegular() && info.Size() == wantSize
}

// RemoveAll deletes every artifact stored for the given object. It is used to
// garbage-collect artifacts when the owning object is deleted.
func (s *Storage) RemoveAll(kind, namespace, name string) error {
	lock := s.lockFor(kind, namespace, name)
	lock.Lock()
	defer lock.Unlock()

	dir := filepath.Join(s.basePath, ArtifactDir(kind, namespace, name))
	if err := os.RemoveAll(dir); err != nil {
		return fmt.Errorf("failed to remove artifact directory %q: %w", dir, err)
	}

	return nil
}

// RemoveAllButCurrent removes every artifact stored for the given object except
// the one at keepRelPath, so that stale revisions do not accumulate on disk.
func (s *Storage) RemoveAllButCurrent(kind, namespace, name, keepRelPath string) error {
	lock := s.lockFor(kind, namespace, name)
	lock.Lock()
	defer lock.Unlock()

	dir := filepath.Join(s.basePath, ArtifactDir(kind, namespace, name))
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}

		return fmt.Errorf("failed to read artifact directory %q: %w", dir, err)
	}

	keep := filepath.Base(keepRelPath)
	for _, entry := range entries {
		if entry.IsDir() || entry.Name() == keep {
			continue
		}
		if err := os.Remove(filepath.Join(dir, entry.Name())); err != nil {
			return fmt.Errorf("failed to remove stale artifact %q: %w", entry.Name(), err)
		}
	}

	return nil
}

// ArtifactObject identifies an on-disk artifact set by the object coordinates
// encoded in its storage path.
type ArtifactObject struct {
	Kind      string
	Namespace string
	Name      string
}

// ListArtifactObjects walks the storage tree and returns every object that has
// an artifact directory on disk, decoded from the
// <kind>/<namespace>/<name>/ layout. It is used by the garbage collector to
// find directories whose owning object may no longer exist.
func (s *Storage) ListArtifactObjects() ([]ArtifactObject, error) {
	var objects []ArtifactObject

	kinds, err := os.ReadDir(s.basePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}

		return nil, fmt.Errorf("failed to read storage base path: %w", err)
	}

	for _, kindEntry := range kinds {
		if !kindEntry.IsDir() {
			continue
		}
		kind := kindEntry.Name()
		namespaces, err := os.ReadDir(filepath.Join(s.basePath, kind))
		if err != nil {
			return nil, fmt.Errorf("failed to read kind directory %q: %w", kind, err)
		}
		for _, nsEntry := range namespaces {
			if !nsEntry.IsDir() {
				continue
			}
			namespace := nsEntry.Name()
			names, err := os.ReadDir(filepath.Join(s.basePath, kind, namespace))
			if err != nil {
				return nil, fmt.Errorf("failed to read namespace directory %q/%q: %w", kind, namespace, err)
			}
			for _, nameEntry := range names {
				if !nameEntry.IsDir() {
					continue
				}
				objects = append(objects, ArtifactObject{
					Kind:      kind,
					Namespace: namespace,
					Name:      nameEntry.Name(),
				})
			}
		}
	}

	return objects, nil
}

// DiskUsage reports the total bytes and file count occupied by stored
// artifacts. It walks the storage tree; callers should invoke it on a schedule
// (e.g. the GC sweep), not on the hot path.
func (s *Storage) DiskUsage() (totalBytes int64, fileCount int, err error) {
	walkErr := filepath.Walk(s.basePath, func(_ string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.Mode().IsRegular() {
			totalBytes += info.Size()
			fileCount++
		}

		return nil
	})
	if walkErr != nil {
		if os.IsNotExist(walkErr) {
			return 0, 0, nil
		}

		return 0, 0, fmt.Errorf("failed to compute disk usage: %w", walkErr)
	}

	return totalBytes, fileCount, nil
}

// VerifyIntegrity re-hashes stored artifacts and removes any whose content no
// longer matches the checksum encoded in its filename (<checksum>.tar.gz). This
// detects on-disk corruption / tampering (RFC-0012 startup integrity check).
// Removed artifacts are repackaged lazily by the reconciler on the next
// reconcile of their owning Resource. It returns the number of corrupt
// artifacts removed.
//
// Verification is incremental: an artifact whose (size, mtime) fingerprint is
// unchanged since it last verified clean is skipped, so steady-state sweeps only
// re-hash new or changed files rather than the whole store. Removal of a corrupt
// file takes the per-key lock to avoid racing a concurrent Archive for the same
// object.
func (s *Storage) VerifyIntegrity() (removed int, err error) {
	objects, err := s.ListArtifactObjects()
	if err != nil {
		return 0, err
	}

	for _, obj := range objects {
		dir := filepath.Join(s.basePath, ArtifactDir(obj.Kind, obj.Namespace, obj.Name))
		entries, err := os.ReadDir(dir)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}

			return removed, fmt.Errorf("failed to read artifact directory %q: %w", dir, err)
		}
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".tar.gz") {
				continue
			}
			relPath := filepath.Join(ArtifactDir(obj.Kind, obj.Namespace, obj.Name), entry.Name())

			// Incremental skip: unchanged since last clean verification.
			info, statErr := entry.Info()
			if statErr == nil && s.fingerprintUnchanged(relPath, info) {
				continue
			}

			wantChecksum := strings.TrimSuffix(entry.Name(), ".tar.gz")
			ok, err := s.Verify(relPath, "sha256:"+wantChecksum)
			if err != nil {
				return removed, err
			}
			if ok {
				// Remember the fingerprint so the next sweep skips it.
				if info != nil {
					s.verifiedCache.Store(relPath, fingerprintOf(info))
				}

				continue
			}

			// Corrupt: drop the cache entry and remove under the per-key lock so a
			// concurrent Archive for the same object cannot race the delete.
			s.verifiedCache.Delete(relPath)
			lock := s.lockFor(obj.Kind, obj.Namespace, obj.Name)
			lock.Lock()
			rmErr := os.Remove(filepath.Join(dir, entry.Name()))
			lock.Unlock()
			if rmErr != nil && !os.IsNotExist(rmErr) {
				return removed, fmt.Errorf("failed to remove corrupt artifact %q: %w", entry.Name(), rmErr)
			}
			removed++
		}
	}

	return removed, nil
}

// fingerprintUnchanged reports whether relPath's cached clean fingerprint
// matches its current (size, mtime).
func (s *Storage) fingerprintUnchanged(relPath string, info os.FileInfo) bool {
	cached, ok := s.verifiedCache.Load(relPath)
	if !ok {
		return false
	}
	fp, ok := cached.(fileFingerprint)

	return ok && fp == fingerprintOf(info)
}

// fingerprintOf computes the (size, mtime) fingerprint of a file.
func fingerprintOf(info os.FileInfo) fileFingerprint {
	return fileFingerprint{size: info.Size(), modTimeNs: info.ModTime().UnixNano()}
}

// Server returns an http.Handler that serves the stored artifacts. It only
// permits GET/HEAD requests for regular files below the storage base path;
// directory listing is disabled to avoid enumerating artifacts across
// namespaces, and requests are counted/timed via the metrics recorder.
func (s *Storage) Server() http.Handler {
	fileServer := http.FileServer(noDirListingFS{http.Dir(s.basePath)})

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := &statusRecorder{ResponseWriter: w, status: http.StatusOK}

		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(sw, "method not allowed", http.StatusMethodNotAllowed)
			s.recordServe(sw.status, start)

			return
		}

		fileServer.ServeHTTP(sw, r)
		s.recordServe(sw.status, start)
	})
}

func (s *Storage) recordServe(status int, start time.Time) {
	if s.metrics == nil {
		return
	}
	s.metrics.ServeDurationSeconds.Observe(time.Since(start).Seconds())
	s.metrics.ServeRequestsTotal.WithLabelValues(statusClass(status)).Inc()
}

func statusClass(status int) string {
	switch {
	case status >= 500: //nolint:mnd // HTTP status class
		return "5xx"
	case status >= 400: //nolint:mnd // HTTP status class
		return "4xx"
	default:
		return "2xx"
	}
}

// statusRecorder captures the response status code for metrics.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

// noDirListingFS wraps an http.FileSystem and denies opening directories, so
// http.FileServer cannot render autoindex pages that would enumerate artifacts.
type noDirListingFS struct {
	fs http.FileSystem
}

func (n noDirListingFS) Open(name string) (http.File, error) {
	f, err := n.fs.Open(name)
	if err != nil {
		return nil, err
	}
	info, err := f.Stat()
	if err != nil {
		_ = f.Close()

		return nil, err
	}
	if info.IsDir() {
		_ = f.Close()

		return nil, os.ErrNotExist
	}

	return f, nil
}
