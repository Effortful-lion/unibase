package httpx

import "errors"

// ClientError 包装 HTTP 客户端调用过程中的错误。
// 包含 HTTP 状态码、响应体和原始错误，调用方可通过 IsXXX 系列函数判断错误类型。
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

// StatusCode 返回 HTTP 响应状态码。
func (e *ClientError) StatusCode() int {
	return e.statusCode
}

// Body 返回响应体原始字节。
func (e *ClientError) Body() []byte {
	return e.body
}

// IsTimeout 判断是否为超时错误。
func IsTimeout(err error) bool {
	var netErr interface{ Timeout() bool }
	return errors.As(err, &netErr) && netErr.Timeout()
}

// IsCanceled 判断是否为请求被取消。
func IsCanceled(err error) bool {
	return errors.Is(err, contextCanceled) || errors.Is(err, contextDeadline)
}

// IsClientError 判断是否为 HTTP 客户端错误（4xx）。
func IsClientError(err error) bool {
	var clientErr *ClientError
	if !errors.As(err, &clientErr) {
		return false
	}
	return clientErr.statusCode >= 400 && clientErr.statusCode < 500
}

// IsServerError 判断是否为 HTTP 服务端错误（5xx）。
func IsServerError(err error) bool {
	var clientErr *ClientError
	if !errors.As(err, &clientErr) {
		return false
	}
	return clientErr.statusCode >= 500
}

// IsStatus 判断错误是否对应指定状态码。
func IsStatus(err error, statusCode int) bool {
	var clientErr *ClientError
	if !errors.As(err, &clientErr) {
		return false
	}
	return clientErr.statusCode == statusCode
}

// newClientError 创建 ClientError。
func newClientError(statusCode int, body []byte, err error) *ClientError {
	return &ClientError{
		statusCode: statusCode,
		body:       body,
		err:        err,
	}
}

// 用于 errors.Is 匹配的 sentinel error
var (
	contextCanceled = errors.New("context canceled")
	contextDeadline = errors.New("context deadline exceeded")
)
