package domain

var (
	ErrInvalidArgument = &Error{kind: "invalid argument"}
	ErrNotFound        = &Error{kind: "not found"}
	ErrInternal        = &Error{kind: "internal"}
)

// Error は種別 (事前定義の変数) に、メッセージと原因を被せたもの。errors.Is で種別と原因の両方に当たる
type Error struct {
	kind  string
	msg   string
	cause error
	base  *Error
}

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

func (e *Error) With(msg string) error {
	return &Error{kind: e.kind, msg: msg, base: e}
}

func (e *Error) Wrap(cause error, msg string) error {
	return &Error{kind: e.kind, msg: msg, cause: cause, base: e}
}

func (e *Error) Is(target error) bool {
	t, ok := target.(*Error)
	return ok && (e == t || e.base == t)
}

func (e *Error) Unwrap() error {
	return e.cause
}
