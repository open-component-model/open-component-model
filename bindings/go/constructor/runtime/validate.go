package runtime

import (
	"fmt"
)

// Validate validates the component.
func (c *Component) Validate() error {
	if err := c.ComponentMeta.Validate(); err != nil {
		return fmt.Errorf("component meta: %w", err)
	}

	if err := c.Provider.Validate(); err != nil {
		return fmt.Errorf("provider: %w", err)
	}

	for i, res := range c.Resources {
		if err := res.Validate(); err != nil {
			return fmt.Errorf("resource[%d]: %w", i, err)
		}
	}

	for i, src := range c.Sources {
		if err := src.Validate(); err != nil {
			return fmt.Errorf("source[%d]: %w", i, err)
		}
	}

	for i, ref := range c.References {
		if err := ref.Validate(); err != nil {
			return fmt.Errorf("reference[%d]: %w", i, err)
		}
	}

	return nil
}

// Validate validates the provider.
func (p *Provider) Validate() error {
	if p.Name == "" {
		return fmt.Errorf("name must be set")
	}
	return nil
}

// Validate validates the resource.
func (r *Resource) Validate() error {
	if err := r.ElementMeta.Validate(); err != nil {
		return fmt.Errorf("element meta: %w", err)
	}

	if r.Type == "" {
		return fmt.Errorf("type must be set")
	}

	if r.Relation == "" {
		return fmt.Errorf("relation must be set")
	}

	if err := r.AccessOrInput.Validate(); err != nil {
		return fmt.Errorf("access or input: %w", err)
	}

	return nil
}

// Validate validates the source.
func (s *Source) Validate() error {
	if err := s.ElementMeta.Validate(); err != nil {
		return fmt.Errorf("element meta: %w", err)
	}

	if s.Type == "" {
		return fmt.Errorf("type must be set")
	}

	if err := s.AccessOrInput.Validate(); err != nil {
		return fmt.Errorf("access or input: %w", err)
	}

	return nil
}

// Validate validates the reference.
func (r *Reference) Validate() error {
	if err := r.ElementMeta.Validate(); err != nil {
		return fmt.Errorf("element meta: %w", err)
	}

	if r.Component == "" {
		return fmt.Errorf("component must be set")
	}

	return nil
}

// Validate validates the element meta.
func (m *ElementMeta) Validate() error {
	if err := m.ObjectMeta.Validate(); err != nil {
		return err
	}

	if m.ExtraIdentity != nil {
		if _, ok := m.ExtraIdentity[IdentityAttributeName]; ok {
			return fmt.Errorf("extra identity must not contain name attribute")
		}
	}

	return nil
}

// Validate validates the object meta.
func (m *ObjectMeta) Validate() error {
	if m.Name == "" {
		return fmt.Errorf("name must be set")
	}

	if m.Version == "" {
		return fmt.Errorf("version must be set")
	}

	return nil
}

// Validate validates the component meta.
func (m *ComponentMeta) Validate() error {
	return m.ObjectMeta.Validate()
}
