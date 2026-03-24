package internal

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"ocm.software/open-component-model/bindings/go/credentials"
	"ocm.software/open-component-model/bindings/go/repository"
)

type stubRepoProvider struct {
	repository.ComponentVersionRepositoryProvider
}

type stubResourceRepo struct {
	repository.ResourceRepository
}

type stubCredResolver struct {
	credentials.Resolver
}

func TestNewBuilder(t *testing.T) {
	b := NewDefaultBuilder(&stubRepoProvider{}, &stubResourceRepo{}, &stubCredResolver{})
	assert.NotNil(t, b)
}
