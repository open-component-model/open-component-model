package repository

import (
	"context"
	"fmt"
	"net/http"

	"ocm.software/open-component-model/bindings/go/blob"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/repository"
	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/bindings/go/wget/checksum"
	"ocm.software/open-component-model/bindings/go/wget/checksum/httpverify"
	"ocm.software/open-component-model/bindings/go/wget/internal/download"
	accessspec "ocm.software/open-component-model/bindings/go/wget/spec/access"
	v1 "ocm.software/open-component-model/bindings/go/wget/spec/access/v1"
)

// Assert that the wget resource repository offers the streaming capability.
var _ repository.StreamingResourceRepository = (*ResourceRepository)(nil)

// DownloadResourceStream returns a lazy, replayable blob for a wget resource that
// streams the HTTP body on demand instead of materializing it to a temporary file.
// It is the streaming counterpart of [ResourceRepository.DownloadResource] and
// backs the by-value transfer path to a remote target.
//
// Both verification legs of DownloadResource are preserved, inline and
// stream-then-verify. The configured checksum policy runs over the streamed bytes
// once the body ends: under Require and Prefer the source-advertised checksum is
// verified, and Skip performs no policy verification. The resource digest is
// verified by wrapping the lazy blob with [repository.VerifyDownload], which checks
// the content against the descriptor digest as it is read. A mismatch on either leg
// fails the read and the close, so a corrupted transfer still fails even though the
// body has already been forwarded to the target.
func (r *ResourceRepository) DownloadResourceStream(ctx context.Context, resource *descriptor.Resource, credentials runtime.Typed) (blob.ReadOnlyBlob, error) {
	if resource == nil {
		return nil, fmt.Errorf("resource is required")
	}
	if resource.Access == nil {
		return nil, fmt.Errorf("resource access is required")
	}

	wget := &v1.Wget{}
	if err := accessspec.Scheme.Convert(resource.Access, wget); err != nil {
		return nil, fmt.Errorf("error converting resource access spec: %w", err)
	}

	policy, hasPolicy := httpverify.PolicyForMode(r.checksumConfig.ModeForURL(wget.URL))

	opts := []download.Option{
		download.WithClient(r.client),
		download.WithMaxDownloadSize(r.maxDownloadSize),
		download.WithCredentials(credentials),
	}

	var verify download.VerifyFunc
	if hasPolicy {
		opts = append(opts, download.WithDigestAlgorithms(httpverify.DigestAlgorithms(policy)...))
		url := wget.URL
		verify = func(headers http.Header, digests map[string]string) error {
			if _, _, err := checksum.Resolve(ctx, policy, checksum.Input{
				URL:      url,
				Headers:  headers,
				Computed: digests,
			}); err != nil {
				return fmt.Errorf("checksum verification failed for wget access %q: %w", url, err)
			}
			return nil
		}
	}

	stream := download.NewStream(ctx, download.Request{
		URL:        wget.URL,
		MediaType:  wget.MediaType,
		Header:     wget.Header,
		Verb:       wget.Verb,
		Body:       wget.Body,
		NoRedirect: wget.NoRedirect,
	}, verify, opts...)

	return repository.VerifyDownload(ctx, resource, stream)
}
