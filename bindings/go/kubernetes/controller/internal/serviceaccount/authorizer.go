// Package serviceaccount authorizes the objects a reconciliation reads or watches on behalf of an OCM object against the
// service account of that object, so the controller's own cluster-wide permissions do not reach across namespaces.
package serviceaccount

import (
	"context"
	"errors"
	"fmt"

	authorizationv1 "k8s.io/api/authorization/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"ocm.software/open-component-model/bindings/go/kubernetes/controller/api/v1alpha1"
	"ocm.software/open-component-model/bindings/go/kubernetes/controller/internal/ocm"
)

// DefaultName is the name of the default service account in a namespace.
const DefaultName = "default"

// Name return name or DefaultName.
func Name(name string) string {
	if name == "" {
		return DefaultName
	}

	return name
}

// Username defines the user for the service account to check.
func Username(namespace, name string) string {
	return fmt.Sprintf("system:serviceaccount:%s:%s", namespace, name)
}

// Groups returns the groups the API server authenticates a service account of namespace with.
func Groups(namespace string) []string {
	return []string{"system:serviceaccounts", "system:serviceaccounts:" + namespace, "system:authenticated"}
}

// Subject is an OCM object that reads its configuration with the permissions of a service account in its namespace.
type Subject interface {
	v1alpha1.ConfigRefProvider
	GetServiceAccountName() string
}

// Authorizer reviews what the service account of an OCM object may read or watch.
type Authorizer struct {
	// reader reads service accounts uncached to avoid a cluster-wide informer for them.
	reader client.Reader
	// reviews creates the SubjectAccessReviews. Unlike a SelfSubjectAccessReview the subject is given so
	// no impersonation is needed.
	reviews client.Writer
	// reviewSameNamespace also reviews configuration in the namespace of the object. Without it the namespace is the
	// trust boundary, as it is for a Pod that mounts a Secret.
	reviewSameNamespace bool
}

func NewAuthorizer(reader client.Reader, reviews client.Writer, reviewSameNamespace bool) *Authorizer {
	return &Authorizer{reader: reader, reviews: reviews, reviewSameNamespace: reviewSameNamespace}
}

// Watch is a resource that is watched for drift in Namespace. An empty Namespace means cluster-scoped.
type Watch struct {
	Resource  schema.GroupResource
	Namespace string
}

type reference struct {
	verb      string
	resource  schema.GroupResource
	namespace string
	name      string
}

// EffectiveConfig returns the effective OCM configuration of obj, see ocm.GetEffectiveConfig. We first read and check
// every reference. Then, we authorize every object in the gathered configs.
func (a *Authorizer) EffectiveConfig(ctx context.Context, c client.Client, obj Subject, parent v1alpha1.ConfigRefProvider) ([]v1alpha1.OCMConfiguration, error) {
	var objects []v1alpha1.OCMConfiguration
	for _, config := range obj.GetSpecifiedOCMConfig() {
		if config.Kind != "Secret" && config.Kind != "ConfigMap" {
			objects = append(objects, config)
		}
	}
	if err := a.AuthorizeConfigs(ctx, obj, objects); err != nil {
		return nil, fmt.Errorf("failed to authorize ocm config: %w", err)
	}

	configs, err := ocm.GetEffectiveConfig(ctx, c, obj, parent)
	if err != nil {
		return nil, fmt.Errorf("failed to get effective config: %w", err)
	}

	if err := a.AuthorizeConfigs(ctx, obj, configs); err != nil {
		return nil, fmt.Errorf("failed to authorize effective ocm config: %w", err)
	}

	return configs, nil
}

// AuthorizeConfigs returns a Forbidden error for every referenced object the service account of obj can not GET.
// References into its own namespace, which an empty namespace means, pass unless the same namespace is reviewed.
func (a *Authorizer) AuthorizeConfigs(ctx context.Context, obj Subject, configs []v1alpha1.OCMConfiguration) error {
	var refs []reference
	for _, config := range configs {
		ref, err := configReference(obj.GetNamespace(), config)
		if err != nil {
			return err
		}

		if !a.reviewSameNamespace && ref.namespace == obj.GetNamespace() {
			continue
		}
		refs = append(refs, ref)
	}

	return a.authorize(ctx, obj, refs)
}

