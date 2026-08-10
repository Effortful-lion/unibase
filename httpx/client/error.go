package client

import (
	"context"
	"errors"
)

// ClientError 包装 HTTP 客户端调用过程中的错误。
// 包含 HTTP 状态码、响应体和原始错误。
type ClientError struct {
	statusCode int
	body       []byte
	err        error
}

func (e *ClientError) Error() string {
	if e.body != nil {
		return e.err.Error() + ": " + string(e.body)
	}
	return e.err.Error()
}

func (e *ClientError) Unwrap() error {
	return e.err
}

func (e *ClientError) StatusCode() int { return e.statusCode }
func (e *ClientError) Body() []byte    { return e.body }

// IsTimeout 判断是否为超时错误。
func IsTimeout(err error) bool {
	var netErr interface{ Timeout() bool }
	return errors.As(err, &netErr) && netErr.Timeout()
}

// IsCanceled 判断是否为请求被取消。
func IsCanceled(err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}

// IsClientError 判断是否为 HTTP 客户端错误（4xx）。
func IsClientError(err error) bool {
	var e *ClientError
	if !errors.As(err, &e) {
		return false
	}
	return e.statusCode >= 400 && e.statusCode < 500
}

// IsServerError 判断是否为 HTTP 服务端错误（5xx）。
func IsServerError(err error) bool {
	var e *ClientError
	if !errors.As(err, &e) {
		return false
	}
	return e.statusCode >= 500
}

// IsStatus 判断错误是否对应指定状态码。
func IsStatus(err error, statusCode int) bool {
	var e *ClientError
	if !errors.As(err, &e) {
		return false
	}
	return e.statusCode == statusCode
}

func newClientError(statusCode int, body []byte, err error) *ClientError {
	return &ClientError{statusCode: statusCode, body: body, err: err}
}
