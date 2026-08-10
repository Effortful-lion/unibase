package params

import (
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
)

// QueryInt 从 Gin context 获取查询参数并转为 int。
// 不存在或解析失败返回 0。
func QueryInt(c *gin.Context, key string) int {
	v, _ := QueryIntE(c, key)
	return v
}

// QueryIntE 从 Gin context 获取查询参数并转为 int，返回值和错误。
func QueryIntE(c *gin.Context, key string) (int, error) {
	return strconv.Atoi(c.Query(key))
}

// QueryIntDefault 获取查询参数并转为 int，不存在或解析失败时返回 defaultValue。
func QueryIntDefault(c *gin.Context, key string, defaultValue int) int {
	v := c.Query(key)
	if v == "" {
		return defaultValue
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return defaultValue
	}
	return n
}

// QueryInt64 从 Gin context 获取查询参数并转为 int64。
func QueryInt64(c *gin.Context, key string) int64 {
	v, _ := QueryInt64E(c, key)
	return v
}

// QueryInt64E 从 Gin context 获取查询参数并转为 int64。
func QueryInt64E(c *gin.Context, key string) (int64, error) {
	return strconv.ParseInt(c.Query(key), 10, 64)
}

// QueryBool 从 Gin context 获取查询参数并转为 bool。
// Gin 原生 c.Query 只返回 string，这里补类型转换。
func QueryBool(c *gin.Context, key string) bool {
	v, _ := QueryBoolE(c, key)
	return v
}

// QueryBoolE 从 Gin context 获取查询参数并转为 bool，返回值和错误。
func QueryBoolE(c *gin.Context, key string) (bool, error) {
	return strconv.ParseBool(c.Query(key))
}

// QueryBoolDefault 获取查询参数并转为 bool，不存在或解析失败时返回 defaultValue。
func QueryBoolDefault(c *gin.Context, key string, defaultValue bool) bool {
	v := c.Query(key)
	if v == "" {
		return defaultValue
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return defaultValue
	}
	return b
}

// QueryFloat 从 Gin context 获取查询参数并转为 float64。
func QueryFloat(c *gin.Context, key string) float64 {
	v, _ := QueryFloatE(c, key)
	return v
}

// QueryFloatE 从 Gin context 获取查询参数并转为 float64。
func QueryFloatE(c *gin.Context, key string) (float64, error) {
	return strconv.ParseFloat(c.Query(key), 64)
}

// QueryTime 从 Gin context 获取查询参数并解析为 time.Time。
// layout 指定时间格式，默认 RFC3339。
func QueryTime(c *gin.Context, key, layout string) (time.Time, error) {
	if layout == "" {
		layout = time.RFC3339
	}
	return time.Parse(layout, c.Query(key))
}

// QueryTimeDefault 获取查询参数并解析为 time.Time，不存在或解析失败时返回 defaultValue。
// layout 为空时默认使用 time.RFC3339。
func QueryTimeDefault(c *gin.Context, key, layout string, defaultValue time.Time) time.Time {
	if layout == "" {
		layout = time.RFC3339
	}
	v := c.Query(key)
	if v == "" {
		return defaultValue
	}
	t, err := time.Parse(layout, v)
	if err != nil {
		return defaultValue
	}
	return t
}

// QuerySlice 获取查询参数的多个值。
// Gin 原生 c.QueryArray 同名，这里保留为 params 包的能力。
func QuerySlice(c *gin.Context, key string) []string {
	vals := c.QueryArray(key)
	if len(vals) == 0 {
		return nil
	}
	return vals
}
