package repositoryupload

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	helmaccessv1 "ocm.software/open-component-model/bindings/go/helm/spec/access/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
	uploadv1alpha1 "ocm.software/open-component-model/bindings/go/transfer/transformation/spec/v1alpha1"
)

// HelmServer is the server-specific part of a Helm chart upload, see [Uploader.UploadHelm].
type HelmServer interface {
	// Name is "artifactory" or "nexus", used in logs and errors.
	Name() string
	// HelmRepository is the published Helm/v1 helmRepository.
	HelmRepository() string
	// UploadURL is where the chart is stored; it identifies the upload in logs and credentials.
	UploadURL() string
	// DeployURL is where the chart is PUT: UploadURL plus server-specific request parameters.
	DeployURL() string
	// UploadHeader returns the upload request header; sha256Hex is empty when unknown.
	UploadHeader(sha256Hex string) http.Header
	// Claim checks that the upload location may be written for this resource. stored reports
	// that it already holds content with sha256Hex, so nothing needs to be written.
	Claim(ctx context.Context, c *Client, sha256Hex string) (stored bool, err error)
	// Reuse makes content the repository already stores under sha256Hex available without uploading it.
	Reuse(ctx context.Context, c *Client, sha256Hex string) (bool, error)
	// RejectedUploadStored reports whether the repository stores the content of an upload it rejected.
	RejectedUploadStored(ctx context.Context, c *Client, sha256Hex string) bool
	// Chart returns the chart name and version the server recorded for the stored content;
	// isChart=false when it recorded none, i.e. did not recognize the content as a chart.
	Chart(ctx context.Context, c *Client, sha256Hex string) (name, version string, isChart bool, err error)
	// Discard removes uploaded content that must not be published.
	Discard(ctx context.Context, c *Client, sha256Hex string) error
}

// UploadHelm uploads the packaged chart located in the resource content to a Helm repository and
// returns the resource with a Helm/v1 access on it. The chart is not parsed: its name and version
// are the chart metadata the server records for the uploaded chart. Where the chart is stored
// and how the metadata is read depends on the server, see [HelmServer].
func (u *Uploader) UploadHelm(ctx context.Context, c *Client, spec *uploadv1alpha1.RepositoryUploadSpec, src *descriptor.Resource, srv HelmServer) (*descriptor.Resource, error) {
	req, err := u.Open(ctx, spec, src)
	if err != nil {
		return nil, err
	}
	chart, err := u.Charts.Open(ctx, req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = chart.Close() }()

	expected, known, err := KnownDigest(src.Digest, chart.Archive, chart.FromOCI)
	if err != nil {
		return nil, err
	}

	// The upload location must be free, hold this resource's earlier upload, or already hold
	// the chart; nothing else is ever overwritten.
	reused, err := srv.Claim(ctx, c, known)
	if err != nil {
		return nil, err
	}
	if !reused && known != "" {
		if reused, err = srv.Reuse(ctx, c, known); err != nil {
			return nil, err
		}
	}
	digestHex := known
	uploadURL := RedactURL(srv.UploadURL())
	if reused {
		slog.InfoContext(ctx, "reused helm chart content already stored in the helm repository",
			"server", srv.Name(), "resource", src.ToIdentity(), "url", uploadURL)
	} else {
		computed, complete, err := UploadBlob(ctx, c, chart.Archive, srv.DeployURL(), srv.UploadHeader(known), nil)
		switch {
		case err != nil:
			// A repository that rejects redeploying a chart still stores its content, e.g.
			// from an earlier transfer of the same resource without a source digest.
			if known != "" || !complete || !srv.RejectedUploadStored(ctx, c, computed) {
				return nil, err
			}
			slog.InfoContext(ctx, "helm repository already stores the chart the upload was rejected for",
				"server", srv.Name(), "resource", src.ToIdentity(), "url", uploadURL)
		case known != "" && computed != known:
			if err := srv.Discard(ctx, c, computed); err != nil {
				slog.WarnContext(ctx, "failed removing uploaded content that must not be published", "url", uploadURL, "error", err)
			}
			return nil, fmt.Errorf("digest mismatch: expected %s, got %s", known, computed)
		default:
			slog.InfoContext(ctx, "uploaded helm chart", "server", srv.Name(), "resource", src.ToIdentity(), "url", uploadURL)
		}
		digestHex = computed
	}

	// The upload succeeded, so the server stores the content either way; isChart only
	// reports whether it recognized that content as a helm chart and recorded its metadata.
	name, version, isChart, err := srv.Chart(ctx, c, digestHex)
	if err != nil {
		return nil, err
	}
	if !isChart {
		// Remove the stored non-chart content again (Artifactory deletes the uploaded file;
		// Nexus cannot).
		if err := srv.Discard(ctx, c, digestHex); err != nil {
			slog.WarnContext(ctx, "failed removing uploaded content that must not be published", "url", uploadURL, "error", err)
		}
		return nil, fmt.Errorf("content of resource %s is not a helm chart: %s recorded no chart name and version for %s", src.ToIdentity(), srv.Name(), uploadURL)
	}
	if strings.ContainsAny(name, ":/") || strings.Contains(version, "/") {
		return nil, fmt.Errorf("%s recorded an invalid chart name %q or version %q for %s", srv.Name(), name, version, uploadURL)
	}

	out := src.DeepCopy()
	out.Access = &helmaccessv1.Helm{
		Type:           runtime.NewVersionedType(helmaccessv1.Type, helmaccessv1.Version),
		HelmRepository: srv.HelmRepository(),
		HelmChart:      name + ":" + version,
	}
	out.Digest = UploadedDigest(src.Digest, expected, digestHex)
	return out, nil
}
