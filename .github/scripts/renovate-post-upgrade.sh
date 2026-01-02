#! /bin/sh

command -v go >/dev/null 2>&1 || exit 0; find . -name "go.mod" -type f -path \'*/integration/*\' -exec dirname {} \\; | while read dir; do echo "Running explicit go mod tidy for integration test in $dir"; cd "$dir" && go mod tidy && cd - > /dev/null; done
