package runtime

import (
	"fmt"
)

// ValidationError represents a validation error with a field path and message
type ValidationError struct {
	Field   string
	Message string
}

func (e *ValidationError) Error() string {
	if e.Field != "" {
		return fmt.Sprintf("%s: %s", e.Field, e.Message)
	}
	return e.Message
}

// newValidationError creates a new validation error
func newValidationError(field, message string) error {
	return &ValidationError{
		Field:   field,
		Message: message,
	}
}

// validateRequiredString checks if a string field is set
func validateRequiredString(field, value string) error {
	if value == "" {
		return newValidationError(field, "must be set")
	}
	return nil
}

// Validate checks for duplicate identities and validates AccessOrInput fields in the component constructor.
func (cc *ComponentConstructor) Validate() error {
	if cc == nil {
		return nil
	}
	for ci, c := range cc.Components {
		if err := c.Validate(); err != nil {
			return fmt.Errorf("invalid component %d': %w", ci, err)
		}
	}
	return nil
}

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
	return validateRequiredString("name", p.Name)
}

// Validate validates the resource.
func (r *Resource) Validate() error {
	if err := r.ElementMeta.Validate(); err != nil {
		return fmt.Errorf("element meta: %w", err)
	}

	if err := validateRequiredString("type", r.Type); err != nil {
		return err
	}

	if err := validateRequiredString("relation", string(r.Relation)); err != nil {
		return err
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

	if err := validateRequiredString("type", s.Type); err != nil {
		return err
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

	return validateRequiredString("component", r.Component)
}

// Validate validates the element meta.
func (m *ElementMeta) Validate() error {
	if err := m.ObjectMeta.Validate(); err != nil {
		return err
	}

	if m.ExtraIdentity != nil {
		if _, ok := m.ExtraIdentity[IdentityAttributeName]; ok {
			return newValidationError("extra identity", "must not contain name attribute")
		}
	}

	return nil
}

// Validate validates the object meta.
func (m *ObjectMeta) Validate() error {
	if err := validateRequiredString("name", m.Name); err != nil {
		return err
	}

	return validateRequiredString("version", m.Version)
}

// Validate validates the component meta.
func (m *ComponentMeta) Validate() error {
	return m.ObjectMeta.Validate()
}
