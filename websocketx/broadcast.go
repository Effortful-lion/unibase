package websocketx

import (
	"context"
	"fmt"
)

// ── 广播 ──────────────────────────────────────────────────────

// Broadcast 向所有连接广播消息，except 中的 Session ID 会被排除。
func (h *Hub) Broadcast(ctx context.Context, msg *CmdMessage, except ...string) error {
	h.mu.RLock()
	sessions := make([]*Session, 0, len(h.sessions))
	for _, s := range h.sessions {
		sessions = append(sessions, s)
	}
	h.mu.RUnlock()

	exclude := make(map[string]bool, len(except))
	for _, id := range except {
		exclude[id] = true
	}

	var firstErr error
	for _, s := range sessions {
		if exclude[s.ID()] {
			continue
		}
		if err := s.Conn().Write(ctx, msg); err != nil {
			if firstErr == nil {
				firstErr = err
			}
		}
	}

	if h.metrics.onBroadcast != nil {
		h.metrics.onBroadcast(string(MetricEventBroadcast), StandardMetricLabels(MetricEventBroadcast, map[string]string{
			"count": fmt.Sprintf("%d", len(sessions)-len(exclude)),
			"error": fmt.Sprintf("%v", firstErr),
		}))
	}

	return firstErr
}

// BroadcastToRoom 向指定房间广播消息。
// roomID 为空时退化为全量广播。
// except 中的 Session ID 会被排除。
func (h *Hub) BroadcastToRoom(ctx context.Context, roomID string, msg *CmdMessage, except ...string) error {
	if roomID == "" {
		return h.Broadcast(ctx, msg, except...)
	}

	h.mu.RLock()
	members, ok := h.roomIndex[roomID]
	if !ok {
		h.mu.RUnlock()
		return nil
	}

	sessions := make([]*Session, 0, len(members))
	for sid := range members {
		if s, exists := h.sessions[sid]; exists {
			sessions = append(sessions, s)
		}
	}
	h.mu.RUnlock()

	exclude := make(map[string]bool, len(except))
	for _, id := range except {
		exclude[id] = true
	}

	var firstErr error
	for _, s := range sessions {
		if exclude[s.ID()] {
			continue
		}
		if err := s.Conn().Write(ctx, msg); err != nil {
			if firstErr == nil {
				firstErr = err
			}
		}
	}

	if h.metrics.onBroadcast != nil {
		h.metrics.onBroadcast(string(MetricEventBroadcastRoom), StandardMetricLabels(MetricEventBroadcastRoom, map[string]string{
			"room_id": roomID,
			"count":   fmt.Sprintf("%d", len(sessions)-len(exclude)),
			"error":   fmt.Sprintf("%v", firstErr),
		}))
	}

	return firstErr
}

// BroadcastToUser 向指定 userID 的 Session 发消息。
// 用户不在线时静默返回 nil。
func (h *Hub) BroadcastToUser(ctx context.Context, userID string, msg *CmdMessage) error {
	h.mu.RLock()
	session, ok := h.userIndex[userID]
	h.mu.RUnlock()

	if !ok {
		return nil
	}

	if err := session.Conn().Write(ctx, msg); err != nil {
		return err
	}

	if h.metrics.onBroadcast != nil {
		h.metrics.onBroadcast(string(MetricEventBroadcast), StandardMetricLabels(MetricEventBroadcast, map[string]string{
			"target":     "user",
			"user_id":    userID,
			"session_id": session.ID(),
		}))
	}
	return nil
}
