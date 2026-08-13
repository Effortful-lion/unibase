package params

import (
	"fmt"
	"net/http"
	"reflect"
	"regexp"
	"strings"

	"github.com/go-playground/validator/v10"
)

// validateEngine 全局校验引擎实例，所有校验统一走这里。
var validateEngine *validator.Validate

func init() {
	validateEngine = validator.New()
}

// ValidationRule 校验规则函数签名。
// 由 Rule.Build() 生成，传给 BindWith / MustBindWith。
type ValidationRule func(v *validator.Validate, obj any) error

// BindingBody 绑定器接口，和 gin/binding.Binding 对齐。
type BindingBody interface {
	Bind(req *http.Request, obj any) error
	Name() string
}

// ── 规则注册 ────────────────────────────────────────────────

// RegisterRule 注册自定义校验规则，注册后所有 Bind 系列函数自动生效。
func RegisterRule(tagValue string, fn validator.Func) error {
	return validateEngine.RegisterValidation(tagValue, fn)
}

// ── 校验规则构造器 ──────────────────────────────────────────

// Rule 创建一个字段校验规则构造器。
// 链式调用各校验方法后，Build() 返回 ValidationRule，可直接用于 MustBindWith。
// 内置规则（Required/Min/Max 等）通过 tag 字符串实现，框架自动走 validateEngine。
// 自定义规则通过 Custom() 注册到引擎后也可在 struct tag 中使用。
//
// 示例：
//
//	// 直接用于 MustBindWith
//	params.MustBindWith(c, &req, binding.JSON,
//	    params.Rule("Name").Required().Min(1).Max(100).Build(),
//	)
//
//	// 自定义规则，同时支持 struct tag 和 MustBindWith
//	mobileRule := func(fl validator.FieldLevel) bool {
//	    return regexp.MustCompile(`^1[3-9]\d{9}$`).MatchString(fl.Field().String())
//	}
//	params.RegisterRule("mobile", mobileRule)
//	// struct tag 使用
//	type Req struct { Mobile string `validate:"required,mobile"` }
//	// MustBindWith 使用
//	params.MustBindWith(c, &req, binding.JSON,
//	    params.Rule("Mobile").Required().Custom(mobileRule).Build(),
//	)
func Rule(field string) *FieldRule {
	return &FieldRule{field: field}
}

// FieldRule 字段校验规则构造器。
// 链式调用校验方法，Build() 生成 ValidationRule。
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

// Custom 添加自定义校验规则（需先通过 RegisterRule 注册）。
// tag 为 RegisterRule 时使用的 tag 名。
func (r *FieldRule) Custom(tag string) *FieldRule {
	r.rules = append(r.rules, tag)
	return r
}

// Build 返回 ValidationRule，用于 MustBindWith / BindWith。
func (r *FieldRule) Build() ValidationRule {
	if len(r.rules) == 0 {
		return nil
	}
	tag := strings.Join(r.rules, ",")
	return func(v *validator.Validate, obj any) error {
		val := reflect.ValueOf(obj)
		if val.Kind() == reflect.Pointer {
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

// ── 内置校验函数（validator.Func 签名，可用于 RegisterRule 和 Custom） ──

// RequiredFunc 必填校验：零值视为无效。
func RequiredFunc() validator.Func {
	return func(fl validator.FieldLevel) bool {
		val := reflect.ValueOf(fl.Field().Interface())
		switch val.Kind() {
		case reflect.String:
			return val.String() != ""
		case reflect.Bool:
			return val.Bool()
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			return val.Int() != 0
		case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
			return val.Uint() != 0
		case reflect.Float32, reflect.Float64:
			return val.Float() != 0
		case reflect.Slice, reflect.Array, reflect.Map, reflect.Pointer, reflect.Interface:
			return !val.IsNil()
		default:
			return true
		}
	}
}

// MinFunc 最小值校验（字符串长度、数值或数组长度）。
func MinFunc(min int) validator.Func {
	return func(fl validator.FieldLevel) bool {
		val := reflect.ValueOf(fl.Field().Interface())
		switch val.Kind() {
		case reflect.String, reflect.Slice, reflect.Array:
			return val.Len() >= min
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			return val.Int() >= int64(min)
		case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
			return val.Uint() >= uint64(min)
		case reflect.Float32, reflect.Float64:
			return val.Float() >= float64(min)
		default:
			return true
		}
	}
}

// MaxFunc 最大值校验（字符串长度、数值或数组长度）。
func MaxFunc(max int) validator.Func {
	return func(fl validator.FieldLevel) bool {
		val := reflect.ValueOf(fl.Field().Interface())
		switch val.Kind() {
		case reflect.String, reflect.Slice, reflect.Array:
			return val.Len() <= max
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			return val.Int() <= int64(max)
		case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
			return val.Uint() <= uint64(max)
		case reflect.Float32, reflect.Float64:
			return val.Float() <= float64(max)
		default:
			return true
		}
	}
}

// LenFunc 固定长度校验。
func LenFunc(length int) validator.Func {
	return func(fl validator.FieldLevel) bool {
		val := reflect.ValueOf(fl.Field().Interface())
		switch val.Kind() {
		case reflect.String, reflect.Slice, reflect.Array:
			return val.Len() == length
		default:
			return true
		}
	}
}

// EmailFunc 邮箱格式校验。
func EmailFunc() validator.Func {
	re := regexp.MustCompile(`^[^@\s]+@[^@\s]+\.[^@\s]+$`)
	return func(fl validator.FieldLevel) bool {
		s, ok := fl.Field().Interface().(string)
		if !ok || s == "" {
			return true
		}
		return re.MatchString(s)
	}
}

// URLFunc URL 格式校验。
func URLFunc() validator.Func {
	re := regexp.MustCompile(`^https?://`)
	return func(fl validator.FieldLevel) bool {
		s, ok := fl.Field().Interface().(string)
		if !ok || s == "" {
			return true
		}
		return re.MatchString(s)
	}
}

// ── 规则组合 ──────────────────────────────────────────────────

// And 组合多个 validator.Func，任一失败即返回 false。
func And(fns ...validator.Func) validator.Func {
	return func(fl validator.FieldLevel) bool {
		for _, fn := range fns {
			if fn == nil {
				continue
			}
			if !fn(fl) {
				return false
			}
		}
		return true
	}
}

// Or 组合多个 validator.Func，任一通过即返回 true。
func Or(fns ...validator.Func) validator.Func {
	return func(fl validator.FieldLevel) bool {
		for _, fn := range fns {
			if fn == nil {
				continue
			}
			if fn(fl) {
				return true
			}
		}
		return false
	}
}

// ── 内部工具 ──────────────────────────────────────────────────

// customTag 将一个 validator.Func 包装为匿名 tag 并注册到引擎。
func customTag(fn validator.Func) string {
	tag := "custom_" + nextID()
	err := validateEngine.RegisterValidation(tag, fn)
	if err != nil {
		return ""
	}
	return tag
}

var idCounter uint64

func nextID() string {
	idCounter++
	return fmt.Sprintf("%d", idCounter)
}
