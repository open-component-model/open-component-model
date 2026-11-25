package oci

import (
	"github.com/oras-project/oras-go/v3"
	"github.com/oras-project/oras-go/v3/content"
)

// SignatureGraph wraps src in a signatureGraph for external tests.
func SignatureGraph(src content.ReadOnlyGraphStorage) content.ReadOnlyGraphStorage {
	return signatureGraph{src}
}

// RejectAccessReferrers exposes rejectAccessReferrers to external tests.
var RejectAccessReferrers = rejectAccessReferrers

// ExtendedCopyGraphOptions exposes the extended copy options of repo to external tests.
func (repo *Repository) ExtendedCopyGraphOptions() oras.ExtendedCopyGraphOptions {
	return repo.extendedCopyGraphOptions()
}
