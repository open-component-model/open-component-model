package main

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestCostRegistryUnavailable(t *testing.T) {
	r := require.New(t)
	after := scrapes{registryErr: errors.New("connection refused")}

	c, err := costOf(raw{after: after}, time.Second)

	r.ErrorContains(err, "registry metrics unavailable: connection refused")
	r.Nil(c.RegistryRequests)
}
