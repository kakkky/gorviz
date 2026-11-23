package domain

import "fmt"

var (
	ErrInvalidArgument = &Error{kind: "invalid argument"}
	ErrNotFound        = &Error{kind: "not found"}
	ErrInternal        = &Error{kind: "internal"}
)

type Error struct {
	kind  string
	msg   string
	cause error
}

func (e *Error) Error() string {
	if e.cause != nil {
		return fmt.Sprintf("%s: %s: %v", e.kind, e.msg, e.cause)
	}
	return fmt.Sprintf("%s: %s", e.kind, e.msg)
}

func (e *Error) With(msg string) *Error { return &Error{kind: e.kind, msg: msg} }

func (e *Error) Wrap(cause error, msg string) *Error {
	return &Error{kind: e.kind, msg: msg, cause: cause}
}

func (e *Error) Message() string { return e.msg }

func (e *Error) Unwrap() error { return e.cause }

func (e *Error) Is(target error) bool {
	t, ok := target.(*Error)
	return ok && t.kind == e.kind
}
