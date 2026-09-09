package file_test

import (
	"crypto/rand"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/input/file"
	v1 "ocm.software/open-component-model/bindings/go/input/file/spec/v1"
)

// BenchmarkGetV1FileBlob measures the per-blob cost of blob creation for file
// inputs, isolating the MIME type discovery that runs when File.MediaType is
// not set explicitly.
//
// Sub-benchmarks:
//
//   - detect: no media type set (discovery reads the first bytes of the blob)
//
//   - explicit: media type set (discovery skipped, baseline)
//
//   - -drain variants additionally consume the whole blob, approximating the
//     downstream digest/copy work a constructor does per blob; this puts the
//     constant discovery cost into proportion.
//
//     go test -run '^$' -bench BenchmarkGetV1FileBlob -benchmem -count 10 ./input/file/ > run.txt
//     benchstat -col '/mediaType@(explicit detect)' -row '/size,/drain' run.txt
func BenchmarkGetV1FileBlob(b *testing.B) {
	for _, size := range []int64{4 << 10, 1 << 20} {
		b.Run(fmt.Sprintf("size=%d", size), func(b *testing.B) {
			r := require.New(b)

			dir := b.TempDir()
			name := "blob.bin"
			// random content: worst case for the matcher tree (falls back to
			// application/octet-stream after all matchers miss)
			f, err := os.Create(filepath.Join(dir, name))
			r.NoError(err)
			_, err = io.CopyN(f, rand.Reader, size)
			r.NoError(err)
			r.NoError(f.Close())

			// key=value sub-benchmark names: benchstat parses these into keys,
			// so the discovery variants become pivotable dimensions
			// (benchstat -col mediaType ...)
			for _, mt := range []struct {
				name      string
				mediaType string
			}{
				{name: "detect", mediaType: ""},
				{name: "explicit", mediaType: "application/octet-stream"},
			} {
				b.Run("mediaType="+mt.name, func(b *testing.B) {
					spec := v1.File{Path: name, MediaType: mt.mediaType}
					for _, drain := range []bool{false, true} {
						b.Run(fmt.Sprintf("drain=%v", drain), func(b *testing.B) {
							b.ReportAllocs()
							for b.Loop() {
								blb, err := file.GetV1FileBlob(spec, dir)
								if err != nil {
									b.Fatal(err)
								}
								if drain {
									rc, err := blb.ReadCloser()
									if err != nil {
										b.Fatal(err)
									}
									if _, err := io.Copy(io.Discard, rc); err != nil {
										b.Fatal(err)
									}
									if err := rc.Close(); err != nil {
										b.Fatal(err)
									}
								}
							}
						})
					}
				})
			}
		})
	}
}
