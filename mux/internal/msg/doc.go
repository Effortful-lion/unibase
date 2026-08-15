// Package msg 提供 mux.Context 所需的 Session 适配实现。
//
// 该包将 HTTP 请求和 WebSocket 连接统一适配为 mux.Session 接口：
//   - HTTPRequestSession：HTTP 请求级 Session，生命周期仅限当前请求
//   - WSSession：WebSocket 长连接 Session，封装 websocketx.Session
//
// AuthMiddleware 通过类型断言区分这两种实现，分别注入 userID。
package msg
