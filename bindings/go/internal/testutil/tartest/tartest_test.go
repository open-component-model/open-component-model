package tartest_test

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"errors"
	"io"
	"testing"

	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/internal/testutil/tartest"
)

func TestGzipped(t *testing.T) {
	tests := []struct {
		name    string
		entries []tartest.Entry
	}{
		{name: "empty archive"},
		{name: "one file", entries: []tartest.Entry{{Name: "README", Data: []byte("hello")}}},
		{name: "keeps order and empty data", entries: []tartest.Entry{{Name: "b"}, {Name: "a", Data: []byte{0x00, 0xff}}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := require.New(t)

			gz, err := gzip.NewReader(bytes.NewReader(tartest.Gzipped(t, tt.entries...)))
			r.NoError(err)
			tr := tar.NewReader(gz)

			var got []tartest.Entry
			for {
				hdr, err := tr.Next()
				if errors.Is(err, io.EOF) {
					break
				}
				r.NoError(err)
				r.Equal(byte(tar.TypeReg), hdr.Typeflag)
				r.Equal(int64(0o644), hdr.Mode)
				data, err := io.ReadAll(tr)
				r.NoError(err)
				got = append(got, tartest.Entry{Name: hdr.Name, Data: data})
			}

			r.Len(got, len(tt.entries))
			for i, want := range tt.entries {
				r.Equal(want.Name, got[i].Name)
				r.Equal(string(want.Data), string(got[i].Data), "data of %s", want.Name)
			}
		})
	}
}