// AuthorizeWatches returns a Forbidden error for every watch the service account of obj may not list and watch.
func (a *Authorizer) AuthorizeWatches(ctx context.Context, obj Subject, watches []Watch) error {
	refs := make([]reference, 0, 2*len(watches)) //nolint:mnd // list and watch
	for _, watch := range watches {
		for _, verb := range []string{"list", "watch"} {
			refs = append(refs, reference{verb: verb, resource: watch.Resource, namespace: watch.Namespace})
		}
	}

	return a.authorize(ctx, obj, refs)
}

// configReference returns the reference to get the object of config, which defaults to namespace.
func configReference(namespace string, config v1alpha1.OCMConfiguration) (reference, error) {
	ref := reference{verb: "get", namespace: config.Namespace, name: config.Name}
	if ref.namespace == "" {
		ref.namespace = namespace
	}

	switch config.Kind {
	case "Secret":
		ref.resource = corev1.Resource("secrets")
	case "ConfigMap":
		ref.resource = corev1.Resource("configmaps")
	case v1alpha1.KindRepository:
		ref.resource = v1alpha1.GroupVersion.WithResource("repositories").GroupResource()
	case v1alpha1.KindComponent:
		ref.resource = v1alpha1.GroupVersion.WithResource("components").GroupResource()
	case v1alpha1.KindResource:
		ref.resource = v1alpha1.GroupVersion.WithResource("resources").GroupResource()
	default:
		return reference{}, fmt.Errorf("unsupported reference kind: %s", config.Kind)
	}

	return ref, nil
}

// authorize asks the API server with a SubjectAccessReview for each of refs, as a SelfSubjectAccessReview would
// review the controller. The service account is only looked up if there is something to review.
func (a *Authorizer) authorize(ctx context.Context, obj Subject, refs []reference) error {
	if len(refs) == 0 {
		return nil
	}

	// A review of a missing service account could still be allowed through the groups of its namespace.
	key := client.ObjectKey{Namespace: obj.GetNamespace(), Name: Name(obj.GetServiceAccountName())}
	if err := a.reader.Get(ctx, key, &corev1.ServiceAccount{}); err != nil {
		return fmt.Errorf("failed to get service account %s: %w", key, err)
	}

	var errs []error
	// deduplicate references (multiple objects could reference the same object)
	reviewed := make(map[reference]struct{}, len(refs))
	for _, ref := range refs {
		if _, ok := reviewed[ref]; ok {
			continue
		}
		reviewed[ref] = struct{}{}

		review := &authorizationv1.SubjectAccessReview{
			Spec: authorizationv1.SubjectAccessReviewSpec{
				User:   Username(key.Namespace, key.Name),
				Groups: Groups(key.Namespace),
				ResourceAttributes: &authorizationv1.ResourceAttributes{
					Namespace: ref.namespace,
					Verb:      ref.verb,
					Group:     ref.resource.Group,
					Resource:  ref.resource.Resource,
					Name:      ref.name,
				},
			},
		}
		if err := a.reviews.Create(ctx, review); err != nil {
			return fmt.Errorf("failed to review %s access of service account %s to %s %s/%s: %w",
				ref.verb, key, ref.resource, ref.namespace, ref.name, err)
		}

		// A review that could not be evaluated is no denial, so granting access would not help.
		if !review.Status.Allowed && review.Status.EvaluationError != "" {
			errs = append(errs, fmt.Errorf("failed to evaluate %s access of service account %s to %s %s/%s: %s",
				ref.verb, key, ref.resource, ref.namespace, ref.name, review.Status.EvaluationError))

			continue
		}

		if !review.Status.Allowed {
			scope := "in namespace " + ref.namespace
			if ref.namespace == "" {
				scope = "cluster-wide"
			}
			errs = append(errs, apierrors.NewForbidden(ref.resource, ref.name, fmt.Errorf(
				"service account %s may not %s it %s", key, ref.verb, scope)))
		}
	}

	return errors.Join(errs...)
}
