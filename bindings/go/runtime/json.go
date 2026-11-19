package runtime

import (
	"encoding/json/v2"
)

// JSONOptions are the marshal options every OCM serialization uses.
//
// encoding/json/v2 changed three defaults that OCM's wire format depends on.
// Component descriptors are stored as content-addressed blobs and signed over a
// normalized form, so a change in the produced bytes changes digests and breaks
// signature verification of already-signed component versions. The options below
// pin the three defaults back to the behavior the format was specified with:
//
//   - Deterministic: v2 marshals Go maps in a random order. Identities, labels and
//     OCI annotations are maps, so without this the same object produces different
//     bytes - and a different digest - on every call.
//   - FormatNilMapAsNull / FormatNilSliceAsNull: v2 encodes a nil map as {} and a nil
//     slice as [], where the format (and every descriptor written so far) has null.
//
// HTML escaping is deliberately left at the v2 default (off), which is what
// normalisation always used and what its golden signatures rest on. The one place
// where v1's escaping reaches stored bytes opts back in locally, see
// [ocm.software/open-component-model/bindings/go/descriptor/runtime.ConvertToV2Provider].
//
// Everything else uses the v2 defaults, which are deliberately stricter than v1:
// object member names are matched case-sensitively and duplicate member names are
// rejected.
//
// The options are marshal-only; passing them to [json.Unmarshal] is a no-op.
// Decoding uses [JSONDecodeOptions].
var JSONOptions = json.JoinOptions(
	json.Deterministic(true),
	json.FormatNilMapAsNull(true),
	json.FormatNilSliceAsNull(true),
)

// JSONDecodeOptions are the unmarshal options for typed data that came out of a
// user-authored document.
//
// Such documents (.ocmconfig, CRs, constructor files) are read as YAML through
// sigs.k8s.io/yaml, which converts to JSON and decodes with encoding/json v1, so a
// non-canonical member spelling like "DockerConfigFile" survives into the nested
// [Raw] bytes verbatim. Decoding those bytes with the v2 default - case-sensitive
// matching, no rejection of unknown members - would drop the member silently and
// leave the field empty, which surfaces far from its cause (an anonymous pull, a
// 401, a "required field missing" on a document that supplies it).
//
// Matching the leniency of the document the bytes came from keeps that from
// happening. Strictness about members the target does not declare is the job of
// [DecodeStrict], which callers use where user input must be validated.
var JSONDecodeOptions = json.MatchCaseInsensitiveNames(true)
