package ocm

import (
	"github.com/Masterminds/semver/v3"
	"github.com/fluxcd/pkg/apis/meta"
	"github.com/mandelsoft/vfs/pkg/vfs"
	"ocm.software/ocm/api/ocm/extensions/repositories/ctf"
	"ocm.software/ocm/api/utils/accessobj"

	. "github.com/mandelsoft/goutils/testutils"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	. "ocm.software/ocm/api/helper/builder"

	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"ocm.software/open-component-model/kubernetes/controller/api/v1alpha1"

	ocmctx "ocm.software/ocm/api/ocm"
	v1 "ocm.software/ocm/api/ocm/compdesc/meta/v1"
	resourcetypes "ocm.software/ocm/api/ocm/extensions/artifacttypes"
	"ocm.software/ocm/api/ocm/extensions/attrs/signingattr"
	"ocm.software/ocm/api/ocm/extensions/repositories/ocireg"
	"ocm.software/ocm/api/ocm/tools/signing"
	"ocm.software/ocm/api/tech/signing/handlers/rsa"
	"ocm.software/ocm/api/utils/accessio"
	"ocm.software/ocm/api/utils/mime"
	ctrl "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
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

	Context("verify component version", func() {
		const (
			CTFPath       = "/ctf"
			TestComponent = "ocm.software/test"
			Reference     = "referenced-test"
			RefComponent  = "ocm.software/referenced-test"
			Resource      = "testresource"
			Version1      = "1.0.0-rc.1"
			Version2      = "2.0.0"

			Signature1 = "signature1"
			Signature2 = "signature2"
			Signature3 = "signature3"
		)
		var (
			octx ocmctx.Context
			repo ocmctx.Repository
			cv   ocmctx.ComponentVersionAccess
			env  *Builder
		)

		BeforeEach(func() {
			env = NewBuilder()

			By("setup ocm")
			privkey1, pubkey1 := Must2(rsa.CreateKeyPair())
			privkey2, pubkey2 := Must2(rsa.CreateKeyPair())
			privkey3, pubkey3 := Must2(rsa.CreateKeyPair())

			env.OCMCommonTransport(CTFPath, accessio.FormatDirectory, func() {
				env.Component(TestComponent, func() {
					env.Version(Version1, func() {
						env.Resource(Resource, Version1, resourcetypes.PLAIN_TEXT, v1.LocalRelation, func() {
							env.BlobData(mime.MIME_TEXT, []byte("testdata"))
						})
						env.Reference(Reference, RefComponent, Version2, func() {
						})
					})
				})
				env.Component(RefComponent, func() {
					env.Version(Version2, func() {
					})
				})
			})
			repo = Must(ctf.Open(env, accessobj.ACC_WRITABLE, CTFPath, vfs.FileMode(vfs.O_RDWR), env))
			cv = Must(repo.LookupComponentVersion(TestComponent, Version1))

			opts := signing.NewOptions(
				signing.Resolver(repo),
			)
			_ = Must(signing.SignComponentVersion(cv, Signature1, signing.PrivateKey(Signature1, privkey1), opts))
			_ = Must(signing.SignComponentVersion(cv, Signature2, signing.PrivateKey(Signature2, privkey2), opts))
			_ = Must(signing.SignComponentVersion(cv, Signature3, signing.PrivateKey(Signature3, privkey3), opts))

			octx = env.OCMContext()
			signreg := signingattr.Get(octx)
			signreg.RegisterPublicKey(Signature1, pubkey1)
			signreg.RegisterPublicKey(Signature2, pubkey2)
			signreg.RegisterPublicKey(Signature3, pubkey3)
		})

		AfterEach(func() {
			Close(cv)
			Close(repo)
			MustBeSuccessful(env.Cleanup())
		})

		It("without retrieving descriptors", func(ctx SpecContext) {
			MustBeSuccessful(VerifyComponentVersion(ctx, cv, repo, []string{Signature1, Signature2, Signature3}))
		})
		It("with retrieving descriptors", func(ctx SpecContext) {
			descriptors := Must(VerifyComponentVersion(ctx, cv, repo, []string{Signature1, Signature2, Signature3}))
			Expect(descriptors.List).To(HaveLen(2))
		})
		It("list component versions without verification", func(ctx SpecContext) {
			descriptors := Must(ListComponentDescriptors(ctx, cv, repo))
			Expect(descriptors.List).To(HaveLen(2))
		})
	})

	Context("is downgradable", func() {
		const (
			CTFPath       = "/ctf"
			TestComponent = "ocm.software/test"
		)
		var (
			env *Builder

			repo ocmctx.Repository
			cv1  ocmctx.ComponentVersionAccess
			cv2  ocmctx.ComponentVersionAccess
			cv3  ocmctx.ComponentVersionAccess
		)
		BeforeEach(func(ctx SpecContext) {
			env = NewBuilder()

			v1 := "2.0.0"
			v2 := "1.1.0"
			v3 := "0.9.0"
			env.OCMCommonTransport(CTFPath, accessio.FormatDirectory, func() {
				env.Component(TestComponent, func() {
					env.Version(v1, func() {
						env.Label(v1alpha1.OCMLabelDowngradable, `> 1.0.0`)
					})
				})
				env.Component(TestComponent, func() {
					env.Version(v2, func() {
					})
				})
				env.Component(TestComponent, func() {
					env.Version(v3, func() {
					})
				})
			})

			repo = Must(ctf.Open(env, accessobj.ACC_WRITABLE, CTFPath, vfs.FileMode(vfs.O_RDWR), env))
			cv1 = Must(repo.LookupComponentVersion(TestComponent, v1))
			cv2 = Must(repo.LookupComponentVersion(TestComponent, v2))
			cv3 = Must(repo.LookupComponentVersion(TestComponent, v3))
		})

		AfterEach(func() {
			Close(cv1)
			Close(cv2)
			Close(cv3)
			Close(repo)
			MustBeSuccessful(env.Cleanup())
		})

		It("true", func(ctx SpecContext) {
			Expect(Must(IsDowngradable(ctx, cv1, cv2))).To(BeTrue())
		})
		It("false", func(ctx SpecContext) {
			Expect(Must(IsDowngradable(ctx, cv1, cv3))).To(BeFalse())
		})
	})
})
