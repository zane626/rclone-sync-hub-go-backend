package apperror

import (
	"errors"
	"net/http"
)

type Error struct {
	Status  int
	Code    string
	Message string
	Cause   error
}

func (e *Error) Error() string {
	if e.Cause != nil {
		return e.Cause.Error()
	}
	return e.Message
}

func (e *Error) Unwrap() error { return e.Cause }

func Validation(message string, cause error) error {
	return &Error{Status: http.StatusBadRequest, Code: "validation_error", Message: message, Cause: cause}
}

func Conflict(message string, cause error) error {
	return &Error{Status: http.StatusConflict, Code: "conflict", Message: message, Cause: cause}
}

func NotFound(message string, cause error) error {
	return &Error{Status: http.StatusNotFound, Code: "not_found", Message: message, Cause: cause}
}

func As(err error) (*Error, bool) {
	var appError *Error
	return appError, errors.As(err, &appError)
}
