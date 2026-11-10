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
// Everything else uses the v2 defaults, which are deliberately stricter than v1:
// object member names are matched case-sensitively, duplicate member names are
// rejected, and strings are not HTML-escaped.
//
// The options are marshal-only; passing them to [json.Unmarshal] is a no-op. Decoding
// takes no OCM-wide options, so plain json.Unmarshal is used for reading.
var JSONOptions = json.JoinOptions(
	json.Deterministic(true),
	json.FormatNilMapAsNull(true),
	json.FormatNilSliceAsNull(true),
)
