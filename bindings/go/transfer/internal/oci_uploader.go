package internal

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"

	celparser "ocm.software/open-component-model/bindings/go/cel/expression/parser"
	descriptorv2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	helmv1 "ocm.software/open-component-model/bindings/go/helm/spec/access/v1"
	ociv1 "ocm.software/open-component-model/bindings/go/oci/spec/access/v1"
	"ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/oci"
	"ocm.software/open-component-model/bindings/go/runtime"
	transferv1alpha1 "ocm.software/open-component-model/bindings/go/transfer/v1alpha1/spec"
	transformv1alpha1 "ocm.software/open-component-model/bindings/go/transform/spec/v1alpha1"
)

const (
	// referenceNameAlias is the imageReference alias for the source reference name
	// (repository[:tag] without registry).
	referenceNameAlias = "referenceName"
	// targetRepositoryAlias is the imageReference alias for the target OCI registry
	// base URL including its sub path.
	targetRepositoryAlias = "targetRepository"
)

// ociTargetRepository returns the image reference prefix of an OCI registry target:
// its base URL plus the sub path, if any. It reports false for any other target.
func ociTargetRepository(toSpec runtime.Typed) (string, bool) {
	repo, err := convertToConcreteRepo(toSpec)
	if err != nil {
		return "", false
	}
	ociRepo, ok := repo.(*oci.Repository)
	if !ok {
		return "", false
	}
	if ociRepo.SubPath == "" {
		return ociRepo.BaseUrl, true
	}
	return ociRepo.BaseUrl + "/" + ociRepo.SubPath, true
}

// ociReferenceName returns the reference name an OCI uploader uses for access and
// reports whether the uploader supports the access at all. OCI images and Helm charts
// use their repository[:tag] without registry; local blobs holding an OCI manifest use
// their recorded reference name, which may be empty.
func ociReferenceName(access runtime.Typed) (string, bool, error) {
	switch acc := access.(type) {
	case *ociv1.OCIImage:
		name, err := getReferenceName(acc.ImageReference)
		if err != nil {
			return "", true, fmt.Errorf("cannot derive reference name: %w", err)
		}
		return name, true, nil
	case *helmv1.Helm:
		ref, err := acc.ChartReference()
		if err != nil {
			return "", true, fmt.Errorf("cannot derive reference name: %w", err)
		}
		name, err := getReferenceName(ref)
		if err != nil {
			return "", true, fmt.Errorf("cannot derive reference name: %w", err)
		}
		return name, true, nil
	case *descriptorv2.LocalBlob:
		if !isOCICompliantManifest(acc.MediaType) {
			return "", false, nil
		}
		return acc.ReferenceName, true, nil
	default:
		return "", false, nil
	}
}

// identifierUsed reports whether the CEL source expr references ident as an identifier.
func identifierUsed(expr, ident string) bool {
	return celparser.RewriteIdentifier(expr, ident, "") != expr
}

// ociImageReference resolves the target image reference for resource under u and
// reports whether the uploader applies. Without an imageReference template it applies
// only to an OCI registry target and a non-empty reference name, and yields
// targetRepository + "/" + referenceName. With a template, the available aliases are
// rewritten; referencing an unavailable alias is an error.
func ociImageReference(u *transferv1alpha1.OCIUploaderConfig, access runtime.Typed, resource descriptorv2.Resource, baseID string, i int, toSpec runtime.Typed) (string, bool, error) {
	referenceName, supported, err := ociReferenceName(access)
	if err != nil || !supported {
		return "", false, err
	}
	targetRepository, isOCITarget := ociTargetRepository(toSpec)

	if u.ImageReference == "" {
		if !isOCITarget || referenceName == "" {
			return "", false, nil
		}
		return targetRepository + "/" + referenceName, true, nil
	}

	aliases := map[string]string{resourceAlias: resourceNodePath(baseID, i)}
	if referenceName != "" {
		aliases[referenceNameAlias] = strconv.Quote(referenceName)
	}
	if isOCITarget {
		aliases[targetRepositoryAlias] = strconv.Quote(targetRepository)
	}
	imageReference, exprs, err := templateString(u.ImageReference, aliases)
	if err != nil {
		return "", false, fmt.Errorf("cannot template imageReference: %w", err)
	}
	for _, expr := range exprs {
		if !isOCITarget && identifierUsed(expr, targetRepositoryAlias) {
			return "", false, fmt.Errorf("imageReference references %s, but target %s is not an OCI registry", targetRepositoryAlias, targetKind(toSpec))
		}
		if referenceName == "" && identifierUsed(expr, referenceNameAlias) {
			return "", false, fmt.Errorf("imageReference references %s, but resource %s has no reference name", referenceNameAlias, resource.ToIdentity())
		}
	}
	return imageReference, true, nil
}

// processOCIUploader emits the transformations that upload resource as a separate OCI
// artifact according to u. It reports false without emitting anything when the
// uploader does not apply to the resource, so the caller falls through to the next
// uploader or the default handling. It returns the CEL spec-field expressions of the
// file buffers produced, for cleanup.
func processOCIUploader(ctx context.Context, resource descriptorv2.Resource, access runtime.Typed, u *transferv1alpha1.OCIUploaderConfig, baseID, id string, val *discoveryValue, tgd *transformv1alpha1.TransformationGraphDefinition, toSpec runtime.Typed, resourceTransformIDs map[int]string, i int) (bool, []string, error) {
	imageReference, ok, err := ociImageReference(u, access, resource, baseID, i, toSpec)
	if err != nil {
		return false, nil, err
	}
	if !ok {
		slog.DebugContext(ctx, "oci uploader does not apply to resource, using default handling",
			"component", val.Descriptor.Component.Name, "version", val.Descriptor.Component.Version,
			"resource", resource.ToIdentity().String(), "accessType", resource.Access.Type.String(),
			"target", targetKind(toSpec))
		return false, nil, nil
	}

	resourceID := identityToTransformationID(resource.ToIdentity())
	switch access.(type) {
	case *ociv1.OCIImage:
		// Streaming (TransferOCIArtifact) produces no temp file, so there is nothing to clean up.
		if err := processOCIArtifactStreaming(resource, id, tgd, resourceTransformIDs, i, imageReference, transferLabel(&val.Descriptor.Component, resource.Name, toSpec)); err != nil {
			return false, nil, fmt.Errorf("cannot process OCI artifact resource: %w", err)
		}
		return true, nil, nil
	case *helmv1.Helm:
		if err := processHelm(resource, id, val, tgd, toSpec, resourceTransformIDs, i, imageReference); err != nil {
			return false, nil, fmt.Errorf("cannot process Helm Chart resource: %w", err)
		}
		return true, helmFileExpressions(id, resourceID), nil
	case *descriptorv2.LocalBlob:
		if err := processLocalBlob(resource, id, val, tgd, toSpec, resourceTransformIDs, i, imageReference); err != nil {
			return false, nil, fmt.Errorf("failed processing local blob resource: %w", err)
		}
		return true, []string{fmt.Sprintf("${%sAdd%s.spec.file}", id, resourceID)}, nil
	default:
		// ociReferenceName only supports the access types above.
		return false, nil, fmt.Errorf("unsupported access type %T for oci uploader", access)
	}
}
