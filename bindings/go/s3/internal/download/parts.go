package download

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/smithy-go"
	smithyhttp "github.com/aws/smithy-go/transport/http"
	"golang.org/x/sync/errgroup"

	"ocm.software/open-component-model/bindings/go/blob/filesystem"
)

// partConcurrency bounds how many parts of a multipart object are in flight at once,
// the first part included.
const partConcurrency = 8

// byteRange is the span of an object a response carries, as its Content-Range
// reports it: bytes start through end, both inclusive, of total.
type byteRange struct {
	start, end, total int64
}

func (r byteRange) length() int64 { return r.end - r.start + 1 }

// parseContentRange parses a "bytes <start>-<end>/<total>" Content-Range. It reports
// false for anything else, including an unknown total ("*"), since a span of an
// object of unknown size cannot be placed.
func parseContentRange(v string) (byteRange, bool) {
	spec, ok := strings.CutPrefix(v, "bytes ")
	if !ok {
		return byteRange{}, false
	}
	span, total, ok := strings.Cut(spec, "/")
	if !ok {
		return byteRange{}, false
	}
	start, end, ok := strings.Cut(span, "-")
	if !ok {
		return byteRange{}, false
	}
	var r byteRange
	var err error
	if r.start, err = strconv.ParseInt(start, 10, 64); err != nil {
		return byteRange{}, false
	}
	if r.end, err = strconv.ParseInt(end, 10, 64); err != nil {
		return byteRange{}, false
	}
	if r.total, err = strconv.ParseInt(total, 10, 64); err != nil {
		return byteRange{}, false
	}
	if r.start < 0 || r.end < r.start || r.end >= r.total {
		return byteRange{}, false
	}
	return r, true
}

// getFirstPart requests part 1 of the object. A store that does not know part numbers
// is asked again for the whole object; one that ignores them answers with the whole
// object anyway.
func getFirstPart(ctx context.Context, client *s3.Client, req Request, in *s3.GetObjectInput) (*s3.GetObjectOutput, []func(*s3.Options), error) {
	get := func(in *s3.GetObjectInput) (*s3.GetObjectOutput, []func(*s3.Options), error) {
		return inBucketRegion(ctx, client, req, func(optFns ...func(*s3.Options)) (*s3.GetObjectOutput, error) {
			return client.GetObject(ctx, in, optFns...)
		})
	}

	byPart := *in
	byPart.PartNumber = aws.Int32(1)
	out, regionOpts, err := get(&byPart)
	if err == nil || !partNumberUnsupported(err) {
		return out, regionOpts, err
	}
	return get(in)
}

// partNumberUnsupported reports whether a store rejected the partNumber parameter
// itself, rather than the object or the caller.
func partNumberUnsupported(err error) bool {
	var response *smithyhttp.ResponseError
	if errors.As(err, &response) && response.HTTPStatusCode() == http.StatusNotImplemented {
		return true
	}
	var api smithy.APIError
	if !errors.As(err, &api) {
		return false
	}
	switch api.ErrorCode() {
	case "NotImplemented", "InvalidArgument", "InvalidRequest":
		return true
	default:
		return false
	}
}

// partCount returns the number of parts of a multipart object: PartsCount where the
// store reports it, otherwise the "-<parts>" suffix S3 and compatible stores give the
// ETag of a multipart object. Zero means neither tells.
func partCount(out *s3.GetObjectOutput) int32 {
	if n := aws.ToInt32(out.PartsCount); n > 0 {
		return n
	}
	etag := strings.Trim(aws.ToString(out.ETag), `"`)
	i := strings.LastIndexByte(etag, '-')
	if i < 0 {
		return 0
	}
	n, err := strconv.ParseInt(etag[i+1:], 10, 32)
	if err != nil || n < 1 {
		return 0
	}
	return int32(n)
}

