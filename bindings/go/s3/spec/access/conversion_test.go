package access_test

import (
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
		t.Run(typ, func(t *testing.T) {
			r := require.New(t)
			legacy := typ == "S3/v1" || typ == "s3/v1"
			bucket, key := "bucketName", "objectKey"
			if legacy {
				bucket, key = "bucket", "key"
			}
			wireType, err := runtime.TypeFromString(typ)
			r.NoError(err)
			obj, err := accessspec.Scheme.NewObject(wireType)
			r.NoError(err)
			r.NoError(json.Unmarshal([]byte(fmt.Sprintf(`{%q:"b",%q:"k"}`, bucket, key)), obj))
			r.Equal(wireType, obj.GetType(), "decoding without a type must retain the registered alias")
			if legacy {
				r.IsType(&v1.S3{}, obj)
			} else {
				r.IsType(&v2.S3{}, obj)
			}
			data := []byte(fmt.Sprintf(`{"type":%q,%q:"b",%q:"k","region":"r","version":"ver","mediaType":"m","endpoint":"https://store.example","usePathStyle":true}`, typ, bucket, key))
			for _, source := range []runtime.Typed{&runtime.Raw{}, &runtime.Unstructured{}, obj} {
				t.Run(fmt.Sprintf("%T", source), func(t *testing.T) {
					r := require.New(t)
					r.NoError(json.Unmarshal(data, source))
					r.NoError(accessspec.Scheme.Convert(source, obj))
					wire, err := json.Marshal(obj)
					r.NoError(err)
					r.JSONEq(string(data), string(wire))
					before, err := json.Marshal(source)
					r.NoError(err)
					out, err := accessspec.ConvertToV2(source)
					r.NoError(err)
					expectedType := wireType
					if legacy {
						expectedType = accessspec.V2VersionedType
					}
					r.Equal(&v2.S3{Type: expectedType, BucketName: "b", ObjectKey: "k", Region: "r", Version: "ver", MediaType: "m", Endpoint: "https://store.example", UsePathStyle: true}, out)
					r.NoError(out.Validate())
					wire, err = json.Marshal(out)
					r.NoError(err)
					r.JSONEq(fmt.Sprintf(`{"type":%q,"bucketName":"b","objectKey":"k","region":"r","version":"ver","mediaType":"m","endpoint":"https://store.example","usePathStyle":true}`, expectedType.String()), string(wire))
					out.BucketName, out.ObjectKey = "changed", "changed"
					after, err := json.Marshal(source)
					r.NoError(err)
					r.Equal(string(before), string(after))
				})
			}
		})
	}
}

func TestConvertToV2_WithoutType(t *testing.T) {
	for _, source := range []runtime.Typed{&v1.S3{Bucket: "b", Key: "k"}, &v2.S3{BucketName: "b", ObjectKey: "k"}} {
		t.Run(fmt.Sprintf("%T", source), func(t *testing.T) {
			r := require.New(t)
			before := source.DeepCopyTyped()
			out, err := accessspec.ConvertToV2(source)
			r.NoError(err)
			r.Equal(&v2.S3{Type: accessspec.V2VersionedType, BucketName: "b", ObjectKey: "k"}, out)
			out.BucketName, out.ObjectKey = "changed", "changed"
			r.Equal(before, source)
		})
	}
}

func TestS3_V2AcceptsLegacyFields(t *testing.T) {
	for _, typ := range []string{"S3/v2", "s3/v2"} {
		t.Run(typ, func(t *testing.T) {
			r := require.New(t)
			data := []byte(fmt.Sprintf(`{"type":%q,"bucket":"b","key":"k"}`, typ))
			for _, input := range []runtime.Typed{&runtime.Raw{}, &runtime.Unstructured{}, &v2.S3{}} {
				r.NoError(json.Unmarshal(data, input))
				out, err := accessspec.ConvertToV2(input)
				r.NoError(err)
				r.Equal("b", out.BucketName)
				r.Equal("k", out.ObjectKey)
				r.NoError(out.Validate())
			}
		})
	}
}

