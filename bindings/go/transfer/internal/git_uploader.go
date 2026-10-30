package internal

import (
	"context"
	"fmt"
	"net/url"
	"slices"
	"strings"

	descriptorv2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	gitv1 "ocm.software/open-component-model/bindings/go/git/spec/access/v1"
	gitv1alpha1 "ocm.software/open-component-model/bindings/go/git/transformation/spec/v1alpha1"
	"ocm.software/open-component-model/bindings/go/runtime"
	transferv1alpha1 "ocm.software/open-component-model/bindings/go/transfer/v1alpha1/spec"
	transformv1alpha1 "ocm.software/open-component-model/bindings/go/transform/spec/v1alpha1"
	"ocm.software/open-component-model/bindings/go/transform/spec/v1alpha1/meta"
)

// gitArchiveMediaType is the media type of the archive a Git resource is downloaded as, and
// so of a local blob a Git resource was copied into before its origin was recorded (see
// [transferv1alpha1.GitLocalBlobMediaType]).
const gitArchiveMediaType = "application/x-tgz"

// processGitUploader emits the transformations that push resource, selected by u, into a
// Git repository: the archive holding its history is read (GetGitResource for a Git access
// pinned to a commit, the source's GetLocalResource for a local blob holding such an archive)
// and pushed (AddGitResource). The target repository and ref come from the uploader config,
// which must set both explicitly. A resource the uploader cannot push is an error: the
// uploader's match selected it, so the config must be adjusted. It returns the CEL spec-field
// expressions of the file buffers produced, for cleanup.
func processGitUploader(ctx context.Context, resource descriptorv2.Resource, access runtime.Typed, u *transferv1alpha1.GitUploaderConfig, aliases map[string]string, env *uploaderEnv, id string, val *discoveryValue, tgd *transformv1alpha1.TransformationGraphDefinition, resourceTransformIDs map[int]string, i int) ([]string, error) {
	resourceID := identityToTransformationID(resource.ToIdentity())
	getResourceID := fmt.Sprintf("%sGet%s", id, resourceID)
	addResourceID := fmt.Sprintf("%sAdd%s", id, resourceID)

	var commit string
	var err error
	switch acc := access.(type) {
	case *gitv1.Git:
		commit = acc.Commit
		var getTransform transformv1alpha1.GenericTransformation
		if getTransform, err = getGitTransformation(resource, acc, getResourceID, val); err != nil {
			return nil, err
		}
		tgd.Transformations = append(tgd.Transformations, getTransform)
	case *descriptorv2.LocalBlob:
		if !isGitArchiveLocalBlob(acc) {
			return nil, fmt.Errorf("git uploader cannot upload local blob with media type %q: not a Git archive (adjust match)", acc.MediaType)
		}
		if strings.TrimSpace(u.Repository) == "" || strings.TrimSpace(u.Ref) == "" {
			return nil, fmt.Errorf("git uploader cannot derive the target from a local blob, which does not carry its Git origin: set repository and ref in the git uploader config")
		}
		if err = appendGetLocalResourceTransform(tgd, resource, val, getResourceID, getLabel(&val.Descriptor.Component, resource.Name)); err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("git uploader cannot upload access type %s (adjust match)", resource.Access.Type)
	}

	repositoryTemplate, repositoryValue, err := uploaderTemplate(ctx, "repository", u.Repository, aliases, env)
	if err != nil {
		return nil, err
	}
	refTemplate, refValue, err := uploaderTemplate(ctx, "ref", u.Ref, aliases, env)
	if err != nil {
		return nil, err
	}
	if err := validateGitTarget(repositoryValue, refValue, commit); err != nil {
		return nil, err
	}

	targetAccess := map[string]any{
		"type":       runtime.NewVersionedType(gitv1.Type, gitv1.Version).String(),
		"repository": repositoryTemplate,
		"ref":        refTemplate,
	}
	if commit != "" {
		targetAccess["commit"] = commit
	}
	tgd.Transformations = append(tgd.Transformations, transformv1alpha1.GenericTransformation{
		TransformationMeta: meta.TransformationMeta{
			Type:  gitv1alpha1.AddGitResourceV1alpha1,
			ID:    addResourceID,
			Label: uploaderLabel(&val.Descriptor.Component, resource.Name, gitHost(repositoryValue)),
		},
		Spec: &runtime.Unstructured{Data: map[string]any{
			"resource": map[string]any{
				"name":          fmt.Sprintf("${%s.output.resource.name}", getResourceID),
				"version":       fmt.Sprintf("${%s.output.resource.version}", getResourceID),
				"type":          fmt.Sprintf("${%s.output.resource.type}", getResourceID),
				"relation":      fmt.Sprintf("${%s.output.resource.relation}", getResourceID),
				"access":        targetAccess,
				"digest":        fmt.Sprintf("${has(%s.output.resource.digest) ? %s.output.resource.digest : null}", getResourceID, getResourceID),
				"labels":        fmt.Sprintf("${has(%s.output.resource.labels) ? %s.output.resource.labels : []}", getResourceID, getResourceID),
				"extraIdentity": fmt.Sprintf("${has(%s.output.resource.extraIdentity) ? %s.output.resource.extraIdentity : {}}", getResourceID, getResourceID),
				"srcRefs":       fmt.Sprintf("${has(%s.output.resource.srcRefs) ? %s.output.resource.srcRefs : []}", getResourceID, getResourceID),
			},
			"file": fmt.Sprintf("${%s.output.file}", getResourceID),
		}},
	})
	resourceTransformIDs[i] = addResourceID
	return []string{fmt.Sprintf("${%s.spec.file}", addResourceID)}, nil
}

