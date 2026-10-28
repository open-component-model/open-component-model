package gzippool_test

import (
	"bytes"
	"compress/gzip"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/blob/compression/gzippool"
)

// Pooled output must match a fresh writer byte for byte, even after a previous user
// changed the header or left data unflushed: reproducible archives digest it.
func TestGetMatchesFreshWriter(t *testing.T) {
	data := []byte(strings.Repeat("reproducible gzip output ", 4096))

	var want bytes.Buffer
	fresh := gzip.NewWriter(&want)
	_, err := fresh.Write(data)
	require.NoError(t, err)
	require.NoError(t, fresh.Close())

	for name, previousUse := range map[string]func(*gzip.Writer){
		"closed with custom header": func(gw *gzip.Writer) {
			gw.Name, gw.Comment, gw.ModTime = "name", "comment", time.Unix(1, 0)
			_, _ = gw.Write([]byte("previous"))
			_ = gw.Close()
		},
		"returned without close": func(gw *gzip.Writer) {
			_, _ = gw.Write([]byte("unflushed"))
		},
	} {
		t.Run(name, func(t *testing.T) {
			r := require.New(t)
			var discard bytes.Buffer
			prev := gzippool.Get(&discard)
			previousUse(prev)
			gzippool.Put(prev)

			var got bytes.Buffer
			gw := gzippool.Get(&got)
			_, err := gw.Write(data)
			r.NoError(err)
			r.NoError(gw.Close())
			gzippool.Put(gw)

			r.Equal(want.Bytes(), got.Bytes())
		})
	}
}
