package oci

import (
	"testing"

	"github.com/stretchr/testify/require"
	"oras.land/oras-go/v2/errdef"
)

func TestVersionToOCITag(t *testing.T) {
	tests := []struct {
		name    string
		version string
		wantTag string
		wantErr bool
	}{
		{
			name:    "semver build metadata is substituted",
			version: "1.0.0+build.5",
			wantTag: "1.0.0.build-build.5",
			wantErr: false,
		},
		{
			name:    "plain semver passes through",
			version: "1.2.3",
			wantTag: "1.2.3",
			wantErr: false,
		},
		{
			name:    "calver full is a valid tag",
			version: "2024.03.15",
			wantTag: "2024.03.15",
			wantErr: false,
		},
		{
			name:    "ubuntu calver is a valid tag",
			version: "22.04",
			wantTag: "22.04",
			wantErr: false,
		},
		{
			name:    "build number is a valid tag",
			version: "1837",
			wantTag: "1837",
			wantErr: false,
		},
		{
			name:    "epoch colon is not a valid tag",
			version: "1:2.3",
			wantErr: true,
		},
		{
			name:    "leading dot is not a valid tag",
			version: ".1.2.3",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := require.New(t)
			tag, err := VersionToOCITag(t.Context(), tt.version)
			if tt.wantErr {
				r.Error(err)
				r.ErrorIs(err, errdef.ErrInvalidReference)
				return
			}
			r.NoError(err)
			r.Equal(tt.wantTag, tag)
		})
	}
}

func TestVersionToOCITag_LengthLimit(t *testing.T) {
	r := require.New(t)
	long := make([]byte, 200)
	for i := range long {
		long[i] = 'a'
	}
	_, err := VersionToOCITag(t.Context(), string(long))
	r.Error(err)
	r.ErrorIs(err, errdef.ErrInvalidReference)
	r.Contains(err.Error(), "exceeds the 128 character limit")
}
