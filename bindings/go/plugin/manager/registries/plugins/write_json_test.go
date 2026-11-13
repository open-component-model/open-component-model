package plugins_test

import (
	"encoding/json/jsontext"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/plugins"
)

func TestWriteJSON(t *testing.T) {
	t.Run("a marshalable value is written as the body", func(t *testing.T) {
		r := require.New(t)

		rec := httptest.NewRecorder()
		plugins.WriteJSON(rec, map[string]string{"b": "2", "a": "1"})

		r.Equal(http.StatusOK, rec.Code)
		r.JSONEq(`{"a":"1","b":"2"}`, rec.Body.String())
	})

	t.Run("a value that fails half way through yields an error-only 500", func(t *testing.T) {
		r := require.New(t)

		// An empty, non-nil jsontext.Value marshals the opening members and then fails,
		// so writing straight into the response would commit a 200 and a truncated body.
		response := struct {
			Name  string         `json:"name"`
			Value jsontext.Value `json:"value"`
		}{Name: "component", Value: jsontext.Value{}}

		rec := httptest.NewRecorder()
		plugins.WriteJSON(rec, response)

		r.Equal(http.StatusInternalServerError, rec.Code)
		r.NotContains(rec.Body.String(), `"name"`, "a failed response must not carry partial JSON")
	})
}
