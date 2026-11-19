package user.dockerfile.ocm_ds_0001_test

import data.user.dockerfile.ocm_ds_0001 as check
import rego.v1

# result.new is a Trivy builtin; opa test runs with it declared in its
# capabilities and replaced here by the message.
mock_result(msg, _) := msg

digest := "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

pinned := sprintf("alpine:3.22@%s", [digest])

# Commands as Trivy parses them: instruction flags stay in Flags, never in Value.
cmd(name, flags, value) := {"Cmd": name, "Flags": flags, "Value": value}

from(image) := cmd("from", [], [image])

from_as(image, stage) := cmd("from", [], [image, "AS", stage])

denied(stages) := r if {
	r := check.deny with input as {"Stages": [{"Commands": cmds} | some cmds in stages]}
		with result.new as mock_result
}

test_tag_denied if {
	denied([[from("alpine:3.22")]]) == {`image "alpine:3.22" is not pinned by digest`}
}

test_pinned_and_scratch_allowed if {
	count(denied([
		[cmd("from", ["--platform=$BUILDPLATFORM"], [sprintf("golang:1.25@%s", [digest]), "AS", "build"])],
		[from("scratch")],
	])) == 0
}

test_earlier_stage_allowed if {
	count(denied([[from_as(pinned, "base")], [from("base")]])) == 0
}

test_later_stage_denied if {
	denied([[from("later")], [from_as(pinned, "later")]]) == {`image "later" is not pinned by digest`}
}

test_self_alias_denied if {
	denied([[from_as("alpine", "alpine")]]) == {`image "alpine" is not pinned by digest`}
}

# BuildKit also pulls images named by COPY --from and RUN --mount=...,from=.
test_copy_from_image_denied if {
	denied([[from_as(pinned, "base"), cmd("copy", ["--from=busybox:1.37"], ["/a", "/b"])]]) == {
		`image "busybox:1.37" is not pinned by digest`,
	}
}

test_mount_from_image_denied if {
	denied([[from_as(pinned, "base"), cmd("run", ["--mount=type=bind,from=busybox:1.37,target=/b"], ["true"])]]) == {
		`image "busybox:1.37" is not pinned by digest`,
	}
}

test_copy_from_earlier_stage_allowed if {
	count(denied([
		[from_as(pinned, "base")],
		[
			from("scratch"),
			cmd("copy", ["--from=base"], ["/a", "/b"]),
			cmd("copy", ["--from=0"], ["/a", "/b"]),
		],
	])) == 0
}

test_mount_without_source_allowed if {
	count(denied([[from_as(pinned, "base"), cmd("run", ["--mount=type=cache,target=/go/pkg/mod"], ["go", "mod", "download"])]])) == 0
}
