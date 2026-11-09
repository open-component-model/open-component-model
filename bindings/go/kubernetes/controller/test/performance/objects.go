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
		ObjectMeta: w.meta(i, w.repositoryName(i)),
		Spec: v1alpha1.RepositorySpec{
			RepositorySpec: &apiextensionsv1.JSON{Raw: spec},
			Interval:       metav1.Duration{Duration: reconcileInterval},
		},
	}, nil
}

func (w workload) component(i int) *v1alpha1.Component {
	semver := initialVersion
	if w.usesConstraint() {
		semver = versionConstraint
	}
	c := &v1alpha1.Component{
		ObjectMeta: w.meta(i, fmt.Sprintf("c-%04d", i)),
		Spec: v1alpha1.ComponentSpec{
			RepositoryRef: corev1.LocalObjectReference{Name: w.repositoryName(i)},
			Component:     w.componentName(i),
			Semver:        semver,
			Interval:      metav1.Duration{Duration: reconcileInterval},
		},
	}
	if w.signed() {
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
		ObjectMeta: w.meta(0, signingSecretName),
		StringData: map[string]string{v1alpha1.OCMConfigKey: cfg},
	}, nil
}

func (w workload) resource(i int) *v1alpha1.Resource {
	ref := v1alpha1.ResourceReference{Resource: ocmruntime.Identity{"name": resourceName}}
	if w.hasReferenceChain() {
		for range w.depth {
			ref.ReferencePath = append(ref.ReferencePath, ocmruntime.Identity{"name": referenceName})
		}
	}
	return &v1alpha1.Resource{
		ObjectMeta: w.meta(i, fmt.Sprintf("r-%04d", i)),
		Spec: v1alpha1.ResourceSpec{
			ComponentRef: corev1.LocalObjectReference{Name: fmt.Sprintf("c-%04d", i)},
			Resource:     v1alpha1.ResourceID{ByReference: ref},
		},
	}
}

// fanoutResource is the j-th Resource of object i. It wraps around when
// there are more Resources than descriptor entries.
func (w workload) fanoutResource(i, j int) *v1alpha1.Resource {
	return &v1alpha1.Resource{
		ObjectMeta: w.meta(i, fmt.Sprintf("r-%04d-%03d", i, j)),
		Spec: v1alpha1.ResourceSpec{
			ComponentRef: corev1.LocalObjectReference{Name: fmt.Sprintf("c-%04d", i)},
			Resource: v1alpha1.ResourceID{ByReference: v1alpha1.ResourceReference{
				Resource: ocmruntime.Identity{"name": imageResourceName(j % w.resources)},
			}},
		},
	}
}

func (w workload) deployer(i int) *v1alpha1.NamespacedDeployer {
	return &v1alpha1.NamespacedDeployer{
		ObjectMeta: w.meta(i, fmt.Sprintf("d-%04d", i)),
		Spec: v1alpha1.NamespacedDeployerSpec{
			ResourceRef:        corev1.LocalObjectReference{Name: fmt.Sprintf("r-%04d", i)},
			ServiceAccountName: deployerAccount,
		},
	}
}

// deployerRBAC is the service account the NamespacedDeployers of object i's namespace apply the ConfigMaps as.
func (w workload) deployerRBAC(i int) []client.Object {
	return []client.Object{
		&corev1.ServiceAccount{ObjectMeta: w.meta(i, deployerAccount)},
		&rbacv1.Role{
			ObjectMeta: w.meta(i, deployerAccount),
			Rules: []rbacv1.PolicyRule{{
				APIGroups: []string{""},
				Resources: []string{"configmaps"},
				Verbs:     []string{"get", "list", "watch", "create", "update", "patch", "delete"},
			}},
		},
		&rbacv1.RoleBinding{
			ObjectMeta: w.meta(i, deployerAccount),
			RoleRef:    rbacv1.RoleRef{APIGroup: rbacv1.GroupName, Kind: "Role", Name: deployerAccount},
			Subjects:   []rbacv1.Subject{{Kind: rbacv1.ServiceAccountKind, Name: deployerAccount, Namespace: w.namespaceOf(i)}},
		},
	}
}

// objectsToCreate returns everything the run applies, the signing Secret, deployer RBAC and Repositories first.
func (w workload) objectsToCreate() ([]client.Object, error) {
	// Tenants get the deployer RBAC and the Repository in each namespace.
	perNamespace, repositories := 1, w.repositories()
	if w.tenants() {
		perNamespace, repositories = w.objects, w.objects
	}
	objs := make([]client.Object, 0, 1+3*perNamespace+repositories+(2+w.resourcesPerComponent())*w.objects)
	if w.signed() {
		secret, err := w.signingSecret()
		if err != nil {
			return nil, err
		}
		objs = append(objs, secret)
	}
	if w.hasDeployers() {
		for i := range perNamespace {
			objs = append(objs, w.deployerRBAC(i)...)
		}
	}
	for i := range repositories {
		repo, err := w.repository(i)
		if err != nil {
			return nil, err
		}
		objs = append(objs, repo)
	}
	for i := range w.objects {
		objs = append(objs, w.component(i))
		switch {
		case w.scenario == scenarioFanout:
			for j := range w.fanout {
				objs = append(objs, w.fanoutResource(i, j))
			}
		case w.hasResources():
			objs = append(objs, w.resource(i))
		}
		if w.hasDeployers() {
			objs = append(objs, w.deployer(i))
		}
	}
	return objs, nil
}

// meta is the metadata of an object in object i's namespace.
func (w workload) meta(i int, name string) metav1.ObjectMeta {
	return metav1.ObjectMeta{Name: name, Namespace: w.namespaceOf(i), Labels: map[string]string{runLabel: w.runID}}
}
