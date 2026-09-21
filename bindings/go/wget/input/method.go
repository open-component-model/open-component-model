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
	"ocm.software/open-component-model/bindings/go/wget/checksum/httpverify"
	"ocm.software/open-component-model/bindings/go/wget/internal/download"
	wgetconfigv1alpha1 "ocm.software/open-component-model/bindings/go/wget/spec/config/v1alpha1"
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
	// WgetConfig steers the wget behavioural knobs — today, the default
	// [inputv1.ChecksumPolicy] that applies when a resource's spec does not
	// carry its own checksumPolicy, and per-host overrides thereof. When nil,
	// the input honours only spec-level policies; the access-side digest
	// processor honours the same config.
	WgetConfig *wgetconfigv1alpha1.Config
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

	// Spec-level ChecksumPolicy wins over the config's defaultChecksumPolicy /
	// host override, so descriptor authors always control their resources'
	// verification even when an operator's config sets a stricter default.
	policySpec := effectiveChecksumPolicySpec(&wget, i.WgetConfig)
	policy, hasPolicy, err := toChecksumPolicy(policySpec, &wget)
	if err != nil {
		return nil, fmt.Errorf("invalid checksum policy for wget input from %q: %w", wget.URL, err)
	}

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
		if err := httpverify.Verify(ctx, client, credentials, wget.URL, policy, data); err != nil {
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

// effectiveChecksumPolicySpec returns the ChecksumPolicy that should be applied
// to this wget input, following the precedence documented on
// [wgetconfigv1alpha1.Config]: a spec-level policy wins over config-supplied
// values, and inside the config a host-scoped override wins over the default.
func effectiveChecksumPolicySpec(wget *v1.Wget, cfg *wgetconfigv1alpha1.Config) *v1.ChecksumPolicy {
	if wget.ChecksumPolicy != nil {
		return wget.ChecksumPolicy
	}
	return cfg.PolicyForURL(wget.URL)
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
// package's Policy, defaulting OnMissing to fail and compiling each externalUrl
// source's URL CEL expression against the wget input. It returns ok=false when
// no policy is configured, in which case the digest is computed from the stream.
func toChecksumPolicy(spec *v1.ChecksumPolicy, wget *v1.Wget) (checksum.Policy, bool, error) {
	if spec == nil {
		return checksum.Policy{}, false, nil
	}
	policy := checksum.Policy{OnMissing: checksum.Fail}
	if spec.OnMissing == v1.OnMissingCompute {
		policy.OnMissing = checksum.Compute
	}
	for i, src := range spec.Sources {
		s := checksum.Source{
			Type:    checksum.SourceType(src.Type),
			Headers: src.Headers,
		}
		// An unsupported algorithm extension is a hard error: silently dropping it
		// lets Resolve fall back to the full algorithm set and silently verify
		// against an algorithm the user never asked for. Fail fast with the source
		// index so the offending entry in the policy is easy to locate.
		algs, err := checksum.AlgorithmsFromExtensions(src.Algorithms)
		if err != nil {
			return checksum.Policy{}, false, fmt.Errorf("checksum policy source #%d: %w", i, err)
		}
		s.Algorithms = algs
		if src.Type == v1.ChecksumSourceExternalURL {
			resolver, err := checksumURLResolver(src.URL, wget)
			if err != nil {
				return checksum.Policy{}, false, fmt.Errorf("checksum policy source #%d: %w", i, err)
			}
			s.ResolveURL = resolver
		}
		policy.Sources = append(policy.Sources, s)
	}
	return policy, true, nil
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
			New:  alg.New,
		})
	}
	return out
}
