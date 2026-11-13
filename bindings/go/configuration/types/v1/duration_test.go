package v1_test

import (
	"encoding/json"
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	typesv1 "ocm.software/open-component-model/bindings/go/configuration/types/v1"
)

func TestDuration_MarshalJSON(t *testing.T) {
	tests := []struct {
		name     string
		duration typesv1.Duration
		want     string
	}{
		{
			name:     "zero duration",
			duration: typesv1.Duration(0),
			want:     `"0s"`,
		},
		{
			name:     "one second",
			duration: typesv1.Duration(time.Second),
			want:     `"1s"`,
		},
		{
			name:     "complex duration",
			duration: typesv1.Duration(2*time.Hour + 30*time.Minute + 15*time.Second),
			want:     `"2h30m15s"`,
		},
		{
			name:     "sub-millisecond",
			duration: typesv1.Duration(500 * time.Microsecond),
			want:     `"500µs"`,
		},
		{
			name:     "negative duration",
			duration: typesv1.Duration(-5 * time.Second),
			want:     `"-5s"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := require.New(t)
			got, err := tt.duration.MarshalJSON()
			r.NoError(err)
			r.JSONEq(tt.want, string(got))
		})
	}
}

func TestDuration_UnmarshalJSON(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    typesv1.Duration
		wantErr bool
	}{
		{
			name:  "string: simple seconds",
			input: `"5s"`,
			want:  typesv1.Duration(5 * time.Second),
		},
		{
			name:  "string: complex duration",
			input: `"1h30m"`,
			want:  typesv1.Duration(time.Hour + 30*time.Minute),
		},
		{
			name:  "string: zero",
			input: `"0s"`,
			want:  typesv1.Duration(0),
		},
		{
			name:  "string: negative",
			input: `"-10s"`,
			want:  typesv1.Duration(-10 * time.Second),
		},
		{
			name:  "numeric: nanoseconds as float64",
			input: `1000000000`,
			want:  typesv1.Duration(time.Second),
		},
		{
			name:  "numeric: zero",
			input: `0`,
			want:  typesv1.Duration(0),
		},
		{
			name:  "numeric: nanoseconds below a second",
			input: `1500000000`,
			want:  typesv1.Duration(1500 * time.Millisecond),
		},
		{
			name:    "invalid: not a valid duration string",
			input:   `"notaduration"`,
			wantErr: true,
		},
		{
			name:    "invalid: boolean",
			input:   `true`,
			wantErr: true,
		},
		{
			name:    "invalid: array",
			input:   `[1,2]`,
			wantErr: true,
		},
		{
			name:    "invalid: object",
			input:   `{"seconds":5}`,
			wantErr: true,
		},
		{
			name:    "invalid: malformed json",
			input:   `{bad`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := require.New(t)
			var d typesv1.Duration
			err := d.UnmarshalJSON([]byte(tt.input))
			if tt.wantErr {
				r.Error(err)
				return
			}
			r.NoError(err)
			r.Equal(tt.want, d)
		})
	}
}

func TestDuration_RoundTrip(t *testing.T) {
	tests := []struct {
		name     string
		duration typesv1.Duration
	}{
		{name: "zero", duration: typesv1.Duration(0)},
		{name: "one second", duration: typesv1.Duration(time.Second)},
		{name: "complex", duration: typesv1.Duration(3*time.Hour + 15*time.Minute + 42*time.Second)},
		{name: "negative", duration: typesv1.Duration(-30 * time.Second)},
		{name: "sub-second", duration: typesv1.Duration(250 * time.Millisecond)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := require.New(t)
			data, err := tt.duration.MarshalJSON()
			r.NoError(err)

			var got typesv1.Duration
			err = got.UnmarshalJSON(data)
			r.NoError(err)
			r.Equal(tt.duration, got)
		})
	}
}

func TestDuration_JSONStructIntegration(t *testing.T) {
	r := require.New(t)
	type wrapper struct {
		Timeout typesv1.Duration `json:"timeout"`
	}

	w := wrapper{Timeout: typesv1.Duration(5 * time.Second)}
	data, err := json.Marshal(w)
	r.NoError(err)
	r.Contains(string(data), `"5s"`)

	var got wrapper
	err = json.Unmarshal(data, &got)
	r.NoError(err)
	r.Equal(w.Timeout, got.Timeout)
}

func TestDuration_UnmarshalJSON_NullKeepsValue(t *testing.T) {
	r := require.New(t)
	var v struct {
		D typesv1.Duration `json:"d"`
	}
	v.D = typesv1.Duration(time.Hour)
	r.NoError(json.Unmarshal([]byte(`{"d": null}`), &v))
	r.Equal(typesv1.Duration(time.Hour), v.D)
}

func TestDuration_UnmarshalJSON_InvalidStringNamesValue(t *testing.T) {
	var d typesv1.Duration
	require.ErrorContains(t, json.Unmarshal([]byte(`"soon"`), &d), `invalid duration "soon"`)
}

func TestDuration_UnmarshalJSON_IntegerNanoseconds(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		want    time.Duration
		wantErr bool
	}{
		{name: "max int64 is exact", in: `9223372036854775807`, want: time.Duration(math.MaxInt64)},
		{name: "min int64 is exact", in: `-9223372036854775808`, want: time.Duration(math.MinInt64)},
		{name: "precision beyond float64 is kept", in: `9007199254740993`, want: time.Duration(9007199254740993)},
		{name: "overflow is rejected", in: `9223372036854775808`, wantErr: true},
		{name: "fraction is rejected", in: `1.5`, wantErr: true},
		{name: "exponent is rejected", in: `1e9`, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := require.New(t)
			var d typesv1.Duration
			err := json.Unmarshal([]byte(tt.in), &d)
			if tt.wantErr {
				r.Error(err)
				return
			}
			r.NoError(err)
			r.Equal(tt.want, time.Duration(d))
		})
	}
}
