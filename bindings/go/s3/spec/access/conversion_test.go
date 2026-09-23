package access_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/runtime"
	accessspec "ocm.software/open-component-model/bindings/go/s3/spec/access"
	v1 "ocm.software/open-component-model/bindings/go/s3/spec/access/v1"
	v2 "ocm.software/open-component-model/bindings/go/s3/spec/access/v2"
)

func TestConvertToV2_WireFormats(t *testing.T) {
	for _, typ := range []string{"S3/v1", "s3/v1", "S3/v2", "s3/v2", "S3", "s3"} {
		for _, legacy := range []bool{false, true} {
			if typ == "S3/v1" || typ == "s3/v1" {
				if !legacy {
					continue
				}
			}
			t.Run(fmt.Sprintf("%s/legacy=%v", typ, legacy), func(t *testing.T) {
				r := require.New(t)
				bucket, key := "bucketName", "objectKey"
				if legacy {
					bucket, key = "bucket", "key"
				}
				data := []byte(fmt.Sprintf(`{"type":%q,%q:"b",%q:"k","region":"r","version":"ver","mediaType":"m","endpoint":"https://store.example","usePathStyle":true}`, typ, bucket, key))
				raw := &runtime.Raw{}
				r.NoError(json.Unmarshal(data, raw))
				unstructured := runtime.NewUnstructured()
				r.NoError(json.Unmarshal(data, &unstructured))
				for _, source := range []runtime.Typed{raw, &unstructured} {
					obj, err := accessspec.Scheme.NewObject(source.GetType())
					r.NoError(err)
					if legacy && (typ == "S3/v2" || typ == "s3/v2") {
						r.Error(accessspec.Scheme.Convert(source, obj))
						_, err = accessspec.ConvertToV2(source)
						r.Error(err)
						continue
					}
					r.NoError(accessspec.Scheme.Convert(source, obj))
					if typ == "S3/v1" || typ == "s3/v1" {
						r.IsType(&v1.S3{}, obj)
						wire, err := json.Marshal(obj)
						r.NoError(err)
						r.JSONEq(string(data), string(wire))
					} else {
						r.IsType(&v2.S3{}, obj)
					}
					for _, input := range []runtime.Typed{source, obj} {
						before, err := json.Marshal(input)
						r.NoError(err)
						out, err := accessspec.ConvertToV2(input)
						r.NoError(err)
						expectedType := source.GetType()
						if legacy || expectedType.Version == "" {
							expectedType = accessspec.V2VersionedType
						}
						r.Equal(&v2.S3{Type: expectedType, BucketName: "b", ObjectKey: "k", Region: "r", Version: "ver", MediaType: "m", Endpoint: "https://store.example", UsePathStyle: true}, out)
						r.NoError(out.Validate())
						wire, err := json.Marshal(out)
						r.NoError(err)
						r.JSONEq(fmt.Sprintf(`{"type":%q,"bucketName":"b","objectKey":"k","region":"r","version":"ver","mediaType":"m","endpoint":"https://store.example","usePathStyle":true}`, expectedType.String()), string(wire))
						out.BucketName = "changed"
						after, err := json.Marshal(input)
						r.NoError(err)
						r.Equal(string(before), string(after))
					}
				}
			})
		}
	}
}

