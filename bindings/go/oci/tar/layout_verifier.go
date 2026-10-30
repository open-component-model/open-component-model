package tar

import (
	"archive/tar"
	"bufio"
	"compress/gzip"
	"context"
	"encoding/json"
	"encoding/json/jsontext"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"path"
	"slices"
	"strings"

	"github.com/opencontainers/go-digest"
	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
	"oras.land/oras-go/v2/errdef"

	"ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/ctf"
	"ocm.software/open-component-model/bindings/go/oci/spec/annotations"
	"ocm.software/open-component-model/bindings/go/oci/spec/layout"
)

const (
	// annotationMainArtifact is set by legacy artifact sets on their index to the
	// digest of the artifact they hold.
	annotationMainArtifact = "software.ocm/main"
	// jsonTokenKeep is how much of a string or number token, and of a whitespace
	// run, reaches the decoder, which buffers each token whole. Every token whose
	// value verification compares (digests, media types, sizes, the few annotation
	// values it reads) is far shorter; a longer one keeps at least this many bytes,
	// so it still cannot equal any of them.
	jsonTokenKeep = 4 << 10
)

// jsonOptions decode like encoding/json, which consumers of a layout use, in
// everything that decides which members fill which fields. They are not the full
// [json.DefaultOptionsV1], which buffers every skipped value whole.
var jsonOptions = jsonv2.JoinOptions(
	jsonv2.MatchCaseInsensitiveNames(true),
	json.MatchCaseSensitiveDelimiter(true),
	json.MergeWithLegacySemantics(true),
	jsontext.AllowDuplicateNames(true),
	jsontext.AllowInvalidUTF8(true),
)

// LayoutVerifier verifies an OCI layout or a legacy artifact set (see
// [ctf.IsArtifactSetMediaType]) while it is read: that the artifact it holds has the
// expected root digest, and that every blob of that artifact's graph is present and
// matches its digest. Blobs are hashed while a streaming JSON decoder reads them,
// with tokens truncated to [jsonTokenKeep]; of manifests and indexes only the
// media type, digest and size of the descriptors they reference are retained.
// Memory therefore grows with the number of descriptors, never with blob size.
//
// It implements the ArtifactVerifier of package repository/integrity.
type LayoutVerifier struct{}

// Accepts reports whether mediaType is that of an OCI layout or a legacy artifact set.
func (LayoutVerifier) Accepts(mediaType string) bool {
	return layout.IsOCILayoutMediaType(mediaType) || ctf.IsArtifactSetMediaType(mediaType)
}

// Verify returns rc wrapped so that it serves the archive unchanged and fails, once
// the archive is read to its end, if it does not hold the artifact root. Closing the
// reader before its end reads the rest of the archive to complete the verification.
// size is the byte size of the archive or [blob.SizeUnknown]; when known, the read
// that reaches it already reports the verification result, so consumers reading
// exactly size bytes see a failure before they close.
func (LayoutVerifier) Verify(rc io.ReadCloser, root digest.Digest, size int64) io.ReadCloser {
	return newLayoutVerifier(rc, root, size)
}

func newLayoutVerifier(rc io.ReadCloser, root digest.Digest, size int64) *layoutVerifier {
	pr, pw := io.Pipe()
	v := &layoutVerifier{base: rc, pw: pw, size: size, result: make(chan error, 1)}
	go func() {
		err := inspectLayout(pr, root)
		_ = pr.CloseWithError(err)
		v.result <- err
	}()
	return v
}

// layoutVerifier tees everything read from base to an inspector reading the
// archive from the other end of a pipe.
type layoutVerifier struct {
	base   io.ReadCloser
	pw     *io.PipeWriter
	result chan error
	// size is the expected archive size, or negative if unknown.
	size int64
	// read counts the bytes read from base.
	read int64

	// err is sticky: the first failure of reading or verifying.
	err error
	// readErr is the error base failed with, if any.
	readErr error
	// eof is set once base is read to its end and the archive verified.
	eof bool
	// collected is set once the inspector reported its result.
	collected bool
}

