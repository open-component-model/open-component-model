package remotestore

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"path"

	"oras.land/oras-go/v2/content"
	"oras.land/oras-go/v2/errdef"
	"oras.land/oras-go/v2/registry/remote"
	"oras.land/oras-go/v2/registry/remote/auth"
	"oras.land/oras-go/v2/registry/remote/errcode"

	"ocm.software/open-component-model/bindings/go/oci/spec"
)

// ErrTagDeletionDisabled is returned when the registry responds with 405 Method Not Allowed
// to a tag-deletion request, indicating that the registry does not support tag deletion
// (e.g. REGISTRY_STORAGE_DELETE_ENABLED is not set).
var ErrTagDeletionDisabled = fmt.Errorf("registry does not support tag deletion (405 Method Not Allowed)")

// RemoteStore wraps *remote.Repository and adds content.Untagger support.
//
// oras-go implements content.Untagger only for its local OCI layout store
// (content/oci.Store). The remote registry client (registry/remote.Repository)
// intentionally omits it: the OCI Distribution Spec treats
// DELETE /v2/<name>/manifests/<tag> as optional, and not all registries honor it.
type RemoteStore struct {
	*remote.Repository
}

var (
	_ spec.Store       = (*RemoteStore)(nil) // general store spec
	_ content.Untagger = (*RemoteStore)(nil) // content.Untagger opt-in
)

// Untag removes the given tag from the remote registry without deleting the underlying manifest.
// The registry must have tag deletion enabled; a 405 response means it is disabled server-side.
func (r *RemoteStore) Untag(ctx context.Context, reference string) error {
	ref := r.Reference
	ref.Reference = reference
	if err := ref.ValidateReferenceAsTag(); err != nil {
		return fmt.Errorf("invalid tag reference %q: %w", reference, err)
	}
	ctx = auth.AppendRepositoryScope(ctx, ref, auth.ActionDelete)

	scheme := "https"
	if r.PlainHTTP {
		scheme = "http"
	}
	endpoint := &url.URL{
		Scheme: scheme,
		Host:   ref.Host(),
		Path:   path.Join("/v2", ref.Repository, "manifests", reference),
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, endpoint.String(), http.NoBody)
	if err != nil {
		return fmt.Errorf("failed to build delete request for alias %q: %w", reference, err)
	}

	client := r.Client
	if client == nil {
		client = auth.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to delete alias %q: %w", reference, err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			slog.Error("Failed to close response body for alias", "reference", reference, "err", err)
		}
	}()

	switch resp.StatusCode {
	case http.StatusOK, http.StatusAccepted, http.StatusNoContent:
		return nil
	case http.StatusNotFound:
		return errdef.ErrNotFound
	case http.StatusMethodNotAllowed:
		return ErrTagDeletionDisabled
	default:
		errResp := &errcode.ErrorResponse{
			Method:     resp.Request.Method,
			URL:        resp.Request.URL,
			StatusCode: resp.StatusCode,
		}
		var body struct {
			Errors errcode.Errors `json:"errors"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&body); err == nil {
			errResp.Errors = body.Errors
		}
		return errResp
	}
}
