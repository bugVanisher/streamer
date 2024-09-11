package errs

import (
	"github.com/pkg/errors"
)

const (
	CodeDuplicateStream = 1001
	CodeStreamNotExist  = 1002
	CodeUnknown         = 9999
	CodeConnectURL      = 2001
)

var (
	ErrDuplicateStream = New(CodeDuplicateStream, "duplicate stream")
	ErrStreamNotExist  = New(CodeStreamNotExist, "stream not exist")
	ErrConnectURL      = New(CodeConnectURL, "connect url error")
)

const (
	Success = "success"
)

type Error struct {
	Code int32
	Msg  string
}

func (e *Error) Error() string {
	return e.Msg
}

func New(code int32, msg string) error {
	return &Error{
		Code: code,
		Msg:  msg,
	}
}

func Code(e error) int32 {
	if e == nil {
		return 0
	}
	err, ok := e.(*Error)
	if !ok {
		return CodeUnknown
	}

	if err == (*Error)(nil) {
		return 0
	}
	return err.Code
}

func Msg(e error) string {
	if e == nil {
		return Success
	}
	err, ok := e.(*Error)
	if !ok {
		return "unknown error: " + e.Error()
	}

	if err == (*Error)(nil) {
		return Success
	}

	return err.Msg
}

func Wrapf(err error, format string, args ...interface{}) error {
	return errors.Wrapf(err, format, args...)
}
