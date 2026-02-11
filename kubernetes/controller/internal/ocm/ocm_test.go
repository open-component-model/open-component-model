package ocm

import (
	"encoding/json"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/Masterminds/semver/v3"
	"github.com/fluxcd/pkg/apis/meta"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sruntime "k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	descruntime "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/oci"
	"ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/ctf"
	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/kubernetes/controller/api/v1alpha1"
	"ocm.software/open-component-model/kubernetes/controller/internal/test"
)

var _ = Describe("ocm utility", func() {
	Context("get effective config", func() {
		const (
			Namespace  = "test-namespace"
			Repository = "test-repository"
			ConfigMap  = "test-configmap"
			Secret     = "test-secret"
		)
		var (
			bldr *fake.ClientBuilder
			clnt ctrl.Client
		)

		// setup scheme
		scheme := k8sruntime.NewScheme()
		utilruntime.Must(clientgoscheme.AddToScheme(scheme))

		utilruntime.Must(v1alpha1.AddToScheme(scheme))

		BeforeEach(func() {
			bldr = fake.NewClientBuilder()
			bldr.WithScheme(scheme)
		})

		AfterEach(func() {
			bldr = nil
			clnt = nil
		})

		It("no config", func(ctx SpecContext) {
			spec := &ctf.Repository{
				Type:       runtime.NewVersionedType(ctf.Type, "v1"),
				FilePath:   "dummy",
				AccessMode: ctf.AccessModeReadOnly,
			}
			specdata, err := json.Marshal(spec)
			Expect(err).ToNot(HaveOccurred())

			repo := v1alpha1.Repository{
				Spec: v1alpha1.RepositorySpec{
					RepositorySpec: &apiextensionsv1.JSON{Raw: specdata},
				},
			}

			config, err := GetEffectiveConfig(ctx, nil, &repo, nil)
			Expect(err).ToNot(HaveOccurred())
			Expect(config).To(BeEmpty())
		})

		It("no config inherits propagate entries from parent", func(ctx SpecContext) {
			parent := &v1alpha1.Repository{
				Status: v1alpha1.RepositoryStatus{
					EffectiveOCMConfig: []v1alpha1.OCMConfiguration{
						{
							NamespacedObjectKindReference: meta.NamespacedObjectKindReference{
								APIVersion: corev1.SchemeGroupVersion.String(),
								Kind:       "Secret",
								Name:       Secret,
								Namespace:  Namespace,
							},
							Policy: v1alpha1.ConfigurationPolicyPropagate,
						},
					},
				},
			}

			child := &v1alpha1.Component{
				Spec: v1alpha1.ComponentSpec{},
			}

			config, err := GetEffectiveConfig(ctx, nil, child, parent)
			Expect(err).ToNot(HaveOccurred())
			Expect(config).To(Equal(parent.Status.EffectiveOCMConfig))
		})

		It("no config does not inherit do-not-propagate entries from parent", func(ctx SpecContext) {
			parent := &v1alpha1.Repository{
				Status: v1alpha1.RepositoryStatus{
					EffectiveOCMConfig: []v1alpha1.OCMConfiguration{
						{
							NamespacedObjectKindReference: meta.NamespacedObjectKindReference{
								APIVersion: corev1.SchemeGroupVersion.String(),
								Kind:       "Secret",
								Name:       Secret,
								Namespace:  Namespace,
							},
							Policy: v1alpha1.ConfigurationPolicyDoNotPropagate,
						},
					},
				},
			}

			child := &v1alpha1.Component{
				Spec: v1alpha1.ComponentSpec{},
			}

			config, err := GetEffectiveConfig(ctx, nil, child, parent)
			Expect(err).ToNot(HaveOccurred())
			Expect(config).To(BeEmpty())
		})

		It("no config inherits only propagate entries from parent with mixed policies", func(ctx SpecContext) {
			propagateEntry := v1alpha1.OCMConfiguration{
				NamespacedObjectKindReference: meta.NamespacedObjectKindReference{
					APIVersion: corev1.SchemeGroupVersion.String(),
					Kind:       "Secret",
					Name:       Secret,
					Namespace:  Namespace,
				},
				Policy: v1alpha1.ConfigurationPolicyPropagate,
			}
			doNotPropagateEntry := v1alpha1.OCMConfiguration{
				NamespacedObjectKindReference: meta.NamespacedObjectKindReference{
					APIVersion: corev1.SchemeGroupVersion.String(),
					Kind:       "ConfigMap",
					Name:       ConfigMap,
					Namespace:  Namespace,
				},
				Policy: v1alpha1.ConfigurationPolicyDoNotPropagate,
			}

			parent := &v1alpha1.Repository{
				Status: v1alpha1.RepositoryStatus{
					EffectiveOCMConfig: []v1alpha1.OCMConfiguration{
						propagateEntry,
						doNotPropagateEntry,
					},
				},
			}

			child := &v1alpha1.Component{
				Spec: v1alpha1.ComponentSpec{},
			}

			config, err := GetEffectiveConfig(ctx, nil, child, parent)
			Expect(err).ToNot(HaveOccurred())
			Expect(config).To(Equal([]v1alpha1.OCMConfiguration{propagateEntry}))
		})

		It("explicit config takes precedence over parent", func(ctx SpecContext) {
			parent := &v1alpha1.Repository{
				Status: v1alpha1.RepositoryStatus{
					EffectiveOCMConfig: []v1alpha1.OCMConfiguration{
						{
							NamespacedObjectKindReference: meta.NamespacedObjectKindReference{
								APIVersion: corev1.SchemeGroupVersion.String(),
								Kind:       "Secret",
								Name:       "parent-secret",
								Namespace:  Namespace,
							},
							Policy: v1alpha1.ConfigurationPolicyPropagate,
						},
					},
				},
			}

			childConfig := []v1alpha1.OCMConfiguration{
				{
					NamespacedObjectKindReference: meta.NamespacedObjectKindReference{
						APIVersion: corev1.SchemeGroupVersion.String(),
						Kind:       "Secret",
						Name:       Secret,
						Namespace:  Namespace,
					},
					Policy: v1alpha1.ConfigurationPolicyDoNotPropagate,
				},
			}
			child := &v1alpha1.Component{
				Spec: v1alpha1.ComponentSpec{
					OCMConfig: childConfig,
				},
			}

			config, err := GetEffectiveConfig(ctx, nil, child, parent)
			Expect(err).ToNot(HaveOccurred())
			Expect(config).To(Equal(childConfig))
		})

		It("duplicate config", func(ctx SpecContext) {
			spec := &ctf.Repository{
				Type:       runtime.NewVersionedType(ctf.Type, "v1"),
				FilePath:   "dummy",
				AccessMode: ctf.AccessModeReadOnly,
			}
			specdata, err := json.Marshal(spec)
			Expect(err).ToNot(HaveOccurred())

			configMap := corev1.ConfigMap{
				TypeMeta: metav1.TypeMeta{
					APIVersion: corev1.SchemeGroupVersion.String(),
					Kind:       "ConfigMap",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-configmap",
					Namespace: Namespace,
				},
			}

			ocmConfig := []v1alpha1.OCMConfiguration{
				{
					NamespacedObjectKindReference: meta.NamespacedObjectKindReference{
						APIVersion: configMap.APIVersion,
						Kind:       configMap.Kind,
						Name:       configMap.Name,
						Namespace:  configMap.Namespace,
					},
					Policy: v1alpha1.ConfigurationPolicyPropagate,
				},
				{
					NamespacedObjectKindReference: meta.NamespacedObjectKindReference{
						APIVersion: configMap.APIVersion,
						Kind:       configMap.Kind,
						Name:       configMap.Name,
						Namespace:  configMap.Namespace,
					},
					Policy: v1alpha1.ConfigurationPolicyPropagate,
				},
			}
			repo := v1alpha1.Repository{
				Spec: v1alpha1.RepositorySpec{
					RepositorySpec: &apiextensionsv1.JSON{Raw: specdata},
					OCMConfig:      ocmConfig,
				},
			}

			config, err := GetEffectiveConfig(ctx, nil, &repo, nil)
			Expect(err).ToNot(HaveOccurred())
			// Equal instead of consists of because the order of the
			// configuration is important
			Expect(config).To(Equal(ocmConfig))
		})

		It("api version defaulting for configmaps", func(ctx SpecContext) {
			repo := v1alpha1.Repository{
				Spec: v1alpha1.RepositorySpec{
					OCMConfig: []v1alpha1.OCMConfiguration{
						{
							NamespacedObjectKindReference: meta.NamespacedObjectKindReference{
								Kind:      "ConfigMap",
								Namespace: Namespace,
								Name:      ConfigMap,
							},
						},
					},
				},
			}

			config, err := GetEffectiveConfig(ctx, nil, &repo, nil)
			Expect(err).ToNot(HaveOccurred())
			Expect(config[0].APIVersion).To(Equal(corev1.SchemeGroupVersion.String()))
		})

		It("api version defaulting for secrets", func(ctx SpecContext) {
			repo := v1alpha1.Repository{
				Spec: v1alpha1.RepositorySpec{
					OCMConfig: []v1alpha1.OCMConfiguration{
						{
							NamespacedObjectKindReference: meta.NamespacedObjectKindReference{
								Kind:      "Secret",
								Namespace: Namespace,
								Name:      Secret,
							},
						},
					},
				},
			}

			config, err := GetEffectiveConfig(ctx, nil, &repo, nil)
			Expect(err).ToNot(HaveOccurred())
			Expect(config[0].APIVersion).To(Equal(corev1.SchemeGroupVersion.String()))
		})

		It("empty api version for ocm controller kinds", func(ctx SpecContext) {
			repo := v1alpha1.Repository{
				Spec: v1alpha1.RepositorySpec{
					OCMConfig: []v1alpha1.OCMConfiguration{
						{
							NamespacedObjectKindReference: meta.NamespacedObjectKindReference{
								Kind:      v1alpha1.KindRepository,
								Namespace: Namespace,
								Name:      Secret,
							},
						},
					},
				},
			}

			config, err := GetEffectiveConfig(ctx, nil, &repo, nil)
			Expect(err).To(HaveOccurred())
			Expect(config).To(BeNil())
		})

		It("unsupported api version", func(ctx SpecContext) {
			repo := v1alpha1.Repository{
				Spec: v1alpha1.RepositorySpec{
					OCMConfig: []v1alpha1.OCMConfiguration{
						{
							NamespacedObjectKindReference: meta.NamespacedObjectKindReference{
								Kind:      "Deployment",
								Namespace: Namespace,
								Name:      Secret,
							},
						},
					},
				},
			}

			config, err := GetEffectiveConfig(ctx, nil, &repo, nil)
			Expect(err).To(HaveOccurred())
			Expect(config).To(BeNil())
		})

		It("referenced object not found", func(ctx SpecContext) {
			configMap := corev1.ConfigMap{
				TypeMeta: metav1.TypeMeta{
					APIVersion: corev1.SchemeGroupVersion.String(),
					Kind:       "ConfigMap",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-configmap",
					Namespace: Namespace,
				},
			}

			ocmConfig := []v1alpha1.OCMConfiguration{
				{
					NamespacedObjectKindReference: meta.NamespacedObjectKindReference{
						APIVersion: configMap.APIVersion,
						Kind:       configMap.Kind,
						Name:       configMap.Name,
						Namespace:  configMap.Namespace,
					},
					Policy: v1alpha1.ConfigurationPolicyDoNotPropagate,
				},
			}
			repo := v1alpha1.Repository{
				TypeMeta: metav1.TypeMeta{
					APIVersion: v1alpha1.GroupVersion.String(),
					Kind:       v1alpha1.KindRepository,
				},
				ObjectMeta: metav1.ObjectMeta{
					Namespace: Namespace,
					Name:      Repository,
				},
				Spec: v1alpha1.RepositorySpec{
					OCMConfig: ocmConfig,
				},
				Status: v1alpha1.RepositoryStatus{
					EffectiveOCMConfig: ocmConfig,
				},
			}
			bldr.WithObjects(&repo)

			comp := v1alpha1.Component{
				Spec: v1alpha1.ComponentSpec{
					RepositoryRef: corev1.LocalObjectReference{
						Name: repo.Name,
					},
					OCMConfig: []v1alpha1.OCMConfiguration{
						{
							NamespacedObjectKindReference: meta.NamespacedObjectKindReference{
								APIVersion: repo.APIVersion,
								Kind:       repo.Kind,
								Name:       repo.Name,
								Namespace:  repo.Namespace,
							},
						},
					},
				},
			}
			bldr.WithObjects(&comp)

			clnt = bldr.Build()
			config, err := GetEffectiveConfig(ctx, clnt, &comp, nil)
			Expect(err).ToNot(HaveOccurred())
			Expect(config).To(BeEmpty())
		})

		It("referenced object does no propagation", func(ctx SpecContext) {
			configMap := corev1.ConfigMap{
				TypeMeta: metav1.TypeMeta{
					APIVersion: corev1.SchemeGroupVersion.String(),
					Kind:       "ConfigMap",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-configmap",
					Namespace: Namespace,
				},
			}
			bldr.WithObjects(&configMap)

			ocmConfig := []v1alpha1.OCMConfiguration{
				{
					NamespacedObjectKindReference: meta.NamespacedObjectKindReference{
						APIVersion: configMap.APIVersion,
						Kind:       configMap.Kind,
						Name:       configMap.Name,
						Namespace:  configMap.Namespace,
					},
					Policy: v1alpha1.ConfigurationPolicyDoNotPropagate,
				},
			}
			repo := v1alpha1.Repository{
				TypeMeta: metav1.TypeMeta{
					APIVersion: v1alpha1.GroupVersion.String(),
					Kind:       v1alpha1.KindRepository,
				},
				ObjectMeta: metav1.ObjectMeta{
					Namespace: Namespace,
					Name:      Repository,
				},
				Spec: v1alpha1.RepositorySpec{
					OCMConfig: ocmConfig,
				},
				Status: v1alpha1.RepositoryStatus{
					EffectiveOCMConfig: ocmConfig,
				},
			}
			bldr.WithObjects(&repo)

			comp := v1alpha1.Component{
				Spec: v1alpha1.ComponentSpec{
					RepositoryRef: corev1.LocalObjectReference{
						Name: repo.Name,
					},
					OCMConfig: []v1alpha1.OCMConfiguration{
						{
							NamespacedObjectKindReference: meta.NamespacedObjectKindReference{
								APIVersion: repo.APIVersion,
								Kind:       repo.Kind,
								Name:       repo.Name,
								Namespace:  repo.Namespace,
							},
						},
					},
				},
			}
			bldr.WithObjects(&comp)

			clnt = bldr.Build()
			config, err := GetEffectiveConfig(ctx, clnt, &comp, nil)
			Expect(err).ToNot(HaveOccurred())
			Expect(config).To(BeEmpty())
		})

		It("referenced object does propagation", func(ctx SpecContext) {
			configMap := corev1.ConfigMap{
				TypeMeta: metav1.TypeMeta{
					APIVersion: corev1.SchemeGroupVersion.String(),
					Kind:       "ConfigMap",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-configmap",
					Namespace: Namespace,
				},
			}
			bldr.WithObjects(&configMap)

			ocmConfig := []v1alpha1.OCMConfiguration{
				{
					NamespacedObjectKindReference: meta.NamespacedObjectKindReference{
						APIVersion: configMap.APIVersion,
						Kind:       configMap.Kind,
						Name:       configMap.Name,
						Namespace:  configMap.Namespace,
					},
					Policy: v1alpha1.ConfigurationPolicyPropagate,
				},
			}
			repo := v1alpha1.Repository{
				TypeMeta: metav1.TypeMeta{
					APIVersion: v1alpha1.GroupVersion.String(),
					Kind:       v1alpha1.KindRepository,
				},
				ObjectMeta: metav1.ObjectMeta{
					Namespace: Namespace,
					Name:      Repository,
				},
				Spec: v1alpha1.RepositorySpec{
					OCMConfig: ocmConfig,
				},
				Status: v1alpha1.RepositoryStatus{
					EffectiveOCMConfig: ocmConfig,
				},
			}
			bldr.WithObjects(&repo)

			comp := v1alpha1.Component{
				Spec: v1alpha1.ComponentSpec{
					RepositoryRef: corev1.LocalObjectReference{
						Name: repo.Name,
					},
					OCMConfig: []v1alpha1.OCMConfiguration{
						{
							NamespacedObjectKindReference: meta.NamespacedObjectKindReference{
								APIVersion: repo.APIVersion,
								Kind:       repo.Kind,
								Name:       repo.Name,
								Namespace:  repo.Namespace,
							},
							Policy: v1alpha1.ConfigurationPolicyDoNotPropagate,
						},
					},
				},
			}
			bldr.WithObjects(&comp)

			clnt = bldr.Build()
			config, err := GetEffectiveConfig(ctx, clnt, &comp, nil)
			Expect(err).ToNot(HaveOccurred())

			// the propagation policy (here, set in repository) is not inherited
			ocmConfig[0].Policy = v1alpha1.ConfigurationPolicyDoNotPropagate
			Expect(config).To(Equal(ocmConfig))
		})
	})

	Context("get latest valid component version and regex filter", func() {
		const (
			TestComponent = "ocm.software/test"
			Version1      = "1.0.0-rc.1"
			Version2      = "2.0.0"
			Version3      = "3.0.0"
		)

		var (
			repo *oci.Repository
		)

		BeforeEach(func(ctx SpecContext) {
			ctfpath := GinkgoT().TempDir()
			repo, _ = test.SetupCTFComponentVersionRepository(ctx, ctfpath, []*descruntime.Descriptor{
				{
					Component: descruntime.Component{
						ComponentMeta: descruntime.ComponentMeta{
							ObjectMeta: descruntime.ObjectMeta{
								Name:    TestComponent,
								Version: Version1,
							},
						},
						Provider: descruntime.Provider{Name: "ocm.software"},
					},
				},
				{
					Component: descruntime.Component{
						ComponentMeta: descruntime.ComponentMeta{
							ObjectMeta: descruntime.ObjectMeta{
								Name:    TestComponent,
								Version: Version2,
							},
						},
						Provider: descruntime.Provider{Name: "ocm.software"},
					},
				},
				{
					Component: descruntime.Component{
						ComponentMeta: descruntime.ComponentMeta{
							ObjectMeta: descruntime.ObjectMeta{
								Name:    TestComponent,
								Version: Version3,
							},
						},
						Provider: descruntime.Provider{Name: "ocm.software"},
					},
				},
			})
		})

		It("without filter", func(ctx SpecContext) {
			versions, err := repo.ListComponentVersions(ctx, TestComponent)
			Expect(err).ToNot(HaveOccurred())

			versionLatest, err := GetLatestValidVersion(ctx, versions, "<2.5.0")
			Expect(err).ToNot(HaveOccurred())

			version2, err := semver.NewVersion(Version2)
			Expect(err).ToNot(HaveOccurred())

			Expect(versionLatest.Equal(version2))
		})

		It("with filter", func(ctx SpecContext) {
			versions, err := repo.ListComponentVersions(ctx, TestComponent)
			Expect(err).ToNot(HaveOccurred())

			regexpFilterFn, err := RegexpFilter(".*-rc.*")
			Expect(err).ToNot(HaveOccurred())

			versionLatest, err := GetLatestValidVersion(ctx, versions, "<2.5.0", regexpFilterFn)
			Expect(err).ToNot(HaveOccurred())

			version1, err := semver.NewVersion(Version1)
			Expect(err).ToNot(HaveOccurred())

			Expect(versionLatest.Equal(version1))
		})
	})

	Context("is downgradable", func() {
		var (
			cv1 *descruntime.Descriptor
			cv2 *descruntime.Descriptor
			cv3 *descruntime.Descriptor
		)
		BeforeEach(func(ctx SpecContext) {
			cv1 = &descruntime.Descriptor{
				Meta: descruntime.Meta{},
				Component: descruntime.Component{
					ComponentMeta: descruntime.ComponentMeta{
						ObjectMeta: descruntime.ObjectMeta{
							Name:    "ocm.software/test",
							Version: "2.0.0",
							Labels: []descruntime.Label{
								{
									Name:  v1alpha1.OCMLabelDowngradable,
									Value: json.RawMessage(`"> 1.0.0"`),
								},
							},
						},
					},
				},
			}

			cv2 = &descruntime.Descriptor{
				Meta: descruntime.Meta{},
				Component: descruntime.Component{
					ComponentMeta: descruntime.ComponentMeta{
						ObjectMeta: descruntime.ObjectMeta{
							Name:    "ocm.software/test",
							Version: "1.1.0",
						},
					},
				},
			}

			cv3 = &descruntime.Descriptor{
				Meta: descruntime.Meta{},
				Component: descruntime.Component{
					ComponentMeta: descruntime.ComponentMeta{
						ObjectMeta: descruntime.ObjectMeta{
							Name:    "ocm.software/test",
							Version: "0.9.0",
						},
					},
				},
			}
		})

		It("true", func(ctx SpecContext) {
			ok, err := IsDowngradable(ctx, cv1, cv2)
			Expect(err).ToNot(HaveOccurred())
			Expect(ok).To(BeTrue())
		})
		It("false", func(ctx SpecContext) {
			ok, err := IsDowngradable(ctx, cv1, cv3)
			Expect(err).ToNot(HaveOccurred())
			Expect(ok).To(BeFalse())
		})
	})
})