// storeParts writes a multipart object into file and returns a blob backed by it. first
// is the response for part 1 and firstRange its span. It closes file whether or not it
// succeeds, but never removes it; the caller does that in one place.
//
// The remaining parts are fetched in parallel, each with its own GetObject, and written
// at the offset its Content-Range names. Each part is checked against the part-level
// checksum S3 stored for it, see [spanChecksum]. Every request is held to the object part 1
// came from: to its version, and through If-Match to its ETag, so an object overwritten
// mid-download fails rather than mixing two objects. A store that reports no part count
// gets the rest of the object as one ranged GET. Before the blob is returned, the spans
// written must tile the object exactly.
func storeParts(ctx context.Context, file *os.File, client *s3.Client, regionOpts []func(*s3.Options), in *s3.GetObjectInput, first *s3.GetObjectOutput, firstRange byteRange, req Request) (*filesystem.Blob, error) {
	path := file.Name()
	err := writeParts(ctx, file, client, regionOpts, in, first, firstRange)
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return nil, fmt.Errorf("error writing s3 object %s/%s to %s: %w", req.BucketName, req.ObjectKey, path, err)
	}

	b, err := filesystem.GetBlobFromOSPath(path)
	if err != nil {
		return nil, fmt.Errorf("error creating blob for s3 object %s/%s from %s: %w", req.BucketName, req.ObjectKey, path, err)
	}
	return b, nil
}

func writeParts(ctx context.Context, file *os.File, client *s3.Client, regionOpts []func(*s3.Options), in *s3.GetObjectInput, first *s3.GetObjectOutput, firstRange byteRange) error {
	total := firstRange.total

	follow := *in
	follow.IfMatch = first.ETag
	if in.VersionId == nil {
		if v := aws.ToString(first.VersionId); v != "" && v != UnversionedVersionID {
			follow.VersionId = &v
		}
	}

	var mu sync.Mutex
	written := []byteRange{}
	write := func(out *s3.GetObjectOutput, body io.Reader, r byteRange) error {
		n, err := verifiedCopy(io.NewOffsetWriter(file, r.start), io.LimitReader(body, r.length()+1), out, r)
		if err != nil {
			return err
		}
		if n != r.length() {
			return fmt.Errorf("bytes %d-%d delivered %d of %d bytes", r.start, r.end, n, r.length())
		}
		mu.Lock()
		written = append(written, r)
		mu.Unlock()
		return nil
	}

	// fetch requests one more span of the object and writes it where it belongs. want
	// checks the span a response claims before anything is written.
	fetch := func(ctx context.Context, in *s3.GetObjectInput, want func(byteRange) error) error {
		out, err := client.GetObject(ctx, in, regionOpts...)
		if err != nil {
			return err
		}
		defer func() { _ = out.Body.Close() }()
		r, ok := parseContentRange(aws.ToString(out.ContentRange))
		if !ok || r.total != total {
			return fmt.Errorf("response spans %q, not part of an object of %d bytes", aws.ToString(out.ContentRange), total)
		}
		if err := want(r); err != nil {
			return err
		}
		return write(out, out.Body, r)
	}

	group, gctx := errgroup.WithContext(ctx)
	group.SetLimit(partConcurrency)
	// Part 1 was requested before the group existed; closing its body is how a failure
	// elsewhere stops it.
	stop := context.AfterFunc(gctx, func() { _ = first.Body.Close() })
	defer stop()
	group.Go(func() error {
		if err := write(first, first.Body, firstRange); err != nil {
			return fmt.Errorf("part 1: %w", err)
		}
		return nil
	})

	if parts := partCount(first); parts > 1 {
		for p := int32(2); p <= parts; p++ {
			part := follow
			part.PartNumber = aws.Int32(p)
			group.Go(func() error {
				if err := fetch(gctx, &part, func(byteRange) error { return nil }); err != nil {
					return fmt.Errorf("part %d of %d: %w", p, parts, err)
				}
				return nil
			})
		}
	} else {
		rest := follow
		rest.PartNumber = nil
		rest.Range = aws.String(fmt.Sprintf("bytes=%d-", firstRange.end+1))
		group.Go(func() error {
			return fetch(gctx, &rest, func(r byteRange) error {
				if r.start != firstRange.end+1 || r.end != total-1 {
					return fmt.Errorf("asked for bytes %d-%d, got %d-%d", firstRange.end+1, total-1, r.start, r.end)
				}
				return nil
			})
		})
	}
	if err := group.Wait(); err != nil {
		return err
	}

	// Parts are written where they claim to belong, so a store numbering parts
	// inconsistently could leave gaps or overlaps that no single response reveals.
	slices.SortFunc(written, func(a, b byteRange) int { return cmp.Compare(a.start, b.start) })
	var next int64
	for _, r := range written {
		if r.start != next {
			return fmt.Errorf("parts do not tile the object: expected bytes from %d, got %d-%d", next, r.start, r.end)
		}
		next = r.end + 1
	}
	if next != total {
		return fmt.Errorf("parts cover %d of %d bytes", next, total)
	}
	return nil
}
