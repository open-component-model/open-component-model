# METADATA
# title: Roles do not grant write access to Secrets
# description: No Role or ClusterRole may create, update, patch or delete Secrets. Read access is reviewed and accepted per role (KSV-0041 in .github/benchmarks/trivyignore.yaml); this check keeps a write grant from hiding behind that acceptance.
# scope: package
# schemas:
#   - input: schema["kubernetes"]
# custom:
#   id: OCM-KSV-0003
#   avd_id: OCM-KSV-0003
#   severity: HIGH
#   short_code: no-secret-write
#   recommended_action: Grant at most get, list and watch on secrets.
#   input:
#     selector:
#       - type: kubernetes
package user.kubernetes.ocm_ksv_0003

import rego.v1

write_verbs := {"create", "update", "patch", "delete", "deletecollection", "*"}

# Secrets are core resources, so only the core group "" or "*" grants them.
grants_secrets(rule) if {
	some group in rule.apiGroups
	group in {"", "*"}
	some resource in rule.resources
	resource in {"secrets", "*"}
}

deny contains res if {
	input.kind in {"Role", "ClusterRole"}
	some rule in input.rules
	grants_secrets(rule)
	some verb in rule.verbs
	verb in write_verbs
	res := result.new(sprintf("%s %q grants %q on secrets", [input.kind, input.metadata.name, verb]), rule)
}
