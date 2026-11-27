package repository

import (
	"fmt"
	"io"
	"strings"

	"ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// isLegacyType reports the access type spellings OCM v1 builds a files-only archive
// for (git, git/v1alpha1, Git/v1alpha1). v1 keeps that archive so existing digests stay
// valid, and it carries file owners, so v2 cannot reproduce it: v2 archives the files
// together with the Git history.
func isLegacyType(t runtime.Type) bool {
	return t.GetName() == "git" || t.GetVersion() == "v1alpha1"
}

// legacyHint adds the legacy access type to a digest mismatch. The digest may have been
// recorded by OCM v1 or by OCM v2 (which keeps the legacy type), and the two cannot be told
// apart, so the hint is conditional: a mismatch can also mean the content changed.
func legacyHint(t runtime.Type, err error) error {
	if err == nil || !strings.Contains(err.Error(), "digest mismatch") {
		return err
	}
	return fmt.Errorf("%w: the resource uses the legacy git access type %q; if OCM v1 recorded its digest, the digest "+
		"covers a files-only archive that OCM v2 cannot reproduce, so re-add the resource with access type Git/v1; "+
		"otherwise the content does not match the recorded digest", err, t.String())
}

// legacyBlob wraps the errors of the reads of base with legacyHint.
type legacyBlob struct {
	base blob.ReadOnlyBlob
	typ  runtime.Type
}

var (
	_ blob.ReadOnlyBlob   = (*legacyBlob)(nil)
	_ blob.MediaTypeAware = (*legacyBlob)(nil)
	_ blob.DigestAware    = (*legacyBlob)(nil)
	_ blob.SizeAware      = (*legacyBlob)(nil)
)

func (b *legacyBlob) ReadCloser() (io.ReadCloser, error) {
	rc, err := b.base.ReadCloser()
	if err != nil {
		return nil, legacyHint(b.typ, err)
	}
	return &legacyReader{rc: rc, typ: b.typ}, nil
}

func (b *legacyBlob) MediaType() (string, bool) {
	if m, ok := b.base.(blob.MediaTypeAware); ok {
		return m.MediaType()
	}
	return "", false
}

func (b *legacyBlob) Digest() (string, bool) {
	if d, ok := b.base.(blob.DigestAware); ok {
		return d.Digest()
	}
	return "", false
}

func (b *legacyBlob) Size() int64 {
	if s, ok := b.base.(blob.SizeAware); ok {
		return s.Size()
	}
	return blob.SizeUnknown
}

type legacyReader struct {
	rc  io.ReadCloser
	typ runtime.Type
}

func (r *legacyReader) Read(p []byte) (int, error) {
	n, err := r.rc.Read(p)
	if err == io.EOF {
		return n, err
	}
	return n, legacyHint(r.typ, err)
}

func (r *legacyReader) Close() error { return legacyHint(r.typ, r.rc.Close()) }
