package file

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/pflag"
)

const Type = "path"

// Flag defines a path flag that checks if the value is an existing file.
type Flag struct {
	path      *string
	isRegular bool
	isDir     bool
	exists    bool
}

func (f *Flag) String() string {
	return *f.path
}

func (f *Flag) IsRegularFile() bool {
	return f.isRegular
}

func (f *Flag) IsDir() bool {
	return f.isDir
}

func (f *Flag) Exists() bool {
	return f.exists
}

func (f *Flag) Open() (io.ReadCloser, error) {
	if f.exists {
		return os.Open(*f.path)
	}
	return nil, fmt.Errorf("file %q does not exist", *f.path)
}

func (f *Flag) Set(s string) error {
	*f.path = s
	info, err := os.Stat(s)
	if err != nil {
		if os.IsNotExist(err) {
			f.exists = false
		} else {
			return fmt.Errorf("unable to stat path %q: %w", f.path, err)
		}
	} else {
		if f.isRegular = info.Mode().IsRegular(); !f.isRegular {
			return fmt.Errorf("path %q is not a regular file", f.path)
		}
		if f.isDir = info.IsDir(); f.isDir {
			return fmt.Errorf("path %q is a directory", f.path)
		}
	}
	return nil
}

func (f *Flag) Type() string {
	return Type
}

func Var(f *pflag.FlagSet, name string, value string, usage string) {
	actual := strings.Clone(value)
	flag := Flag{path: &actual}
	f.Var(&flag, name, usage)
}

func VarP(f *pflag.FlagSet, name, shorthand string, value string, usage string) {
	actual := strings.Clone(value)
	flag := Flag{path: &actual}
	f.VarP(&flag, name, shorthand, usage)
}

func Get(f *pflag.FlagSet, name string) (*Flag, error) {
	flag := f.Lookup(name)
	if flag == nil {
		return nil, fmt.Errorf("flag accessed but not defined: %s", name)
	}

	if flag.Value.Type() != Type {
		return nil, fmt.Errorf("trying to get %s value of flag of type %s", Type, flag.Value.Type())
	}

	val, ok := flag.Value.(*Flag)
	if !ok {
		return nil, fmt.Errorf("flag %s is not of type %s", name, Type)
	}
	return val, nil
}
