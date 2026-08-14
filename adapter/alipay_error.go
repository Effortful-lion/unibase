package adapter

import "errors"

// AlipayError 支付宝接口返回的业务错误。
type AlipayError struct {
	// Code 错误码。
	Code string

	// Msg 错误信息。
	Msg string

	// SubMsg 子错误信息。
	SubMsg string
}

func (e *AlipayError) Error() string {
	return "alipay: " + e.Code + ": " + e.Msg + ": " + e.SubMsg
}

// IsAlipayError 检查 err 是否为 *AlipayError。
func IsAlipayError(err error) bool {
	var apErr *AlipayError
	return errors.As(err, &apErr)
}
