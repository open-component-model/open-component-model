package versioncheck

import (
	"testing"

	generic "ocm.software/open-component-model/bindings/go/configuration/generic/v1/spec"
	"ocm.software/open-component-model/bindings/go/runtime"
)

func TestLookupConfig_Nil(t *testing.T) {
	cfg, err := LookupConfig(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Disabled {
		t.Error("expected Disabled = false for nil config")
	}
}

func TestLookupConfig_Empty(t *testing.T) {
	cfg, err := LookupConfig(&generic.Config{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Disabled {
		t.Error("expected Disabled = false for empty config")
	}
}

func TestLookupConfig_Disabled(t *testing.T) {
	raw := &runtime.Raw{
		Type: runtime.NewVersionedType(ConfigType, ConfigVersion),
		Data: []byte(`{"type":"versioncheck.config.ocm.software/v1alpha1","disabled":true}`),
	}

	cfg, err := LookupConfig(&generic.Config{
		Configurations: []*runtime.Raw{raw},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !cfg.Disabled {
		t.Error("expected Disabled = true")
	}
}

func TestLookupConfig_NotDisabled(t *testing.T) {
	raw := &runtime.Raw{
		Type: runtime.NewVersionedType(ConfigType, ConfigVersion),
		Data: []byte(`{"type":"versioncheck.config.ocm.software/v1alpha1","disabled":false}`),
	}

	cfg, err := LookupConfig(&generic.Config{
		Configurations: []*runtime.Raw{raw},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Disabled {
		t.Error("expected Disabled = false")
	}
}

func TestConfig_GetSetType(t *testing.T) {
	c := &Config{}
	typ := runtime.NewVersionedType(ConfigType, ConfigVersion)
	c.SetType(typ)
	if got := c.GetType(); got != typ {
		t.Errorf("GetType() = %v, want %v", got, typ)
	}
}

func TestConfig_DeepCopy(t *testing.T) {
	c := &Config{
		Type:     runtime.NewVersionedType(ConfigType, ConfigVersion),
		Disabled: true,
	}
	cpy := c.DeepCopy()
	if cpy == c {
		t.Error("DeepCopy should return a new pointer")
	}
	if cpy.Disabled != c.Disabled {
		t.Error("DeepCopy should preserve Disabled field")
	}
}

func TestConfig_DeepCopy_Nil(t *testing.T) {
	var c *Config
	if c.DeepCopy() != nil {
		t.Error("DeepCopy of nil should return nil")
	}
}

func TestConfig_DeepCopyTyped(t *testing.T) {
	c := &Config{
		Type:     runtime.NewVersionedType(ConfigType, ConfigVersion),
		Disabled: true,
	}
	typed := c.DeepCopyTyped()
	if typed == nil {
		t.Error("DeepCopyTyped should not return nil")
	}
	if _, ok := typed.(*Config); !ok {
		t.Error("DeepCopyTyped should return *Config")
	}
}
