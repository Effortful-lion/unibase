// Package ranger 提供 HTTP Range 请求解析和断点续传能力。
//
// 快速开始：
//
//	ranges, err := ranger.ParseRange(req.Header.Get("Range"), fileSize)
//	if err != nil { ... }
//	// 返回 Content-Range 头
//	c.Header("Content-Range", ranger.ContentRange(ranges[0]))
//
// 能力：ParseRange（解析 bytes= 开头）、ContentRange（构造响应头）、
// IsSatisfiable（判断是否可满足）、IsRangeRequest（检测 Range 头）。
package ranger