func (v *layoutVerifier) Read(p []byte) (int, error) {
	if v.err != nil {
		return 0, v.err
	}
	if v.eof {
		return 0, io.EOF
	}

	n, err := v.base.Read(p)
	v.read += int64(n)
	if v.size >= 0 && v.read > v.size {
		v.err = fmt.Errorf("layout exceeds its size of %d bytes", v.size)
		_ = v.pw.CloseWithError(v.err)
		_ = v.collect()
		return n, v.err
	}
	if n > 0 {
		if _, writeErr := v.pw.Write(p[:n]); writeErr != nil {
			// the inspector gave up on the archive, it reports why.
			v.err = v.collect()
			if v.err == nil {
				v.err = writeErr
			}
			return n, v.err
		}
	}
	if err == nil && v.size >= 0 && v.read == v.size {
		// The archive is complete at size; base is not read further but closed in Close.
		_ = v.pw.Close()
		if v.err = v.collect(); v.err != nil {
			return n, v.err
		}
		v.eof = true
		return n, nil
	}

	switch {
	case errors.Is(err, io.EOF):
		_ = v.pw.Close()
		if v.err = v.collect(); v.err != nil {
			return n, v.err
		}
		v.eof = true
		return n, io.EOF
	case err != nil:
		_ = v.pw.CloseWithError(err)
		_ = v.collect()
		v.readErr, v.err = err, err
		return n, err
	}
	return n, nil
}

// Close completes the verification of an archive that was not read to its end (e.g.
// by a tar reader that stops at the end marker) and closes base.
func (v *layoutVerifier) Close() error {
	if v.err == nil && !v.eof {
		_, _ = blob.CopyReader(io.Discard, v)
		if v.readErr != nil {
			v.err = fmt.Errorf("layout verification incomplete: %w", v.readErr)
		}
	}
	if !v.collected {
		_ = v.pw.CloseWithError(errors.New("layout reader closed"))
		_ = v.collect()
	}
	return errors.Join(v.err, v.base.Close())
}

func (v *layoutVerifier) collect() error {
	if v.collected {
		return nil
	}
	v.collected = true
	return <-v.result
}

// inspectLayout reads an OCI layout or legacy artifact set tar, optionally gzipped,
// from r to its end and verifies that it holds the complete artifact root.
func inspectLayout(r io.Reader, root digest.Digest) error {
	buffered := bufio.NewReader(r)
	var archive io.Reader = buffered
	if magic, _ := buffered.Peek(2); len(magic) == 2 && magic[0] == 0x1F && magic[1] == 0x8B {
		gz, err := gzip.NewReader(buffered)
		if err != nil {
			return fmt.Errorf("failed to read gzip header of layout: %w", err)
		}
		defer gz.Close()
		archive = gz
	}

	ins := &layoutInspection{
		names:    make(map[string]struct{}),
		indexes:  make(map[string]*layoutIndex, 1),
		seen:     make(map[digest.Digest]int64),
		retained: make(retainedGraph),
	}
	tr := tar.NewReader(archive)
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return fmt.Errorf("failed to read layout: %w", err)
		}
		if err := ins.entry(hdr, tr); err != nil {
			return err
		}
	}
	// What follows the end marker (padding, the gzip trailer) is read as well, so
	// that a corrupt gzip checksum surfaces and the whole archive passes through.
	if _, err := blob.CopyReader(io.Discard, archive); err != nil {
		return fmt.Errorf("failed to read layout: %w", err)
	}
	if _, err := blob.CopyReader(io.Discard, buffered); err != nil {
		return fmt.Errorf("failed to read layout: %w", err)
	}

	return ins.verify(context.Background(), root)
}

type layoutInspection struct {
	// names holds the entries read, to refuse ambiguous archives.
	names map[string]struct{}
	// indexes holds index.json and artifact-descriptor.json.
	indexes map[string]*layoutIndex
	// seen maps the digests of all blobs read to their size.
	seen map[digest.Digest]int64
	// retained holds the decoded blobs that may be manifests or indexes.
	retained retainedGraph
	// otherEntries counts the files that are neither index nor blob nor oci-layout,
	// and otherSample holds the first maxLoggedUntracked of them for the warning.
	otherEntries int
	otherSample  []string
	// dec and truncator are reused to decode each entry.
	dec       jsontext.Decoder
	truncator jsonTruncator
}

