package types

import (
	"context"
	"errors"
	"fmt"
)

// ErrorCode 是标准化的 LLM 错误码。
type ErrorCode string

const (
	ErrCodeUnknown         ErrorCode = "unknown"
	ErrCodeAuthentication  ErrorCode = "authentication"
	ErrCodeRateLimit       ErrorCode = "rate_limit"
	ErrCodeTimeout         ErrorCode = "timeout"
	ErrCodeCanceled        ErrorCode = "canceled"
	ErrCodeInvalidRequest  ErrorCode = "invalid_request"
	ErrCodeContentFilter   ErrorCode = "content_filter"
	ErrCodeTokenLimit      ErrorCode = "token_limit"
	ErrCodeProviderUnavail ErrorCode = "provider_unavailable"
	ErrCodeTool            ErrorCode = "tool"
	ErrCodeNotImplemented  ErrorCode = "not_implemented"
)

// Error 是 llmkit 的统一错误类型，支持 errors.Is 判断。
type Error struct {
	Code     ErrorCode
	Provider string
	Message  string
	Details  map[string]any
	Cause    error
}

func (e *Error) Error() string {
	if e.Provider != "" {
		return fmt.Sprintf("%s: %s: %s", e.Provider, e.Code, e.Message)
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

func (e *Error) Unwrap() error { return e.Cause }

func (e *Error) Is(target error) bool {
	if target == nil {
		return false
	}
	if t, ok := target.(*Error); ok {
		return e.Code == t.Code
	}
	switch e.Code {
	case ErrCodeCanceled:
		return target == context.Canceled
	case ErrCodeTimeout:
		return target == context.DeadlineExceeded
	}
	return false
}

// NewError 创建 Error。
func NewError(code ErrorCode, provider, message string) *Error {
	return &Error{Code: code, Provider: provider, Message: message, Details: make(map[string]any)}
}

func (e *Error) WithCause(cause error) *Error {
	e.Cause = cause
	return e
}

func (e *Error) WithDetail(key string, value any) *Error {
	if e.Details == nil {
		e.Details = make(map[string]any)
	}
	e.Details[key] = value
	return e
}

// 便捷判断函数
func IsRateLimitError(err error) bool {
	var e *Error
	return as(err, &e) && e.Code == ErrCodeRateLimit
}

func IsTimeoutError(err error) bool {
	var e *Error
	return as(err, &e) && e.Code == ErrCodeTimeout
}

func IsAuthenticationError(err error) bool {
	var e *Error
	return as(err, &e) && e.Code == ErrCodeAuthentication
}

func IsCanceledError(err error) bool {
	return err == context.Canceled || IsErrorCode(err, ErrCodeCanceled)
}

func IsErrorCode(err error, code ErrorCode) bool {
	var e *Error
	return as(err, &e) && e.Code == code
}

func as(err error, target any) bool {
	return errors.As(err, target)
}
