package dn_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	"ocm.software/open-component-model/bindings/go/rsa/signing/handler/internal/dn"
)

func TestParse_Plain(t *testing.T) {
	got, err := dn.Parse("open-component-model")
	require.NoError(t, err)
	require.Equal(t, "CN=open-component-model", got.String())
}

func TestParse_SingleField(t *testing.T) {
	got, err := dn.Parse("CN=open-component-model")
	require.NoError(t, err)
	require.Equal(t, "CN=open-component-model", got.String())
}

func TestParse_TwoFields(t *testing.T) {
	got, err := dn.Parse("CN=open-component-model,C=DE")
	require.NoError(t, err)
	require.Equal(t, "CN=open-component-model,C=DE", got.String())
}

func TestParse_ThreeFields(t *testing.T) {
	got, err := dn.Parse("CN=open-component-model,C=DE,ST=BW")
	require.NoError(t, err)
	require.Equal(t, "CN=open-component-model,ST=BW,C=DE", got.String())
}

func TestParse_DoubleFields_PlusAfter(t *testing.T) {
	got, err := dn.Parse("CN=open-component-model,C=DE+C=US")
	require.NoError(t, err)
	require.Equal(t, "CN=open-component-model,C=DE+C=US", got.String())
}

func TestParse_DoubleFields_PlusBefore(t *testing.T) {
	got, err := dn.Parse("C=DE+C=US,CN=open-component-model")
	require.NoError(t, err)
	require.Equal(t, "CN=open-component-model,C=DE+C=US", got.String())
}

func TestParse_DoubleFields_WithOthers(t *testing.T) {
	got, err := dn.Parse("C=DE+C=US,CN=open-component-model,L=Walldorf,O=open-component-model")
	require.NoError(t, err)
	require.Equal(t, "CN=open-component-model,O=open-component-model,L=Walldorf,C=DE+C=US", got.String())
}
