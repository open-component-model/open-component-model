package integration_test

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/opencontainers/go-digest"
	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/oci"
	urlresolver "ocm.software/open-component-model/bindings/go/oci/resolver/url"
)

// Test_Integration_WriteOnceNoReferrersAPI_Ownership covers the ownership
// referrer attach against a registry that (a) has no Referrers API, so oras
// falls back to the sha256-<subject> tag-schema index, and (b) is write-once
// for tags. Neither the CTF unit stores nor Nexus 3.96 (which supports the
// Referrers API) reach this path.
//
// It pins two contracts:
//   - re-attaching the identical referrer converges: oras detects the index is
//     unchanged and skips the tag write, so there is no redeploy 409 to swallow.
//   - attaching a different referrer to the same subject must surface the 409
//     from the index-tag update rather than be silently swallowed (the
//     regression CodeRabbit/@matthiasbruns flagged against pushIdempotent).
func Test_Integration_WriteOnceNoReferrersAPI_Ownership(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	const version = "1.0.0"

	srv := httptest.NewServer(newWriteOnceRegistry())
	t.Cleanup(srv.Close)
	host := strings.TrimPrefix(srv.URL, "http://")

	resolver, err := urlresolver.New(
		urlresolver.WithBaseURL(host),
		urlresolver.WithPlainHTTP(true),
		urlresolver.WithBaseClient(createAuthClient(host, "ignored", "ignored")),
	)
	require.NoError(t, err)
	repo, err := oci.NewRepository(oci.WithResolver(resolver), oci.WithTempDir(t.TempDir()))
	require.NoError(t, err)

	subject := pushOwnershipByReferenceImage(t, ctx, repo,
		fmt.Sprintf("%s/app/subject:%s", host, version), []byte("write-once-subject"))
	res := byReferenceResource("subject", version, subject)

	t.Run("identical re-attach converges without a 409", func(t *testing.T) {
		r := require.New(t)
		r.NoError(repo.AddOwnership(ctx, "ocm.software/owner-a", version, res, nil), "first attach must succeed")
		r.NoError(repo.AddOwnership(ctx, "ocm.software/owner-a", version, res, nil),
			"identical re-attach must converge: the referrers index is unchanged, so oras writes no tag")
		assertOwnershipReferrerCount(t, ctx, resolver, subject, 1)
	})

	t.Run("different owner on the same subject surfaces the index 409", func(t *testing.T) {
		r := require.New(t)
		err := repo.AddOwnership(ctx, "ocm.software/owner-b", version, res, nil)
		r.Error(err, "a changed referrers index must re-tag sha256-<subject>, which a write-once registry rejects")
		r.ErrorContains(err, "referrers index")
		// The pre-existing owner-a referrer must remain the only discoverable one.
		assertOwnershipReferrerCount(t, ctx, resolver, subject, 1)
	})
}

// writeOnceRegistry is a minimal OCI distribution registry for tests: blobs and
// manifests are content-addressed and idempotent by digest, but a manifest PUT
// to an existing *tag* is rejected (write-once, like a Nexus hosted repo with
// redeploy disabled). It has no Referrers API (GET /referrers/ -> 404), forcing
// oras onto the sha256-<subject> tag-schema fallback.
type writeOnceRegistry struct {
	mu        sync.Mutex
	blobs     map[digest.Digest][]byte
	manifests map[digest.Digest]storedManifest
	tags      map[string]digest.Digest // "<name>:<tag>" -> digest
}

type storedManifest struct {
	body        []byte
	contentType string
}

func newWriteOnceRegistry() *writeOnceRegistry {
	return &writeOnceRegistry{
		blobs:     map[digest.Digest][]byte{},
		manifests: map[digest.Digest]storedManifest{},
		tags:      map[string]digest.Digest{},
	}
}

