package credentials

import (
	"bytes"
	"log/slog"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/runtime"
)

func TestPathPrefixWarningIsLoggedOnce(t *testing.T) {
	r := require.New(t)

	var logs bytes.Buffer
	defaultLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	pathPrefixWarning = sync.Once{}
	t.Cleanup(func() {
		slog.SetDefault(defaultLogger)
		pathPrefixWarning = sync.Once{}
	})

	pathPrefixToPath(t.Context(), runtime.Identity{runtime.IdentityAttributeType: "Git"})
	r.Empty(logs.String())

	for _, prefix := range []string{"a", "b", "c"} {
		pathPrefixToPath(t.Context(), runtime.Identity{runtime.IdentityAttributeType: "Git", legacyIdentityAttributePathPrefix: prefix})
	}
	r.Equal(1, strings.Count(logs.String(), "level=WARN"))
	r.Contains(logs.String(), "https://ocm.software/docs/how-to/migrate-legacy-credentials/")
}