func TestScheme_V2FieldAliases(t *testing.T) {
	for _, typ := range []string{"S3", "s3", "S3/v2", "s3/v2"} {
		for _, tt := range []struct {
			name   string
			fields string
			bad    bool
		}{
			{"legacy", `"bucket":"b","key":"k"`, false},
			{"modern", `"bucketName":"b","objectKey":"k"`, false},
			{"legacy case insensitive", `"BUCKET":"b","KEY":"k"`, false},
			{"modern case insensitive", `"BUCKETNAME":"b","OBJECTKEY":"k"`, false},
			{"both shapes", `"bucket":"b","key":"k","bucketName":"b","objectKey":"k"`, true},
			{"crossed fields", `"bucket":"b","objectKey":"k"`, true},
			{"reverse crossed fields", `"bucketName":"b","key":"k"`, true},
			{"empty mixed field", `"bucket":"b","key":"k","bucketName":""`, true},
			{"null mixed field", `"bucketName":"b","objectKey":"k","key":null`, true},
			{"mixed case insensitive", `"BUCKET":"b","OBJECTKEY":"k"`, true},
		} {
			for _, source := range []runtime.Typed{&runtime.Raw{}, &runtime.Unstructured{}} {
				t.Run(fmt.Sprintf("%s/%s/%T", typ, tt.name, source), func(t *testing.T) {
					r := require.New(t)
					data := []byte(fmt.Sprintf(`{"type":%q,%s,"region":"r","version":"ver","mediaType":"m","endpoint":"https://store.example","usePathStyle":true}`, typ, tt.fields))
					r.NoError(json.Unmarshal(data, source))
					before := source.DeepCopyTyped()
					obj, err := accessspec.Scheme.NewObject(source.GetType())
					r.NoError(err)
					r.IsType(&v2.S3{}, obj)
					err = accessspec.Scheme.Convert(source, obj)
					r.Equal(before, source)
					direct := &v2.S3{}
					directErr := json.Unmarshal(data, direct)
					if tt.bad {
						r.ErrorContains(err, "ambiguous S3 access spec")
						r.ErrorContains(directErr, "ambiguous S3 access spec")
						return
					}
					r.NoError(err)
					r.NoError(directErr)
					wantType, err := runtime.TypeFromString(typ)
					r.NoError(err)
					expected := &v2.S3{Type: wantType, BucketName: "b", ObjectKey: "k", Region: "r", Version: "ver", MediaType: "m", Endpoint: "https://store.example", UsePathStyle: true}
					r.Equal(expected, obj)
					r.Equal(expected, direct)
					out, err := accessspec.ConvertToV2(source)
					r.NoError(err)
					r.Equal(expected, out)
					r.NoError(out.Validate())
					wire, err := json.Marshal(obj)
					r.NoError(err)
					r.JSONEq(fmt.Sprintf(`{"type":%q,"bucketName":"b","objectKey":"k","region":"r","version":"ver","mediaType":"m","endpoint":"https://store.example","usePathStyle":true}`, typ), string(wire))
				})
			}
		}
	}
}

func TestConvertToV2_InvalidSource(t *testing.T) {
	for _, tt := range []struct {
		name    string
		input   runtime.Typed
		wantErr string
	}{
		{"nil", nil, "unsupported"},
		{"nil v1", (*v1.S3)(nil), "must not be nil"},
		{"nil v2", (*v2.S3)(nil), "must not be nil"},
		{"nil raw", (*runtime.Raw)(nil), "must not be nil"},
		{"nil unstructured", (*runtime.Unstructured)(nil), "must not be nil"},
		{"unknown wire type", &runtime.Raw{Type: runtime.NewVersionedType("unknown", "v1")}, "resolve"},
		{"malformed JSON", &runtime.Raw{Type: accessspec.V1VersionedType, Data: []byte(`{`)}, "decode"},
		{"unsupported representation", runtime.Identity{"type": "S3/v2"}, "unsupported"},
		{"v1 with v2 type", &v1.S3{Type: runtime.NewVersionedType("s3", "v2")}, "invalid type"},
		{"v1 with unversioned type", &v1.S3{Type: runtime.NewUnversionedType("s3")}, "invalid type"},
		{"v1 with unknown name", &v1.S3{Type: runtime.NewVersionedType("unknown", "v1")}, "invalid type"},
		{"v1 with unknown version", &v1.S3{Type: runtime.NewVersionedType("S3", "v3")}, "invalid type"},
		{"v2 with v1 type", &v2.S3{Type: runtime.NewVersionedType("s3", "v1")}, "invalid type"},
		{"v2 with unknown name", &v2.S3{Type: runtime.NewVersionedType("unknown", "v2")}, "invalid type"},
		{"v2 with unknown unversioned name", &v2.S3{Type: runtime.NewUnversionedType("unknown")}, "invalid type"},
		{"v2 with unknown version", &v2.S3{Type: runtime.NewVersionedType("S3", "v3")}, "invalid type"},
		{"v2 with missing name", &v2.S3{Type: runtime.Type{Version: "v2"}}, "invalid type"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			r := require.New(t)
			var before runtime.Typed
			if tt.input != nil {
				before = tt.input.DeepCopyTyped()
			}
			out, err := accessspec.ConvertToV2(tt.input)
			r.ErrorContains(err, tt.wantErr)
			r.Nil(out)
			if before != nil {
				r.Equal(before, tt.input)
			}
		})
	}
}
