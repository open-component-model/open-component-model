# METADATA
# title: Base images are pinned by digest
# description: Every FROM refers to scratch, an earlier build stage, or an image pinned by digest (image@sha256:...), so a build cannot pull a changed base image unnoticed and updates arrive as reviewed digest changes.
# scope: package
# schemas:
#   - input: schema["dockerfile"]
# custom:
#   id: OCM-DS-0001
#   avd_id: OCM-DS-0001
#   severity: HIGH
#   short_code: base-image-digest
#   recommended_action: Reference the base image as <repository>:<tag>@sha256:<digest>.
#   input:
#     selector:
#       - type: dockerfile
package user.dockerfile.ocm_ds_0001

import rego.v1

# The stage name a stage defines with "FROM <image> AS <name>".
stage_name(stage) := lower(cmd.Value[i + 1]) if {
	some cmd in stage.Commands
	cmd.Cmd == "from"
	some i, v in cmd.Value
	lower(v) == "as"
}

# Only a stage defined before stage j can be its base: "FROM alpine AS alpine"
# pulls the image alpine.
earlier_stage(j, name) if {
	some k, stage in input.Stages
	k < j
	stage_name(stage) == name
}

# The image of a FROM: its first value that is not a --flag.
from_image(cmd) := [v | some v in cmd.Value; not startswith(v, "--")][0]

deny contains res if {
	some j, stage in input.Stages
	some cmd in stage.Commands
	cmd.Cmd == "from"
	image := from_image(cmd)
	lower(image) != "scratch"
	not earlier_stage(j, lower(image))
	not regex.match(`@sha256:[a-f0-9]{64}$`, image)
	res := result.new(sprintf("base image %q is not pinned by digest", [image]), cmd)
}
