package runtime

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRaw_UnmarshalJSON_Success(t *testing.T) {
	input := `{"type":"example","foo":"bar"}`

	var raw Raw
	err := json.Unmarshal([]byte(input), &raw)

	require.NoError(t, err)
	require.Equal(t, NewUngroupedUnversionedType("example"), raw.Type)
	require.NotEmpty(t, raw.Data)

	// Ensure data is canonicalized (e.g., keys are sorted)
	expectedCanonical := `{"foo":"bar","type":"example"}`
	require.JSONEq(t, expectedCanonical, string(raw.Data))
}

func TestRaw_UnmarshalJSON_InvalidJSON(t *testing.T) {
	input := `{"type":"example",`

	var raw Raw
	err := json.Unmarshal([]byte(input), &raw)

	require.Error(t, err)
	require.Contains(t, err.Error(), "unexpected end of JSON input")
}

func TestRaw_MarshalJSON(t *testing.T) {
	original := []byte(`{"foo":"bar","type":"example"}`)

	raw := Raw{
		Type: NewUngroupedUnversionedType("example"),
		Data: original,
	}

	data, err := json.Marshal(&raw)

	require.NoError(t, err)
	require.Equal(t, original, data)
}

func TestRaw_GetSetType(t *testing.T) {
	raw := &Raw{}
	raw.SetType(NewUngroupedUnversionedType("testtype"))

	require.Equal(t, NewUngroupedUnversionedType("testtype"), raw.GetType())
}

func TestRaw_String(t *testing.T) {
	raw := &Raw{Data: []byte("some raw data")}

	require.Equal(t, "some raw data", raw.String())
}
