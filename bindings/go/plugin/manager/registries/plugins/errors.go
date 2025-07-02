package plugins

import "net/http"

type Error struct {
	Err    error `json:"error"`
	Status int   `json:"status"`
}

// NewError creates a new Error instance with the provided error and HTTP status code.
func NewError(err error, status int) *Error {
	return &Error{Err: err, Status: status}
}

func (e *Error) Write(w http.ResponseWriter) {
	http.Error(w, e.Err.Error(), e.Status)
}
