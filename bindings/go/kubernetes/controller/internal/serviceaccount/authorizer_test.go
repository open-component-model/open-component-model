package serviceaccount

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	authorizationv1 "k8s.io/api/authorization/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"ocm.software/open-component-model/bindings/go/kubernetes/controller/api/v1alpha1"
)

const namespace = "team-a"

// serviceAccounts counts the lookups of the only service account that exists.
type serviceAccounts struct {
	client.Reader
	existing string
	lookups  int
}

func (s *serviceAccounts) Get(_ context.Context, key client.ObjectKey, _ client.Object, _ ...client.GetOption) error {
	s.lookups++
	if key.Name != s.existing {
		return apierrors.NewNotFound(corev1.Resource("serviceaccounts"), key.Name)
	}

	return nil
}

// reviews records the namespaces of the reviewed objects and allows none of them.
type reviews struct {
	client.Writer
	namespaces []string
}

func (r *reviews) Create(_ context.Context, obj client.Object, _ ...client.CreateOption) error {
	review := obj.(*authorizationv1.SubjectAccessReview)
	r.namespaces = append(r.namespaces, review.Spec.ResourceAttributes.Namespace)

	return nil
}

func component(serviceAccount string, configs ...v1alpha1.OCMConfiguration) *v1alpha1.Component {
	return &v1alpha1.Component{
		ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: "component"},
		Spec:       v1alpha1.ComponentSpec{ServiceAccountName: serviceAccount, OCMConfig: configs},
	}
}

func config(kind, namespace string) v1alpha1.OCMConfiguration {
	return v1alpha1.OCMConfiguration{
		NamespacedObjectKindReference: v1alpha1.NamespacedObjectKindReference{
			APIVersion: v1alpha1.GroupVersion.String(), Kind: kind, Name: "creds", Namespace: namespace,
		},
	}
}

func TestAuthorizeConfigs(t *testing.T) {
	tests := []struct {
		name                string
		reviewSameNamespace bool
		serviceAccount      string
		configs             []v1alpha1.OCMConfiguration
		reviewed            []string
		lookups             int
		err                 string
	}{
		{
			name:    "trusts its own namespace without looking up the service account",
			configs: []v1alpha1.OCMConfiguration{config("Secret", ""), config("Secret", namespace)},
		},
		{
			name:           "trusts its own namespace even if the service account is missing",
			serviceAccount: "missing",
			configs:        []v1alpha1.OCMConfiguration{config("Secret", namespace)},
		},
		{
			name:     "reviews another namespace only, and once",
			configs:  []v1alpha1.OCMConfiguration{config("Secret", namespace), config("Secret", "team-b"), config("Secret", "team-b")},
			reviewed: []string{"team-b"},
			lookups:  1,
			err:      "service account team-a/default may not get it in namespace team-b",
		},
		{
			name:           "does not review for a missing service account",
			serviceAccount: "missing",
			configs:        []v1alpha1.OCMConfiguration{config("Secret", "team-b")},
			lookups:        1,
			err:            `serviceaccounts "missing" not found`,
		},
		{
			name:                "reviews its own namespace if asked to",
			reviewSameNamespace: true,
			configs:             []v1alpha1.OCMConfiguration{config("Secret", ""), config("Secret", "team-b")},
			reviewed:            []string{namespace, "team-b"},
			lookups:             1,
			err:                 "service account team-a/default may not get it in namespace team-a",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := require.New(t)
			accounts := &serviceAccounts{existing: DefaultName}
			recorded := &reviews{}
			authorizer := NewAuthorizer(accounts, recorded, tt.reviewSameNamespace)

			err := authorizer.AuthorizeConfigs(t.Context(), component(tt.serviceAccount), tt.configs)

			if tt.err == "" {
				r.NoError(err)
			} else {
				r.ErrorContains(err, tt.err)
			}
			r.Equal(tt.reviewed, recorded.namespaces)
			r.Equal(tt.lookups, accounts.lookups)
		})
	}
}

func TestAuthorizeWatchesReviewsItsOwnNamespace(t *testing.T) {
	r := require.New(t)
	recorded := &reviews{}
	authorizer := NewAuthorizer(&serviceAccounts{existing: DefaultName}, recorded, false)
	configMaps := Watch{Resource: corev1.Resource("configmaps"), Namespace: namespace}

	err := authorizer.AuthorizeWatches(t.Context(), component(""), []Watch{configMaps, configMaps})

	r.True(apierrors.IsForbidden(err), "the drift watch is reviewed where the configuration is trusted")
	r.ErrorContains(err, "may not list it in namespace team-a")
	r.ErrorContains(err, "may not watch it in namespace team-a")
	r.Equal([]string{namespace, namespace}, recorded.namespaces, "list and watch are reviewed once per resource")
}

func TestEffectiveConfig(t *testing.T) {
	t.Run("reviews a Secret of the spec once", func(t *testing.T) {
		r := require.New(t)
		recorded := &reviews{}
		authorizer := NewAuthorizer(&serviceAccounts{existing: DefaultName}, recorded, false)

		// Without a client, as Secrets are not read to resolve the configuration.
		_, err := authorizer.EffectiveConfig(t.Context(), nil, component("", config("Secret", "team-b")), nil)

		r.ErrorContains(err, `secrets "creds" is forbidden`)
		r.Equal([]string{"team-b"}, recorded.namespaces)
	})

	t.Run("reviews an OCM object of the spec before reading it", func(t *testing.T) {
		r := require.New(t)
		recorded := &reviews{}
		authorizer := NewAuthorizer(&serviceAccounts{existing: DefaultName}, recorded, false)

		// Without a client, as reading the Repository would panic.
		_, err := authorizer.EffectiveConfig(t.Context(), nil, component("", config(v1alpha1.KindRepository, "team-b")), nil)

		r.ErrorContains(err, `repositories.delivery.ocm.software "creds" is forbidden`)
		r.Equal([]string{"team-b"}, recorded.namespaces)
	})
}