func TestConvertToV2_Typed(t *testing.T) {
	t.Run("v1 without type", func(t *testing.T) {
		r := require.New(t)
		source := &v1.S3{Bucket: "b", Key: "k", Region: "r", Version: "v", MediaType: "m", Endpoint: "http://localhost", UsePathStyle: true}
		out, err := accessspec.ConvertToV2(source)
		r.NoError(err)
		r.Equal(&v2.S3{Type: accessspec.V2VersionedType, BucketName: "b", ObjectKey: "k", Region: "r", Version: "v", MediaType: "m", Endpoint: "http://localhost", UsePathStyle: true}, out)
		r.True(source.Type.IsEmpty())
	})
	t.Run("v2 without type", func(t *testing.T) {
		r := require.New(t)
		source := &v2.S3{BucketName: "b", ObjectKey: "k"}
		out, err := accessspec.ConvertToV2(source)
		r.NoError(err)
		r.Equal(accessspec.V2VersionedType, out.Type)
		r.True(source.Type.IsEmpty())
		out.ObjectKey = "changed"
		r.Equal("k", source.ObjectKey)
	})
	for _, source := range []runtime.Typed{nil, (*v1.S3)(nil), (*v2.S3)(nil), (*runtime.Raw)(nil), (*runtime.Unstructured)(nil), &v2.S3{Type: accessspec.V1VersionedType}, &runtime.Raw{Type: runtime.NewVersionedType("unknown", "v1")}, &runtime.Raw{Type: accessspec.V1VersionedType, Data: []byte(`{`)}, runtime.Identity{"type": "S3/v2"}} {
		t.Run(fmt.Sprintf("invalid/%T", source), func(t *testing.T) {
			r := require.New(t)
			out, err := accessspec.ConvertToV2(source)
			r.Error(err)
			r.Nil(out)
		})
	}
}

func TestS3_LegacyFields(t *testing.T) {
	for _, typ := range []string{"S3", "s3"} {
		for _, tt := range []struct {
			name, fields string
			wantErr      bool
		}{
			{"legacy", `"bucket":"b","key":"k"`, false},
			{"current", `"bucketName":"b","objectKey":"k"`, false},
			{"equal mixed", `"bucket":"b","bucketName":"b","key":"k","objectKey":"k"`, false},
			{"partial mixed", `"bucket":"b","objectKey":"k"`, false},
			{"case insensitive", `"BUCKET":"b","KEY":"k","BucketName":"b","ObjectKey":"k"`, false},
			{"bucket conflict", `"bucket":"other","bucketName":"b","key":"k"`, true},
			{"key conflict", `"bucket":"b","key":"other","objectKey":"k"`, true},
			{"empty conflict", `"bucket":"b","bucketName":"","key":"k"`, true},
			{"null conflict", `"bucket":"b","bucketName":null,"key":"k"`, true},
			{"case conflict", `"BUCKET":"other","bucketName":"b","key":"k"`, true},
			{"invalid bucket", `"bucket":42,"key":"k"`, true},
			{"invalid key", `"bucket":"b","key":true`, true},
		} {
			t.Run(typ+"/"+tt.name, func(t *testing.T) {
				r := require.New(t)
				data := []byte(fmt.Sprintf(`{"type":%q,%s}`, typ, tt.fields))
				for _, input := range []runtime.Typed{&runtime.Raw{}, &runtime.Unstructured{}} {
					r.NoError(json.Unmarshal(data, input))
					out, err := accessspec.ConvertToV2(input)
					if tt.wantErr {
						r.Error(err)
						r.Nil(out)
					} else {
						r.NoError(err)
						r.Equal("b", out.BucketName)
						r.Equal("k", out.ObjectKey)
					}
				}
			})
		}
	}
}

func TestS3_ExplicitV2RejectsLegacyFields(t *testing.T) {
	for _, typ := range []string{"S3/v2", "s3/v2"} {
		for _, fields := range []string{`"bucket":"b"`, `"key":"k"`, `"BUCKET":null`, `"key":""`} {
			t.Run(typ+fields, func(t *testing.T) {
				r := require.New(t)
				var out v2.S3
				r.Error(json.Unmarshal([]byte(fmt.Sprintf(`{"type":%q,"bucketName":"b","objectKey":"k",%s}`, typ, fields)), &out))
			})
		}
	}
}

