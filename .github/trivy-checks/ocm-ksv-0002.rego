# METADATA
# title: Containers define liveness and readiness probes
# description: Every container of a workload has a liveness and a readiness probe, so Kubernetes restarts hung containers and routes no traffic to containers that are not ready.
# scope: package
# schemas:
#   - input: schema["kubernetes"]
# custom:
#   id: OCM-KSV-0002
#   avd_id: OCM-KSV-0002
#   severity: MEDIUM
#   short_code: health-probes
#   recommended_action: Set livenessProbe and readinessProbe on every container.
#   input:
#     selector:
#       - type: kubernetes
package user.kubernetes.ocm_ksv_0002

import rego.v1

deny contains res if {
	some c in input.spec.template.spec.containers
	some probe in ["livenessProbe", "readinessProbe"]
	not c[probe]
	res := result.new(sprintf("container %q has no %s", [c.name, probe]), c)
}
