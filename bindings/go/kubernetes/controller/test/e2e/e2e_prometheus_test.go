package e2e

import (
	"fmt"
	"os/exec"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"ocm.software/open-component-model/bindings/go/kubernetes/controller/test/utils"
)

// Release installed by the helm/install task in Taskfile.yml.
const prometheusReleaseSelector = "app.kubernetes.io/instance=ocm-k8s-toolkit,app.kubernetes.io/name=ocm-k8s-toolkit,control-plane=controller-manager"

var _ = Describe("Prometheus metrics E2E", Ordered, func() {
	// Names resolved in BeforeAll by release labels and (for the Service) by metrics port
	// name, never by string concatenation; the chart's resourceName helper may truncate names.
	var svc, sm string

	AfterEach(func() {
		if CurrentSpecReport().Failed() {
			utils.DumpLogs(namespace, "servicemonitor")
		}
	})

	BeforeAll(func(ctx SpecContext) {
		for _, kind := range []struct {
			resource string
			jsonpath string
			target   *string
		}{
			// The metrics Service is the one whose port is named metrics (monitoring/service.yaml).
			{"service", `{.items[?(@.spec.ports[0].name=="metrics")].metadata.name}`, &svc},
			// The chart renders exactly one ServiceMonitor per release.
			{"servicemonitor", "{.items[*].metadata.name}", &sm},
		} {
			cmd := exec.CommandContext(ctx, "kubectl", "get", kind.resource,
				"-n", namespace,
				"-l", prometheusReleaseSelector,
				"-o", "jsonpath="+kind.jsonpath,
			)
			out, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred(),
				fmt.Sprintf("listing %s labeled %s", kind.resource, prometheusReleaseSelector))
			names := strings.Fields(string(out))
			Expect(names).To(HaveLen(1),
				fmt.Sprintf("metrics %s of release ocm-k8s-toolkit", kind.resource))
			*kind.target = names[0]
		}
	})

	It("routes the metrics Service to the controller pod", func(ctx SpecContext) {
		res := "service/" + svc + " -n " + namespace

		By("verifying the Service port name is metrics")
		portName, err := utils.GetResourceField(ctx, res, "{.spec.ports[0].name}")
		Expect(err).NotTo(HaveOccurred())
		Expect(portName).To(Equal("metrics"))

		By("verifying the Service port number is 8443")
		port, err := utils.GetResourceField(ctx, res, "{.spec.ports[0].port}")
		Expect(err).NotTo(HaveOccurred())
		Expect(port).To(Equal("8443"))

		By("verifying the Service targetPort is metrics (by name)")
		targetPort, err := utils.GetResourceField(ctx, res, "{.spec.ports[0].targetPort}")
		Expect(err).NotTo(HaveOccurred())
		Expect(targetPort).To(Equal("metrics"))

		By("waiting for the Service Endpoints to be populated (pod selector match)")
		Eventually(func() string {
			v, err := utils.GetResourceField(ctx, "endpoints/"+svc+" -n "+namespace,
				"{.subsets[0].addresses[0].ip}")
			if err != nil {
				return ""
			}
			return v
		}, timeout).ShouldNot(BeEmpty(),
			"Endpoints IP must be non-empty; empty means selector or port-name mismatch")
	})

	It("serves controller metrics through the Service", func(ctx SpecContext) {
		By("scraping /metrics through the API server service proxy")
		Eventually(func(g Gomega) {
			cmd := exec.CommandContext(ctx, "kubectl", "get", "--raw",
				"/api/v1/namespaces/"+namespace+"/services/http:"+svc+":metrics/proxy/metrics",
			)
			out, err := utils.Run(cmd)
			g.Expect(err).NotTo(HaveOccurred())
			// controller_runtime_reconcile_total is registered unconditionally at init;
			// its presence proves the proxy reached the metrics endpoint.
			g.Expect(string(out)).To(ContainSubstring("controller_runtime_reconcile_total"))
		}, timeout).Should(Succeed())
	})

	It("points the ServiceMonitor at the metrics Service", func(ctx SpecContext) {
		res := "servicemonitor/" + sm + " -n " + namespace

		By("verifying the ServiceMonitor endpoint port is metrics")
		smPort, err := utils.GetResourceField(ctx, res, "{.spec.endpoints[0].port}")
		Expect(err).NotTo(HaveOccurred())
		Expect(smPort).To(Equal("metrics"))

		By("verifying the ServiceMonitor endpoint path is /metrics")
		smPath, err := utils.GetResourceField(ctx, res, "{.spec.endpoints[0].path}")
		Expect(err).NotTo(HaveOccurred())
		Expect(smPath).To(Equal("/metrics"))

		By("verifying the ServiceMonitor has no scheme (secure=false)")
		smScheme, err := utils.GetResourceField(ctx, res, "{.spec.endpoints[0].scheme}")
		Expect(err).NotTo(HaveOccurred())
		Expect(smScheme).To(BeEmpty())

		By("verifying the ServiceMonitor selector matches the Service labels")
		svcRes := "service/" + svc + " -n " + namespace
		for _, key := range []string{
			`app\.kubernetes\.io/name`,
			`app\.kubernetes\.io/instance`,
			`control-plane`,
		} {
			smVal, err := utils.GetResourceField(ctx, res,
				"{.spec.selector.matchLabels."+key+"}")
			Expect(err).NotTo(HaveOccurred())
			Expect(smVal).NotTo(BeEmpty(),
				fmt.Sprintf("ServiceMonitor matchLabels[%s] must be set", key))

			svcVal, err := utils.GetResourceField(ctx, svcRes,
				"{.metadata.labels."+key+"}")
			Expect(err).NotTo(HaveOccurred())
			Expect(smVal).To(Equal(svcVal),
				fmt.Sprintf("ServiceMonitor matchLabels[%s] must match Service label", key))
		}
	})
})
