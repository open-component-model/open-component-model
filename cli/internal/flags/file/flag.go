package file

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"strings"

	"github.com/spf13/pflag"
)

// Type is the type name for the path flag.
// It represents a flag that holds a file path.
const Type = "path"

// Flag defines a path flag that checks if the value is an existing file.
type Flag struct {
	path *string
	fs.FileInfo
}

func (f *Flag) String() string {
	return *f.path
}

func (f *Flag) Exists() bool {
	return f.FileInfo != nil
}

func (f *Flag) Open() (io.ReadCloser, error) {
	if f.Exists() {
		return os.Open(*f.path)
	}
	return nil, fmt.Errorf("file %q does not exist", *f.path)
}

func (f *Flag) Set(s string) error {
	if f.path == nil {
		f.path = new(string)
	}
	*f.path = s
	info, err := os.Stat(s)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("unable to stat path %q: %w", *f.path, err)
	} else {
		f.FileInfo = info
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
	flag := &Flag{}
	_ = flag.Set(actual)
	f.VarP(flag, name, shorthand, usage)
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
