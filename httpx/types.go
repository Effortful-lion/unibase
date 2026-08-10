package httpx

import (
	"net/http"
)

// ContextKey 用于 context.WithValue 的 key 类型。
// 避免与第三方包的 key 冲突。
type ContextKey string

func (k ContextKey) String() string { return string(k) }

// HeaderOpt 响应头操作函数类型。
type HeaderOpt func(http.Header)

// WithHeader 设置单个响应头。
func WithHeader(k, v string) HeaderOpt {
	return func(h http.Header) {
		h.Set(k, v)
	}
}

// WithHeaders 批量设置响应头。
func WithHeaders(headers map[string]string) HeaderOpt {
	return func(h http.Header) {
		for k, v := range headers {
			h.Set(k, v)
		}
	}
}

// WithCORS 设置 CORS 响应头。
func WithCORS(origin string) HeaderOpt {
	return func(h http.Header) {
		h.Set("Access-Control-Allow-Origin", origin)
		h.Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		h.Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		h.Set("Access-Control-Max-Age", "86400")
	}
}

// StandardHeaders HTTP 标准头常量。
var StandardHeaders = struct {
	ContentType        string
	ContentLength      string
	ContentDisposition string
	AcceptRanges       string
	ContentRange       string
	ContentEncoding    string
	CacheControl       string
	Authorization      string
	ETag               string
}{
	ContentType:        "Content-Type",
	ContentLength:      "Content-Length",
	ContentDisposition: "Content-Disposition",
	AcceptRanges:       "Accept-Ranges",
	ContentRange:       "Content-Range",
	ContentEncoding:    "Content-Encoding",
	CacheControl:       "Cache-Control",
	Authorization:      "Authorization",
	ETag:               "ETag",
}

// MIMETypes MIME 类型映射。
var MIMETypes = struct {
	ByExtension map[string]string
}{
	ByExtension: map[string]string{
		".html": "text/html",
		".css":  "text/css",
		".js":   "application/javascript",
		".json": "application/json",
		".xml":  "application/xml",
		".png":  "image/png",
		".jpg":  "image/jpeg",
		".gif":  "image/gif",
		".svg":  "image/svg+xml",
		".pdf":  "application/pdf",
		".zip":  "application/zip",
		".tar":  "application/x-tar",
	},
}
