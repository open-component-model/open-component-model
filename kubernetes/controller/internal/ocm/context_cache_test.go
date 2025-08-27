package ocm_test

import (
	"encoding/base64"
	"encoding/pem"
	"slices"

	. "github.com/mandelsoft/goutils/testutils"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	. "ocm.software/ocm/api/helper/builder"
	. "ocm.software/open-component-model/kubernetes/controller/internal/ocm"

	"github.com/fluxcd/pkg/apis/meta"
	"github.com/mandelsoft/vfs/pkg/vfs"
	"ocm.software/ocm/api/ocm/extensions/attrs/signingattr"
	"ocm.software/ocm/api/ocm/extensions/repositories/ctf"
	"ocm.software/ocm/api/ocm/tools/signing"
	"ocm.software/ocm/api/tech/maven/identity"
	"ocm.software/ocm/api/tech/signing/handlers/rsa"
	"ocm.software/ocm/api/tech/signing/signutils"
	"ocm.software/ocm/api/utils/accessio"
	"ocm.software/ocm/api/utils/accessobj"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	common "ocm.software/ocm/api/utils/misc"
	ctrl "sigs.k8s.io/controller-runtime/pkg/client"

	"ocm.software/open-component-model/kubernetes/controller/api/v1alpha1"
)

const (
	CTFPath       = "/ctf"
	TestComponent = "ocm.software/test"
	Version1      = "1.0.0-rc.1"

	Signature1 = "signature1"
	Signature2 = "signature2"
	Signature3 = "signature3"

	Config1 = "config1"
	Config2 = "config2"
	Config3 = "config3"

	Secret1 = "secret1"
	Secret2 = "secret2"
	Secret3 = "secret3"
)

