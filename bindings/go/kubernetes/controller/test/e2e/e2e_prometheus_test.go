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
	// sm is only looked up: finding exactly one proves the API server accepted the chart's
	// ServiceMonitor; its fields are covered by chart/tests/monitoring.
	var svc, sm string

	AfterEach(func(ctx SpecContext) {
		if !CurrentSpecReport().Failed() {
			return
		}
		// Controller logs are collected by the suite and the CI workflow; this adds the metrics wiring.
		out, err := utils.Run(exec.CommandContext(ctx, "kubectl", "get",
			"service,endpointslices,servicemonitor",
			"-n", namespace,
			"-l", prometheusReleaseSelector,
			"-o", "wide",
		))
		GinkgoLogr.Info("[DIAG] metrics wiring", "output", string(out), "error", err)
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
		// The Service and ServiceMonitor fields are covered by chart/tests/monitoring; only a
		// live cluster shows that the selector and the named targetPort resolve to the pod.
		By("waiting for a ready EndpointSlice address on the metrics port (pod selector and port-name match)")
		Eventually(func() string {
			// EndpointSlice, not v1 Endpoints: reading Endpoints prints a deprecation warning on
			// Kubernetes 1.33+, which utils.Run returns with stdout and would pass this check.
			// The port filter matters: an unresolvable targetPort still yields a slice with the
			// pod address, only with no ports.
			v, err := utils.GetResourceField(ctx,
				"endpointslices -n "+namespace+" -l kubernetes.io/service-name="+svc,
				`{.items[?(@.ports[0].name=="metrics")].endpoints[?(@.conditions.ready==true)].addresses[0]}`)
			if err != nil {
				return ""
			}
			return v
		}, timeout).ShouldNot(BeEmpty(),
			"EndpointSlice address must be non-empty; empty means selector or port-name mismatch")
	})

	It("serves controller metrics through the Service", func(ctx SpecContext) {
		By("scraping /metrics through the API server service proxy")
		Eventually(func(g Gomega) {
			cmd := exec.CommandContext(ctx, "kubectl", "get", "--raw",
				"/api/v1/namespaces/"+namespace+"/services/http:"+svc+":metrics/proxy/metrics",
			)
			out, err := utils.Run(cmd)
			g.Expect(err).NotTo(HaveOccurred())
			// controller-runtime creates the controller_runtime_reconcile_total series when a
			// controller starts; their presence proves the proxy reached the manager's metrics endpoint.
			g.Expect(string(out)).To(ContainSubstring("controller_runtime_reconcile_total"))
		}, timeout).Should(Succeed())
	})
})
