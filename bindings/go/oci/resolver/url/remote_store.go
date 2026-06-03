package url

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"

	"oras.land/oras-go/v2/errdef"
	"oras.land/oras-go/v2/registry/remote"
	"oras.land/oras-go/v2/registry/remote/auth"
	"oras.land/oras-go/v2/registry/remote/errcode"
)

// remoteStore wraps *remote.Repository and adds content.Untagger support.
// oras-go does not implement Untag for remote repositories; we issue the
// DELETE /v2/<name>/manifests/<tag> call defined by the OCI distribution spec directly.
type remoteStore struct {
	*remote.Repository
}

// Untag removes the given tag from the remote registry without deleting the underlying manifest.
// The registry must have tag deletion enabled; a 405 response means it is disabled server-side.
func (r *remoteStore) Untag(ctx context.Context, reference string) error {
	ref := r.Reference
	ref.Reference = reference
	ctx = auth.AppendRepositoryScope(ctx, ref, auth.ActionDelete)

	scheme := "https"
	if r.PlainHTTP {
		scheme = "http"
	}
	url := fmt.Sprintf("%s://%s/v2/%s/manifests/%s", scheme, ref.Host(), ref.Repository, reference)

	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, url, nil)
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
		err := resp.Body.Close()
		if err != nil {
			slog.Error("Failed to close response body for alias", "reference", reference, "err", err)
		}
	}()

	switch resp.StatusCode {
	case http.StatusOK, http.StatusAccepted, http.StatusNoContent:
		return nil
	case http.StatusNotFound:
		return errdef.ErrNotFound
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
