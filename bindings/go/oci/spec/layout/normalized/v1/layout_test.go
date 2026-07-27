package v1_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	layout "ocm.software/open-component-model/bindings/go/oci/spec/layout/normalized/v1"
)

func TestLayoutConstants(t *testing.T) {
	assert.Equal(t, "v1", layout.LayoutVersion)
	assert.Equal(t, "software.ocm.component-model/layout-version", layout.AnnotationLayoutVersion)
	assert.Equal(t, "software.ocm.component-model/normalisation-algo", layout.AnnotationNormalisationAlgo)
}

func TestAccessFallbackTag(t *testing.T) {
	got := layout.AccessFallbackTag("sha256:abcdef0123456789")
	assert.Equal(t, "sha256-abcdef0123456789.acc", got)
}
