# OCM's own Trivy checks run next to Trivy's built-in checks (image-scan.yml,
# job "chart") for what no built-in check covers.

# METADATA
# title: Container images are pinned by digest
# description: Every container image is referenced by digest (image@sha256:...), so the deployed image cannot change under a tag.
# scope: package
# schemas:
#   - input: schema["kubernetes"]
# custom:
#   id: OCM-KSV-0001
#   avd_id: OCM-KSV-0001
#   severity: HIGH
#   short_code: image-digest
#   recommended_action: Reference the image as <repository>:<tag>@sha256:<digest>.
#   input:
#     selector:
#       - type: kubernetes
package user.kubernetes.ocm_ksv_0001

import rego.v1

containers contains c if {
	some c in input.spec.template.spec.containers
}

containers contains c if {
	some c in input.spec.template.spec.initContainers
}

deny contains res if {
	some c in containers
	not regex.match(`@sha256:[a-f0-9]{64}$`, c.image)
	res := result.new(sprintf("container %q uses image %q, which is not pinned by digest", [c.name, c.image]), c)
}
