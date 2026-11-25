package e2e

import (
	"encoding/json/v2"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"

	genericv1 "ocm.software/open-component-model/bindings/go/configuration/generic/v1/spec"
	credentialsv1 "ocm.software/open-component-model/bindings/go/credentials/spec/config/v1"
	"ocm.software/open-component-model/bindings/go/kubernetes/controller/pkg/configuration"
	"ocm.software/open-component-model/bindings/go/kubernetes/controller/test/utils"
	"ocm.software/open-component-model/bindings/go/oci/looseref"
	"ocm.software/open-component-model/bindings/go/runtime"
	signingspec "ocm.software/open-component-model/bindings/go/signing/v1alpha1/spec"
)

const (
	ComponentConstructor = "component-constructor.yaml"
	Bootstrap            = "bootstrap.yaml"
	Manifests            = "manifests.yaml"
	Rgd                  = "rgd.yaml"
	KroInstance          = "kro-instance.yaml"
	K8sManifest          = "k8s-manifest.yaml"
	// SigningConfig is the per-example file holding private signing keys and
	// signer configuration. It is never applied to the cluster; the controller
	// reads verification credentials from the Secret in configuration.yaml.
	SigningConfig = "signing.ocmconfig"
	// Configuration is the per-example configuration.yaml that must be applied
	// before bootstrap so the controller can read HTTP and verification config.
	Configuration         = "configuration.yaml"
	CrossplaneComposition = "crossplane-composition.yaml"
	CrossplaneInstance    = "crossplane-instance.yaml"
)

// ignoreExamples lists examples that are tested elsewhere or should be skipped.
var ignoreExamples = map[string]struct{}{
	"applyset-pruning":   {}, // tested in e2e_applyset_test.go
	"replication-simple": {}, // tested in e2e_replication_test.go
}

