// Package serviceaccount authorizes the objects a reconciliation reads on behalf of an OCM object against the
// service account of that object, so the controller's own cluster-wide permissions are never what grants access.
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
)

// DefaultName is the service account used when an object names none. It exists in every namespace and has no
// permissions unless an operator grants them.
const DefaultName = "default"

// Name returns the given service account name, or DefaultName if it is empty.
func Name(name string) string {
	if name == "" {
		return DefaultName
	}

	return name
}

// Username returns the user name the API server authenticates the service account as.
func Username(namespace, name string) string {
	return fmt.Sprintf("system:serviceaccount:%s:%s", namespace, name)
}

// Groups returns the groups the API server authenticates a service account of namespace with.
func Groups(namespace string) []string {
	return []string{"system:serviceaccounts", "system:serviceaccounts:" + namespace, "system:authenticated"}
}

// Authorizer creates Access checks for service accounts.
type Authorizer struct {
	// reader reads service accounts uncached to avoid a cluster-wide informer for them.
	reader client.Reader
	// reviews creates the SubjectAccessReviews. Unlike a SelfSubjectAccessReview it names the subject, so the
	// controller needs no impersonation grant.
	reviews client.Writer
}

func NewAuthorizer(reader client.Reader, reviews client.Writer) *Authorizer {
	return &Authorizer{reader: reader, reviews: reviews}
}

// Access checks what a single service account may read. It is meant to live for one reconciliation.
type Access struct {
	namespace      string
	serviceAccount string
	reviews        client.Writer
	allowed        map[reference]bool
}

type reference struct {
	resource  schema.GroupResource
	namespace string
	name      string
}

// For returns the Access of the service account name in namespace, falling back to DefaultName.
func (a *Authorizer) For(ctx context.Context, namespace, name string) (*Access, error) {
	key := client.ObjectKey{Namespace: namespace, Name: Name(name)}
	if err := a.reader.Get(ctx, key, &corev1.ServiceAccount{}); err != nil {
		return nil, fmt.Errorf("failed to get service account %s: %w", key, err)
	}

	return &Access{
		namespace:      key.Namespace,
		serviceAccount: key.Name,
		reviews:        a.reviews,
		allowed:        make(map[reference]bool),
	}, nil
}

// AuthorizeConfigs returns a Forbidden error for every referenced object the service account may not get. An empty
// namespace in a reference means the namespace of the service account.
func (a *Access) AuthorizeConfigs(ctx context.Context, configs []v1alpha1.OCMConfiguration) error {
	var errs []error
	for _, config := range configs {
		ref, err := a.reference(config)
		if err != nil {
			return err
		}

		allowed, err := a.authorize(ctx, ref)
		if err != nil {
			return err
		}
		if !allowed {
			errs = append(errs, apierrors.NewForbidden(ref.resource, ref.name, fmt.Errorf(
				"service account %s/%s may not get it in namespace %s", a.namespace, a.serviceAccount, ref.namespace)))
		}
	}

	return errors.Join(errs...)
}

func (a *Access) reference(config v1alpha1.OCMConfiguration) (reference, error) {
	ref := reference{namespace: config.Namespace, name: config.Name}
	if ref.namespace == "" {
		ref.namespace = a.namespace
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

func (a *Access) authorize(ctx context.Context, ref reference) (bool, error) {
	if allowed, ok := a.allowed[ref]; ok {
		return allowed, nil
	}

	review := &authorizationv1.SubjectAccessReview{
		Spec: authorizationv1.SubjectAccessReviewSpec{
			User:   Username(a.namespace, a.serviceAccount),
			Groups: Groups(a.namespace),
			ResourceAttributes: &authorizationv1.ResourceAttributes{
				Namespace: ref.namespace,
				Verb:      "get",
				Group:     ref.resource.Group,
				Resource:  ref.resource.Resource,
				Name:      ref.name,
			},
		},
	}
	if err := a.reviews.Create(ctx, review); err != nil {
		return false, fmt.Errorf("failed to review access of service account %s/%s to %s %s/%s: %w",
			a.namespace, a.serviceAccount, ref.resource, ref.namespace, ref.name, err)
	}

	a.allowed[ref] = review.Status.Allowed

	return review.Status.Allowed, nil
}
