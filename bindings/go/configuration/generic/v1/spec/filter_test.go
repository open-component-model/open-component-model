package spec

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"
	"ocm.software/open-component-model/bindings/go/runtime"
)

func TestFilter(t *testing.T) {
	type args struct {
		config  *Config
		options *FilterOptions
	}
	tests := []struct {
		name    string
		args    args
		want    *Config
		wantErr bool
	}{
		{
			name: "passing filter with empty options should result in empty config",
			args: args{
				config: &Config{
					Type: runtime.Type{
						Version: "v1",
						Name:    "test",
					},
					Configurations: []*runtime.Raw{
						{
							Type: runtime.Type{
								Version: "v1",
								Name:    "test2",
							},
						},
						{
							Type: runtime.Type{
								Version: "v1",
								Name:    "test3",
							},
						},
					},
				},
				options: &FilterOptions{},
			},
			want: &Config{
				Type: runtime.Type{
					Version: "v1",
					Name:    "test",
				},
			},
		},
		{
			name: "passing filter with option should filter",
			args: args{
				config: &Config{
					Type: runtime.Type{
						Version: "v1",
						Name:    "test",
					},
					Configurations: []*runtime.Raw{
						{
							Type: runtime.Type{
								Version: "v1",
								Name:    "test2",
							},
						},
						{
							Type: runtime.Type{
								Version: "v1",
								Name:    "test3",
							},
						},
					},
				},
				options: &FilterOptions{
					ConfigTypes: []runtime.Type{
						{
							Version: "v1",
							Name:    "test2",
						},
					},
				},
			},
			want: &Config{
				Type: runtime.Type{
					Version: "v1",
					Name:    "test",
				},
				Configurations: []*runtime.Raw{
					{
						Type: runtime.Type{
							Version: "v1",
							Name:    "test2",
						},
					},
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Filter(tt.args.config, tt.args.options)
			if (err != nil) != tt.wantErr {
				t.Errorf("Filter() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Filter() got = %v, want %v", got, tt.want)
			}
		})
	}
}

type testConfig struct {
	Type  runtime.Type `json:"type"`
	Value string       `json:"value"`
}

func (c *testConfig) GetType() runtime.Type {
	return c.Type
}

func (c *testConfig) SetType(typ runtime.Type) {
	c.Type = typ
}

func (c *testConfig) DeepCopyTyped() runtime.Typed {
	return &testConfig{
		Type:  c.Type,
		Value: c.Value,
	}
}

func TestFilterForType(t *testing.T) {
	r := require.New(t)

	schemeWithTestConfig := runtime.NewScheme()
	r.NoError(schemeWithTestConfig.RegisterWithAlias(&testConfig{},
		runtime.NewUnversionedType("test"),
		runtime.NewVersionedType("test", "v1")))

	type args struct {
		scheme *runtime.Scheme
		config *Config
	}
	tests := []struct {
		name    string
		args    args
		want    []*testConfig
		wantErr bool
	}{
		{
			name: "passing config with registered type should succeed",
			args: args{
				scheme: schemeWithTestConfig,
				config: &Config{
					Type: runtime.Type{
						Version: "v1",
						Name:    "test",
					},
					Configurations: []*runtime.Raw{
						{
							Type: runtime.Type{
								Version: "v1",
								Name:    "test",
							},
							Data: []byte(`{"type": "test/v1", "value": "value1"}`),
						},
					},
				},
			},
			want: []*testConfig{
				{
					Type: runtime.Type{
						Version: "v1",
						Name:    "test",
					},
					Value: "value1",
				},
			},
			wantErr: false,
		},
		{
			name: "passing config with unregistered type should return error",
			args: args{
				scheme: runtime.NewScheme(),
				config: &Config{
					Type: runtime.Type{
						Version: "v1",
						Name:    "test",
					},
					Configurations: []*runtime.Raw{
						{
							Type: runtime.Type{
								Version: "v1",
								Name:    "test",
							},
						},
					},
				},
			},
			want:    nil,
			wantErr: true,
		},
		{
			name: "passing config with empty configurations should return empty slice",
			args: args{
				scheme: schemeWithTestConfig,
				config: &Config{
					Type: runtime.Type{
						Version: "v1",
						Name:    "test",
					},
					Configurations: []*runtime.Raw{},
				},
			},
			want:    []*testConfig{},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := FilterForType[*testConfig](tt.args.scheme, tt.args.config)
			if (err != nil) != tt.wantErr {
				t.Errorf("FilterForType() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("FilterForType() got = %v, want %v", got, tt.want)
			}
		})
	}
}
