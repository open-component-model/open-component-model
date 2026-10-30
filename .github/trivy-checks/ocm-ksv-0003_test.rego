package user.kubernetes.ocm_ksv_0003_test

import data.user.kubernetes.ocm_ksv_0003 as check
import rego.v1

mock_result(msg, _) := msg

denied(kind, rule) := r if {
	r := check.deny with input as {
		"apiVersion": "rbac.authorization.k8s.io/v1",
		"kind": kind,
		"metadata": {"name": "fixture"},
		"rules": [rule],
	}
		with result.new as mock_result
}

test_read_allowed if {
	count(denied("ClusterRole", {"apiGroups": [""], "resources": ["secrets"], "verbs": ["get", "list", "watch"]})) == 0
}

test_other_group_allowed if {
	count(denied("ClusterRole", {
		"apiGroups": ["delivery.ocm.software"],
		"resources": ["*"],
		"verbs": ["create", "update", "patch", "delete"],
	})) == 0
}

test_wildcard_denied if {
	denied("ClusterRole", {"apiGroups": ["*"], "resources": ["*"], "verbs": ["*"]}) == {
		`ClusterRole "fixture" grants "*" on secrets`,
	}
}

test_write_denied if {
	denied("Role", {"apiGroups": [""], "resources": ["configmaps", "secrets"], "verbs": ["get", "create", "patch"]}) == {
		`Role "fixture" grants "create" on secrets`,
		`Role "fixture" grants "patch" on secrets`,
	}
}

test_other_kind_ignored if {
	count(denied("ConfigMap", {"apiGroups": ["*"], "resources": ["*"], "verbs": ["*"]})) == 0
}