func (ins *layoutInspection) entry(hdr *tar.Header, r io.Reader) error {
	if hdr.Typeflag == tar.TypeDir {
		return nil
	}
	// Names are resolved like go-tarfs (used by ReadOCILayout) resolves them, so that
	// aliases such as .//index.json, which it serves in place of an earlier entry,
	// count as repeated entries. Rooting the name first also maps absolute and
	// escaping names onto the layout root instead of letting them pass unchecked.
	name := strings.TrimPrefix(path.Clean("/"+hdr.Name), "/")
	isIndex := name == ociImageSpecV1.ImageIndexFile || name == ctf.ArtifactSetDescriptorFileName
	blobName, isBlob := strings.CutPrefix(name, ctf.BlobsDirectoryName+"/")
	if !isIndex && !isBlob {
		if name != ociImageSpecV1.ImageLayoutFile {
			ins.otherEntries++
			if len(ins.otherSample) < maxLoggedUntracked {
				ins.otherSample = append(ins.otherSample, name)
			}
		}
		return nil
	}
	if hdr.Typeflag != tar.TypeReg {
		return fmt.Errorf("layout entry %s is not a regular file", hdr.Name)
	}
	if _, repeated := ins.names[name]; repeated {
		return fmt.Errorf("layout contains entry %s more than once", hdr.Name)
	}
	ins.names[name] = struct{}{}

	if isIndex {
		var idx layoutIndex
		if err := ins.decodeJSON(r, &idx); err != nil {
			return fmt.Errorf("failed to decode layout index %s: %w", name, err)
		}
		ins.indexes[name] = &idx
		return nil
	}
	return ins.blob(hdr, blobName, r)
}

// blob verifies a blob stored as blobs/<algorithm>/<encoded> (OCI layout) or
// blobs/<algorithm>.<encoded> (legacy artifact set) against the digest its name gives.
func (ins *layoutInspection) blob(hdr *tar.Header, blobName string, r io.Reader) error {
	var dig digest.Digest
	if algorithm, encoded, nested := strings.Cut(blobName, "/"); nested {
		dig = digest.NewDigestFromEncoded(digest.Algorithm(algorithm), encoded)
	} else {
		dig = digest.Digest(ctf.ToDigest(blobName))
	}
	if err := dig.Validate(); err != nil {
		if errors.Is(err, digest.ErrDigestUnsupported) {
			return fmt.Errorf("layout entry %s uses unsupported digest algorithm %s", hdr.Name, dig.Algorithm())
		}
		return fmt.Errorf("layout entry %s does not name a valid digest: %w", hdr.Name, err)
	}

	hash := dig.Algorithm().Digester()
	hashed := io.TeeReader(r, hash.Hash())
	// Every blob is offered to the decoder, which gives up on anything that is not
	// JSON at its first byte. What it leaves unread is only hashed.
	var node graphNode
	retain := ins.decodeJSON(hashed, &node) == nil && node.SchemaVersion == 2
	if retain {
		// encoding/json refuses trailing data, so consumers could not read the blob.
		_, err := ins.dec.ReadToken()
		retain = errors.Is(err, io.EOF)
	}
	if _, err := blob.CopyReader(io.Discard, hashed); err != nil {
		return fmt.Errorf("failed to read layout entry %s: %w", hdr.Name, err)
	}
	if got := hash.Digest(); got != dig {
		return fmt.Errorf("layout entry %s does not match its digest: got %s", hdr.Name, got)
	}

	ins.seen[dig] = hdr.Size
	if retain {
		ins.retained[dig] = &node
	}
	return nil
}

// decodeJSON decodes the next JSON value of r into out, reading through
// ins.truncator so that the decoder never buffers a long token.
func (ins *layoutInspection) decodeJSON(r io.Reader, out any) error {
	ins.truncator = jsonTruncator{r: r}
	ins.dec.Reset(&ins.truncator, jsonOptions)
	return jsonv2.UnmarshalDecode(&ins.dec, out)
}

// jsonTruncator passes JSON through, but keeps only the first jsonTokenKeep bytes
// of every string and number and collapses whitespace runs to one byte. Valid JSON
// stays valid with the same structure; only the values of overlong tokens change.
// Invalid input passes through to fail the decoder.
type jsonTruncator struct {
	r io.Reader
	// run is the length of the current string or number.
	run                         int
	inString, inNumber, inSpace bool
	// escape is -1 after a backslash in a string, the hex digits left of a \u
	// escape, or 0; keepEscape tells whether the escape is passed on.
	escape     int
	keepEscape bool
	// needDigit is set after a number's sign, point or exponent, which a digit
	// must follow to keep the number valid.
	needDigit bool
}

