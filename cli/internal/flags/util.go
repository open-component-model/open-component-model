package flags

import (
	"fmt"

	"github.com/spf13/pflag"
)

func Get[T any](f *pflag.FlagSet, name string, ftype string, convFunc func(sval string) (T, error)) (T, error) {
	flag := f.Lookup(name)
	if flag == nil {
		err := fmt.Errorf("flag accessed but not defined: %s", name)
		return *new(T), err
	}

	if flag.Value.Type() != ftype {
		err := fmt.Errorf("trying to Get %s value of flag of type %s", ftype, flag.Value.Type())
		return *new(T), err
	}

	sval := flag.Value.String()
	result, err := convFunc(sval)
	if err != nil {
		return *new(T), err
	}
	return result, nil
}
