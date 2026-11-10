package main

import (
	"encoding/json"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"ocm.software/open-component-model/bindings/go/kubernetes/controller/api/v1alpha1"
	ocmruntime "ocm.software/open-component-model/bindings/go/runtime"
)

func (w workload) repository(i int) (*v1alpha1.Repository, error) {
	fields := map[string]string{"type": "OCIRegistry", "baseUrl": w.registryURL}
	if subPath := w.subPath(i); subPath != "" {
		fields["subPath"] = subPath
	}
	spec, err := json.Marshal(fields)
	if err != nil {
		return nil, err
	}
	return &v1alpha1.Repository{
		ObjectMeta: w.meta(w.repositoryName(i)),
		Spec: v1alpha1.RepositorySpec{
			RepositorySpec: &apiextensionsv1.JSON{Raw: spec},
			Interval:       metav1.Duration{Duration: reconcileInterval},
		},
	}, nil
}

func (w workload) component(i int) *v1alpha1.Component {
	c := &v1alpha1.Component{
		ObjectMeta: w.meta(fmt.Sprintf("c-%04d", i)),
		Spec: v1alpha1.ComponentSpec{
			RepositoryRef: corev1.LocalObjectReference{Name: w.repositoryName(i)},
			Component:     w.componentName(i),
			Semver:        versionConstraint,
			Interval:      metav1.Duration{Duration: reconcileInterval},
		},
	}
	if w.sign {
		// Propagated, so the Resource and Deployer verify the root as well.
		c.Spec.OCMConfig = []v1alpha1.OCMConfiguration{{
			NamespacedObjectKindReference: v1alpha1.NamespacedObjectKindReference{
				APIVersion: "v1", Kind: "Secret", Name: signingSecretName,
			},
			Policy: v1alpha1.ConfigurationPolicyPropagate,
		}}
	}
	return c
}

// signingSecret holds the OCM config that makes the controller verify the
// signatures named signatureName with the run's public key.
func (w workload) signingSecret() (*corev1.Secret, error) {
	cfg, err := verificationConfig(w.publicKeyPEM)
	if err != nil {
		return nil, err
	}
	return &corev1.Secret{
		ObjectMeta: w.meta(signingSecretName),
		StringData: map[string]string{v1alpha1.OCMConfigKey: cfg},
	}, nil
}

func (w workload) resource(i int) *v1alpha1.Resource {
	ref := v1alpha1.ResourceReference{Resource: ocmruntime.Identity{"name": resourceName}}
	for range w.depth {
		ref.ReferencePath = append(ref.ReferencePath, ocmruntime.Identity{"name": referenceName})
	}
	return &v1alpha1.Resource{
		ObjectMeta: w.meta(fmt.Sprintf("r-%04d", i)),
		Spec: v1alpha1.ResourceSpec{
			ComponentRef: corev1.LocalObjectReference{Name: fmt.Sprintf("c-%04d", i)},
			Resource:     v1alpha1.ResourceID{ByReference: ref},
		},
	}
}

func (w workload) deployer(i int) *v1alpha1.NamespacedDeployer {
	return &v1alpha1.NamespacedDeployer{
		ObjectMeta: w.meta(fmt.Sprintf("d-%04d", i)),
		Spec: v1alpha1.NamespacedDeployerSpec{
			ResourceRef:        corev1.LocalObjectReference{Name: fmt.Sprintf("r-%04d", i)},
			ServiceAccountName: deployerAccount,
		},
	}
}

// deployerRBAC is the service account the NamespacedDeployers apply the ConfigMaps as.
func (w workload) deployerRBAC() []client.Object {
	return []client.Object{
		&corev1.ServiceAccount{ObjectMeta: w.meta(deployerAccount)},
		&rbacv1.Role{
			ObjectMeta: w.meta(deployerAccount),
			Rules: []rbacv1.PolicyRule{{
				APIGroups: []string{""},
				Resources: []string{"configmaps"},
				Verbs:     []string{"get", "list", "watch", "create", "update", "patch", "delete"},
			}},
		},
		&rbacv1.RoleBinding{
			ObjectMeta: w.meta(deployerAccount),
			RoleRef:    rbacv1.RoleRef{APIGroup: rbacv1.GroupName, Kind: "Role", Name: deployerAccount},
			Subjects:   []rbacv1.Subject{{Kind: rbacv1.ServiceAccountKind, Name: deployerAccount, Namespace: w.namespace}},
		},
	}
}

// objectsToCreate returns everything the run applies, the signing Secret, deployer RBAC and Repositories first.
func (w workload) objectsToCreate() ([]client.Object, error) {
	objs := make([]client.Object, 0, 1+3+w.repositories()+3*w.objects)
	if w.sign {
		secret, err := w.signingSecret()
		if err != nil {
			return nil, err
		}
		objs = append(objs, secret)
	}
	objs = append(objs, w.deployerRBAC()...)
	for i := range w.repositories() {
		repo, err := w.repository(i)
		if err != nil {
			return nil, err
		}
		objs = append(objs, repo)
	}
	for i := range w.objects {
		objs = append(objs, w.component(i), w.resource(i), w.deployer(i))
	}
	return objs, nil
}

func (w workload) meta(name string) metav1.ObjectMeta {
	return metav1.ObjectMeta{Name: name, Namespace: w.namespace, Labels: map[string]string{runLabel: w.runID}}
}