func (t *jsonTruncator) Read(p []byte) (int, error) {
	for {
		n, err := t.r.Read(p)
		kept := 0
		for _, c := range p[:n] {
			if t.keep(c) {
				p[kept] = c
				kept++
			}
		}
		if kept > 0 || err != nil {
			return kept, err
		}
	}
}

func (t *jsonTruncator) keep(c byte) bool {
	if t.inString {
		switch {
		case t.escape == -1:
			t.escape = 0
			if c == 'u' {
				t.escape = 4
			}
			return t.keepEscape
		case t.escape > 0:
			t.escape--
			return t.keepEscape
		case c == '"':
			t.inString = false
			return true
		}
		keep := t.run < jsonTokenKeep
		t.run++
		if c == '\\' {
			t.escape, t.keepEscape = -1, keep
		}
		return keep
	}

	isDigit := c >= '0' && c <= '9'
	if t.inNumber {
		switch {
		case isDigit:
			keep := t.run < jsonTokenKeep || t.needDigit
			t.run++
			t.needDigit = false
			return keep
		case c == '.', c == 'e', c == 'E', c == '+', c == '-':
			t.needDigit = true
			return true
		}
		t.inNumber = false
	}
	if c == ' ' || c == '\t' || c == '\n' || c == '\r' {
		keep := !t.inSpace
		t.inSpace = true
		return keep
	}
	t.inSpace = false
	switch {
	case c == '"':
		t.inString, t.run = true, 0
	case isDigit || c == '-':
		t.inNumber, t.run, t.needDigit = true, 1, c == '-'
	}
	return true
}

// verify checks, once the whole archive is read, that its main artifact is root and
// that every blob of root's graph was read with the size the graph references.
func (ins *layoutInspection) verify(ctx context.Context, root digest.Digest) error {
	idx, ok := ins.indexes[ociImageSpecV1.ImageIndexFile]
	if !ok {
		if idx, ok = ins.indexes[ctf.ArtifactSetDescriptorFileName]; !ok {
			return fmt.Errorf("layout has no %s or %s", ociImageSpecV1.ImageIndexFile, ctf.ArtifactSetDescriptorFileName)
		}
	}
	manifests := make([]ociImageSpecV1.Descriptor, len(idx.Manifests))
	for i, manifest := range idx.Manifests {
		if manifest.MediaType == "" || manifest.Size <= 0 {
			return fmt.Errorf("layout index entry %s has no media type or size", manifest.Digest)
		}
		manifests[i] = manifest.descriptor()
	}

	top, err := selectRoot(ctx, ins.retained.edges, manifests)
	if err != nil {
		return err
	}
	if top.Digest != root {
		return fmt.Errorf("layout main artifact is %s, expected %s", top.Digest, root)
	}
	// Consumers may pick the main artifact through TopLevelArtifacts rather than
	// selectRoot; both must agree, or a consumer could unpack an unverified artifact.
	for _, manifest := range manifests {
		if isManifestMediaType(manifest.MediaType) {
			if _, ok := ins.retained[manifest.Digest]; !ok {
				return fmt.Errorf("layout manifest %s listed in the index cannot be inspected", manifest.Digest)
			}
		}
	}
	if top := topLevelArtifacts(ctx, ins.retained.edges, manifests); len(top) == 1 && top[0].Digest != root {
		return fmt.Errorf("layout main artifact is %s, expected %s", top[0].Digest, root)
	}
	if main := idx.Annotations[annotationMainArtifact]; main != "" && main != root.String() {
		return fmt.Errorf("layout annotation %s names %s, expected %s", annotationMainArtifact, main, root)
	}

	visited := make(map[digest.Digest]struct{}, len(ins.seen))
	pending := []ociImageSpecV1.Descriptor{top}
	for len(pending) > 0 {
		node := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		if _, done := visited[node.Digest]; done {
			continue
		}
		visited[node.Digest] = struct{}{}

		size, ok := ins.seen[node.Digest]
		if !ok {
			return fmt.Errorf("layout blob %s referenced by the artifact graph is missing", node.Digest)
		}
		if size != node.Size {
			return fmt.Errorf("layout blob %s has size %d, but the artifact graph references %d bytes", node.Digest, size, node.Size)
		}
		children, err := ins.retained.successors(node)
		if err != nil {
			return fmt.Errorf("layout manifest %s cannot be read: %w", node.Digest, err)
		}
		pending = append(pending, children...)
	}
	ins.warnUntracked(ctx, root, manifests, visited)
	return nil
}

