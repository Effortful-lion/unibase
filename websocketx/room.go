package websocketx

// ── 房间管理 ──────────────────────────────────────────────────

// JoinRoom 将指定 Session 加入房间，同时维护 Hub.roomIndex。
// 若 Session 已在该房间，幂等无副作用。
func (h *Hub) JoinRoom(session *Session, roomID string) {
	h.mu.Lock()
	defer h.mu.Unlock()

	// 确保 roomIndex 初始化
	if h.roomIndex[roomID] == nil {
		h.roomIndex[roomID] = make(map[string]struct{})
	}
	h.roomIndex[roomID][session.id] = struct{}{}
	session.mu.Lock()
	session.rooms[roomID] = struct{}{}
	session.mu.Unlock()
}

// LeaveRoom 将指定 Session 从房间移除。
func (h *Hub) LeaveRoom(session *Session, roomID string) {
	h.mu.Lock()
	defer h.mu.Unlock()

	session.mu.Lock()
	delete(session.rooms, roomID)
	session.mu.Unlock()

	if members, ok := h.roomIndex[roomID]; ok {
		delete(members, session.id)
		if len(members) == 0 {
			delete(h.roomIndex, roomID)
		}
	}
}

// ListRoomUsers 返回房间内所有已注册 Session ID 列表。
func (h *Hub) ListRoomUsers(roomID string) []string {
	h.mu.RLock()
	defer h.mu.RUnlock()

	members, ok := h.roomIndex[roomID]
	if !ok {
		return nil
	}
	ids := make([]string, 0, len(members))
	for sid := range members {
		if _, exists := h.sessions[sid]; exists {
			ids = append(ids, sid)
		}
	}
	return ids
}

// CountRooms 返回当前房间数量。
func (h *Hub) CountRooms() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.roomIndex)
}
