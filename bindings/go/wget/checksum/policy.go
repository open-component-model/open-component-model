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

// Input carries what a policy needs from a completed download.
type Input struct {
	// URL is the artifact URL, used only for log context.
	URL string
	// Headers are the download response headers.
	Headers map[string][]string
	// Computed maps algorithm OCM names to the hex digest computed over the
	// downloaded bytes. MUST contain every algorithm any source may verify
	// against; the caller pre-computes them during the download.
	Computed map[string]string
}

// Resolve walks policy.Sources and, for the first that yields an expected
// checksum, verifies in.Computed against it. A mismatched checksum is a hard
// error. A stream source (or Compute-on-missing exhaustion) yields (_, false,
// nil), signalling "compute and store without verification".
func Resolve(ctx context.Context, policy Policy, in Input) (expected Expected, verified bool, err error) {
	for i, src := range policy.Sources {
		switch src.Type {
		case SourceStream:
			slog.DebugContext(ctx, "checksum: source is stream — no verification",
				"url", in.URL, "index", i)
			return Expected{}, false, nil
		case SourceHTTPHeader:
			candidates := FromHeaders(in.Headers, src.Headers)
			if exp, ok := Select(candidates, src.Algorithms); ok {
				if verr := Verify(in.Computed, exp); verr != nil {
					return Expected{}, false, verr
				}
				return exp, true, nil
			}
			slog.DebugContext(ctx, "checksum: header source yielded no candidate",
				"url", in.URL, "index", i, "candidates", len(candidates))
		default:
			return Expected{}, false, fmt.Errorf("unsupported checksum source type %q", src.Type)
		}
	}

	if policy.OnMissing == Compute {
		slog.DebugContext(ctx, "checksum: no source yielded a digest; onMissing=compute — no verification",
			"url", in.URL)
		return Expected{}, false, nil
	}
	return Expected{}, false, fmt.Errorf("no checksum could be obtained from any configured source and onMissing is %q", orFail(policy.OnMissing))
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
