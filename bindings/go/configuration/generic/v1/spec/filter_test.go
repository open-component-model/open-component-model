package spec

import (
	"reflect"
	"testing"

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