// maxLoggedUntracked bounds the names a warning about untracked content lists.
const maxLoggedUntracked = 10

// warnUntracked logs the content of a verified layout that nothing tracks: blobs that
// neither the main artifact nor any other manifest of the index reaches, and files
// outside the layout structure. Consumers ignore such content, but it may hide data
// that was not meant to be shipped. visited holds the main artifact's graph.
func (ins *layoutInspection) warnUntracked(ctx context.Context, root digest.Digest, manifests []ociImageSpecV1.Descriptor, visited map[digest.Digest]struct{}) {
	// Other index entries need not be complete; what of them is present is tracked.
	pending := slices.Clone(manifests)
	for len(pending) > 0 {
		node := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		if _, done := visited[node.Digest]; done {
			continue
		}
		visited[node.Digest] = struct{}{}
		if children, err := ins.retained.successors(node); err == nil {
			pending = append(pending, children...)
		}
	}

	var unreferenced []string
	for dig := range ins.seen {
		if _, ok := visited[dig]; !ok {
			unreferenced = append(unreferenced, dig.String())
		}
	}
	if len(unreferenced) == 0 && ins.otherEntries == 0 {
		return
	}
	slices.Sort(unreferenced)
	slog.WarnContext(ctx, "layout holds content that neither its index nor its main artifact references",
		slog.String("root", root.String()),
		slog.Int("unreferencedBlobs", len(unreferenced)),
		slog.Any("unreferencedBlobSample", unreferenced[:min(len(unreferenced), maxLoggedUntracked)]),
		slog.Int("otherFiles", ins.otherEntries),
		slog.Any("otherFileSample", ins.otherSample),
	)
}

// mediaTypeArtifactManifest is the withdrawn OCI artifact manifest, which
// [content.Successors] still follows.
const mediaTypeArtifactManifest = "application/vnd.oci.artifact.manifest.v1+json"

// layoutIndex is what verification reads of index.json or artifact-descriptor.json.
type layoutIndex struct {
	Manifests   []indexEntry    `json:"manifests"`
	Annotations keptAnnotations `json:"annotations"`
}

// indexEntry is a descriptor in a layoutIndex.
type indexEntry struct {
	graphDescriptor
	Annotations keptAnnotations `json:"annotations"`
}

func (e indexEntry) descriptor() ociImageSpecV1.Descriptor {
	desc := e.graphDescriptor.descriptor()
	desc.Annotations = e.Annotations
	return desc
}

// keptAnnotations holds only the annotations verification reads, so that an
// index with many annotations does not cost memory.
type keptAnnotations map[string]string

func (a *keptAnnotations) UnmarshalJSONFrom(dec *jsontext.Decoder) error {
	tok, err := dec.ReadToken()
	if err != nil {
		return err
	}
	switch tok.Kind() {
	case 'n':
		*a = nil
		return nil
	case '{':
	default:
		return fmt.Errorf("annotations must be an object, got %s", tok.Kind())
	}
	for dec.PeekKind() != '}' {
		name, err := dec.ReadToken()
		if err != nil {
			return err
		}
		switch key := name.String(); key {
		case annotationMainArtifact, annotations.OCMLayoutRoot, ociImageSpecV1.AnnotationRefName:
			value, err := dec.ReadToken()
			if err != nil {
				return err
			}
			if value.Kind() != '"' {
				return fmt.Errorf("annotation %s must be a string, got %s", key, value.Kind())
			}
			if *a == nil {
				*a = make(keptAnnotations, 1)
			}
			(*a)[key] = value.String()
		default:
			if err := dec.SkipValue(); err != nil {
				return err
			}
		}
	}
	_, err = dec.ReadToken()
	return err
}

