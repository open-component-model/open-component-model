package user.kubernetes.ocm_ksv_0002_test

import data.user.kubernetes.ocm_ksv_0002 as check
import rego.v1

mock_result(msg, _) := msg

denied(container) := r if {
	r := check.deny with input as {
		"apiVersion": "apps/v1",
		"kind": "Deployment",
		"spec": {"template": {"spec": {"containers": [object.union({"name": "manager"}, container)]}}},
	}
		with result.new as mock_result
}

probe := {"httpGet": {"path": "/healthz", "port": 8081}}

test_probed_allowed if {
	count(denied({"livenessProbe": probe, "readinessProbe": probe})) == 0
}

test_no_probes_denied if {
	denied({}) == {`container "manager" has no livenessProbe`, `container "manager" has no readinessProbe`}
}

test_missing_readiness_denied if {
	denied({"livenessProbe": probe}) == {`container "manager" has no readinessProbe`}
}