func (reg *writeOnceRegistry) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/v2/")
	switch {
	case path == "" || path == "/":
		w.WriteHeader(http.StatusOK)
	case strings.Contains(path, "/blobs/uploads"):
		reg.handleUpload(w, r, path)
	case strings.Contains(path, "/referrers/"):
		// No Referrers API: force the tag-schema fallback.
		w.WriteHeader(http.StatusNotFound)
	case strings.Contains(path, "/blobs/"):
		_, dgst := splitRef(path, "/blobs/")
		reg.handleBlob(w, r, digest.Digest(dgst))
	case strings.Contains(path, "/manifests/"):
		name, ref := splitRef(path, "/manifests/")
		reg.handleManifest(w, r, name, ref)
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

func (reg *writeOnceRegistry) handleUpload(w http.ResponseWriter, r *http.Request, path string) {
	name := path[:strings.Index(path, "/blobs/uploads")]
	switch r.Method {
	case http.MethodPost:
		w.Header().Set("Location", fmt.Sprintf("/v2/%s/blobs/uploads/upload", name))
		w.WriteHeader(http.StatusAccepted)
	case http.MethodPut:
		dgst := digest.Digest(r.URL.Query().Get("digest"))
		body, _ := io.ReadAll(r.Body)
		reg.mu.Lock()
		reg.blobs[dgst] = body
		reg.mu.Unlock()
		w.Header().Set("Docker-Content-Digest", dgst.String())
		w.Header().Set("Location", fmt.Sprintf("/v2/%s/blobs/%s", name, dgst))
		w.WriteHeader(http.StatusCreated)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (reg *writeOnceRegistry) handleBlob(w http.ResponseWriter, r *http.Request, dgst digest.Digest) {
	reg.mu.Lock()
	body, ok := reg.blobs[dgst]
	reg.mu.Unlock()
	if !ok {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	w.Header().Set("Docker-Content-Digest", dgst.String())
	w.Header().Set("Content-Type", "application/octet-stream")
	if r.Method == http.MethodHead {
		w.Header().Set("Content-Length", fmt.Sprintf("%d", len(body)))
		w.WriteHeader(http.StatusOK)
		return
	}
	_, _ = w.Write(body)
}

func (reg *writeOnceRegistry) handleManifest(w http.ResponseWriter, r *http.Request, name, ref string) {
	switch r.Method {
	case http.MethodPut:
		body, _ := io.ReadAll(r.Body)
		dgst := digest.FromBytes(body)
		ct := r.Header.Get("Content-Type")
		reg.mu.Lock()
		defer reg.mu.Unlock()
		if !strings.HasPrefix(ref, "sha256:") {
			// Tag write: write-once. Reject re-tagging an existing tag.
			key := name + ":" + ref
			if _, exists := reg.tags[key]; exists {
				w.WriteHeader(http.StatusConflict)
				_, _ = w.Write([]byte(fmt.Sprintf("redeploy disabled: %s cannot be updated as asset already exists", ref)))
				return
			}
			reg.tags[key] = dgst
		}
		reg.manifests[dgst] = storedManifest{body: body, contentType: ct}
		w.Header().Set("Docker-Content-Digest", dgst.String())
		w.WriteHeader(http.StatusCreated)
	case http.MethodGet, http.MethodHead:
		reg.mu.Lock()
		dgst := digest.Digest(ref)
		if !strings.HasPrefix(ref, "sha256:") {
			d, ok := reg.tags[name+":"+ref]
			if !ok {
				reg.mu.Unlock()
				w.WriteHeader(http.StatusNotFound)
				return
			}
			dgst = d
		}
		m, ok := reg.manifests[dgst]
		reg.mu.Unlock()
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Docker-Content-Digest", dgst.String())
		w.Header().Set("Content-Type", m.contentType)
		w.Header().Set("Content-Length", fmt.Sprintf("%d", len(m.body)))
		if r.Method == http.MethodHead {
			w.WriteHeader(http.StatusOK)
			return
		}
		_, _ = w.Write(m.body)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

// splitRef splits "<name><sep><ref>" at the last occurrence of sep.
func splitRef(path, sep string) (name, ref string) {
	i := strings.LastIndex(path, sep)
	return path[:i], path[i+len(sep):]
}
