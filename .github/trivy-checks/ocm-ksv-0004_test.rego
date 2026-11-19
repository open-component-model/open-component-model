package user.kubernetes.ocm_ksv_0004_test

import data.user.kubernetes.ocm_ksv_0004 as check
import rego.v1

# result.new is a Trivy builtin; opa test runs with it declared in its
# capabilities and replaced here by the message.
mock_result(msg, _) := msg

role(kind, rules) := {"kind": kind, "metadata": {"name": "role"}, "rules": rules}

denied(obj) := r if {
	r := check.deny with input as obj with result.new as mock_result
}

test_serviceaccount_impersonation_denied if {
	denied(role("ClusterRole", [{"apiGroups": [""], "resources": ["serviceaccounts"], "verbs": ["impersonate"]}])) == {
		`ClusterRole "role" grants impersonation of ["serviceaccounts"]`,
	}
}

test_group_impersonation_in_namespaced_role_denied if {
	count(denied(role("Role", [{"apiGroups": [""], "resources": ["groups"], "verbs": ["impersonate"]}]))) == 1
}

test_wildcard_verb_on_serviceaccounts_denied if {
	count(denied(role("ClusterRole", [{"apiGroups": [""], "resources": ["serviceaccounts"], "verbs": ["*"]}]))) == 1
}

test_serviceaccount_read_allowed if {
	count(denied(role("ClusterRole", [{"apiGroups": [""], "resources": ["serviceaccounts"], "verbs": ["get", "list", "watch"]}]))) == 0
}

test_wildcard_verb_on_other_resources_allowed if {
	count(denied(role("Role", [{"apiGroups": [""], "resources": ["configmaps"], "verbs": ["*"]}]))) == 0
}

test_other_kind_ignored if {
	count(denied({"kind": "ServiceAccount", "metadata": {"name": "role"}, "rules": [{"apiGroups": [""], "resources": ["serviceaccounts"], "verbs": ["impersonate"]}]})) == 0
}
