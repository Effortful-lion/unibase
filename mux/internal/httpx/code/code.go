package code

// BusinessCode 业务状态码。
//
// 设计约定：
//   - 空字符串 "" 表示成功
//   - 非空字符串表示业务错误，建议格式为 "1" + 3位数字（如 "10400"）
//   - 底层为 string，JSON marshal/unmarshal 行为与 string 一致
type BusinessCode string

// ── 预定义常量 ──────────────────────────────────────────

const (
	// OK 表示成功。
	OK BusinessCode = "10200"

	// BadRequest 表示参数错误（HTTP 400）。
	BadRequest BusinessCode = "10400"

	// Unauthorized 表示未授权（HTTP 401）。
	Unauthorized BusinessCode = "10401"

	// Forbidden 表示禁止访问（HTTP 403）。
	Forbidden BusinessCode = "10403"

	// NotFound 表示资源不存在（HTTP 404）。
	NotFound BusinessCode = "10404"

	// TooManyRequests 表示请求过于频繁（HTTP 429）。
	TooManyRequests BusinessCode = "10429"

	// InternalError 表示服务器内部错误（HTTP 500）。
	InternalError BusinessCode = "10500"
)

// ── 方法 ─────────────────────────────────────────────────

// IsOK 判断是否为成功码。
func (c BusinessCode) IsOK() bool {
	return c == OK
}
