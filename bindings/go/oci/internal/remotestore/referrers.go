package remotestore

import (
	"context"
	"errors"
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
func Referrers(ctx context.Context, repo *remote.Repository, desc ociImageSpecV1.Descriptor, artifactType string, fn func(referrers []ociImageSpecV1.Descriptor) error) error {
	err := repo.Referrers(ctx, desc, artifactType, fn)
	if !isNameUnknown(err) {
		return err
	}
	slogcontext.FromCtx(ctx).Log(ctx, slog.LevelDebug, "referrers API answered NAME_UNKNOWN, falling back to referrers tag schema",
		slog.String("repository", repo.Reference.String()), slog.String("digest", desc.Digest.String()))
	return tagSchemaRepository(repo).Referrers(ctx, desc, artifactType, fn)
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
func tagSchemaRepository(repo *remote.Repository) *remote.Repository {
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
	// Cannot fail: the capability of a fresh repository is unset.
	_ = fallback.SetReferrersCapability(false)
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
