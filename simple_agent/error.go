package simple_agent

import (
	"errors"
	"fmt"
)

// ErrorCode Agent 错误码。
type ErrorCode string

const (
	ErrCodeTool       ErrorCode = "tool"
	ErrCodeMaxSteps   ErrorCode = "max_steps"
	ErrCodeInvalidArg ErrorCode = "invalid_argument"
	ErrCodeProvider   ErrorCode = "provider"
)

// Error 是 Agent 的统一错误类型。
type Error struct {
	Message string
	Code    ErrorCode
	Cause   error
}

func (e *Error) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("[%s] %s: %v", e.Code, e.Message, e.Cause)
	}
	return fmt.Sprintf("[%s] %s", e.Code, e.Message)
}

func (e *Error) Unwrap() error { return e.Cause }

// Is 支持 errors.Is 判断。
func (e *Error) Is(target error) bool {
	if t, ok := target.(*Error); ok {
		return e.Code == t.Code
	}
	return errors.Is(e.Cause, target)
}
