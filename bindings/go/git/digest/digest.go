// Package digest provides resource digest processing for Git access.
package digest

import (
	repository2 "ocm.software/open-component-model/bindings/go/git/repository"
	"ocm.software/open-component-model/bindings/go/repository"
)

type DigestProcessor struct {
	*repository2.ResourceRepository
}

var _ repository.ResourceDigestProcessor = (*DigestProcessor)(nil)

func NewDigestProcessor(opts ...repository2.Option) *DigestProcessor {
	return &DigestProcessor{ResourceRepository: repository2.NewResourceRepository(opts...)}
}
