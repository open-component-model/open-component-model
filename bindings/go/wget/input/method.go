package input

import (
	"context"
	"fmt"
	nethttp "net/http"
	"net/url"
	"strings"

	"ocm.software/open-component-model/bindings/go/constructor"
	constructorruntime "ocm.software/open-component-model/bindings/go/constructor/runtime"
	httpclient "ocm.software/open-component-model/bindings/go/http"
	httpv1alpha1 "ocm.software/open-component-model/bindings/go/http/spec/config/v1alpha1"
	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/bindings/go/wget/checksum"
	"ocm.software/open-component-model/bindings/go/wget/internal/download"
	wgetcreds "ocm.software/open-component-model/bindings/go/wget/spec/credentials"
	identityv1 "ocm.software/open-component-model/bindings/go/wget/spec/identity/v1"
	"ocm.software/open-component-model/bindings/go/wget/spec/input"
	v1 "ocm.software/open-component-model/bindings/go/wget/spec/input/v1"
)

// genericBlobDigestV1 is the OCM normalisation algorithm recorded for a plain
// downloaded blob, matching the wget resource repository.
const genericBlobDigestV1 = "genericBlobDigest/v1"

var _ constructor.ResourceInputMethod = (*InputMethod)(nil)

// InputMethod implements the [constructor.ResourceInputMethod] interface for wget-based inputs.
// It downloads a resource from an HTTP/S URL declared in the component constructor
// and returns it as a local blob to be stored in the component version.
type InputMethod struct {
	// HTTPConfig configures the HTTP client (timeouts, retries, TLS, routing) used for
	// downloads. When nil, a default client is used.
	HTTPConfig *httpv1alpha1.Config
	// MaxDownloadSize limits the number of bytes read from a response body. When zero,
	// the download package default [download.DefaultMaxDownloadSize] is used. A negative value disables the limit.
	MaxDownloadSize int64
	// TempFolder is the directory the downloaded body is streamed into. When empty,
	// the OS temporary directory is used. The file backing the returned blob is
	// created here and outlives ProcessResource, because it holds the content the
	// constructor stores as a local blob. It is removed once the constructor releases
	// the blob; see [download.Blob].
	TempFolder string
}

func (i *InputMethod) GetInputMethodScheme() *runtime.Scheme {
	return input.Scheme
}

