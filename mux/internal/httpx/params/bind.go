package params

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/Effortful-lion/unibase/mux/internal/httpx/code"
	"github.com/Effortful-lion/unibase/mux/internal/httpx/response"
	"github.com/gin-gonic/gin"
	"github.com/go-playground/validator/v10"
)

// ── Bind 系列：绑定 + struct tag 校验 ──────────────────

// Bind 将请求体绑定到 v，自动根据 Content-Type 选择绑定方式，并执行 struct tag 校验。
// 等价于 Gin 原生的 c.Bind()，但内置 validator 校验。
func Bind(c *gin.Context, v any) error {
	if err := c.ShouldBind(v); err != nil {
		return err
	}
	return validate(v)
}

// BindJSON 将 JSON 请求体绑定到 v 并校验。
func BindJSON(c *gin.Context, v any) error {
	if err := c.ShouldBindJSON(v); err != nil {
		return err
	}
	return validate(v)
}

// BindQuery 将查询参数绑定到 v 并校验。
func BindQuery(c *gin.Context, v any) error {
	if err := c.ShouldBindQuery(v); err != nil {
		return err
	}
	return validate(v)
}

// BindWith 使用指定绑定器绑定，并用自定义规则链校验。
// rules 为空时退化为 struct tag 校验。
func BindWith(c *gin.Context, v any, b BindingBody, rules ...ValidationRule) error {
	if err := c.ShouldBindWith(v, b); err != nil {
		return err
	}
	if len(rules) > 0 {
		return validateRules(v, rules)
	}
	return validate(v)
}

// MustBind 绑定并校验，失败时直接中止请求并返回 400。
func MustBind(c *gin.Context, v any) {
	if err := Bind(c, v); err != nil {
		response.ResponseFail(c, http.StatusBadRequest, code.BadRequest, formatError(err))
	}
}

// MustBindJSON 绑定 JSON 并校验，失败时直接中止请求并返回 400。
func MustBindJSON(c *gin.Context, v any) {
	if err := BindJSON(c, v); err != nil {
		response.ResponseFail(c, http.StatusBadRequest, code.BadRequest, formatError(err))
	}
}

// MustBindQuery 绑定查询参数并校验，失败时直接中止请求并返回 400。
func MustBindQuery(c *gin.Context, v any) {
	if err := BindQuery(c, v); err != nil {
		response.ResponseFail(c, http.StatusBadRequest, code.BadRequest, formatError(err))
	}
}

// MustBindWith 绑定并用自定义规则校验，失败时直接中止请求并返回 400。
func MustBindWith(c *gin.Context, v any, b BindingBody, rules ...ValidationRule) {
	if err := BindWith(c, v, b, rules...); err != nil {
		response.ResponseFail(c, http.StatusBadRequest, code.BadRequest, formatError(err))
	}
}

// ── 校验错误格式化 ──────────────────────────────────────

// ValidationError 表示校验失败的错误。
type ValidationError struct {
	Fields []FieldError
}

func (e *ValidationError) Error() string {
	var msgs []string
	for _, f := range e.Fields {
		msgs = append(msgs, fmt.Sprintf("%s: %s", f.Field, f.Message))
	}
	return strings.Join(msgs, "; ")
}

// FieldError 单个字段的校验错误。
type FieldError struct {
	Field   string
	Message string
}

func formatError(err error) string {
	if err == nil {
		return ""
	}
	var ve validator.ValidationErrors
	if errors.As(err, &ve) {
		e := &ValidationError{Fields: make([]FieldError, 0, len(ve))}
		for _, fe := range ve {
			e.Fields = append(e.Fields, FieldError{
				Field:   fe.Field(),
				Message: messageForTag(fe),
			})
		}
		return e.Error()
	}
	return err.Error()
}

func messageForTag(fe validator.FieldError) string {
	switch fe.Tag() {
	case "required":
		return "is required"
	case "email":
		return "must be a valid email"
	case "min":
		return fmt.Sprintf("must be at least %s characters", fe.Param())
	case "max":
		return fmt.Sprintf("must be at most %s characters", fe.Param())
	case "len":
		return fmt.Sprintf("must be exactly %s characters", fe.Param())
	case "oneof":
		return fmt.Sprintf("must be one of: %s", fe.Param())
	default:
		return fmt.Sprintf("failed validation: %s", fe.Tag())
	}
}

// validate 执行 struct tag 校验。
func validate(v any) error {
	return validateEngine.Struct(v)
}

// validateRules 逐条执行自定义校验规则。
func validateRules(v any, rules []ValidationRule) error {
	for _, rule := range rules {
		if rule == nil {
			continue
		}
		if err := rule(validateEngine, v); err != nil {
			return err
		}
	}
	return nil
}
