package client

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	GET    = "GET"
	POST   = "POST"
	PUT    = "PUT"
	DELETE = "DELETE"
	PATCH  = "PATCH"
)

// Builder 链式构造 HTTP 请求，执行后返回 *Response。
type Builder struct {
	method   string
	url      string
	headers  http.Header
	query    url.Values
	body     []byte
	bodyType string
	tls      *tls.Config
	timeout  time.Duration
	retry    int
	buildErr error
}

func newBuilder(method string) *Builder {
	return &Builder{
		method:  method,
		headers: make(http.Header),
		query:   make(url.Values),
		retry:   3,
	}
}

// Get 构造 GET 请求。
func Get() *Builder { return newBuilder(GET) }

// Post 构造 POST 请求。
func Post() *Builder { return newBuilder(POST) }

// Put 构造 PUT 请求。
func Put() *Builder { return newBuilder(PUT) }

// Delete 构造 DELETE 请求。
func Delete() *Builder { return newBuilder(DELETE) }

// Patch 构造 PATCH 请求。
func Patch() *Builder { return newBuilder(PATCH) }

// URL 设置请求地址。
func (b *Builder) URL(rawURL string) *Builder {
	b.url = rawURL
	return b
}

// Header 添加请求头，可链多次。
func (b *Builder) Header(k, v string) *Builder {
	b.headers.Add(k, v)
	return b
}

// Query 添加查询参数，可链多次。
func (b *Builder) Query(k, v string) *Builder {
	b.query.Add(k, v)
	return b
}

// JSON 设置请求体为 JSON（Content-Type 自动设为 application/json）。
func (b *Builder) JSON(v interface{}) *Builder {
	if b.buildErr != nil {
		return b
	}
	data, err := json.Marshal(v)
	if err != nil {
		b.buildErr = fmt.Errorf("httpx/client: marshal JSON body: %w", err)
		return b
	}
	b.body = data
	b.bodyType = "json"
	return b
}

// Form 设置请求体为 form（Content-Type 自动设为 application/x-www-form-urlencoded）。
func (b *Builder) Form(v url.Values) *Builder {
	b.body = []byte(v.Encode())
	b.bodyType = "form"
	return b
}

// Bytes 设置原始字节请求体。
func (b *Builder) Bytes(data []byte) *Builder {
	b.body = append([]byte(nil), data...)
	b.bodyType = "bytes"
	return b
}

// TLS 设置客户端 TLS 配置。
func (b *Builder) TLS(tlsConfig *tls.Config) *Builder {
	b.tls = tlsConfig
	return b
}

// Timeout 设置请求超时，覆盖 context 的 deadline。
func (b *Builder) Timeout(d time.Duration) *Builder {
	b.timeout = d
	return b
}

// Retry 设置重试次数（默认 3）。
func (b *Builder) Retry(n int) *Builder {
	if n < 0 {
		n = 0
	}
	b.retry = n
	return b
}

// Do 执行请求，返回 *Response。
// ctx 用于取消和 trace，如果设置了 Timeout，会基于 ctx 创建子 context。
func (b *Builder) Do(ctx context.Context) *Response {
	if b.buildErr != nil {
		return &Response{err: b.buildErr}
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if b.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, b.timeout)
		defer cancel()
	}

	// 拼接 URL 和 query
	fullURL := b.url
	if len(b.query) > 0 {
		if strings.Contains(fullURL, "?") {
			fullURL += "&" + b.query.Encode()
		} else {
			fullURL += "?" + b.query.Encode()
		}
	}

	// 构造请求体
	var body io.Reader
	if len(b.body) > 0 {
		body = bytes.NewReader(b.body)
		if b.bodyType == "json" && b.headers.Get("Content-Type") == "" {
			b.headers.Set("Content-Type", "application/json; charset=utf-8")
		}
		if b.bodyType == "form" && b.headers.Get("Content-Type") == "" {
			b.headers.Set("Content-Type", "application/x-www-form-urlencoded; charset=utf-8")
		}
	}

	// 重试循环：retry 次重试，共 retry+1 次尝试
	var lastErr error
	for attempt := 0; attempt <= b.retry; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return &Response{err: ctx.Err()}
			default:
			}
			time.Sleep(100 * time.Millisecond)
			// 回退 body reader 以便重试时重新读取
			if seeker, ok := body.(io.Seeker); ok {
				seeker.Seek(0, io.SeekStart)
			}
		}

		req, err := http.NewRequestWithContext(ctx, b.method, fullURL, body)
		if err != nil {
			return &Response{err: fmt.Errorf("build request: %w", err)}
		}
		req.Header = b.headers

		resp, err := b.doRequest(req)
		if err == nil {
			return newResponse(resp)
		}

		lastErr = err
		if !isRetryable(err) {
			break
		}
	}

	return &Response{err: lastErr}
}

func (b *Builder) doRequest(req *http.Request) (*http.Response, error) {
	if b.tls != nil {
		var transport *http.Transport
		if defaultTransport, ok := http.DefaultTransport.(*http.Transport); ok {
			transport = defaultTransport.Clone()
		} else {
			transport = &http.Transport{}
		}
		transport.TLSClientConfig = b.tls
		client := &http.Client{Transport: transport}
		return client.Do(req)
	}
	return http.DefaultClient.Do(req)
}

func isRetryable(err error) bool {
	return IsTimeout(err)
}
