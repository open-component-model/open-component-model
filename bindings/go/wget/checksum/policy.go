package checksum

import (
	"context"
	"fmt"
	"log/slog"
)

// SourceType mirrors the wget input spec's checksum source types without
// importing the spec.
type SourceType string

const (
	SourceHTTPHeader SourceType = "httpHeader"
	SourceStream     SourceType = "stream"
)

// Source is one resolved checksum strategy.
type Source struct {
	Type SourceType
	// Headers are extra response header names to inspect (httpHeader).
	Headers []string
	// Algorithms restricts which algorithms this source considers, strongest
	// first. Empty means all supported algorithms.
	Algorithms []Algorithm
}

// OnMissing controls behaviour when no source yields an expected checksum.
type OnMissing string

const (
	// Fail aborts. Default for a configured policy.
	Fail OnMissing = "fail"
	// Compute falls back to computing the digest from the stream.
	Compute OnMissing = "compute"
)

// Policy is the resolved checksum policy.
type Policy struct {
	Sources   []Source
	OnMissing OnMissing
}

// BuiltinSources is the fixed source set the reduced checksum configuration
// resolves an enabled policy to: RFC 9530 Content-Digest and the
// x-checksum-* response-header family, accepting every supported algorithm.
// Returned fresh so callers may not mutate the shared slice.
func BuiltinSources() []Source {
	return []Source{
		{Type: SourceHTTPHeader},
	}
}

// Input carries what a policy needs from a download.
type Input struct {
	// URL is the artifact URL, used only for log context.
	URL string
	// Headers are the download response headers.
	Headers map[string][]string
	// Computed maps algorithm OCM names to the hex digest computed over the
	// downloaded content. MUST contain every algorithm any source may verify
	// against; the caller pre-computes them during the download.
	Computed map[string]string
	// ComputedTransferred is Computed for the bytes as transferred, which differ
	// from the content when it was decoded from a content coding. Checksums over
	// the transferred bytes (Content-Digest) are verified against it. Nil means
	// the transferred bytes are the content.
	ComputedTransferred map[string]string
	// Decoded reports that the content was decoded from a content coding, so
	// [ResolveAdvertised] ignores checksums over the transferred bytes: they do
	// not describe the content.
	Decoded bool
}

// computedFor returns the computed digests exp has to be verified against.
func (in Input) computedFor(exp Expected) map[string]string {
	if exp.Transferred && in.ComputedTransferred != nil {
		return in.ComputedTransferred
	}
	return in.Computed
}

// Resolve verifies in.Computed against the checksum [Expect] yields from
// in.Headers. A mismatched checksum is a hard error. When Expect yields none,
// Resolve returns (_, false, nil), signalling "compute and store without
// verification".
func Resolve(ctx context.Context, policy Policy, in Input) (expected Expected, verified bool, err error) {
	exp, ok, err := Expect(ctx, policy, Input{URL: in.URL, Headers: in.Headers}, nil)
	if err != nil || !ok {
		return Expected{}, false, err
	}
	if err := Verify(in.computedFor(exp), exp); err != nil {
		return Expected{}, false, err
	}
	return exp, true, nil
}

// Expect returns the checksum policy expects the content to have: the first
// one a source advertises in in.Headers, restricted to prefer (empty means
// [All]). ok is false when the policy accepts that none is advertised: a
// stream source comes first, or OnMissing is Compute. When the policy requires
// a checksum and none is advertised, Expect fails, so a response can be
// rejected before its body is read.
func Expect(ctx context.Context, policy Policy, in Input, prefer []Algorithm) (Expected, bool, error) {
	exp, ok, err := ResolveAdvertised(ctx, policy, in, prefer)
	if err != nil || ok || policy.OnMissing == Compute || hasStreamSource(policy) {
		return exp, ok, err
	}
	return Expected{}, false, fmt.Errorf("no checksum could be obtained from any configured source and onMissing is %q", orFail(policy.OnMissing))
}

func hasStreamSource(policy Policy) bool {
	for _, src := range policy.Sources {
		if src.Type == SourceStream {
			return true
		}
	}
	return false
}

// RequiredAlgorithms returns the algorithms the caller must compute during the
// download so any source's verification can succeed. SHA-256 (the storage
// algorithm) is always included.
func RequiredAlgorithms(policy Policy) []Algorithm {
	seen := map[string]struct{}{StorageAlgorithm.OCMName: {}}
	out := []Algorithm{StorageAlgorithm}
	add := func(a Algorithm) {
		if _, dup := seen[a.OCMName]; dup {
			return
		}
		seen[a.OCMName] = struct{}{}
		out = append(out, a)
	}
	for _, src := range policy.Sources {
		algs := src.Algorithms
		if len(algs) == 0 {
			algs = All
		}
		for _, a := range algs {
			add(a)
		}
	}
	return out
}

func orFail(m OnMissing) OnMissing {
	if m == "" {
		return Fail
	}
	return m
}

// ResolveAdvertised returns the first digest a source advertises, without
// requiring a completed download or a Computed map. Used by the access-side
// digest processor to pin from what the source claims. A stream source signals
// "no advertised digest here — fall back to download-and-hash".
//
// prefer restricts and orders the accepted algorithms; empty means [All].
func ResolveAdvertised(ctx context.Context, policy Policy, in Input, prefer []Algorithm) (Expected, bool, error) {
	if len(prefer) == 0 {
		prefer = All
	}
	for i, src := range policy.Sources {
		switch src.Type {
		case SourceStream:
			slog.DebugContext(ctx, "checksum: advertised source is stream — no advertised digest",
				"url", in.URL, "index", i)
			return Expected{}, false, nil
		case SourceHTTPHeader:
			candidates := FromHeaders(in.Headers, src.Headers)
			if in.Decoded {
				candidates = contentCandidates(candidates)
			}
			if exp, ok := Select(candidates, intersect(src.Algorithms, prefer)); ok {
				return exp, true, nil
			}
			slog.DebugContext(ctx, "checksum: advertised header source yielded nothing",
				"url", in.URL, "index", i, "candidates", len(candidates))
		default:
			return Expected{}, false, fmt.Errorf("unsupported checksum source type %q", src.Type)
		}
	}
	slog.DebugContext(ctx, "checksum: no source advertised a digest", "url", in.URL)
	return Expected{}, false, nil
}

// intersect returns the algorithms present in both a and prefer, in prefer's
// order. Empty a means "no restriction from the source"; empty prefer means
// "no restriction from the caller".
func intersect(a []Algorithm, prefer []Algorithm) []Algorithm {
	if len(a) == 0 {
		return prefer
	}
	if len(prefer) == 0 {
		return a
	}
	allow := make(map[string]struct{}, len(a))
	for _, x := range a {
		allow[x.OCMName] = struct{}{}
	}
	out := make([]Algorithm, 0, len(prefer))
	for _, p := range prefer {
		if _, ok := allow[p.OCMName]; ok {
			out = append(out, p)
		}
	}
	return out
}
