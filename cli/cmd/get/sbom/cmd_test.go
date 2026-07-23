package sbom

import (
	goruntime "runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/runtime"
)

func archSBOM(name, os, arch string) descriptor.Resource {
	r := descriptor.Resource{}
	r.Name = name
	if os != "" || arch != "" {
		r.ExtraIdentity = runtime.Identity{}
		if os != "" {
			r.ExtraIdentity["os"] = os
		}
		if arch != "" {
			r.ExtraIdentity["architecture"] = arch
		}
	}
	return r
}

func TestSelectHostPlatformSBOMs_PicksHostArch(t *testing.T) {
	linked := []descriptor.Resource{
		archSBOM("podinfo-sbom", "linux", "amd64"),
		archSBOM("podinfo-sbom", "linux", "arm64"),
		archSBOM("podinfo-sbom", goruntime.GOOS, goruntime.GOARCH),
	}
	got := selectHostPlatformSBOMs(linked)
	require.Len(t, got, 1)
	assert.Equal(t, goruntime.GOARCH, got[0].ExtraIdentity["architecture"])
	assert.Equal(t, goruntime.GOOS, got[0].ExtraIdentity["os"])
}

func TestSelectHostPlatformSBOMs_NonArchTaggedUnchanged(t *testing.T) {
	// No architecture identity -> single-platform case, all returned.
	linked := []descriptor.Resource{archSBOM("cli-sbom", "", "")}
	got := selectHostPlatformSBOMs(linked)
	assert.Len(t, got, 1)
}

func TestSelectHostPlatformSBOMs_NoHostMatchKeepsAll(t *testing.T) {
	// Arch-tagged but none matches the host -> keep all arch-tagged for cross-arch audit.
	linked := []descriptor.Resource{
		archSBOM("podinfo-sbom", "plan9", "sparc64"),
		archSBOM("podinfo-sbom", "aix", "ppc64"),
	}
	got := selectHostPlatformSBOMs(linked)
	assert.Len(t, got, 2)
}