// graphNode is what verification reads of an OCI or Docker manifest or index: its
// schema version, which all of them declare as 2, and the descriptors it references.
type graphNode struct {
	SchemaVersion int               `json:"schemaVersion"`
	Config        graphDescriptor   `json:"config"`
	Layers        []graphDescriptor `json:"layers"`
	Manifests     []graphDescriptor `json:"manifests"`
	Blobs         []graphDescriptor `json:"blobs"`
	Subject       *graphDescriptor  `json:"subject"`
}

// graphDescriptor holds the fields of a descriptor that verification reads, so
// that memory grows with the number of descriptors, not with what they carry.
type graphDescriptor struct {
	MediaType string        `json:"mediaType"`
	Digest    digest.Digest `json:"digest"`
	Size      int64         `json:"size"`
}

func (d graphDescriptor) descriptor() ociImageSpecV1.Descriptor {
	return ociImageSpecV1.Descriptor{MediaType: d.MediaType, Digest: d.Digest, Size: d.Size}
}

func descriptors(nodes []ociImageSpecV1.Descriptor, ds []graphDescriptor) []ociImageSpecV1.Descriptor {
	for _, d := range ds {
		nodes = append(nodes, d.descriptor())
	}
	return nodes
}

func (n *graphNode) subject() *ociImageSpecV1.Descriptor {
	if n.Subject == nil {
		return nil
	}
	subject := n.Subject.descriptor()
	return &subject
}

func isManifestMediaType(mediaType string) bool {
	switch mediaType {
	case ociImageSpecV1.MediaTypeImageManifest, ociImageSpecV1.MediaTypeImageIndex,
		mediaTypeDockerManifest, mediaTypeDockerManifestList:
		return true
	}
	return false
}

// retainedGraph holds the manifests and indexes retained from a layout, decoded.
// Their raw bytes are gone, so it answers what [content.Successors] and
// [extractSubjectAndSuccessors] would from them.
type retainedGraph map[digest.Digest]*graphNode

func (g retainedGraph) node(desc ociImageSpecV1.Descriptor) (*graphNode, error) {
	node, ok := g[desc.Digest]
	if !ok {
		return nil, fmt.Errorf("%s: %w", desc.Digest, errdef.ErrNotFound)
	}
	return node, nil
}

// successors mirrors [content.Successors].
func (g retainedGraph) successors(desc ociImageSpecV1.Descriptor) ([]ociImageSpecV1.Descriptor, error) {
	switch desc.MediaType {
	case mediaTypeDockerManifest, ociImageSpecV1.MediaTypeImageManifest,
		mediaTypeDockerManifestList, ociImageSpecV1.MediaTypeImageIndex, mediaTypeArtifactManifest:
	default:
		return nil, nil
	}
	node, err := g.node(desc)
	if err != nil {
		return nil, err
	}
	var nodes []ociImageSpecV1.Descriptor
	if node.Subject != nil && desc.MediaType != mediaTypeDockerManifest && desc.MediaType != mediaTypeDockerManifestList {
		nodes = append(nodes, node.Subject.descriptor())
	}
	switch desc.MediaType {
	case mediaTypeDockerManifest, ociImageSpecV1.MediaTypeImageManifest:
		nodes = append(nodes, node.Config.descriptor())
		return descriptors(nodes, node.Layers), nil
	case mediaTypeArtifactManifest:
		return descriptors(nodes, node.Blobs), nil
	default:
		return descriptors(nodes, node.Manifests), nil
	}
}

// edges mirrors [extractSubjectAndSuccessors].
func (g retainedGraph) edges(_ context.Context, desc ociImageSpecV1.Descriptor) (*ociImageSpecV1.Descriptor, []ociImageSpecV1.Descriptor, error) {
	switch desc.MediaType {
	case ociImageSpecV1.MediaTypeImageManifest, ociImageSpecV1.MediaTypeImageIndex:
		node, err := g.node(desc)
		if err != nil {
			return nil, nil, err
		}
		if desc.MediaType == ociImageSpecV1.MediaTypeImageIndex {
			return node.subject(), descriptors(nil, node.Manifests), nil
		}
		return node.subject(), descriptors([]ociImageSpecV1.Descriptor{node.Config.descriptor()}, node.Layers), nil
	case mediaTypeDockerManifest, mediaTypeDockerManifestList:
		successors, err := g.successors(desc)
		return nil, successors, err
	}
	return nil, nil, nil
}
