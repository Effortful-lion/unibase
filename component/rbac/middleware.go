package auth

import (
	"fmt"
	"net/http"
	"reflect"
	"strings"

	"github.com/gin-gonic/gin"
)

// methodActionMap 将 HTTP method 映射为默认 action。
var methodActionMap = map[string]string{
	http.MethodGet:     "read",
	http.MethodPost:    "create",
	http.MethodPut:     "update",
	http.MethodPatch:   "update",
	http.MethodDelete:  "delete",
	http.MethodOptions: "read",
	http.MethodHead:    "read",
}

// middlewareConfig 中间件配置。
type middlewareConfig struct {
	enforcer          Enforcer
	resourceExtractor func(*gin.Context) string
	claimsKey         string
	actionMapper      map[string]string
	unauthorized      gin.HandlerFunc
	skipPaths         map[string]bool
	domainExtractor   func(*gin.Context) string
}

func defaultMiddlewareConfig() middlewareConfig {
	return middlewareConfig{
		claimsKey:    "jwt_claims",
		actionMapper: methodActionMap,
		unauthorized: defaultUnauthorized,
		skipPaths:    make(map[string]bool),
	}
}

// MiddlewareOption 配置 RBAC 中间件的行为。
type MiddlewareOption func(*middlewareConfig)

// WithResourceExtractor 设置自定义 resource 提取函数。
// 默认从请求路径提取第一个路径段作为 resource。
func WithResourceExtractor(fn func(c *gin.Context) string) MiddlewareOption {
	return func(o *middlewareConfig) {
		if fn != nil {
			o.resourceExtractor = fn
		}
	}
}

// WithActionMapper 设置 method → action 映射表。
// 默认使用标准 REST mapping。
func WithActionMapper(mapper map[string]string) MiddlewareOption {
	return func(o *middlewareConfig) {
		o.actionMapper = mapper
	}
}

// WithClaimsKey 设置 gin.Context 中 JWT claims 的存储 key。
// 默认使用 "jwt_claims"（与 httpx/jwt 中间件一致）。
func WithClaimsKey(key string) MiddlewareOption {
	return func(o *middlewareConfig) {
		if key != "" {
			o.claimsKey = key
		}
	}
}

// WithUnauthorizedHandler 设置权限拒绝时的响应处理。
// 默认返回 403 JSON 响应。
func WithUnauthorizedHandler(handler gin.HandlerFunc) MiddlewareOption {
	return func(o *middlewareConfig) {
		if handler != nil {
			o.unauthorized = handler
		}
	}
}

// WithSkipPath 设置不需要鉴权的路径（白名单）。
func WithSkipPath(path string) MiddlewareOption {
	return func(o *middlewareConfig) {
		o.skipPaths[path] = true
	}
}

// WithSkipPaths 批量设置不需要鉴权的路径。
func WithSkipPaths(paths []string) MiddlewareOption {
	return func(o *middlewareConfig) {
		for _, p := range paths {
			o.skipPaths[p] = true
		}
	}
}

// WithDomainExtractor 设置租户域提取函数。
// 返回非空字符串时，中间件将使用 IsAllowedInDomain 做多租户权限检查。
// 返回空字符串时，回退到全局 IsAllowed。
func WithDomainExtractor(fn func(c *gin.Context) string) MiddlewareOption {
	return func(o *middlewareConfig) {
		o.domainExtractor = fn
	}
}

// defaultUnauthorized 默认权限拒绝响应。
func defaultUnauthorized(c *gin.Context) {
	c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
		"error": "forbidden",
	})
}

