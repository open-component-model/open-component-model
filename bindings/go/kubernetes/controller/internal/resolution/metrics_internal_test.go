package resolution

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestResultLabel(t *testing.T) {
	r := require.New(t)
	r.Equal("success", resultLabel(nil))
	r.Equal("success", resultLabel(fmt.Errorf("%w: %w", ErrNotSafelyDigestible, errors.New("legacy digest"))))
	r.Equal("error", resultLabel(errors.New("unauthorized")))
}
