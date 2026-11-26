package attestation

import (
	"encoding/base64"
	"encoding/json/jsontext"
	"encoding/json/v2"

	"ocm.software/open-component-model/bindings/go/repository"
)

// Payload extracts the inner document an attestation carries: the real SBOM, VEX,
// provenance or other predicate, stripped of the envelope it was published in. It unwraps,
// in order, a Sigstore bundle (its DSSE envelope), a bare DSSE envelope and an in-toto
// statement, returning the statement's predicate; a document published without any envelope
// is returned unchanged. The result is the raw JSON of the inner document, ready to be
// re-serialized to JSON or YAML.
//
// ok is false when the attestation carries no document bytes, or they are not a JSON
// document (for example a binary signature), so there is no inner payload to extract; the
// caller skips such documents.
func Payload(a repository.Attestation) (jsontext.Value, bool) {
	return unwrap(a.Data)
}

// unwrap peels one layer of envelope at a time off the raw document bytes. A sigstore
// bundle nests a DSSE envelope, which nests a base64 in-toto statement, which carries the
// predicate; each branch strips exactly one of those and defers to the inner decoder.
func unwrap(data []byte) (jsontext.Value, bool) {
	value := jsontext.Value(data)
	if len(data) == 0 || !value.IsValid() {
		return nil, false
	}
	var doc map[string]jsontext.Value
	if err := json.Unmarshal(data, &doc); err != nil {
		// A valid JSON value that is not an object (an array or scalar) carries no
		// envelope of its own, so it is already the payload.
		return value, true
	}

	// Sigstore bundle: the DSSE envelope lives under "dsseEnvelope".
	if env, ok := doc["dsseEnvelope"]; ok {
		if p, ok := dssePredicate(env); ok {
			return p, true
		}
	}
	// Bare DSSE envelope: a base64 "payload" tagged by "payloadType" at the top level.
	if _, ok := doc["payloadType"]; ok {
		if p, ok := dssePredicate(value); ok {
			return p, true
		}
	}
	// in-toto statement: "_type" names the statement, "predicate" is the real document.
	if _, ok := doc["_type"]; ok {
		if pred, ok := predicateOf(doc); ok {
			return pred, true
		}
	}
	// A document published without an envelope is itself the payload.
	return value, true
}

// dssePredicate decodes a DSSE envelope's base64 payload and returns the inner document:
// the predicate of the in-toto statement it wraps, or the decoded payload itself when that
// is not such a statement. ok is false when the envelope carries no decodable JSON payload.
func dssePredicate(envelope jsontext.Value) (jsontext.Value, bool) {
	var env struct {
		Payload string `json:"payload"`
	}
	if err := json.Unmarshal(envelope, &env); err != nil || env.Payload == "" {
		return nil, false
	}
	decoded, ok := decodeBase64(env.Payload)
	if !ok || !jsontext.Value(decoded).IsValid() {
		return nil, false
	}
	var stmt map[string]jsontext.Value
	if err := json.Unmarshal(decoded, &stmt); err == nil {
		if pred, ok := predicateOf(stmt); ok {
			return pred, true
		}
	}
	return jsontext.Value(decoded), true
}

// predicateOf returns the "predicate" member of an in-toto statement, mirroring the
// extraction oci/internal/attestation uses: a missing, empty or explicitly null predicate
// is treated as absent so the caller falls back to the enclosing document.
func predicateOf(statement map[string]jsontext.Value) (jsontext.Value, bool) {
	pred, ok := statement["predicate"]
	if !ok || len(pred) == 0 || string(pred) == "null" {
		return nil, false
	}
	return pred, true
}

// decodeBase64 decodes a DSSE payload, accepting both the padded standard encoding the
// in-toto spec mandates and the unpadded variant some producers emit.
func decodeBase64(s string) ([]byte, bool) {
	if decoded, err := base64.StdEncoding.DecodeString(s); err == nil {
		return decoded, true
	}
	if decoded, err := base64.RawStdEncoding.DecodeString(s); err == nil {
		return decoded, true
	}
	return nil, false
}
