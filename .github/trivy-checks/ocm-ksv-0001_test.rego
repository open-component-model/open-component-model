package user.kubernetes.ocm_ksv_0001_test

import data.user.kubernetes.ocm_ksv_0001 as check
import rego.v1

mock_result(msg, _) := msg

pinned := "ghcr.io/example/manager:v1@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

denied(pod_spec) := r if {
	r := check.deny with input as {
		"apiVersion": "apps/v1",
		"kind": "Deployment",
		"spec": {"template": {"spec": pod_spec}},
	}
		with result.new as mock_result
}

test_pinned_allowed if {
	count(denied({"containers": [{"name": "manager", "image": pinned}]})) == 0
}

test_tag_denied if {
	denied({"containers": [{"name": "manager", "image": "ghcr.io/example/manager:v1"}]}) == {
		`container "manager" uses image "ghcr.io/example/manager:v1", which is not pinned by digest`,
	}
}

test_init_container_tag_denied if {
	denied({
		"initContainers": [{"name": "init", "image": "ghcr.io/example/init:v1"}],
		"containers": [{"name": "manager", "image": pinned}],
	}) == {`container "init" uses image "ghcr.io/example/init:v1", which is not pinned by digest`}
}
