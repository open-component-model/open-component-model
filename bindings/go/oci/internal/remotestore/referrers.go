package remotestore

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
	slogcontext "github.com/veqryn/slog-context"
	"oras.land/oras-go/v2/registry/remote"
	"oras.land/oras-go/v2/registry/remote/errcode"
)

// Referrers lists the referrers of desc in repo like [remote.Repository.Referrers], but tolerates
// registries that answer the referrers API with 404 NAME_UNKNOWN for a repository that exists
// (xpkg.crossplane.io does). oras-go reads NAME_UNKNOWN as "repository not found" and fails instead
// of falling back to the referrers tag schema that the distribution spec prescribes for registries
// without the API. Here the lookup is retried against the tag schema, so referrers stay optional.
//
// onReferrers is called once per page of referrers found for desc. An error it returns stops the
// listing and is returned unchanged; it never triggers the tag schema retry.
func Referrers(ctx context.Context, repo *remote.Repository, desc ociImageSpecV1.Descriptor, artifactType string, onReferrers func(referrers []ociImageSpecV1.Descriptor) error) error {
	// oras-go returns callback errors through the same path as registry errors. Track them so a
	// NAME_UNKNOWN produced by the callback is not mistaken for the registry's answer.
	var callbackErr error
	trackedOnReferrers := func(referrers []ociImageSpecV1.Descriptor) error {
		callbackErr = onReferrers(referrers)
		return callbackErr
	}

	err := repo.Referrers(ctx, desc, artifactType, trackedOnReferrers)
	switch {
	case err == nil:
		return nil
	case callbackErr != nil:
		return err
	case !isNameUnknown(err):
		return fmt.Errorf("failed to list referrers of %s in %s: %w", desc.Digest, repo.Reference, err)
	}

	slogcontext.FromCtx(ctx).Log(ctx, slog.LevelDebug, "referrers API answered NAME_UNKNOWN, falling back to referrers tag schema",
		slog.String("repository", repo.Reference.String()), slog.String("digest", desc.Digest.String()))
	if err := tagSchemaRepository(ctx, repo).Referrers(ctx, desc, artifactType, trackedOnReferrers); err != nil {
		if callbackErr != nil {
			return err
		}
		return fmt.Errorf("failed to list referrers of %s in %s via referrers tag schema: %w", desc.Digest, repo.Reference, err)
	}
	return nil
}

// Predecessors is [remote.Repository.Predecessors] on top of [Referrers].
func Predecessors(ctx context.Context, repo *remote.Repository, desc ociImageSpecV1.Descriptor) ([]ociImageSpecV1.Descriptor, error) {
	var res []ociImageSpecV1.Descriptor
	if err := Referrers(ctx, repo, desc, "", func(referrers []ociImageSpecV1.Descriptor) error {
		res = append(res, referrers...)
		return nil
	}); err != nil {
		return nil, err
	}
	return res, nil
}

// tagSchemaRepository returns a copy of repo that always uses the referrers tag schema. repo itself
// keeps probing the API: NAME_UNKNOWN is also the correct answer for a repository that does not
// exist yet, and pinning the shared repo would make later pushes to it skip a working referrers API.
func tagSchemaRepository(ctx context.Context, repo *remote.Repository) *remote.Repository {
	fallback := &remote.Repository{
		Client:               repo.Client,
		Reference:            repo.Reference,
		PlainHTTP:            repo.PlainHTTP,
		ManifestMediaTypes:   repo.ManifestMediaTypes,
		TagListPageSize:      repo.TagListPageSize,
		ReferrerListPageSize: repo.ReferrerListPageSize,
		TagListMaxPages:      repo.TagListMaxPages,
		ReferrerListMaxPages: repo.ReferrerListMaxPages,
		MaxMetadataBytes:     repo.MaxMetadataBytes,
		SkipReferrersGC:      repo.SkipReferrersGC,
		HandleWarning:        repo.HandleWarning,
	}
	// The capability of a fresh repository is unset, so this only fails if oras-go changes that.
	// The listing then probes the API again and surfaces the registry's error, so logging suffices.
	if err := fallback.SetReferrersCapability(false); err != nil {
		slogcontext.FromCtx(ctx).Log(ctx, slog.LevelWarn, "failed to pin fallback repository to referrers tag schema",
			slog.String("repository", repo.Reference.String()), slog.Any("error", err))
	}
	return fallback
}

func isNameUnknown(err error) bool {
	var errResp *errcode.ErrorResponse
	if !errors.As(err, &errResp) || errResp.StatusCode != http.StatusNotFound {
		return false
	}
	for _, e := range errResp.Errors {
		if e.Code == errcode.ErrorCodeNameUnknown {
			return true
		}
	}
	return false
}