func TestS3_SchemeDecodeLegacyRetainsType(t *testing.T) {
	for _, name := range []string{"S3", "s3"} {
		for _, fields := range []string{`"bucket":"b","key":"k"`, `"bucketName":"b","objectKey":"k"`} {
			t.Run(name+"/"+fields, func(t *testing.T) {
				r := require.New(t)
				typ := runtime.NewUnversionedType(name)
				obj, err := accessspec.Scheme.NewObject(typ)
				r.NoError(err)
				data := []byte(fmt.Sprintf(`{"type":%q,%s}`, name, fields))
				r.NoError(accessspec.Scheme.Decode(bytes.NewReader(data), obj))
				r.Equal(typ, obj.GetType())
				out, err := accessspec.ConvertToV2(obj)
				r.NoError(err)
				r.Equal(accessspec.V2VersionedType, out.Type)
				r.Equal(typ, obj.GetType())
				wire, err := json.Marshal(out)
				r.NoError(err)
				r.JSONEq(`{"type":"S3/v2","bucketName":"b","objectKey":"k"}`, string(wire))
			})
		}
	}
}

func TestConvertToV2_RejectsMismatchedTypedType(t *testing.T) {
	for _, tt := range []struct {
		name  string
		input runtime.Typed
	}{
		{"v1 with v2 type", &v1.S3{Type: runtime.NewVersionedType("s3", "v2")}},
		{"v1 with unversioned type", &v1.S3{Type: runtime.NewUnversionedType("s3")}},
		{"v1 with unknown name", &v1.S3{Type: runtime.NewVersionedType("unknown", "v1")}},
		{"v1 with unknown version", &v1.S3{Type: runtime.NewVersionedType("S3", "v3")}},
		{"v2 with v1 type", &v2.S3{Type: runtime.NewVersionedType("s3", "v1")}},
		{"v2 with unknown name", &v2.S3{Type: runtime.NewVersionedType("unknown", "v2")}},
		{"v2 with unknown unversioned name", &v2.S3{Type: runtime.NewUnversionedType("unknown")}},
		{"v2 with unknown version", &v2.S3{Type: runtime.NewVersionedType("S3", "v3")}},
		{"v2 with missing name", &v2.S3{Type: runtime.Type{Version: "v2"}}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			r := require.New(t)
			before := tt.input.DeepCopyTyped()
			out, err := accessspec.ConvertToV2(tt.input)
			r.ErrorContains(err, "invalid type")
			r.Nil(out)
			r.Equal(before, tt.input)
		})
	}
}

func TestS3_ExplicitV1RejectsV2Fields(t *testing.T) {
	for _, name := range []string{"S3/v1", "s3/v1"} {
		for _, field := range []string{`"bucketName":"different"`, `"objectKey":"different"`, `"bucketName":"b"`, `"objectKey":"k"`, `"BUCKETNAME":null`, `"ObjectKey":""`} {
			t.Run(name+"/"+field, func(t *testing.T) {
				r := require.New(t)
				data := []byte(fmt.Sprintf(`{"type":%q,"bucket":"b","key":"k",%s}`, name, field))
				typ, err := runtime.TypeFromString(name)
				r.NoError(err)
				obj, err := accessspec.Scheme.NewObject(typ)
				r.NoError(err)
				r.Error(accessspec.Scheme.Decode(bytes.NewReader(data), obj))
				for _, input := range []runtime.Typed{&runtime.Raw{}, &runtime.Unstructured{}} {
					r.NoError(json.Unmarshal(data, input))
					out, err := accessspec.ConvertToV2(input)
					r.ErrorContains(err, "not supported by S3/v1")
					r.Nil(out)
				}
			})
		}
	}
}

func TestS3_NewObjectDecodeRetainsType(t *testing.T) {
	for _, name := range []string{"S3/v2", "s3/v2", "S3", "s3"} {
		t.Run(name, func(t *testing.T) {
			r := require.New(t)
			typ, err := runtime.TypeFromString(name)
			r.NoError(err)
			obj, err := accessspec.Scheme.NewObject(typ)
			r.NoError(err)
			r.NoError(json.Unmarshal([]byte(`{"bucketName":"b","objectKey":"k"}`), obj))
			r.Equal(typ, obj.GetType())
		})
	}
}
