package params

import (
	"fmt"
	"net/http"
	"reflect"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/go-playground/validator/v10"
)

var validateEngine *validator.Validate

func init() {
	validateEngine = validator.New()
}

// RegisterValidator 注册自定义校验规则。
// 在项目初始化时调用，之后所有 Bind 系列函数自动生效。
// 示例：
//
//	params.RegisterValidator("mobile", func(fl validator.FieldLevel) bool {
//	    return regexp.MatchString(`^1[3-9]\d{9}$`, fl.Field().String())
//	})
//
// 然后在 struct 中使用：
//
//	type CreateUserReq struct {
//	    Mobile string `json:"mobile" validate:"required,mobile"`
//	}
func RegisterValidator(tag string, fn validator.Func) error {
	return validateEngine.RegisterValidation(tag, fn)
}

// ── Bind 系列：绑定 + struct tag 校验 ──────────────────

// Bind 将请求体绑定到 v，自动根据 Content-Type 选择绑定方式，并执行 struct tag 校验。
// 等价于 Gin 原生的 c.Bind()，但内置 validator 校验。
func Bind(c *gin.Context, v interface{}) error {
	if err := c.ShouldBind(v); err != nil {
		return err
	}
	return validate(v)
}

// BindJSON 将 JSON 请求体绑定到 v 并校验。
func BindJSON(c *gin.Context, v interface{}) error {
	if err := c.ShouldBindJSON(v); err != nil {
		return err
	}
	return validate(v)
}

// BindQuery 将查询参数绑定到 v 并校验。
func BindQuery(c *gin.Context, v interface{}) error {
	if err := c.ShouldBindQuery(v); err != nil {
		return err
	}
	return validate(v)
}

// BindWith 使用指定绑定器绑定，并用自定义规则链校验。
// rules 为空时退化为 struct tag 校验。
func BindWith(c *gin.Context, v interface{}, b BindingBody, rules ...ValidationRule) error {
	if err := c.ShouldBindWith(v, b); err != nil {
		return err
	}
	if len(rules) > 0 {
		return validateRules(v, rules)
	}
	return validate(v)
}

// MustBind 绑定并校验，失败时直接中止请求并返回 400。
func MustBind(c *gin.Context, v interface{}) {
	if err := Bind(c, v); err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": formatError(err)})
	}
}

// MustBindJSON 绑定 JSON 并校验，失败时直接中止请求并返回 400。
func MustBindJSON(c *gin.Context, v interface{}) {
	if err := BindJSON(c, v); err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": formatError(err)})
	}
}

// MustBindQuery 绑定查询参数并校验，失败时直接中止请求并返回 400。
func MustBindQuery(c *gin.Context, v interface{}) {
	if err := BindQuery(c, v); err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": formatError(err)})
	}
}

// MustBindWith 绑定并用自定义规则校验，失败时直接中止请求并返回 400。
func MustBindWith(c *gin.Context, v interface{}, b BindingBody, rules ...ValidationRule) {
	if err := BindWith(c, v, b, rules...); err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": formatError(err)})
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
	if ve, ok := err.(validator.ValidationErrors); ok {
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

// ── 自定义规则 Fluent API ──────────────────────────────

// BindingBody 绑定器接口，和 gin/binding.Binding 对齐。
type BindingBody interface {
	Bind(req *http.Request, obj interface{}) error
	Name() string
}

// ValidationRule 校验规则函数。
type ValidationRule func(validate *validator.Validate, v interface{}) error

// Rule 创建一个字段校验规则构造器。
// 使用方式：
//
//	params.MustBindWith(c, &req, binding.JSON,
//	    params.Rule("Name").Required().Min(1).Max(100),
//	    params.Rule("Email").Required().Email(),
//	)
func Rule(field string) *FieldRule {
	return &FieldRule{field: field}
}

// FieldRule 字段校验规则构造器。
type FieldRule struct {
	field string
	rules []string
}

// Required 标记字段为必填。
func (r *FieldRule) Required() *FieldRule {
	r.rules = append(r.rules, "required")
	return r
}

// Min 设置最小值（字符串长度、数值或数组长度）。
func (r *FieldRule) Min(min int) *FieldRule {
	r.rules = append(r.rules, fmt.Sprintf("min=%d", min))
	return r
}

// Max 设置最大值（字符串长度、数值或数组长度）。
func (r *FieldRule) Max(max int) *FieldRule {
	r.rules = append(r.rules, fmt.Sprintf("max=%d", max))
	return r
}

// Len 设置固定长度。
func (r *FieldRule) Len(length int) *FieldRule {
	r.rules = append(r.rules, fmt.Sprintf("len=%d", length))
	return r
}

// Email 验证是否为合法邮箱格式。
func (r *FieldRule) Email() *FieldRule {
	r.rules = append(r.rules, "email")
	return r
}

// URL 验证是否为合法 URL。
func (r *FieldRule) URL() *FieldRule {
	r.rules = append(r.rules, "url")
	return r
}

// OneOf 验证值是否在指定列表中。
func (r *FieldRule) OneOf(values ...string) *FieldRule {
	r.rules = append(r.rules, fmt.Sprintf("oneof=%s", strings.Join(values, " ")))
	return r
}

// Custom 添加自定义校验规则（需先通过 RegisterValidator 注册）。
func (r *FieldRule) Custom(tag string) *FieldRule {
	r.rules = append(r.rules, tag)
	return r
}

// Build 返回校验规则函数。
func (r *FieldRule) Build() ValidationRule {
	if len(r.rules) == 0 {
		return nil
	}
	tag := strings.Join(r.rules, " ")
	return func(v *validator.Validate, obj interface{}) error {
		val := reflect.ValueOf(obj)
		if val.Kind() == reflect.Ptr {
			val = val.Elem()
		}
		if val.Kind() != reflect.Struct {
			return fmt.Errorf("expected struct, got %s", val.Kind())
		}
		field := val.FieldByName(r.field)
		if !field.IsValid() {
			return fmt.Errorf("%s: field not found", r.field)
		}
		return v.Var(field.Interface(), tag)
	}
}

func validateRules(v interface{}, rules []ValidationRule) error {
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

func validate(v interface{}) error {
	return validateEngine.Struct(v)
}
