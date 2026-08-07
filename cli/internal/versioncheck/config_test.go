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
	if cfg.Policy != PolicyAuto {
		t.Errorf("expected Policy = %q, got %q", PolicyAuto, cfg.Policy)
	}
}

func TestLookupConfig_Empty(t *testing.T) {
	cfg, err := LookupConfig(&generic.Config{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Policy != PolicyAuto {
		t.Errorf("expected Policy = %q, got %q", PolicyAuto, cfg.Policy)
	}
}

func TestLookupConfig_PolicyDisable(t *testing.T) {
	raw := &runtime.Raw{
		Type: runtime.NewVersionedType(ConfigType, ConfigVersion),
		Data: []byte(`{"type":"versioncheck.cli.config.ocm.software/v1alpha1","policy":"disable"}`),
	}

	cfg, err := LookupConfig(&generic.Config{
		Configurations: []*runtime.Raw{raw},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Policy != PolicyDisable {
		t.Errorf("expected Policy = %q, got %q", PolicyDisable, cfg.Policy)
	}
}

func TestLookupConfig_PolicyAuto(t *testing.T) {
	raw := &runtime.Raw{
		Type: runtime.NewVersionedType(ConfigType, ConfigVersion),
		Data: []byte(`{"type":"versioncheck.cli.config.ocm.software/v1alpha1","policy":"auto"}`),
	}

	cfg, err := LookupConfig(&generic.Config{
		Configurations: []*runtime.Raw{raw},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Policy != PolicyAuto {
		t.Errorf("expected Policy = %q, got %q", PolicyAuto, cfg.Policy)
	}
}

func TestLookupConfig_InvalidPolicy(t *testing.T) {
	raw := &runtime.Raw{
		Type: runtime.NewVersionedType(ConfigType, ConfigVersion),
		Data: []byte(`{"type":"versioncheck.cli.config.ocm.software/v1alpha1","policy":"invalid"}`),
	}

	_, err := LookupConfig(&generic.Config{
		Configurations: []*runtime.Raw{raw},
	})
	if err == nil {
		t.Fatal("expected error for invalid policy value")
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
		Type:   runtime.NewVersionedType(ConfigType, ConfigVersion),
		Policy: PolicyDisable,
	}
	cpy := c.DeepCopy()
	if cpy == c {
		t.Error("DeepCopy should return a new pointer")
	}
	if cpy.Policy != c.Policy {
		t.Error("DeepCopy should preserve Policy field")
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
		Type:   runtime.NewVersionedType(ConfigType, ConfigVersion),
		Policy: PolicyDisable,
	}
	typed := c.DeepCopyTyped()
	if typed == nil {
		t.Error("DeepCopyTyped should not return nil")
	}
	if _, ok := typed.(*Config); !ok {
		t.Error("DeepCopyTyped should return *Config")
	}
}