// Middleware 创建 RBAC 鉴权中间件。
//
// 使用方式：
//
//	r.GET("/api/posts", auth.Middleware(enforcer), listHandler)
//
// 配合 httpx JWT 中间件：
//
//	r.GET("/api/posts", httpx.JWT(secret), auth.Middleware(enforcer), listHandler)
func Middleware(enforcer Enforcer, opts ...MiddlewareOption) gin.HandlerFunc {
	cfg := defaultMiddlewareConfig()
	cfg.enforcer = enforcer
	for _, opt := range opts {
		opt(&cfg)
	}

	// Build action mapper
	actionMapper := cfg.actionMapper
	if actionMapper == nil {
		actionMapper = methodActionMap
	}

	// Build skip paths
	skipPaths := cfg.skipPaths

	return func(c *gin.Context) {
		// 白名单检查
		path := c.Request.URL.Path
		if skipPaths[path] {
			c.Next()
			return
		}

		// 从 gin.Context 提取主体 ID（由 JWT 中间件设置）
		v, exists := c.Get(cfg.claimsKey)
		if !exists {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthorized: no claims found"})
			return
		}

		subjectID, err := extractSubjectID(v)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": err.Error()})
			return
		}

		// 提取 resource 和 action
		resource := extractResource(c)
		if cfg.resourceExtractor != nil {
			resource = cfg.resourceExtractor(c)
		}
		action := actionMapper[c.Request.Method]
		if action == "" {
			action = c.Request.Method
		}

		// 权限检查
		ctx := c.Request.Context()
		domain := ""
		if cfg.domainExtractor != nil {
			domain = cfg.domainExtractor(c)
		}

		var allowed bool
		var authErr error
		if domain != "" {
			allowed, authErr = cfg.enforcer.IsAllowedInDomain(ctx, subjectID, resource, action, domain)
		} else {
			allowed, authErr = cfg.enforcer.IsAllowed(ctx, subjectID, resource, action)
		}
		if authErr != nil {
			c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "auth error: " + authErr.Error()})
			return
		}

		if !allowed {
			cfg.unauthorized(c)
			return
		}

		c.Set("auth_subject", subjectID)
		c.Set("auth_resource", resource)
		c.Set("auth_action", action)
		if domain != "" {
			c.Set("auth_domain", domain)
		}
		c.Next()
	}
}

// extractSubjectID 从 gin.Context 中的值提取主体 ID。
// 支持类型：string、struct 带 UserID/ID 字段或方法（来自 JWT claims 等）。
func extractSubjectID(v interface{}) (string, error) {
	switch val := v.(type) {
	case string:
		return val, nil
	case interface{ UserID() string }:
		return val.UserID(), nil
	case interface{ ID() string }:
		return val.ID(), nil
	default:
		// 尝试通过反射获取 UserID 或 ID 字段值（兼容 jwt.Claims 等带字段的结构体）
		subjectID, err := extractSubjectIDByReflect(v)
		if err == nil {
			return subjectID, nil
		}
		return "", &AuthError{code: "unsupported_claims_type", message: "unsupported claims type in context"}
	}
}

// extractSubjectIDByReflect 通过反射从结构体中提取 UserID 或 ID 字段值。
func extractSubjectIDByReflect(v interface{}) (string, error) {
	val := reflect.ValueOf(v)
	if val.Kind() == reflect.Ptr {
		if val.IsNil() {
			return "", fmt.Errorf("nil pointer")
		}
		val = val.Elem()
	}
	if val.Kind() != reflect.Struct {
		return "", fmt.Errorf("not a struct")
	}

	// 优先查找 UserID 字段
	if field := val.FieldByName("UserID"); field.IsValid() && field.Kind() == reflect.String {
		return field.String(), nil
	}
	// 其次查找 ID 字段
	if field := val.FieldByName("ID"); field.IsValid() && field.Kind() == reflect.String {
		return field.String(), nil
	}
	return "", fmt.Errorf("no UserID or ID field found")
}

// extractResource 从请求路径提取资源名。
// 默认策略：取第一个路径段。
// 例如：/api/users/123 → "api"
func extractResource(c *gin.Context) string {
	path := c.Request.URL.Path
	// 去掉前导斜杠并分割
	parts := strings.Split(strings.TrimPrefix(path, "/"), "/")
	if len(parts) > 0 && parts[0] != "" {
		return parts[0]
	}
	return "*"
}
