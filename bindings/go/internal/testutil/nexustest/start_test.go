package nexustest

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestWithExposedPorts(t *testing.T) {
	r := require.New(t)

	cfg := config{ports: []string{apiPort}}
	for _, opt := range []Option{WithExposedPorts("5000/tcp", "5001/tcp"), WithExposedPorts("5002/tcp")} {
		opt(&cfg)
	}

	r.Equal([]string{apiPort, "5000/tcp", "5001/tcp", "5002/tcp"}, cfg.ports)
}
