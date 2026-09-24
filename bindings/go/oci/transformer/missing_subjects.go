package transformer

import (
	"fmt"

	"ocm.software/open-component-model/bindings/go/oci"
	ocistream "ocm.software/open-component-model/bindings/go/oci/stream"
	"ocm.software/open-component-model/bindings/go/repository"
)

// missingSubjectsConfigurable is implemented by resource repositories that can
// derive a copy which skips missing subjects and referrers.
type missingSubjectsConfigurable interface {
	WithAllowMissingSubjects(bool) ocistream.ResourceRepository
}

// withAllowMissingSubjects returns repo, or a copy of it that allows missing
// subjects. If repo cannot carry the setting, fallback is used instead (see
// https://github.com/open-component-model/ocm-project/issues/774).
func withAllowMissingSubjects(repo repository.ResourceRepository, fallback missingSubjectsConfigurable, allow bool) (repository.ResourceRepository, error) {
	if !allow {
		return repo, nil
	}
	if configurable, ok := repo.(missingSubjectsConfigurable); ok {
		return configurable.WithAllowMissingSubjects(true), nil
	}
	if fallback != nil {
		return fallback.WithAllowMissingSubjects(true), nil
	}
	return nil, fmt.Errorf("repository %T does not support allowing missing subjects", repo)
}

// setAllowMissingSubjects always sets the value on OCI and CTF repositories to
// avoid leaking state from prior transforms on the same cached instance.
func setAllowMissingSubjects(repo repository.ComponentVersionRepository, allow bool) error {
	if ociRepo, ok := repo.(*oci.Repository); ok {
		ociRepo.SetAllowMissingSubjects(allow)
	} else if allow {
		return fmt.Errorf("allowMissingSubjects is only supported for OCI and CTF repositories, got %T", repo)
	}
	return nil
}
