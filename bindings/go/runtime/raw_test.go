package runtime

import (
	"encoding/json/v2"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRaw_UnmarshalJSON_Success(t *testing.T) {
	input := `{"type":"example","foo":"bar"}`

	var raw Raw
	err := json.Unmarshal([]byte(input), &raw)

	require.NoError(t, err)
	require.Equal(t, NewUnversionedType("example"), raw.Type)
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
	require.Contains(t, err.Error(), "unexpected EOF")
}

func TestRaw_MarshalJSON(t *testing.T) {
	original := []byte(`{"foo":"bar","type":"example"}`)

	raw := Raw{
		Type: NewUnversionedType("example"),
		Data: original,
	}

	data, err := json.Marshal(&raw)

	require.NoError(t, err)
	require.Equal(t, original, data)
}

func TestRaw_GetSetType(t *testing.T) {
	raw := &Raw{}
	raw.SetType(NewUnversionedType("testtype"))

	require.Equal(t, NewUnversionedType("testtype"), raw.GetType())
}

func TestRaw_String(t *testing.T) {
	raw := &Raw{Data: []byte("some raw data")}

	require.Equal(t, "some raw data", raw.String())
}

// The type member is extracted with a nested unmarshal, which must keep the options the
// outer decoder was given: a .ocmconfig read through sigs.k8s.io/yaml keeps the author's
// spelling, and losing the type here makes every dispatch on it silently skip the entry.
func TestRaw_UnmarshalJSONFrom_KeepsDecodeOptions(t *testing.T) {
	var raw Raw
	require.NoError(t, json.Unmarshal([]byte(`{"Type":"example/v1","foo":"bar"}`), &raw, JSONDecodeOptions))

	require.Equal(t, NewVersionedType("example", "v1"), raw.GetType())
}
