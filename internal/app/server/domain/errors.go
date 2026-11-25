package domain

// Error はエラーの種別。With・Wrap で中身を被せたものは、errors.Is で元の種別と一致する
type Error struct {
	kind  string
	msg   string
	cause error
	base  *Error
}

var (
	ErrInvalidArgument = &Error{kind: "invalid argument"}
	ErrNotFound        = &Error{kind: "not found"}
	ErrInternal        = &Error{kind: "internal"}
)

func (e *Error) Error() string {
	s := e.kind
	if e.msg != "" {
		s += ": " + e.msg
	}
	if e.cause != nil {
		s += ": " + e.cause.Error()
	}
	return s
}

func (e *Error) Unwrap() error { return e.cause }

func (e *Error) Is(target error) bool {
	t, ok := target.(*Error)
	if !ok {
		return false
	}
	return e == t || e.base == t
}

func (e *Error) With(msg string) error {
	return &Error{kind: e.kind, msg: msg, base: e}
}

func (e *Error) Wrap(cause error, msg string) error {
	return &Error{kind: e.kind, msg: msg, cause: cause, base: e}
}
