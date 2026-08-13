// Package client 提供 HTTP 客户端能力，采用 Builder 链式调用模式。
//
// 框架无关，不依赖任何 Web 框架。
//
// 快速开始：
//
//	res := client.Get().URL("https://api.example.com/users").Query("id", "123").Do(ctx)
//	if err := res.Err(); err != nil { ... }
//	var user User
//	res.JSON(&user)
//
//	// 带请求体
//	res := client.Post().
//	    URL("https://api.example.com/users").
//	    Header("Authorization", "Bearer xxx").
//	    JSON(createReq).
//	    Timeout(5 * time.Second).
//	    Retry(3).
//	    Do(ctx)
//
// 能力：Get / Post / Put / Delete / Patch，支持 JSON / Form / Bytes body，
// TLS 配置、超时、重试，响应读取（Bytes / Text / JSON / Stream / SaveTo）。
package client