// GetResourceCredentialConsumerIdentity resolves the credential consumer identity for a
// wget input from its URL, using the same wget consumer type as the access type so that
// credentials configured for a host resolve for both.
func (i *InputMethod) GetResourceCredentialConsumerIdentity(_ context.Context, resource *constructorruntime.Resource) (runtime.Identity, error) {
	wget := v1.Wget{}
	if err := i.GetInputMethodScheme().Convert(resource.Input, &wget); err != nil {
		return nil, fmt.Errorf("error converting resource input spec: %w", err)
	}

	if wget.URL == "" {
		return nil, fmt.Errorf("url is required")
	}

	parsed, err := url.Parse(wget.URL)
	if err != nil {
		return nil, fmt.Errorf("wget url is not a valid url: %w", err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, fmt.Errorf("wget url must use http or https scheme, got %q", parsed.Scheme)
	}

	identity, err := identityv1.IdentityFromURL(wget.URL)
	if err != nil {
		return nil, fmt.Errorf("error parsing wget URL to identity: %w", err)
	}

	return identity, nil
}

// ProcessResource downloads the resource described by the wget input specification and
// returns it as local blob data to be stored in the component version.
func (i *InputMethod) ProcessResource(ctx context.Context, resource *constructorruntime.Resource, credentials runtime.Typed) (*constructor.ResourceInputMethodResult, error) {
	wget := v1.Wget{}
	if err := i.GetInputMethodScheme().Convert(resource.Input, &wget); err != nil {
		return nil, fmt.Errorf("error converting resource input spec: %w", err)
	}

	if wget.URL == "" {
		return nil, fmt.Errorf("url is required in wget input spec")
	}

	var client *nethttp.Client
	if i.HTTPConfig != nil {
		client = httpclient.New(httpclient.WithConfig(i.HTTPConfig))
	}

	policy, hasPolicy := toChecksumPolicy(wget.ChecksumPolicy)

	opts := []download.Option{
		download.WithClient(client),
		download.WithCredentials(credentials),
		download.WithTempDir(i.TempFolder),
	}
	if i.MaxDownloadSize != 0 {
		opts = append(opts, download.WithMaxDownloadSize(i.MaxDownloadSize))
	}
	// Determine whether we must compute digests during the download: either a
	// checksum policy needs them, or the resource carries a provided digest to
	// verify. Both cases store the canonical SHA-256 on the resulting blob.
	provided := resource.Digest
	needsDigest := hasPolicy || provided != nil
	if needsDigest {
		opts = append(opts, download.WithDigestAlgorithms(digestAlgorithms(policy)...))
	}

	data, err := download.Download(ctx, download.Request{
		URL:        wget.URL,
		MediaType:  wget.MediaType,
		Header:     wget.Header,
		Verb:       wget.Verb,
		Body:       wget.Body,
		NoRedirect: wget.NoRedirect,
	}, opts...)
	if err != nil {
		return nil, fmt.Errorf("error downloading wget input from %q: %w", wget.URL, err)
	}

	// A provided digest is verified against the downloaded content independently
	// of any policy: both are checked against the actual bytes, never against each
	// other. The documented resource.digest contract is only enforced here for the
	// input path, which otherwise stores the blob without verifying it.
	if provided != nil {
		if err := verifyProvidedDigest(provided, data); err != nil {
			_ = data.Close()
			return nil, fmt.Errorf("provided digest verification failed for wget input from %q: %w", wget.URL, err)
		}
	}

	if hasPolicy {
		if err := i.verifyChecksum(ctx, client, wget.URL, policy, data); err != nil {
			_ = data.Close()
			return nil, fmt.Errorf("checksum verification failed for wget input from %q: %w", wget.URL, err)
		}
	}

	// Record the canonical SHA-256 as the blob's precalculated digest so the
	// resource is stored with SHA-256/genericBlobDigest regardless of which
	// algorithm a policy or provided digest verified against.
	if needsDigest {
		if sha, ok := data.Digests()[checksum.StorageAlgorithm.OCMName]; ok && sha != "" {
			data.SetPrecalculatedDigest("sha256:" + sha)
		}
	}

	return &constructor.ResourceInputMethodResult{
		ProcessedBlobData: data,
	}, nil
}

func (i *InputMethod) GetCredentialTypeScheme() *runtime.Scheme {
	return wgetcreds.Scheme
}

// verifyChecksum resolves the checksum policy against the completed download and
// verifies the transferred bytes against the first source that yields an expected
// checksum. Recording the resulting digest on the blob is handled by the caller.
func (i *InputMethod) verifyChecksum(ctx context.Context, client *nethttp.Client, url string, policy checksum.Policy, data *download.Blob) error {
	fetcher := &checksum.ExternalFetcher{Client: client}
	if _, _, err := checksum.Resolve(ctx, policy, checksum.Input{
		URL:      url,
		Headers:  data.Headers(),
		Computed: data.Digests(),
		Fetch:    fetcher.Fetch,
	}); err != nil {
		return err
	}
	return nil
}

// verifyProvidedDigest checks the computed content digest against a digest pinned
// on the resource. Only the canonical SHA-256/genericBlobDigest form is supported,
// matching how wget resource digests are computed and stored.
func verifyProvidedDigest(provided *constructorruntime.Digest, data *download.Blob) error {
	if provided.HashAlgorithm != "" && !strings.EqualFold(provided.HashAlgorithm, checksum.StorageAlgorithm.OCMName) {
		return fmt.Errorf("unsupported provided hash algorithm %q: only %s is supported", provided.HashAlgorithm, checksum.StorageAlgorithm.OCMName)
	}
	if provided.NormalisationAlgorithm != "" && provided.NormalisationAlgorithm != genericBlobDigestV1 {
		return fmt.Errorf("unsupported provided normalisation algorithm %q: only %s is supported", provided.NormalisationAlgorithm, genericBlobDigestV1)
	}
	computed, ok := data.Digests()[checksum.StorageAlgorithm.OCMName]
	if !ok || computed == "" {
		return fmt.Errorf("no computed %s digest available to verify the provided digest against", checksum.StorageAlgorithm.OCMName)
	}
	// The provided value may be bare hex or go-digest form (sha256:<hex>).
	want := strings.TrimPrefix(provided.Value, "sha256:")
	if !strings.EqualFold(want, computed) {
		return fmt.Errorf("digest mismatch: expected %s, computed %s", want, computed)
	}
	return nil
}

// toChecksumPolicy adapts the input spec's ChecksumPolicy to the checksum
// package's Policy, defaulting OnMissing to fail. It returns ok=false when no
// policy is configured, in which case the digest is computed from the stream.
func toChecksumPolicy(spec *v1.ChecksumPolicy) (checksum.Policy, bool) {
	if spec == nil {
		return checksum.Policy{}, false
	}
	policy := checksum.Policy{OnMissing: checksum.Fail}
	if spec.OnMissing == v1.OnMissingCompute {
		policy.OnMissing = checksum.Compute
	}
	for _, src := range spec.Sources {
		s := checksum.Source{
			Type:        checksum.SourceType(src.Type),
			Headers:     src.Headers,
			URLTemplate: src.URLTemplate,
		}
		// Unknown extensions are dropped here; RequiredAlgorithms/Resolve then fall
		// back to the full supported set, and an unresolvable checksum surfaces via
		// the onMissing behaviour.
		if algs, err := checksum.AlgorithmsFromExtensions(src.Algorithms); err == nil {
			s.Algorithms = algs
		}
		policy.Sources = append(policy.Sources, s)
	}
	return policy, true
}

// digestAlgorithms maps the algorithms a policy may verify against to download
// digest options keyed by their OCM name, so the download computes each hash in a
// single streaming pass.
func digestAlgorithms(policy checksum.Policy) []download.DigestAlgorithm {
	required := checksum.RequiredAlgorithms(policy)
	out := make([]download.DigestAlgorithm, 0, len(required))
	for _, alg := range required {
		out = append(out, download.DigestAlgorithm{
			Name: alg.OCMName,
			New:  alg.Hash.New,
		})
	}
	return out
}