var _ = Describe("controller", func() {
	Context("examples", func() {
		AfterEach(func() {
			if !CurrentSpecReport().Failed() {
				return
			}

			utils.DumpLogs("kro", "rgd")
			utils.DumpLogs("argocd", "applications.argoproj.io")
			utils.DumpCrossplaneDiagnostics()
		})

		for _, example := range examples {
			if _, ok := ignoreExamples[example.Name()]; ok {
				continue
			}
			fInfo, err := os.Stat(filepath.Join(examplesDir, example.Name()))
			Expect(err).NotTo(HaveOccurred())
			if !fInfo.IsDir() {
				continue
			}

			reqFiles := []string{ComponentConstructor, Bootstrap, Configuration}

			It("should deploy the example "+example.Name(), func(ctx SpecContext) {
				By("validating the example directory " + example.Name())
				var files []string
				Expect(filepath.WalkDir(
					filepath.Join(examplesDir, example.Name()),
					func(path string, d os.DirEntry, err error) error {
						if err != nil {
							return err
						}
						if d.IsDir() {
							return nil
						}
						files = append(files, d.Name())
						return nil
					})).To(Succeed())

				Expect(files).To(ContainElements(reqFiles), "required files %s not found in example directory %q", reqFiles, example.Name())

				By("creating and transferring a component version for " + example.Name())
				Expect(utils.PrepareOCMComponent(
					ctx,
					example.Name(),
					filepath.Join(examplesDir, example.Name(), ComponentConstructor),
					imageRegistry,
				)).To(Succeed())

				By("applying the example configuration")
				Expect(utils.DeployResource(ctx, filepath.Join(examplesDir, example.Name(), Configuration))).To(Succeed())

				By("bootstrapping the example")
				Expect(utils.DeployResource(ctx, filepath.Join(examplesDir, example.Name(), Bootstrap))).To(Succeed())
				name := ""

				if slices.Contains(files, Rgd) {
					name = "rgd/" + example.Name()
					Expect(utils.WaitForResource(ctx, "create", timeout, name)).To(Succeed())
					Expect(
						utils.WaitForResource(ctx, "condition=Ready=true", timeout, name)).To(
						Succeed(),
						"The final readiness condition was not set, which means KRO believes the RGD %s was not reconciled correctly", name,
					)
				}

				if slices.Contains(files, KroInstance) {
					By("creating an instance of the example")
					Expect(utils.DeployAndWaitForResource(
						ctx, filepath.Join(examplesDir, example.Name(), KroInstance),
						"condition=Ready=true",
						timeout,
					)).To(Succeed())

					By("checking for ArgoCD Applications on the cluster")
					name = "applications.argoproj.io/" + example.Name()
					Expect(utils.WaitForResource(ctx, "create", timeout, name, "-n", "argocd")).To(Succeed())

					By("validating the ArgoCD Application is Synced")
					Expect(utils.WaitForResource(ctx, "jsonpath={.status.sync.status}=Synced", timeout, name, "-n", "argocd")).To(Succeed())

					name = "deployment.apps/" + example.Name() + "-podinfo"

					By("validating the ArgoCD-managed deployment in default-argocd")

					Expect(utils.WaitForResource(ctx, "create", timeout, name, "-n", "default-argocd")).To(Succeed())
					Expect(utils.WaitForResource(ctx, "condition=Available", timeout, name, "-n", "default-argocd")).To(Succeed())
					Expect(utils.WaitForResource(
						ctx, "condition=Ready=true",
						timeout,
						"pod", "-l", "app.kubernetes.io/name="+example.Name()+"-podinfo", "-n", "default-argocd",
					)).To(Succeed())

				}

				// Crossplane flow: the bootstrap already deployed the OCM Resource and
				// Deployer for crossplane-xrd alongside the kro ones. Here we just wait
				// for the XRD to be Established, then create the XR instance.
				if slices.Contains(files, CrossplaneComposition) &&
					slices.Contains(files, CrossplaneInstance) {
					crossplaneName := example.Name() + "-crossplane"

					By("waiting for XRD to be Established")
					// The XRD metadata.name follows Crossplane's required <plural>.<group>
					// form, and every example derives both parts from the example name:
					// dashes stripped with a "crossplanes" suffix for the plural, and the
					// fixed examples.ocm.software group. Deriving it here mirrors how the
					// kro flow uses example.Name() and avoids reading the composition file.
					xrdName := strings.ReplaceAll(example.Name(), "-", "") + "crossplanes.examples.ocm.software"
					Expect(utils.WaitForResource(ctx, "condition=Established=true", timeout,
						"compositeresourcedefinitions.apiextensions.crossplane.io/"+xrdName)).To(Succeed())

					By("creating the Crossplane XR instance")
					// Mirror the FluxCD/ArgoCD waits below: wait for the XR to be created
					// before waiting on its Ready condition.
					crossplaneInstance := filepath.Join(examplesDir, example.Name(), CrossplaneInstance)
					Expect(utils.DeployResource(ctx, crossplaneInstance)).To(Succeed())
					Expect(utils.WaitForResource(ctx, "create", timeout, "-f", crossplaneInstance)).To(Succeed())
					Expect(utils.WaitForResource(ctx, "condition=Ready=true", timeout, "-f", crossplaneInstance)).To(Succeed())

					By("validating the Crossplane FluxCD-managed deployment")
					name = "deployment.apps/" + crossplaneName + "-podinfo"
					Expect(utils.WaitForResource(ctx, "create", timeout, name)).To(Succeed())
					Expect(utils.WaitForResource(ctx, "condition=Available", timeout, name)).To(Succeed())
					Expect(utils.WaitForResource(
						ctx, "condition=Ready=true",
						timeout,
						"pod", "-l", "app.kubernetes.io/name="+crossplaneName+"-podinfo",
					)).To(Succeed())

					By("checking for Crossplane ArgoCD Application")
					argoCDAppName := "applications.argoproj.io/" + crossplaneName
					Expect(utils.WaitForResource(ctx, "create", timeout, argoCDAppName, "-n", "argocd")).To(Succeed())
					Expect(utils.WaitForResource(ctx, "jsonpath={.status.sync.status}=Synced", timeout, argoCDAppName, "-n", "argocd")).To(Succeed())

					By("validating the Crossplane ArgoCD-managed deployment")
					name = "deployment.apps/" + crossplaneName + "-podinfo"
					Expect(utils.WaitForResource(ctx, "create", timeout, name, "-n", "default-argocd")).To(Succeed())
					Expect(utils.WaitForResource(ctx, "condition=Available", timeout, name, "-n", "default-argocd")).To(Succeed())
					Expect(utils.WaitForResource(
						ctx, "condition=Ready=true",
						timeout,
						"pod", "-l", "app.kubernetes.io/name="+crossplaneName+"-podinfo", "-n", "default-argocd",
					)).To(Succeed())
				}

				By("validating the example")
				name = "deployment.apps/" + example.Name() + "-podinfo"
				Expect(utils.WaitForResource(ctx, "create", timeout, name)).To(Succeed())
				Expect(utils.WaitForResource(ctx, "condition=Available", timeout, name)).To(Succeed())
				Expect(utils.WaitForResource(
					ctx, "condition=Ready=true",
					timeout,
					"pod", "-l", "app.kubernetes.io/name="+example.Name()+"-podinfo",
				)).To(Succeed())

				// Every example with a signing.ocmconfig must produce "verified signature" log
				// entries for each configured signature name. Ready alone doesn't prove the
				// handler ran (the resolver cache could serve a warmed descriptor).
				if slices.Contains(files, SigningConfig) {
					sigNames, err := utils.SigningConfigSignatureNames(filepath.Join(examplesDir, example.Name(), SigningConfig))
					Expect(err).ToNot(HaveOccurred())
					Expect(sigNames).NotTo(BeEmpty(), "signing.ocmconfig must configure at least one named signature")
					for _, sig := range sigNames {
						By(fmt.Sprintf("confirming signature %q was verified by the controller", sig))
						// signature names are based on the example names, so this should be enough to make sure it got
						// verified
						// The log line carries the reconcile fields between the message and the signature.
						expected := fmt.Sprintf(`"msg":"verified signature"[^\n]*"signature":%s`, regexp.QuoteMeta(strconv.Quote(sig)))
						// The controller wraps every handler error (wrong key, missing
						// credential, cosign/gpg failure) into this message. It is logged only
						// after the descriptor and signature are fetched and verification
						// actually ran, so it is terminal for a fixed-config example: a retry
						// cannot make a bad key good. Transient fetch errors surface as a
						// different message, so matching on this one does not flake. Abort the
						// poll immediately instead of burning the full timeout.
						failed := fmt.Sprintf("signature verification failed for signature %s:", sig)
						Eventually(func(g Gomega) string {
							out, err := utils.Run(exec.CommandContext(ctx,
								"kubectl", "logs",
								"-n", "ocm-k8s-toolkit-system",
								"-l", "app.kubernetes.io/name=ocm-k8s-toolkit",
								"--tail=-1",
							))
							g.Expect(err).ToNot(HaveOccurred())
							logs := string(out)
							if strings.Contains(logs, failed) {
								StopTrying(fmt.Sprintf("controller reported a terminal verification failure for signature %q", sig)).Now()
							}
							return logs
						}, timeout).Should(MatchRegexp(expected))
					}

					By("verifying the " + example.Name() + "-ocmconfig Secret contains only public keys and no signers")
					secretName := example.Name() + "-ocmconfig"
					Expect(assertPublicOnlySecret(ctx, secretName, sigNames)).To(Succeed())
				}

				if strings.HasSuffix(example.Name(), "-cross-namespace") {
					By("validating the object in the other namespace")
					Expect(utils.WaitForResource(ctx, "create", timeout,
						"configmap/"+example.Name()+"-dashboard", "-n", example.Name()+"-monitoring",
					)).To(Succeed())
				}

				// Check for configuration and localization
				if strings.HasSuffix(example.Name(), "-configuration-localization") {
					expectedRegistry, err := looseref.ParseReference(imageRegistry + "/")
					Expect(err).NotTo(HaveOccurred())

					assertLocalizedImage := func(resource string) {
						image, err := utils.GetResourceField(ctx, resource, "'{.items[0].spec.containers[0].image}'")
						Expect(err).NotTo(HaveOccurred())
						ref, err := looseref.ParseReference(image)
						Expect(err).NotTo(HaveOccurred(), "container image %q is not a valid OCI reference", image)
						Expect(ref.Registry).To(Equal(expectedRegistry.Registry))
					}

					By("validating the fluxcd localization")
					assertLocalizedImage("pod -l app.kubernetes.io/name=" + example.Name() + "-podinfo")

					By("validating the FluxCD configuration (ui.message)")
					Expect(utils.CompareResourceField(ctx,
						"pod -l app.kubernetes.io/name="+example.Name()+"-podinfo",
						"'{.items[0].spec.containers[0].env[?(@.name==\"PODINFO_UI_MESSAGE\")].value}'",
						example.Name(),
					)).To(Succeed())

					By("validating the ArgoCD localization")
					assertLocalizedImage("pod -l app.kubernetes.io/name=" + example.Name() + "-podinfo -n default-argocd")

					By("validating the ArgoCD configuration (ui.message)")
					Expect(utils.CompareResourceField(ctx,
						"pod -l app.kubernetes.io/name="+example.Name()+"-podinfo -n default-argocd",
						"'{.items[0].spec.containers[0].env[?(@.name==\"PODINFO_UI_MESSAGE\")].value}'",
						example.Name(),
					)).To(Succeed())

					// The Crossplane composition localizes and configures the same podinfo
					// chart, but under the <example>-crossplane-podinfo label. Without these
					// assertions a Crossplane composition that drops the image/ui.message
					// patches still passes, since the checks above only look at the kro pods.
					if slices.Contains(files, CrossplaneComposition) && slices.Contains(files, CrossplaneInstance) {
						crossplanePods := "pod -l app.kubernetes.io/name=" + example.Name() + "-crossplane-podinfo"

						By("validating the Crossplane FluxCD localization")
						assertLocalizedImage(crossplanePods)

						By("validating the Crossplane FluxCD configuration (ui.message)")
						Expect(utils.CompareResourceField(ctx,
							crossplanePods,
							"'{.items[0].spec.containers[0].env[?(@.name==\"PODINFO_UI_MESSAGE\")].value}'",
							example.Name(),
						)).To(Succeed())

						By("validating the Crossplane ArgoCD localization")
						assertLocalizedImage(crossplanePods + " -n default-argocd")

						By("validating the Crossplane ArgoCD configuration (ui.message)")
						Expect(utils.CompareResourceField(ctx,
							crossplanePods+" -n default-argocd",
							"'{.items[0].spec.containers[0].env[?(@.name==\"PODINFO_UI_MESSAGE\")].value}'",
							example.Name(),
						)).To(Succeed())
					}
				}
			})
		}
	})
})

