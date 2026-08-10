package client

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
)

// Response 封装 HTTP 响应，提供多种读取方式。
type Response struct {
	resp *http.Response
	err  error
}

func newResponse(resp *http.Response) *Response {
	return &Response{resp: resp}
}

// Err 返回执行过程中的错误（网络错误、非 2xx、反序列化失败等）。
// 非 2xx 状态码会被包装为 *ClientError。
func (r *Response) Err() error {
	if r.err != nil {
		return r.err
	}
	if r.resp.StatusCode < 200 || r.resp.StatusCode >= 300 {
		body, readErr := io.ReadAll(r.resp.Body)
		r.resp.Body.Close()
		if readErr != nil {
			return newClientError(r.resp.StatusCode, nil, fmt.Errorf("%s: %w", http.StatusText(r.resp.StatusCode), readErr))
		}
		return newClientError(r.resp.StatusCode, body, errors.New(http.StatusText(r.resp.StatusCode)))
	}
	return nil
}

// Status 返回 HTTP 状态码。
// 若响应为 nil（网络错误等未成功发送请求的场景），返回 0。
func (r *Response) Status() int {
	if r.resp == nil {
		return 0
	}
	return r.resp.StatusCode
}

// Header 返回响应头。
func (r *Response) Header() http.Header {
	if r.resp == nil {
		return http.Header{}
	}
	return r.resp.Header
}

// Bytes 读取响应体为字节切片。
func (r *Response) Bytes() ([]byte, error) {
	if err := r.Err(); err != nil {
		return nil, err
	}
	defer r.resp.Body.Close()
	return io.ReadAll(r.resp.Body)
}

// Text 读取响应体为字符串。
func (r *Response) Text() (string, error) {
	b, err := r.Bytes()
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// JSON 将响应体反序列化到 v。
// 非 2xx 状态码返回错误，不会反序列化错误响应体。
func (r *Response) JSON(v interface{}) error {
	if err := r.Err(); err != nil {
		return err
	}
	defer r.resp.Body.Close()
	dec := json.NewDecoder(r.resp.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return err
	}
	if dec.More() {
		return errors.New("response contains multiple JSON values")
	}
	return nil
}

// Stream 返回响应体的读取器，调用方负责关闭。
func (r *Response) Stream() (io.ReadCloser, error) {
	if err := r.Err(); err != nil {
		return nil, err
	}
	return r.resp.Body, nil
}

// SaveTo 将响应体写入文件。
func (r *Response) SaveTo(path string) error {
	if err := r.Err(); err != nil {
		return err
	}
	defer r.resp.Body.Close()

	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	_, err = io.Copy(f, r.resp.Body)
	return err
}

// Raw 返回原始 *http.Response，调用方负责关闭 Body。
func (r *Response) Raw() (*http.Response, error) {
	return r.resp, r.Err()
}
