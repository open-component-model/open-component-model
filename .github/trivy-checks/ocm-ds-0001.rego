# METADATA
# title: Images pulled by the build are pinned by digest
# description: Every image a build pulls - the FROM base image, COPY --from=<image> and RUN --mount=...,from=<image> - refers to scratch, an earlier build stage, or an image pinned by digest (image@sha256:...), so a build cannot pull a changed image unnoticed and updates arrive as reviewed digest changes. The "# syntax=" frontend directive is a comment and cannot be seen here; image-scan.yml checks it separately.
# scope: package
# schemas:
#   - input: schema["dockerfile"]
# custom:
#   id: OCM-DS-0001
#   avd_id: OCM-DS-0001
#   severity: HIGH
#   short_code: base-image-digest
#   recommended_action: Reference the image as <repository>:<tag>@sha256:<digest>.
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

# Only a stage defined before stage j can be its source: "FROM alpine AS alpine"
# pulls the image alpine.
earlier_stage(j, ref) if {
	some k, stage in input.Stages
	k < j
	stage_name(stage) == ref
}

# COPY --from and RUN --mount=from may also name an earlier stage by index.
earlier_stage(j, ref) if {
	regex.match(`^[0-9]+$`, ref)
	to_number(ref) < j
}

# Every image reference BuildKit resolves, as [stage index, reference, command].
# Trivy keeps instruction flags (--platform, --from, --mount) in Flags, so the
# first value of a FROM is its image.
image_refs contains [j, cmd.Value[0], cmd] if {
	some j, stage in input.Stages
	some cmd in stage.Commands
	cmd.Cmd == "from"
}

image_refs contains [j, trim_prefix(flag, "--from="), cmd] if {
	some j, stage in input.Stages
	some cmd in stage.Commands
	cmd.Cmd == "copy"
	some flag in cmd.Flags
	startswith(flag, "--from=")
}

image_refs contains [j, trim_prefix(opt, "from="), cmd] if {
	some j, stage in input.Stages
	some cmd in stage.Commands
	cmd.Cmd == "run"
	some flag in cmd.Flags
	startswith(flag, "--mount=")
	some opt in split(trim_prefix(flag, "--mount="), ",")
	startswith(opt, "from=")
}

deny contains res if {
	some [j, ref, cmd] in image_refs
	lower(ref) != "scratch"
	not earlier_stage(j, lower(ref))
	not regex.match(`@sha256:[a-f0-9]{64}$`, ref)
	res := result.new(sprintf("image %q is not pinned by digest", [ref]), cmd)
}