// assertPublicOnlySecret checks the live configuration without exposing credential values on failure.
func assertPublicOnlySecret(ctx SpecContext, secretName string, sigNames []string) error {
	GinkgoHelper()

	cmd := exec.CommandContext(ctx, "kubectl", "get", "secret", secretName, "-n", "default", "-o", "json")
	out, err := utils.Run(cmd)
	if err != nil {
		return fmt.Errorf("could not fetch Secret %q: %w", secretName, err)
	}

	var secret corev1.Secret
	if err := json.Unmarshal(out, &secret); err != nil {
		return fmt.Errorf("could not decode Secret %q: %w", secretName, err)
	}

	cfg, err := configuration.GetConfigFromSecret(&secret)
	if err != nil {
		return fmt.Errorf("could not extract OCM config from Secret %q: %w", secretName, err)
	}

	signingEntries, err := genericv1.FilterForType[*signingspec.Config](signingspec.Scheme, cfg)
	if err != nil {
		return fmt.Errorf("could not filter signing entries in Secret %q: %w", secretName, err)
	}
	if len(signingEntries) != len(sigNames) {
		return fmt.Errorf("Secret %q: expected %d signature entries, found %d", secretName, len(sigNames), len(signingEntries))
	}
	signingByName := make(map[string]struct{}, len(signingEntries))
	for _, sc := range signingEntries {
		if !slices.Contains(sigNames, sc.Signature) {
			return fmt.Errorf("Secret %q: unexpected signature %q", secretName, sc.Signature)
		}
		if _, exists := signingByName[sc.Signature]; exists {
			return fmt.Errorf("Secret %q: duplicate signature %q", secretName, sc.Signature)
		}
		signingByName[sc.Signature] = struct{}{}
		if sc.Signer != nil || sc.Verifier == nil {
			return fmt.Errorf("Secret %q: signature %q must contain only a verifier", secretName, sc.Signature)
		}
	}

	credScheme := runtime.NewScheme()
	credentialsv1.MustRegister(credScheme)
	credConfigs, err := genericv1.FilterForType[*credentialsv1.Config](credScheme, cfg)
	if err != nil {
		return fmt.Errorf("could not filter credentials entries in Secret %q: %w", secretName, err)
	}
	if len(credConfigs) == 0 {
		return fmt.Errorf("Secret %q: no public-key credentials found", secretName)
	}
	for i, cc := range credConfigs {
		if len(cc.Repositories) > 0 {
			return fmt.Errorf("Secret %q: credentials entry %d must not contain external repository references", secretName, i)
		}
		if len(cc.Consumers) == 0 {
			return fmt.Errorf("Secret %q: credentials entry %d has no consumers", secretName, i)
		}
		for j, consumer := range cc.Consumers {
			if len(consumer.Credentials) == 0 {
				return fmt.Errorf("Secret %q: consumer %d in credentials entry %d has no credentials", secretName, j, i)
			}
			for k, raw := range consumer.Credentials {
				direct := &credentialsv1.DirectCredentials{}
				if err := credScheme.Convert(raw, direct); err != nil {
					return fmt.Errorf("Secret %q: credential %d of consumer %d in entry %d could not be decoded: %w", secretName, k, j, i, err)
				}
				if len(direct.Properties) == 0 {
					return fmt.Errorf("Secret %q: credential %d of consumer %d in entry %d has no properties", secretName, k, j, i)
				}
				for key, val := range direct.Properties {
					if key != "publicKeyPEM" && key != "publicKeyPGP" {
						return fmt.Errorf("Secret %q: credential property key %q is not allowed (only publicKeyPEM or publicKeyPGP)", secretName, key)
					}
					if val == "" {
						return fmt.Errorf("Secret %q: credential property %q has an empty value", secretName, key)
					}
					if strings.Contains(val, "PRIVATE KEY") {
						return fmt.Errorf("Secret %q: credential property %q contains private key material", secretName, key)
					}
				}
			}
		}
	}

	return nil
}
