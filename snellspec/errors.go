package snellspec

import (
	"errors"
	"fmt"
)

// Error is a stable, secret-free code. Safe to log and return to clients.
type Error struct {
	Code string
}

func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	return e.Code
}

func Err(code string) error {
	return &Error{Code: code}
}

func CodeOf(err error) string {
	if err == nil {
		return ""
	}
	var se *Error
	if errors.As(err, &se) {
		return se.Code
	}
	return ""
}

func SafeErrorf(code string, _ ...any) error {
	return Err(code)
}

func MustCode(err error) string {
	if c := CodeOf(err); c != "" {
		return c
	}
	return fmt.Sprintf("%v", err)
}
