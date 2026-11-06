package user.dockerfile.ocm_ds_0001_test

import data.user.dockerfile.ocm_ds_0001 as check
import rego.v1

# result.new is a Trivy builtin; opa test runs with it declared in its
# capabilities and replaced here by the message.
mock_result(msg, _) := msg

digest := "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

# One stage per FROM, its values as Trivy parses them: flags, image, AS, name.
dockerfile(froms) := {"Stages": [{"Commands": [{"Cmd": "from", "Value": v}]} | some v in froms]}

denied(froms) := r if {
	r := check.deny with input as dockerfile(froms) with result.new as mock_result
}

test_tag_denied if {
	denied([["alpine:3.22"]]) == {`base image "alpine:3.22" is not pinned by digest`}
}

test_pinned_and_scratch_allowed if {
	count(denied([
		["--platform=$BUILDPLATFORM", sprintf("golang:1.25@%s", [digest]), "AS", "build"],
		["scratch"],
	])) == 0
}

test_earlier_stage_allowed if {
	count(denied([[sprintf("alpine:3.22@%s", [digest]), "AS", "base"], ["base"]])) == 0
}

test_later_stage_denied if {
	denied([["later"], [sprintf("alpine:3.22@%s", [digest]), "AS", "later"]]) == {`base image "later" is not pinned by digest`}
}

test_self_alias_denied if {
	denied([["alpine", "AS", "alpine"]]) == {`base image "alpine" is not pinned by digest`}
}
