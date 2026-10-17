package location

import "errors"

func mkfifo(string) error {
	return errors.New("named pipes are not supported on windows")
}