var _ = Describe("ocm context caching", func() {
	var (
		env *Builder
	)

	Context("configure context", func() {
		var (
			configmaps    []*corev1.ConfigMap
			secrets       []*corev1.Secret
			configs       []v1alpha1.OCMConfiguration
			verifications []Verification
			clnt          ctrl.Client
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
					})
				})
			})

			By("signing cv", func() {
				repo := Must(ctf.Open(env, accessobj.ACC_WRITABLE, CTFPath, vfs.FileMode(vfs.O_RDWR), env))
				cv := Must(repo.LookupComponentVersion(TestComponent, Version1))

				_ = Must(signing.SignComponentVersion(cv, Signature1, signing.PrivateKey(Signature1, privkey1)))
				_ = Must(signing.SignComponentVersion(cv, Signature2, signing.PrivateKey(Signature2, privkey2)))
				_ = Must(signing.SignComponentVersion(cv, Signature3, signing.PrivateKey(Signature3, privkey3)))

				Expect(repo.AddComponentVersion(cv, true)).To(Succeed())

				Close(cv)
				Close(repo)
			})

			By("setup signsecrets")
			verifications = append(verifications, []Verification{
				{Signature: Signature1, PublicKey: pem.EncodeToMemory(signutils.PemBlockForPublicKey(pubkey1))},
				{Signature: Signature2, PublicKey: pem.EncodeToMemory(signutils.PemBlockForPublicKey(pubkey2))},
				{Signature: Signature3, PublicKey: pem.EncodeToMemory(signutils.PemBlockForPublicKey(pubkey3))},
			}...)

			By("setup configmaps")
			builder := fake.NewClientBuilder()

			config1 := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					UID:  "c1",
					Name: Config1,
				},
				Data: map[string]string{
					v1alpha1.OCMConfigKey: `
type: generic.config.ocm.software/v1
sets:
  set1:
    description: set1
    configurations:
    - type: credentials.config.ocm.software
      consumers:
      - identity:
          type: MavenRepository
          hostname: example.com
          pathprefix: path/ocm
        credentials:
        - type: Credentials
          properties:
            username: testuser1
            password: testpassword1 
`,
				},
			}
			configmaps = append(configmaps, config1)
			builder.WithObjects(config1)

			config2 := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					UID:  "c2",
					Name: Config2,
				},
				Data: map[string]string{
					v1alpha1.OCMConfigKey: `
type: generic.config.ocm.software/v1
sets:
  set2:
    description: set2
    configurations:
    - type: credentials.config.ocm.software
      consumers:
      - identity:
          type: MavenRepository
          hostname: example.com
          pathprefix: path/ocm
        credentials:
        - type: Credentials
          properties:
            username: testuser1
            password: testpassword1 
`,
				},
			}
			configmaps = append(configmaps, config2)
			builder.WithObjects(config2)

			config3 := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					UID:  "c3",
					Name: Config3,
				},
				Data: map[string]string{
					v1alpha1.OCMConfigKey: `
type: generic.config.ocm.software/v1
sets:
  set3:
    description: set3
    configurations:
    - type: credentials.config.ocm.software
      consumers:
      - identity:
          type: MavenRepository
          hostname: example.com
          pathprefix: path/ocm
        credentials:
        - type: Credentials
          properties:
            username: testuser1
            password: testpassword1 
`,
				},
			}
			configmaps = append(configmaps, config3)
			builder.WithObjects(config3)

			By("setup secrets")
			secret1 := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					UID:  "s1",
					Name: Secret1,
				},
				Data: map[string][]byte{
					v1alpha1.OCMConfigKey: []byte(`
type: credentials.config.ocm.software
consumers:
- identity:
    type: MavenRepository
    hostname: example.com
    pathprefix: path1
  credentials:
  - type: Credentials
    properties:
      username: testuser1
      password: testpassword1
`),
				},
			}
			secrets = append(secrets, secret1)
			builder.WithObjects(secret1)

			secret2 := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					UID:  "s2",
					Name: Secret2,
				},
				Data: map[string][]byte{
					v1alpha1.OCMConfigKey: []byte(`
type: credentials.config.ocm.software
consumers:
- identity:
    type: MavenRepository
    hostname: example.com
    pathprefix: path2
  credentials:
  - type: Credentials
    properties:
      username: testuser2
      password: testpassword2
`),
				},
			}
			secrets = append(secrets, secret2)
			builder.WithObjects(secret2)

			secret3 := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					UID:  "s3",
					Name: Secret3,
				},
				Data: map[string][]byte{
					v1alpha1.OCMConfigKey: []byte(`
type: credentials.config.ocm.software
consumers:
- identity:
    type: MavenRepository
    hostname: example.com
    pathprefix: path3
  credentials:
  - type: Credentials
    properties:
      username: testuser3
      password: testpassword3
`),
				},
			}
			secrets = append(secrets, secret3)
			builder.WithObjects(secret3)

			clnt = builder.Build()

			By("setup configs")
			configs = []v1alpha1.OCMConfiguration{
				{
					NamespacedObjectKindReference: meta.NamespacedObjectKindReference{
						APIVersion: corev1.SchemeGroupVersion.String(),
						Kind:       "Secret",
						Name:       secrets[0].Name,
						Namespace:  secrets[0].Namespace,
					},
				},
				{
					NamespacedObjectKindReference: meta.NamespacedObjectKindReference{
						APIVersion: corev1.SchemeGroupVersion.String(),
						Kind:       "Secret",
						Name:       secrets[1].Name,
					},
					Policy: v1alpha1.ConfigurationPolicyDoNotPropagate,
				},
				{
					NamespacedObjectKindReference: meta.NamespacedObjectKindReference{
						Kind: "Secret",
						Name: secrets[2].Name,
					},
					Policy: v1alpha1.ConfigurationPolicyPropagate,
				},
				{
					NamespacedObjectKindReference: meta.NamespacedObjectKindReference{
						APIVersion: corev1.SchemeGroupVersion.String(),
						Kind:       "ConfigMap",
						Name:       configmaps[0].Name,
						Namespace:  configmaps[0].Namespace,
					},
				},
				{
					NamespacedObjectKindReference: meta.NamespacedObjectKindReference{
						APIVersion: corev1.SchemeGroupVersion.String(),
						Kind:       "ConfigMap",
						Name:       configmaps[1].Name,
					},
					Policy: v1alpha1.ConfigurationPolicyDoNotPropagate,
				},
				{
					NamespacedObjectKindReference: meta.NamespacedObjectKindReference{
						Kind: "ConfigMap",
						Name: configmaps[2].Name,
					},
					Policy: v1alpha1.ConfigurationPolicyPropagate,
				},
			}
		})

		AfterEach(func() {
			MustBeSuccessful(env.Cleanup())
		})

		It("get recached", func(ctx SpecContext) {
			contextCache := NewContextCache("test", 1, 1, clnt, GinkgoLogr)
			cacheDone := make(chan error, 1)
			go func() {
				cacheDone <- contextCache.Start(ctx)
			}()
			DeferCleanup(func(ctx SpecContext) {
				Eventually(cacheDone).WithContext(ctx).Should(Receive(Succeed()))
			})

			base := &v1alpha1.Component{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
				},
				Spec: v1alpha1.ComponentSpec{
					OCMConfig: configs,
				},
			}
			for _, verification := range verifications {
				base.Spec.Verify = append(base.Spec.Verify, v1alpha1.Verification{
					Signature: verification.Signature,
					Value:     base64.StdEncoding.EncodeToString(verification.PublicKey),
				})
			}

			octx, session, err := contextCache.GetSession(&GetSessionOptions{
				RepositorySpecification: &apiextensionsv1.JSON{Raw: []byte("arbitrary")},
				OCMConfigurations:       configs,
				VerificationProvider:    base,
			})
			baseId := octx.GetId()
			baseSession := session

			reversedConfigs := slices.Clone(configs)
			slices.Reverse(reversedConfigs)
			reversedbase := base.DeepCopy()
			slices.Reverse(reversedbase.Spec.OCMConfig)

			octx, session2, err := contextCache.GetSession(&GetSessionOptions{
				RepositorySpecification: &apiextensionsv1.JSON{Raw: []byte("arbitrary")},
				OCMConfigurations:       reversedConfigs,
			})
			Expect(octx.GetId()).To(Equal(baseId))
			Expect(session2).To(Equal(baseSession))

			octx, session3, err := contextCache.GetSession(&GetSessionOptions{
				RepositorySpecification: &apiextensionsv1.JSON{Raw: []byte("other")},
				OCMConfigurations:       reversedConfigs,
				VerificationProvider:    base,
			})
			Expect(octx.GetId()).To(Equal(baseId))
			Expect(session3).ToNot(Equal(baseSession))

			MustBeSuccessful(err)
		})

		It("configure context", func(ctx SpecContext) {
			contextCache := NewContextCache("test", 1, 1, clnt, GinkgoLogr)
			cacheDone := make(chan error, 1)

			go func() {
				cacheDone <- contextCache.Start(ctx)
			}()
			DeferCleanup(func(ctx SpecContext) {
				Eventually(cacheDone).WithContext(ctx).Should(Receive(Succeed()))
			})

			base := &v1alpha1.Component{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
				},
				Spec: v1alpha1.ComponentSpec{
					OCMConfig: configs,
				},
			}
			for _, verification := range verifications {
				base.Spec.Verify = append(base.Spec.Verify, v1alpha1.Verification{
					Signature: verification.Signature,
					Value:     base64.StdEncoding.EncodeToString(verification.PublicKey),
				})
			}

			octx, _, err := contextCache.GetSession(&GetSessionOptions{
				RepositorySpecification: &apiextensionsv1.JSON{Raw: []byte("arbitrary")},
				OCMConfigurations:       configs,
				VerificationProvider:    base,
			})
			MustBeSuccessful(err)

			ctf := Must(ctf.Open(octx, accessobj.ACC_READONLY, CTFPath, vfs.FileMode(vfs.O_RDONLY), env))
			cv := Must(ctf.LookupComponentVersion(TestComponent, Version1))

			creds1 := Must(octx.CredentialsContext().GetCredentialsForConsumer(Must(identity.GetConsumerId("https://example.com/path1", "")), identity.IdentityMatcher))
			creds2 := Must(octx.CredentialsContext().GetCredentialsForConsumer(Must(identity.GetConsumerId("https://example.com/path2", "")), identity.IdentityMatcher))
			creds3 := Must(octx.CredentialsContext().GetCredentialsForConsumer(Must(identity.GetConsumerId("https://example.com/path3", "")), identity.IdentityMatcher))
			Expect(Must(creds1.Credentials(octx.CredentialsContext())).Properties().Equals(common.Properties{
				"username": "testuser1",
				"password": "testpassword1",
			})).To(BeTrue())
			Expect(Must(creds2.Credentials(octx.CredentialsContext())).Properties().Equals(common.Properties{
				"username": "testuser2",
				"password": "testpassword2",
			})).To(BeTrue())
			Expect(Must(creds3.Credentials(octx.CredentialsContext())).Properties().Equals(common.Properties{
				"username": "testuser3",
				"password": "testpassword3",
			})).To(BeTrue())

			signreg := signing.Registry(signingattr.Get(octx))
			_ = Must(signing.VerifyComponentVersion(cv, Signature1, signing.NewOptions(signreg)))
			_ = Must(signing.VerifyComponentVersion(cv, Signature2, signing.NewOptions(signreg)))
			_ = Must(signing.VerifyComponentVersion(cv, Signature3, signing.NewOptions(signreg)))

			MustBeSuccessful(octx.ConfigContext().ApplyConfigSet("set1"))
			MustBeSuccessful(octx.ConfigContext().ApplyConfigSet("set2"))
			MustBeSuccessful(octx.ConfigContext().ApplyConfigSet("set3"))
		})
	})
})
