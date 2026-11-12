package test

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	authorizationv1 "k8s.io/api/authorization/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"ocm.software/open-component-model/bindings/go/kubernetes/controller/api/v1alpha1"
	"ocm.software/open-component-model/bindings/go/kubernetes/controller/internal/serviceaccount"
)

// CreateServiceAccount creates the service account name in namespace.
func CreateServiceAccount(ctx context.Context, c client.Client, namespace, name string) {
	GinkgoHelper()

	Expect(createServiceAccount(ctx, c, namespace, name)).To(Succeed())
}

// GrantOCMConfigReads lets the service account sa of saNamespace get the Secrets, ConfigMaps and OCM objects in
// namespace.
func GrantOCMConfigReads(ctx context.Context, c client.Client, namespace, saNamespace, sa string) {
	GinkgoHelper()

	Expect(grantOCMConfigReads(ctx, c, namespace, saNamespace, sa)).To(Succeed())
}

// ProvisionDefaultServiceAccounts stands in for the service account controller envtest does not run. Every namespace
// gets its default service account, allowed to get the OCM configuration in that namespace, for suites that do not
// test the authorization itself.
func ProvisionDefaultServiceAccounts(mgr ctrl.Manager) error {
	c := mgr.GetClient()

	return ctrl.NewControllerManagedBy(mgr).
		Named("test-default-service-accounts").
		For(&corev1.Namespace{}).
		Complete(reconcile.Func(func(ctx context.Context, req reconcile.Request) (reconcile.Result, error) {
			ns := &corev1.Namespace{}
			if err := c.Get(ctx, req.NamespacedName, ns); err != nil {
				return reconcile.Result{}, client.IgnoreNotFound(err)
			}
			// envtest runs no namespace controller either, so a deleted namespace stays terminating and rejects creates.
			if !ns.GetDeletionTimestamp().IsZero() {
				return reconcile.Result{}, nil
			}

			if err := createServiceAccount(ctx, c, ns.GetName(), serviceaccount.DefaultName); err != nil && !apierrors.IsAlreadyExists(err) {
				return reconcile.Result{}, err
			}

			return reconcile.Result{}, grantOCMConfigReads(ctx, c, ns.GetName(), ns.GetName(), serviceaccount.DefaultName)
		}))
}

// PermissiveAuthorizer returns an Authorizer for fake clients, which cannot review access: every service account
// exists and is allowed everything.
func PermissiveAuthorizer() *serviceaccount.Authorizer {
	return serviceaccount.NewAuthorizer(existingServiceAccounts{}, allowedReviews{})
}

type existingServiceAccounts struct{ client.Reader }

func (existingServiceAccounts) Get(context.Context, client.ObjectKey, client.Object, ...client.GetOption) error {
	return nil
}

type allowedReviews struct{ client.Writer }

func (allowedReviews) Create(_ context.Context, obj client.Object, _ ...client.CreateOption) error {
	if review, ok := obj.(*authorizationv1.SubjectAccessReview); ok {
		review.Status.Allowed = true
	}

	return nil
}

func createServiceAccount(ctx context.Context, c client.Client, namespace, name string) error {
	return c.Create(ctx, &corev1.ServiceAccount{Name: name, Namespace: namespace})
}

func grantOCMConfigReads(ctx context.Context, c client.Client, namespace, saNamespace, sa string) error {
	role := &rbacv1.Role{
		Name: saNamespace + "-" + sa, Namespace: namespace,
		Rules: []rbacv1.PolicyRule{
			{APIGroups: []string{""}, Resources: []string{"secrets", "configmaps"}, Verbs: []string{"get"}},
			{
				APIGroups: []string{v1alpha1.GroupVersion.Group},
				Resources: []string{"repositories", "components", "resources"},
				Verbs:     []string{"get"},
			},
		},
	}
	if err := c.Create(ctx, role); err != nil && !apierrors.IsAlreadyExists(err) {
		return err
	}

	binding := &rbacv1.RoleBinding{
		Name: role.GetName(), Namespace: namespace,
		RoleRef:  rbacv1.RoleRef{APIGroup: rbacv1.GroupName, Kind: "Role", Name: role.GetName()},
		Subjects: []rbacv1.Subject{{Kind: rbacv1.ServiceAccountKind, Name: sa, Namespace: saNamespace}},
	}

	if err := c.Create(ctx, binding); err != nil && !apierrors.IsAlreadyExists(err) {
		return err
	}

	return nil
}