// isGitArchiveLocalBlob reports whether access is a local blob holding the archive of a Git
// resource, as left behind by copying a Git resource by value.
func isGitArchiveLocalBlob(access runtime.Typed) bool {
	blob, ok := access.(*descriptorv2.LocalBlob)
	return ok && (blob.MediaType == transferv1alpha1.GitLocalBlobMediaType || blob.MediaType == gitArchiveMediaType)
}

// checkUnusedGitUploaders fails a transfer whose Git uploaders selected nothing while Git
// archive local blobs fell back to being copied as local blobs. That combination means the
// push the configuration asks for silently does not happen: the default match only selects
// Git accesses, and a Git resource copied by value (e.g. across an air gap) is a local blob.
func checkUnusedGitUploaders(uploaders []transferv1alpha1.UploaderConfig, usage *uploaderUsage) error {
	if len(usage.unselectedGitArchives) == 0 {
		return nil
	}
	for idx, u := range uploaders {
		if _, ok := u.(*transferv1alpha1.GitUploaderConfig); !ok || usage.used[idx] {
			continue
		}
		archives := slices.Compact(slices.Sorted(slices.Values(usage.unselectedGitArchives)))
		return fmt.Errorf("git uploader %d (match %q) selected no resource, but these Git archive local blobs would only be copied, not pushed: %s; "+
			`to push them, set match: resource.access.isType("LocalBlob") && has(resource.access.mediaType) && resource.access.mediaType == %q`,
			idx, u.EffectiveMatch(), strings.Join(archives, ", "), transferv1alpha1.GitLocalBlobMediaType)
	}
	return nil
}

// validateGitTarget rejects a target the upload would reject, so a wrong uploader config
// fails the build instead of the running transfer: the upload points a full branch or tag
// ref at the commit. The uploader config must set a full ref; a short name does not say
// whether it names a branch or a tag.
func validateGitTarget(repository, ref, commit string) error {
	target, err := (&gitv1.Git{Repository: repository, Ref: ref, Commit: commit}).Normalize()
	if err != nil {
		return fmt.Errorf("invalid git upload target: %w", err)
	}
	if err := target.Validate(); err != nil {
		return fmt.Errorf("invalid git upload target: %w", err)
	}
	ref = target.Ref
	if strings.HasPrefix(ref, "refs/heads/") || strings.HasPrefix(ref, "refs/tags/") {
		return nil
	}
	if ref != "" && ref != "HEAD" && !strings.HasPrefix(ref, "refs/") {
		return fmt.Errorf("invalid git upload target: ref %q is a short name, which does not say whether it is a branch or a tag; set ref in the git uploader config to the full ref, e.g. refs/heads/%s or refs/tags/%s", ref, ref, ref)
	}
	return fmt.Errorf("invalid git upload target: ref %q is not a full branch or tag ref (set ref in the git uploader config, e.g. refs/heads/main)", ref)
}

// gitHost returns the host of a Git repository URL for the transformation label, or
// "target" for scp-like URLs and local paths. Credentials in the URL never reach the label.
func gitHost(repository string) string {
	parsed, err := url.Parse(repository)
	if err != nil || parsed.Host == "" {
		return "target"
	}
	return parsed.Host
}
