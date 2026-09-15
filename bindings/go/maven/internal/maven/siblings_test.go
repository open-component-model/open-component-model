package maven

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSplitSibling(t *testing.T) {
	for name, want := range map[string][2]string{
		"lib-1.2.3.jar":         {"lib-1.2.3.jar", ""},
		"lib-1.2.3.jar.asc":     {"lib-1.2.3.jar", ".asc"},
		"lib-1.2.3.jar.sha1":    {"lib-1.2.3.jar", ".sha1"},
		"lib-1.2.3.pom.sha512":  {"lib-1.2.3.pom", ".sha512"},
		"lib-1.2.3-sources.jar": {"lib-1.2.3-sources.jar", ""},
	} {
		file, suffix := SplitSibling(name)
		assert.Equal(t, want[0], file, name)
		assert.Equal(t, want[1], suffix, name)
	}
}
