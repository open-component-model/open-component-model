package enum

import (
	"fmt"
	"slices"

	"github.com/spf13/pflag"
)

const Type = "enum"

// Flag is a flag.Value implementation for parsing flags with a one-of-a-set value
// from the provided options. The first option is used as the default value.
type Flag struct {
	target  *string
	options []string
}

func (f *Flag) Type() string {
	return Type
}

// New returns a flag.Value implementation for parsing flags with a one-of-a-set value
// from the provided options. The first option is used as the default value.
func New(options ...string) *Flag {
	if len(options) == 0 {
		panic("options must not be empty")
	}
	return &Flag{target: &options[0], options: options}
}

func (f *Flag) String() string {
	return *f.target
}

func (f *Flag) Set(value string) error {
	if !slices.Contains(f.options, value) {
		return fmt.Errorf("expected one of %q", f.options)
	}

	*f.target = value

	return nil
}

func Get(f *pflag.FlagSet, name string) (string, error) {
	return get[string](f, name, Type, func(sval string) (string, error) {
		return sval, nil
	})
}

func Var(f *pflag.FlagSet, name string, options []string, usage string) {
	flag := New(options...)
	cloned := slices.Clone(options)
	slices.Sort(cloned)
	f.Var(flag, name, fmt.Sprintf("%s\n(must be one of %v)", usage, cloned))
}

func get[T any](f *pflag.FlagSet, name string, ftype string, convFunc func(sval string) (T, error)) (T, error) {
	flag := f.Lookup(name)
	if flag == nil {
		err := fmt.Errorf("flag accessed but not defined: %s", name)
		return *new(T), err
	}

	if flag.Value.Type() != ftype {
		err := fmt.Errorf("trying to get %s value of flag of type %s", ftype, flag.Value.Type())
		return *new(T), err
	}

	sval := flag.Value.String()
	result, err := convFunc(sval)
	if err != nil {
		return *new(T), err
	}
	return result, nil
}
