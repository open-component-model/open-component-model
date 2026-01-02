package ocm

import (
	"encoding/json"

	. "github.com/mandelsoft/goutils/testutils"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	. "ocm.software/ocm/api/helper/builder"

	"github.com/Masterminds/semver/v3"
	"github.com/fluxcd/pkg/apis/meta"
	"github.com/mandelsoft/vfs/pkg/vfs"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ocmctx "ocm.software/ocm/api/ocm"
	"ocm.software/ocm/api/ocm/extensions/repositories/ctf"
	"ocm.software/ocm/api/ocm/extensions/repositories/ocireg"
	"ocm.software/ocm/api/utils/accessio"
	"ocm.software/ocm/api/utils/accessobj"
	ctrl "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	descruntime "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/kubernetes/controller/api/v1alpha1"
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
		scheme := runtime.NewScheme()
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
			specdata, err := ocireg.NewRepositorySpec("ocm.software/mock-repo-spec").MarshalJSON()
			Expect(err).ToNot(HaveOccurred())

			repo := v1alpha1.Repository{
				Spec: v1alpha1.RepositorySpec{
					RepositorySpec: &apiextensionsv1.JSON{Raw: specdata},
				},
			}

			config, err := GetEffectiveConfig(ctx, nil, &repo)
			Expect(err).ToNot(HaveOccurred())
			Expect(config).To(BeEmpty())
		})

		It("duplicate config", func(ctx SpecContext) {
			specdata, err := ocireg.NewRepositorySpec("ocm.software/mock-repo-spec").MarshalJSON()
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

			config, err := GetEffectiveConfig(ctx, nil, &repo)
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

			config, err := GetEffectiveConfig(ctx, nil, &repo)
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

			config, err := GetEffectiveConfig(ctx, nil, &repo)
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

			config, err := GetEffectiveConfig(ctx, nil, &repo)
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

			config, err := GetEffectiveConfig(ctx, nil, &repo)
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
			// do not add the configmap to the client, to check the behaviour
			// if the referenced configuration object is not found
			// bldr.WithObjects(&configMap)

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
			config, err := GetEffectiveConfig(ctx, clnt, &comp)
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
			config, err := GetEffectiveConfig(ctx, clnt, &comp)
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
			config, err := GetEffectiveConfig(ctx, clnt, &comp)
			Expect(err).ToNot(HaveOccurred())

			// the propagation policy (here, set in repository) is not inherited
			ocmConfig[0].Policy = v1alpha1.ConfigurationPolicyDoNotPropagate
			Expect(config).To(Equal(ocmConfig))
		})
	})

	Context("get latest valid component version and regex filter", func() {
		const (
			CTFPath       = "/ctf"
			TestComponent = "ocm.software/test"
			Version1      = "1.0.0-rc.1"
			Version2      = "2.0.0"
			Version3      = "3.0.0"
		)
		var (
			repo ocmctx.Repository
			c    ocmctx.ComponentAccess
			env  *Builder
		)
		BeforeEach(func() {
			env = NewBuilder()

			env.OCMCommonTransport(CTFPath, accessio.FormatDirectory, func() {
				env.Component(TestComponent, func() {
					env.Version(Version1, func() {
					})
				})
				env.Component(TestComponent, func() {
					env.Version(Version2, func() {
					})
				})
				env.Component(TestComponent, func() {
					env.Version(Version3, func() {
					})
				})
			})
			repo = Must(ctf.Open(env, accessobj.ACC_WRITABLE, CTFPath, vfs.FileMode(vfs.O_RDWR), env))
			c = Must(repo.LookupComponent(TestComponent))
		})

		AfterEach(func() {
			Close(c)
			Close(repo)
			MustBeSuccessful(env.Cleanup())
		})
		It("without filter", func(ctx SpecContext) {
			ver := Must(GetLatestValidVersion(ctx, Must(c.ListVersions()), "<2.5.0"))
			Expect(ver.Equal(Must(semver.NewVersion(Version2))))
		})
		It("with filter", func(ctx SpecContext) {
			ver := Must(GetLatestValidVersion(ctx, Must(c.ListVersions()), "<2.5.0", Must(RegexpFilter(".*-rc.*"))))
			Expect(ver.Equal(Must(semver.NewVersion(Version1))))
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
			Expect(Must(IsDowngradable(ctx, cv1, cv2))).To(BeTrue())
		})
		It("false", func(ctx SpecContext) {
			Expect(Must(IsDowngradable(ctx, cv1, cv3))).To(BeFalse())
		})
	})
})
