package domain

import "errors"

// ErrorKind はエラーの種別。返すエラーは種別に With か Wrap を被せて作る
type ErrorKind struct{ name string }

var (
	ErrInvalidArgument = &ErrorKind{name: "invalid argument"}
	ErrNotFound        = &ErrorKind{name: "not found"}
	ErrInternal        = &ErrorKind{name: "internal"}
)

func (k *ErrorKind) Error() string { return k.name }

func (k *ErrorKind) With(msg string) error {
	return &Error{Kind: k, Msg: msg}
}

func (k *ErrorKind) Wrap(cause error, msg string) error {
	return &Error{Kind: k, Msg: msg, Cause: cause}
}

// WithCandidates は見つからなかったときに、打ち間違いの候補やあるものを添える
func (k *ErrorKind) WithCandidates(msg string, candidates []string) error {
	return &Error{Kind: k, Msg: msg, Candidates: candidates}
}

type Error struct {
	Kind       *ErrorKind
	Msg        string
	Cause      error
	Candidates []string
}

func (e *Error) Error() string {
	if e.Cause != nil {
		return e.Kind.name + ": " + e.Msg + ": " + e.Cause.Error()
	}
	return e.Kind.name + ": " + e.Msg
}

func (e *Error) Is(target error) bool { return target == e.Kind }

func (e *Error) Unwrap() error { return e.Cause }

// Message は利用者に見せてよい部分。internal の原因は含めない
func Message(err error) string {
	var e *Error
	if errors.As(err, &e) {
		if e.Kind == ErrInternal {
			return "internal error"
		}
		return e.Msg
	}
	return "internal error"
}

func CandidatesOf(err error) []string {
	var e *Error
	if errors.As(err, &e) {
		return e.Candidates
	}
	return nil
}
