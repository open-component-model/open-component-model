# METADATA
# title: Roles do not grant impersonation
# description: No Role or ClusterRole may impersonate service accounts, users or groups. Impersonation lets its holder act with the permissions of every subject it can name, which bypasses the other RBAC findings including OCM-KSV-0003. Neither benchmark reports it, because Trivy KSV-0043 only looks at group impersonation and the pinned NSA/CISA and MITRE frameworks do not contain Kubescape C-0065. Grants the chart needs are accepted per template in .github/benchmarks/trivyignore.yaml.
# scope: package
# schemas:
#   - input: schema["kubernetes"]
# custom:
#   id: OCM-KSV-0004
#   avd_id: OCM-KSV-0004
#   severity: HIGH
#   short_code: no-impersonation
#   recommended_action: Bind the workload to the permissions it needs instead of letting it impersonate other subjects.
#   input:
#     selector:
#       - type: kubernetes
package user.kubernetes.ocm_ksv_0004

import rego.v1

# The subjects a role can impersonate; users, groups and uids also exist in
# authentication.k8s.io, so the API group is not narrowed here.
impersonation_resources := {"serviceaccounts", "users", "groups", "uids", "*"}

# The verb exists only for impersonation, so it needs no resource check; a
# wildcard verb grants it on every resource the rule names.
grants_impersonation(rule) if "impersonate" in rule.verbs

grants_impersonation(rule) if {
	"*" in rule.verbs
	some resource in rule.resources
	resource in impersonation_resources
}

deny contains res if {
	input.kind in {"Role", "ClusterRole"}
	some rule in input.rules
	grants_impersonation(rule)
	res := result.new(sprintf("%s %q grants impersonation of %v", [input.kind, input.metadata.name, rule.resources]), rule)
}
