// Package serviceaccounttest provides service account fixtures for controller tests.
package serviceaccounttest

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	authorizationv1 "k8s.io/api/authorization/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"

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

// CreateConfigSecret creates the Secret "creds" holding an OCM config in namespace.
func CreateConfigSecret(ctx context.Context, c client.Client, namespace string) *corev1.Secret {
	GinkgoHelper()

	secret := &corev1.Secret{
		Name: "creds", Namespace: namespace,
		StringData: map[string]string{v1alpha1.OCMConfigKey: `
type: generic.config.ocm.software/v1
configurations:
- type: credentials.config.ocm.software
  consumers:
  - identity:
      type: MavenRepository
      hostname: example.com
    credentials:
    - type: Credentials
      properties:
        username: user
        password: secret
`},
	}
	Expect(c.Create(ctx, secret)).To(Succeed())

	return secret
}

// PermissiveAuthorizer returns an Authorizer for fake clients, which cannot review access: every service account
// exists and is allowed everything.
func PermissiveAuthorizer() *serviceaccount.Authorizer {
	return serviceaccount.NewAuthorizer(existingServiceAccounts{}, allowedReviews{}, false)
}

// DenyingAuthorizer returns an Authorizer for fake clients where every service account exists but may read nothing
// outside its namespace.
func DenyingAuthorizer() *serviceaccount.Authorizer {
	return serviceaccount.NewAuthorizer(existingServiceAccounts{}, deniedReviews{}, false)
}

type deniedReviews struct{ client.Writer }

func (deniedReviews) Create(context.Context, client.Object, ...client.CreateOption) error {
	return nil
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
