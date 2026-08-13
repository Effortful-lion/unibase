package websocketx

import "github.com/gorilla/websocket"

// ── 踢下线 ────────────────────────────────────────────────────

// Kick 按 userID 踢掉指定用户的连接。
// 使用 Hub 配置的 closeReasonKick 作为关闭原因。
func (h *Hub) Kick(userID string) error {
	h.mu.RLock()
	session, ok := h.userIndex[userID]
	h.mu.RUnlock()

	if !ok {
		return nil
	}

	return session.Conn().Close(websocket.CloseGoingAway, string(h.closeReasonKick))
}
